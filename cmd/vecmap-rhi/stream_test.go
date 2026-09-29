//go:build vecmap_rhi && integration

package main

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	qt "github.com/mappu/miqt/qt6"
	rhi "github.com/rubiojr/whereami/internal/qtrhi"
	"github.com/rubiojr/whereami/internal/vecmaprhi"
	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestViewerStream(t *testing.T) {
	for _, scenario := range []struct{ loop, name string }{{"basic", "lifecycle"}, {"threaded", "lifecycle"}, {"threaded", "budget"}, {"basic", "reload"}, {"threaded", "reload"}, {"basic", "tiles"}, {"threaded", "tiles"}, {"basic", "live"}, {"threaded", "live"}, {"basic", "live-command"}, {"threaded", "live-command"}, {"basic", "live-command-resident"}, {"threaded", "live-command-resident"}, {"basic", "live-command-symbols"}, {"threaded", "live-command-symbols"}, {"basic", "live-command-dashes"}, {"threaded", "live-command-dashes"}} {
		t.Run("test"+scenario.loop+"-"+scenario.name, func(t *testing.T) {
			binary, err := os.Executable()
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary, "-test.run=^TestViewerStreamProcess$", "-test.v")
			cmd.Env = append(os.Environ(), "WHEREAMI_VIEWER_TEST_PROCESS=1", "QSG_RENDER_LOOP="+scenario.loop, "WHEREAMI_VIEWER_TEST_SCENARIO="+scenario.name)
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", output)
			t.Logf("%s", output)
		})
	}
}

