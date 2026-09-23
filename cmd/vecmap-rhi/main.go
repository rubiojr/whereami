//go:build vecmap_rhi

// Command vecmap-rhi displays a portable scene fixture through generated Qt RHI
// bindings. It deliberately has no dependency on the legacy Qt map bridge.
package main

import (
	"flag"
	"fmt"
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
	reload := flag.Duration("reload", 0, "poll the scene file for live replacements (for example 1s); zero disables")
	flag.Parse()
	if err := run(*path, benchmarkOptions{duration: *duration, animate: *animate, screenshot: *screenshot, foreground: *foreground, diagnostics: *diagnostics, reload: *reload, budget: retained.Budget{Bytes: *uploadBytes, Resources: *uploadResources}}); err != nil {
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
	reload                  time.Duration
	feed                    *sceneFeed
}

func run(path string, options benchmarkOptions) error {
	feed, err := newSceneFeed(path, options.reload)
	if err != nil {
		return err
	}
	defer feed.close()
	options.feed = feed
	return display(*feed.latest.Load(), options)
}

func display(document scene.Document, options benchmarkOptions) error {
	duration, animate, screenshot := options.duration, options.animate, options.screenshot
	worker, err := retained.NewWorkerWithData[*scene.Document](retained.ResidencyLimits{}, options.budget)
	if err != nil {
		return fmt.Errorf("upload budget: %w", err)
	}
	defer func() { worker.Close(); <-worker.Done() }()
	app := qt.NewQApplication([]string{"vecmap-rhi"})
	defer app.Delete()
	item := rhi.NewQQuickItem()
	defer item.Delete()
	item.SetFlag(rhi.QQuickItem__ItemHasContents)
	var frame atomic.Pointer[streamUpdate]
	target := &document
	if options.feed != nil {
		target = options.feed.latest.Load()
	}
	frame.Store(&streamUpdate{target: target, camera: traceCamera(document, 0, false)})
	var mu sync.Mutex
	var samples frameSamples
	var pacing pacingSamples
	var status streamStatus
	stream := &viewerStream{worker: worker,
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
		if options.feed != nil {
			target = options.feed.latest.Load()
			select {
			case err := <-options.feed.errors:
				fmt.Fprintf(os.Stderr, "scene reload: %v (keeping previous target)\n", err)
			default:
			}
		}
		if duration > 0 && elapsed >= duration {
			if !currentStatus.Ready || currentStatus.Current != target {
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
		frame.Store(&streamUpdate{target: target, camera: traceCamera(document, elapsed.Seconds(), animate)})
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
	if finalStatus.Current != nil {
		document = *finalStatus.Current
	}
	fmt.Printf("scene draws=%d labels=%d missing_fonts=%q\n", len(document.Scene.Draws), document.Labels, document.MissingFonts)
	fmt.Printf("upload_budget_bytes=%d upload_budget_resources=%d planner_ready=%t native_generation=%d batch_failures=%d\n", options.budget.Bytes, options.budget.Resources, finalStatus.Ready, finalStatus.Generation, finalStatus.BatchFailures)
	fmt.Printf("backend=%s device=%s\n", latest.Backend, latest.Device)
	fmt.Printf("frames=%d mesh_uploads=%d texture_uploads=%d uploaded_bytes=%d live_meshes=%d live_textures=%d\n", latest.Frames, latest.MeshUploads, latest.TextureUploads, latest.UploadedBytes, latest.LiveMeshes, latest.LiveTextures)
	fmt.Printf("completion_drains=%d completion_drain_cpu=%s\n", latest.CompletionDrains, latest.CompletionDrainTime)
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
