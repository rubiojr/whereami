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
	"github.com/rubiojr/whereami/pkg/vecmap/producer"
	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/tileio"
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
	uploadResources := flag.Int("upload-resources", 4, "maximum resources per upload batch")
	releaseResources := flag.Int("release-resources", 8, "maximum resources per retirement batch; zero uses -upload-resources")
	reload := flag.Duration("reload", 0, "poll the scene file for live replacements (for example 1s); zero disables")
	live := liveOptions{}
	flag.BoolVar(&live.enabled, "live", false, "load live MVT tiles with supplied Liberty fonts")
	flag.StringVar(&live.template, "tile-url", tileio.OpenFreeMapTemplate, "XYZ URL template for -live ({z}/{x}/{y})")
	flag.StringVar(&live.cache, "cache-dir", "", "optional application-owned tile disk cache directory")
	flag.StringVar(&live.glyphs, "glyph-dir", "", "supplied Noto Sans Regular/Bold/Italic 0-255 PBF directory")
	flag.Float64Var(&live.latitude, "latitude", 40.4168, "initial live latitude")
	flag.Float64Var(&live.longitude, "longitude", -3.7038, "initial live longitude")
	flag.Float64Var(&live.zoom, "zoom", 10, "initial live zoom")
	flag.Uint64Var(&live.cacheBytes, "cpu-cache-bytes", 256<<20, "live raw/prepared/fragment/profile cache budget")
	flag.IntVar(&live.workers, "tile-workers", 4, "live transport workers (1-4); use 1 for ordered cache replay")
	flag.BoolVar(&live.cacheOnly, "cache-only", false, "live replay from verified cached tiles only; never fetch missing entries")
	flag.BoolVar(&live.residentGeometry, "resident-geometry", false, "keep fills and shader-extruded lines resident across style-zoom changes")
	flag.BoolVar(&live.residentSymbols, "resident-symbols", false, "keep icon and text quads resident across style-zoom changes that preserve symbol layout")
	flag.Parse()
	if err := run(*path, benchmarkOptions{liveOptions: live, duration: *duration, animate: *animate, screenshot: *screenshot, foreground: *foreground, diagnostics: *diagnostics, reload: *reload, budget: retained.Budget{Bytes: *uploadBytes, Resources: *uploadResources, Releases: *releaseResources}}); err != nil {
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
	liveOptions             liveOptions
	live                    *liveSource
}

func run(path string, options benchmarkOptions) error {
	if options.liveOptions.enabled {
		if path != "" || options.reload != 0 {
			return fmt.Errorf("-live cannot be combined with -scene or -reload")
		}
		live, err := newLiveSource(options.liveOptions, options.budget)
		if err != nil {
			return err
		}
		defer live.bridge.Close()
		options.live = live
		return display(*live.initial, options)
	}
	feed, err := newSceneFeed(path, options.reload)
	if err != nil {
		return err
	}
	defer feed.close()
	options.feed = feed
	return display(*feed.latest.Load(), options)
}