func TestViewerStreamProcess(t *testing.T) {
	if os.Getenv("WHEREAMI_VIEWER_TEST_PROCESS") != "1" {
		t.Skip("runs in a separate Qt process for each render loop")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if os.Getenv("WHEREAMI_VIEWER_TEST_SCENARIO") == "reload" {
		testViewerReload(t)
		return
	}
	if scenario := os.Getenv("WHEREAMI_VIEWER_TEST_SCENARIO"); strings.HasPrefix(scenario, "live-command") {
		// Each scenario adds one resident option to the previous one.
		dashes := scenario == "live-command-dashes"
		testViewerLiveCommand(t, scenario != "live-command", dashes || scenario == "live-command-symbols", dashes)
		return
	}
	app := qt.NewQApplication([]string{"vecmap-stream-test"})
	defer app.Delete()
	if os.Getenv("WHEREAMI_VIEWER_TEST_SCENARIO") == "live" {
		testLiveProducer(t)
		return
	}
	if os.Getenv("WHEREAMI_VIEWER_TEST_SCENARIO") == "tiles" {
		testTileComposition(t)
		return
	}
	guiThread := qt.QThread_CurrentThreadId()
	document := scene.Document{Width: 160, Height: 120, Transforms: []scene.Affine{{M11: 1, M22: 1}}, Scene: scene.Scene{
		Meshes:   []scene.Mesh{{ID: 1, Revision: 1, Vertices: []scene.Vertex{{X: 0, Y: 0}, {X: 100, Y: 0}, {X: 0, Y: 80}, {X: 100, Y: 0}, {X: 100, Y: 80}, {X: 0, Y: 80}}}},
		Textures: []scene.Texture{{ID: 1, Revision: 1, Width: 1, Height: 1, RGBA: []byte{0, 255, 0, 255}}},
		Draws:    []scene.Draw{{Mesh: 1, Count: 6, Material: scene.Material{Kind: scene.Image, Texture: 1, Color: [4]float32{1, 1, 1, 1}}}},
	}}
	budget := retained.Budget{Bytes: 1024, Resources: 1}
	if os.Getenv("WHEREAMI_VIEWER_TEST_SCENARIO") == "budget" {
		budget.Bytes = 1
	}
	worker, err := retained.NewWorkerWithData[*scene.Document](retained.ResidencyLimits{Bytes: 4096, Resources: 8}, budget)
	require.NoError(t, err)
	defer func() { worker.Close(); <-worker.Done() }()
	item := rhi.NewQQuickItem()
	defer item.Delete()
	item.SetFlag(rhi.QQuickItem__ItemHasContents)
	var frame atomic.Pointer[streamUpdate]
	frame.Store(&streamUpdate{target: &document, camera: traceCamera(document, 0, false)})
	var requestReset, resetWhileUploading atomic.Bool
	var pauseSync, pauseReplacement atomic.Bool
	var duringUpload atomic.Int32
	var mu sync.Mutex
	var status streamStatus
	var stats vecmaprhi.Stats
	var renderThread unsafe.Pointer
	stream := &viewerStream{worker: worker}
	stream.report = func(value streamStatus) { mu.Lock(); defer mu.Unlock(); status = value }
	stream.observe = func(value vecmaprhi.Stats) {
		mu.Lock()
		stats = value
		renderThread = qt.QThread_CurrentThreadId()
		mu.Unlock()
		if value.MeshUploads == 2 && pauseReplacement.CompareAndSwap(true, false) {
			pauseSync.Store(true)
		}
		if stream.epoch != nil && stream.epoch.pending != nil && value.MeshUploads > 0 && resetWhileUploading.CompareAndSwap(true, false) {
			duringUpload.Add(1)
			// Invalidate after recording, before endFrame/acknowledgement. Native
			// wrappers must survive the current frame through DeleteLater.
			stream.epoch.renderer.Node.ReleaseResources()
		}
	}
	var retired *nativeEpoch // used only by render-thread callbacks
	item.OnUpdatePaintNode(func(_ func(*rhi.QSGNode, *rhi.QQuickItem__UpdatePaintNodeData) *rhi.QSGNode, old *rhi.QSGNode, _ *rhi.QQuickItem__UpdatePaintNodeData) *rhi.QSGNode {
		if pauseSync.Load() {
			return old
		}
		if old != nil && requestReset.Swap(false) {
			retired = stream.epoch
			if retired.pending != nil {
				duringUpload.Add(1)
			}
			retired.renderer.Node.ReleaseResources()
		}
		node := stream.sync(item, old, *frame.Load())
		if retired != nil && retired != stream.epoch {
			stream.complete(retired, vecmaprhi.BatchResult{Reset: true}) // stale reset cannot kill replacement
		}
		return node
	})
	engine, err := createBenchmarkWindow(document, item, benchmarkOptions{foreground: true})
	require.NoError(t, err)
	defer engine.Delete()
	if budget.Bytes == 1 {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			item.Update()
			qt.QCoreApplication_ProcessEvents()
			mu.Lock()
			failed := status.Err != nil
			mu.Unlock()
			if failed {
				break
			}
			time.Sleep(8 * time.Millisecond)
		}
		mu.Lock()
		assert.ErrorIs(t, status.Err, retained.ErrBudget)
		assert.False(t, status.Ready)
		assert.Zero(t, stats.MeshUploads)
		assert.LessOrEqual(t, stats.UploadedBytes, uint64(4), "only backend-owned white is outside the budget")
		mu.Unlock()
		return
	}
	pump := func(generation uint64, ready bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			item.Update()
			qt.QCoreApplication_ProcessEvents()
			mu.Lock()
			value := status
			mu.Unlock()
			require.NoError(t, value.Err)
			if value.Generation >= generation && (!ready || (value.Ready && value.Current == frame.Load().target)) {
				return
			}
			time.Sleep(8 * time.Millisecond)
		}
		t.Fatal("viewer did not reach requested generation/readiness")
	}
	pixel := func(x, y int, green bool) {
		t.Helper()
		image := item.Window().GrabWindow()
		defer image.Delete()
		require.False(t, image.IsNull())
		scale := float64(image.Width()) / float64(document.Width)
		color := image.PixelColor(int(float64(x)*scale), int(float64(y)*scale))
		defer runtime.KeepAlive(color)
		if green {
			assert.InDelta(t, 0, color.Red(), 2)
			assert.InDelta(t, 255, color.Green(), 2)
		} else {
			assert.Greater(t, color.Red(), 200)
		}
	}
	pump(1, true)
	pixel(5, 5, true)
	mu.Lock()
	initial := stats
	observedThread := renderThread
	mu.Unlock()
	if os.Getenv("QSG_RENDER_LOOP") == "threaded" {
		assert.NotEqual(t, guiThread, observedThread)
	} else {
		assert.Equal(t, guiThread, observedThread)
	}
	camera := streamCamera{affine: scene.Affine{M11: 1, M22: 1, DX: 20}, dpr: 1}
	frame.Store(&streamUpdate{target: &document, camera: camera})
	item.Update()
	pixel(5, 5, false)
	pixel(30, 30, true)
	mu.Lock()
	assert.Equal(t, initial.UploadedBytes, stats.UploadedBytes)
	mu.Unlock()
	// A replacement changes transform-slot count/order. During partial uploads
	// the old scene must keep its old mapping, not use target slot zero at x=1000.
	replacement := document
	replacement.Scene.Meshes = append([]scene.Mesh(nil), document.Scene.Meshes...)
	replacement.Scene.Meshes[0].Revision++
	replacement.Scene.Textures = append([]scene.Texture(nil), document.Scene.Textures...)
	replacement.Scene.Textures[0].Revision++
	replacement.Scene.Draws = append([]scene.Draw(nil), document.Scene.Draws...)
	replacement.Scene.Draws[0].Transform = 1
	replacement.Transforms = []scene.Affine{{M11: 1, M22: 1, DX: 1000}, {M11: 1, M22: 1, DX: 30}}
	require.NoError(t, replacement.Validate())
	pauseReplacement.Store(true)
	frame.Store(&streamUpdate{target: &replacement, camera: camera})
	deadline := time.Now().Add(5 * time.Second)
	for !pauseSync.Load() && time.Now().Before(deadline) {
		item.Update()
		qt.QCoreApplication_ProcessEvents()
		mu.Lock()
		failure := status.Err
		mu.Unlock()
		require.NoError(t, failure)
		time.Sleep(8 * time.Millisecond)
	}
	require.True(t, pauseSync.Load())
	pixel(30, 30, true)
	mu.Lock()
	assert.Same(t, &document, status.Current)
	assert.False(t, status.Ready)
	mu.Unlock()
	pauseSync.Store(false)
	pump(1, true)
	pixel(30, 30, false)
	pixel(60, 30, true)
	// A mapping-only target with exactly resident resource versions publishes
	// without upload and without using the prior target's mapping.
	mappingOnly := replacement
	mappingOnly.Transforms = []scene.Affine{{M11: 1, M22: 1, DX: 1000}, {M11: 1, M22: 1, DX: 5}}
	mu.Lock()
	uploaded := stats.UploadedBytes
	mu.Unlock()
	frame.Store(&streamUpdate{target: &mappingOnly, camera: camera})
	pump(1, true)
	pixel(30, 30, true)
	mu.Lock()
	assert.Equal(t, uploaded, stats.UploadedBytes)
	mu.Unlock()
	requestReset.Store(true)
	pump(2, true)
	pixel(30, 30, true)
	resetWhileUploading.Store(true)
	requestReset.Store(true)
	pump(4, true)
	pixel(30, 30, true)
	assert.Positive(t, duringUpload.Load())
	mu.Lock()
	assert.Equal(t, uint64(1), stats.MeshUploads, "new native namespace reuploads once")
	assert.Equal(t, uint64(2), stats.TextureUploads, "target texture plus implicit white")
	assert.Zero(t, status.BatchFailures)
	t.Logf("loop=%s backend=%s device=%s generation=%d", os.Getenv("QSG_RENDER_LOOP"), stats.Backend, stats.Device, status.Generation)
	mu.Unlock()
	// Clearing a live target selects an empty frame while retirement completes;
	// reselecting it restores its own mapping in the same native generation.
	frame.Store(&streamUpdate{camera: camera})
	pump(4, true)
	pixel(30, 30, false)
	frame.Store(&streamUpdate{target: &mappingOnly, camera: camera})
	pump(4, true)
	pixel(30, 30, true)
	// Leave a fresh generation in flight; deferred teardown and worker cancellation
	// must not wait for an acknowledgement from the destroyed scene graph.
	requestReset.Store(true)
	pump(5, false)
}
