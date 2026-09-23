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
	rhi "github.com/rubiojr/whereami/internal/qtrhi"
	"github.com/rubiojr/whereami/internal/vecmaprhi"
	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
)

func main() {
	runtime.LockOSThread()
	path := flag.String("scene", "", "portable scene JSON from vecmap-fixture")
	duration := flag.Duration("duration", 5*time.Second, "run duration; zero runs until window closes")
	animate := flag.Bool("animate", true, "replay a deterministic pan/zoom/bearing trace")
	screenshot := flag.String("screenshot", "", "save a PNG before exiting")
	foreground := flag.Bool("foreground", false, "keep the benchmark window on top and request activation")
	diagnostics := flag.Bool("diagnostics", false, "report timer delivery and window state around pacing gaps")
	uploadBytes := flag.Uint64("upload-bytes", 32<<20, "maximum geometry/index/RGBA bytes per upload batch (resources are indivisible)")
	uploadResources := flag.Int("upload-resources", 2, "maximum resource operations per upload or retirement batch")
	flag.Parse()
	if err := run(*path, benchmarkOptions{duration: *duration, animate: *animate, screenshot: *screenshot, foreground: *foreground, diagnostics: *diagnostics, budget: retained.Budget{Bytes: *uploadBytes, Resources: *uploadResources}}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type benchmarkOptions struct {
	duration                time.Duration
	animate                 bool
	screenshot              string
	foreground, diagnostics bool
	budget                  retained.Budget
}

func run(path string, options benchmarkOptions) error {
	document, err := readDocument(path)
	if err != nil {
		return err
	}
	return display(document, options)
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

func display(document scene.Document, options benchmarkOptions) error {
	duration, animate, screenshot := options.duration, options.animate, options.screenshot
	worker, err := retained.NewWorker(retained.ResidencyLimits{}, options.budget)
	if err != nil {
		return fmt.Errorf("upload budget: %w", err)
	}
	defer func() { worker.Close(); <-worker.Done() }()
	app := qt.NewQApplication([]string{"vecmap-rhi"})
	defer app.Delete()
	item := rhi.NewQQuickItem()
	defer item.Delete()
	item.SetFlag(rhi.QQuickItem__ItemHasContents)
	var frame atomic.Pointer[scene.Frame]
	frame.Store(&scene.Frame{Scene: &document.Scene, Transforms: document.Transforms, DevicePixelRatio: 1})
	var mu sync.Mutex
	var samples frameSamples
	var pacing pacingSamples
	var status streamStatus
	stream := &viewerStream{worker: worker, target: &document.Scene,
		observe: func(stats vecmaprhi.Stats) { mu.Lock(); defer mu.Unlock(); samples.add(stats) },
		report:  func(value streamStatus) { mu.Lock(); defer mu.Unlock(); status = value },
	}
	item.OnUpdatePaintNode(func(_ func(*rhi.QSGNode, *rhi.QQuickItem__UpdatePaintNodeData) *rhi.QSGNode, old *rhi.QSGNode, _ *rhi.QQuickItem__UpdatePaintNodeData) *rhi.QSGNode {
		return stream.sync(item, old, *frame.Load())
	})
	engine, err := createBenchmarkWindow(document, item, options)
	if err != nil {
		return err
	}
	window := item.Window()
	start := time.Now()
	timer := qt.NewQTimer()
	var screenshotError error
	timer.OnTimeout(func() {
		mu.Lock()
		currentStatus := status
		mu.Unlock()
		if currentStatus.Err != nil {
			qt.QCoreApplication_Quit()
			return
		}
		if options.diagnostics {
			state := windowState{Visible: window.IsVisible(), Active: window.IsActive(), Exposed: window.IsExposed()}
			swaps := engine.RootObjects()[0].Property("swapCount")
			state.Swaps = swaps.ToInt()
			mu.Lock()
			pacing.add(time.Now(), samples.lastFrame, samples.latest.Frames, state)
			mu.Unlock()
		}
		elapsed := time.Since(start)
		if duration > 0 && elapsed >= duration {
			if !currentStatus.Ready {
				screenshotError = fmt.Errorf("scene uploads did not become ready before the duration elapsed")
			} else if screenshot != "" {
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
	mu.Lock()
	finalStatus := status // teardown invalidates native readiness
	mu.Unlock()
	engine.Delete()
	mu.Lock()
	defer mu.Unlock()
	latest := samples.latest
	fmt.Printf("platform=%s foreground=%t trace_geographic=%t\n", qt.QGuiApplication_PlatformName(), options.foreground, document.Camera != nil && len(document.TileSpaces) > 0)
	if options.diagnostics {
		pacing.report()
	}
	fmt.Printf("scene draws=%d labels=%d missing_fonts=%q\n", len(document.Scene.Draws), document.Labels, document.MissingFonts)
	fmt.Printf("upload_budget_bytes=%d upload_budget_resources=%d planner_ready=%t native_generation=%d batch_failures=%d\n", options.budget.Bytes, options.budget.Resources, finalStatus.Ready, finalStatus.Generation, finalStatus.BatchFailures)
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
	if finalStatus.Err != nil {
		return fmt.Errorf("upload worker: %w", finalStatus.Err)
	}
	if !finalStatus.Ready {
		return fmt.Errorf("scene uploads incomplete")
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
	if stats.Frames < s.latest.Frames {
		s.lastFrame = time.Time{}
	}
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