func display(document scene.Document, options benchmarkOptions) error {
	screenshot := options.screenshot
	var worker *retained.WorkerWithData[*scene.Document]
	var err error
	if options.live != nil {
		worker = options.live.bridge.Worker()
	} else {
		worker, err = retained.NewWorkerWithData[*scene.Document](retained.ResidencyLimits{}, options.budget)
	}
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
	if options.live != nil {
		stream.bridge = options.live.bridge
		target = options.live.bridge.Target()
		frame.Store(&streamUpdate{target: target, camera: traceCamera(document, 0, false)})
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
		update, err := options.sampleUpdate(document, target, elapsed)
		if err != nil {
			screenshotError = err
			qt.QCoreApplication_Quit()
			return
		}
		target = update.target
		if done, err := options.finish(elapsed, currentStatus, target); done {
			screenshotError = err
			if err == nil && screenshot != "" {
				image := item.Window().GrabWindow()
				if image.IsNull() || !image.Save(screenshot) {
					screenshotError = fmt.Errorf("save screenshot %q", screenshot)
				}
				image.Delete()
			}
			qt.QCoreApplication_Quit()
			return
		}
		frame.Store(&update)
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
	fmt.Printf("platform=%s foreground=%t trace_geographic=%t resident_geometry=%t resident_symbols=%t\n", qt.QGuiApplication_PlatformName(), options.foreground, document.Camera != nil && len(document.TileSpaces) > 0, options.liveOptions.residentGeometry, options.liveOptions.residentSymbols)
	if options.diagnostics {
		pacing.report()
	}
	if finalStatus.Current != nil {
		document = *finalStatus.Current
	}
	fmt.Printf("scene draws=%d labels=%d missing_fonts=%q\n", len(document.Scene.Draws), document.Labels, document.MissingFonts)
	if options.live != nil {
		s := options.live.p.Status()
		fmt.Printf("producer_loads=%d prepares=%d builds=%d rejected=%d peak_jobs=%d peak_cache_bytes=%d peak_leases=%d peak_lease_bytes=%d pending=%d failed=%d last_error=%q\n", s.Loads, s.Prepares, s.Builds, s.Rejected, s.PeakJobs, s.PeakCacheBytes, s.PeakLeases, s.PeakLeaseBytes, s.Pending, s.Failed, s.LastError)
		fmt.Printf("producer_requested=%d selected=%d fallbacks=%d error_stage=%q response_bytes=%d raw_capacity_bytes=%d\n", s.Requested, s.SelectedTiles, s.Fallbacks, s.LastErrorStage, s.ResponseBytes, s.RawCapacityBytes)
		fmt.Printf("cache_raw=%d prepared=%d fragments=%d profiles=%d peak_raw=%d peak_prepared=%d peak_fragments=%d peak_profiles=%d\n", s.Cache.Raw, s.Cache.Prepared, s.Cache.Fragments, s.Cache.Profiles, s.PeakCache.Raw, s.PeakCache.Prepared, s.PeakCache.Fragments, s.PeakCache.Profiles)
		fmt.Printf("preparation_evictions=%d preparation_bytes_freed=%d uncached_preparations=%d capacity_retries=%d continuity_evictions=%d continuity_bytes_freed=%d\n", s.PreparationEvictions, s.PreparationBytesFreed, s.UncachedPreparations, s.CapacityRetries, s.ContinuityEvictions, s.ContinuityBytesFreed)
		fmt.Printf("load_style_reuses=%d style_reuses=%d skipped_builds=%d deferred_selections=%d unchanged_current=%d\n", s.LoadStyleReuses, s.StyleReuses, s.SkippedBuilds, s.DeferredSelections, s.UnchangedCurrent)
		fmt.Printf("style_adoptions=%d held_styles=%d style_held=%t reused_versions=%d\n", s.StyleAdoptions, s.HeldStyles, s.StyleHeld, s.ReusedVersions)
		b := options.live.bridge.Stats()
		fmt.Printf("bridge_received=%d coalesced=%d same_snapshot=%d targets=%d superseded=%d settlements=%d first_visible_current=%s\n", b.Received, b.Coalesced, b.SameSnapshot, b.Targets, b.Superseded, b.Settlements, b.FirstVisibleCurrent)
		fmt.Printf("bridge_upload_batches=%d upload_bytes=%d overtaken_upload_bytes=%d release_batches=%d released_resources=%d\n", b.UploadBatches, b.UploadBytes, b.OvertakenUploadBytes, b.ReleaseBatches, b.ReleasedResources)
		fmt.Printf("bridge_current_changes=%d first_current_tiles=%d first_current_labels=%d current_tiles=%d current_fallbacks=%d longest_current_hold=%s\n", b.CurrentChanges, b.FirstCurrentTiles, b.FirstCurrentLabels, b.CurrentTiles, b.CurrentFallbacks, b.LongestCurrentHold)
		fmt.Printf("bridge_current_targets=%d target_errors=%d longest_current_age=%s total_current_age=%s\n", b.CurrentTargets, b.TargetErrors, b.LongestCurrentAge, b.TotalCurrentAge)
		for _, phase := range []struct {
			name  string
			value producer.PhaseTime
		}{{"load", s.Loading}, {"prepare", s.Preparing}, {"build", s.Building}, {"select", s.Selecting}} {
			fmt.Printf("producer_%s_count=%d wall_total=%s wall_max=%s\n", phase.name, phase.value.Count, phase.value.Total, phase.value.Maximum)
		}
	}
	fmt.Printf("upload_budget_bytes=%d upload_budget_resources=%d release_budget_resources=%d planner_ready=%t native_generation=%d batch_failures=%d\n", options.budget.Bytes, options.budget.Resources, options.budget.Releases, finalStatus.Ready, finalStatus.Generation, finalStatus.BatchFailures)
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
