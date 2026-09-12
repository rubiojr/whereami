//go:build vecmap_rhi

// Command vecmap-rhi displays a portable scene fixture through generated Qt RHI
// bindings. It deliberately has no dependency on the legacy Qt map bridge.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/qml"
	rhi "github.com/rubiojr/whereami/internal/qtrhi"
	"github.com/rubiojr/whereami/internal/vecmaprhi"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
)

func main() {
	runtime.LockOSThread()
	path := flag.String("scene", "", "portable scene JSON from vecmap-fixture")
	duration := flag.Duration("duration", 5*time.Second, "run duration; zero runs until window closes")
	animate := flag.Bool("animate", true, "replay a deterministic pan/zoom/bearing trace")
	screenshot := flag.String("screenshot", "", "save a PNG before exiting")
	flag.Parse()
	if err := run(*path, *duration, *animate, *screenshot); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(path string, duration time.Duration, animate bool, screenshot string) error {
	document, err := readDocument(path)
	if err != nil {
		return err
	}
	return display(document, duration, animate, screenshot)
}

func readDocument(path string) (scene.Document, error) {
	file, err := os.Open(path)
	if err != nil {
		return scene.Document{}, err
	}
	var document scene.Document
	err = json.NewDecoder(io.LimitReader(file, 128<<20)).Decode(&document)
	file.Close()
	if err != nil {
		return document, err
	}
	if err := document.Validate(); err != nil {
		return document, err
	}
	return document, nil
}

func display(document scene.Document, duration time.Duration, animate bool, screenshot string) error {
	app := qt.NewQApplication([]string{"vecmap-rhi"})
	defer app.Delete()
	item := rhi.NewQQuickItem()
	defer item.Delete()
	item.SetFlag(rhi.QQuickItem__ItemHasContents)
	var frame atomic.Pointer[scene.Frame]
	frame.Store(&scene.Frame{Scene: &document.Scene, Transforms: document.Transforms, DevicePixelRatio: 1})
	var renderer *vecmaprhi.Renderer
	var mu sync.Mutex
	var samples frameSamples
	item.OnUpdatePaintNode(func(_ func(*rhi.QSGNode, *rhi.QQuickItem__UpdatePaintNodeData) *rhi.QSGNode, old *rhi.QSGNode, _ *rhi.QQuickItem__UpdatePaintNodeData) *rhi.QSGNode {
		if old == nil {
			renderer = vecmaprhi.New(item, func(stats vecmaprhi.Stats) {
				mu.Lock()
				defer mu.Unlock()
				samples.add(stats)
			})
		}
		renderer.Sync(*frame.Load())
		return renderer.Node.QSGNode
	})
	engine := qml.NewQQmlApplicationEngine()
	engine.RootContext().SetContextProperty("mapItem", item.QObject)
	engine.LoadData([]byte(fmt.Sprintf(`import QtQuick
import QtQuick.Window
Window {visible:false; width:%d; height:%d; color:"#f8f4f0"; title:"vecmap RHI — retained scene prototype"
 Item {id:host; anchors.fill:parent; clip:true
  Binding {target:mapItem;property:"parent";value:host}
  Binding {target:mapItem;property:"width";value:host.width}
  Binding {target:mapItem;property:"height";value:host.height}
 }
}`, document.Width, document.Height)))
	if len(engine.RootObjects()) == 0 {
		engine.Delete()
		return fmt.Errorf("load QML window")
	}
	graphics := rhi.NewQQuickGraphicsConfiguration()
	graphics.SetTimestamps(true)
	item.Window().SetGraphicsConfiguration(graphics)
	graphics.Delete()
	visible := qt.NewQVariant8(true)
	engine.RootObjects()[0].SetProperty("visible", visible)
	visible.Delete()
	start := time.Now()
	timer := qt.NewQTimer()
	var screenshotError error
	timer.OnTimeout(func() {
		elapsed := time.Since(start)
		if duration > 0 && elapsed >= duration {
			if screenshot != "" {
				image := item.Window().GrabWindow()
				if image.IsNull() || !image.Save(screenshot) {
					screenshotError = fmt.Errorf("save screenshot %q", screenshot)
				}
				image.Delete()
			}
			qt.QCoreApplication_Quit()
			return
		}
		transforms := traceTransforms(document, elapsed.Seconds(), animate)
		frame.Store(&scene.Frame{Scene: &document.Scene, Transforms: transforms, DevicePixelRatio: 1})
		item.Update()
	})
	timer.Start(8)
	qt.QApplication_Exec()
	timer.Delete()
	engine.Delete()
	mu.Lock()
	defer mu.Unlock()
	latest := samples.latest
	fmt.Printf("scene draws=%d labels=%d missing_fonts=%q\n", len(document.Scene.Draws), document.Labels, document.MissingFonts)
	fmt.Printf("backend=%s device=%s\n", latest.Backend, latest.Device)
	fmt.Printf("frames=%d mesh_uploads=%d texture_uploads=%d uploaded_bytes=%d live_meshes=%d live_textures=%d\n", latest.Frames, latest.MeshUploads, latest.TextureUploads, latest.UploadedBytes, latest.LiveMeshes, latest.LiveTextures)
	for name, values := range map[string][]time.Duration{"prepare_cpu": samples.prepare, "submit_cpu": samples.submit, "gpu_previous_frame": samples.gpu, "render_callback_interval": samples.cadence} {
		if len(values) > 0 {
			slices.Sort(values)
			fmt.Printf("%s samples=%d p50=%s p95=%s p99=%s\n", name, len(values), values[len(values)/2], values[min(len(values)-1, len(values)*95/100)], values[min(len(values)-1, len(values)*99/100)])
		}
	}
	if latest.Error != "" {
		return fmt.Errorf("renderer: %s", latest.Error)
	}
	if latest.Frames == 0 {
		return fmt.Errorf("renderer produced no frames")
	}
	return screenshotError
}

type frameSamples struct {
	latest                        vecmaprhi.Stats
	lastFrame                     time.Time
	prepare, submit, gpu, cadence []time.Duration
}

func (s *frameSamples) add(stats vecmaprhi.Stats) {
	if stats.Frames > s.latest.Frames {
		if stats.Frames > 30 && len(s.prepare) < 60000 {
			s.prepare = append(s.prepare, stats.PrepareTime)
			s.submit = append(s.submit, stats.SubmitTime)
			if stats.GPUTime > 0 {
				s.gpu = append(s.gpu, stats.GPUTime)
			}
			if !s.lastFrame.IsZero() {
				s.cadence = append(s.cadence, time.Since(s.lastFrame))
			}
		}
		s.lastFrame = time.Now()
	}
	s.latest = stats
}

func traceTransforms(document scene.Document, t float64, animate bool) []scene.Affine {
	if document.Camera != nil && len(document.TileSpaces) > 0 {
		camera := document.Camera.WithViewport(float64(document.Width), float64(document.Height))
		if animate {
			camera.Zoom += 0.2 * math.Sin(t)
			camera.Bearing += 0.15 * math.Sin(t*0.7) * 180 / math.Pi
			camera = camera.Normalized().Panned(-math.Sin(t*1.3)*20, 0)
		}
		return document.FrameAt(camera).Transforms
	}
	transforms := slices.Clone(document.Transforms)
	if !animate {
		return transforms
	}
	scale := float32(math.Exp2(0.2 * math.Sin(t)))
	sin, cos := math.Sincos(0.15 * math.Sin(t*0.7))
	for i, base := range transforms {
		a, b := float32(cos)*scale, float32(sin)*scale
		cx, cy := float32(document.Width)/2, float32(document.Height)/2
		transforms[i] = scene.Affine{M11: a*base.M11 - b*base.M21, M12: a*base.M12 - b*base.M22, M21: b*base.M11 + a*base.M21, M22: b*base.M12 + a*base.M22, DX: cx + a*(base.DX-cx) - b*(base.DY-cy) + float32(math.Sin(t*1.3))*20, DY: cy + b*(base.DX-cx) + a*(base.DY-cy)}
	}
	return transforms
}
