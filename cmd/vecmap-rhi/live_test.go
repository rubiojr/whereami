//go:build vecmap_rhi && integration

package main

import (
	"encoding/binary"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	qt "github.com/mappu/miqt/qt6"
	rhi "github.com/rubiojr/whereami/internal/qtrhi"
	"github.com/rubiojr/whereami/internal/vecmaprhi"
	"github.com/rubiojr/whereami/pkg/vecmap/producer"
	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
	"github.com/rubiojr/whereami/pkg/vecmap/tiles"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func coloredMVT(color string) []byte {
	field := func(n uint64, data []byte) []byte {
		out := binary.AppendUvarint(nil, n<<3|2)
		out = binary.AppendUvarint(out, uint64(len(data)))
		return append(out, data...)
	}
	var commands []byte
	for _, v := range []uint64{9, 0, 0, 26, 512, 0, 0, 512, 511, 0, 15} {
		commands = binary.AppendUvarint(commands, v)
	}
	feature := append(field(2, []byte{0, 0}), 24, 3)
	feature = append(feature, field(4, commands)...)
	layer := append(field(1, []byte("land")), field(2, feature)...)
	layer = append(layer, field(3, []byte("color"))...)
	layer = append(layer, field(4, field(1, []byte(color)))...)
	layer = append(layer, 40, 128, 2, 120, 2)
	return field(3, layer)
}

func testLiveProducer(t *testing.T) {
	t.Helper()
	leftGate, rightGate := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		color := "#ff0000"
		var gate <-chan struct{}
		switch r.URL.Path {
		case "/1/0/1.pbf":
			color = "#00ff00"
			gate = leftGate
		case "/1/1/1.pbf":
			color = "#0000ff"
			gate = rightGate
		}
		if gate != nil {
			select {
			case <-gate:
			case <-r.Context().Done():
				return
			}
		}
		_, _ = w.Write(coloredMVT(color))
	}))
	defer server.Close()
	template := server.URL + "/{z}/{x}/{y}.pbf"
	load, err := producer.HTTPLoader(nil, t.TempDir(), template)
	require.NoError(t, err)
	limits := producer.DefaultLimits()
	limits.Workers = 2
	limits.RawBytes = 1024
	p, err := producer.New(load, limits)
	require.NoError(t, err)
	left, right := view.TileID{Z: 1, Y: 1}, view.TileID{Z: 1, X: 1, Y: 1}
	camera := view.NewCamera(view.TileCoordinate(left, view.ScreenPoint{X: 256, Y: 128}), 1, 0, 160, 120)
	initial := &scene.Document{Width: 160, Height: 120, Camera: &camera, TileSpaces: []scene.TileSpace{{Tile: view.TileID{}}}}
	bridge, err := producer.NewBridge(p, initial, retained.ResidencyLimits{Bytes: 8192, Resources: 32}, retained.Budget{Bytes: 1024, Resources: 1})
	require.NoError(t, err)
	defer bridge.Close()
	layers, err := style.Parse([]byte(`{"version":8,"layers":[{"id":"land","type":"fill","source-layer":"land","paint":{"fill-color":["get","color"]}}]}`))
	require.NoError(t, err)
	request := producer.Request{Camera: camera, Targets: []view.TileID{left, right}, Style: &producer.Style{Source: template, Epoch: 1, Layers: layers, Options: tiles.PrepareOptions{Zoom: 1, Indexed: true}, Bytes: 1024}, Assets: &producer.Assets{Epoch: 1, Bytes: 1}}
	var revision atomic.Uint64
	submit := func() { rev, err := p.Submit(request); require.NoError(t, err); revision.Store(rev) }
	submit()
	item := rhi.NewQQuickItem()
	defer item.Delete()
	item.SetFlag(rhi.QQuickItem__ItemHasContents)
	var mu sync.Mutex
	var status streamStatus
	var stats vecmaprhi.Stats
	var pause, pauseAfterUpload, reset, earlyReady atomic.Bool
	var pauseAtUpload atomic.Uint64
	pauseAtUpload.Store(2)
	var update atomic.Pointer[streamCamera]
	update.Store(&streamCamera{geographic: &camera, dpr: 1})
	stream := &viewerStream{worker: bridge.Worker(), bridge: bridge}
	stream.report = func(s streamStatus) { mu.Lock(); status = s; mu.Unlock() }
	stream.observe = func(s vecmaprhi.Stats) {
		mu.Lock()
		stats = s
		mu.Unlock()
		if s.MeshUploads == pauseAtUpload.Load() && pauseAfterUpload.CompareAndSwap(true, false) {
			pause.Store(true)
		}
		// Destruction reports the old epoch before Restarted installs the new
		// generation. Only a live replacement can prematurely grant readiness.
		if stream.epoch != nil && !stream.epoch.dead && !stream.epoch.renderer.Initialized() && bridge.Ready(revision.Load()) {
			earlyReady.Store(true)
		}
	}
	item.OnUpdatePaintNode(func(_ func(*rhi.QSGNode, *rhi.QQuickItem__UpdatePaintNodeData) *rhi.QSGNode, old *rhi.QSGNode, _ *rhi.QQuickItem__UpdatePaintNodeData) *rhi.QSGNode {
		if pause.Load() {
			return old
		}
		if old != nil && reset.Swap(false) {
			stream.epoch.renderer.Node.ReleaseResources()
		}
		node := stream.sync(item, old, streamUpdate{target: bridge.Target(), camera: *update.Load()})
		if stream.epoch != nil && bridge.Ready(revision.Load()) && !stream.epoch.renderer.FrameSelected() {
			earlyReady.Store(true)
		}
		return node
	})
	engine, err := createBenchmarkWindow(*initial, item, benchmarkOptions{foreground: true})
	require.NoError(t, err)
	defer engine.Delete()
	pump := func(check func(streamStatus) bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			item.Update()
			qt.QCoreApplication_ProcessEvents()
			mu.Lock()
			value := status
			mu.Unlock()
			require.NoError(t, value.Err)
			if check(value) {
				return
			}
			time.Sleep(8 * time.Millisecond)
		}
		t.Fatalf("live transition timed out: producer=%+v", p.Status())
	}
	ready := func(s streamStatus) bool {
		return s.Ready && s.Current == bridge.Target() && bridge.Ready(revision.Load())
	}
	pixel := func(x int, want [3]int) {
		t.Helper()
		image := item.Window().GrabWindow()
		defer image.Delete()
		require.False(t, image.IsNull())
		scale := float64(image.Width()) / 160
		color := image.PixelColor(int(float64(x)*scale), int(30*scale))
		defer runtime.KeepAlive(color)
		assert.InDelta(t, want[0], color.Red(), 2)
		assert.InDelta(t, want[1], color.Green(), 2)
		assert.InDelta(t, want[2], color.Blue(), 2)
	}
	pump(func(s streamStatus) bool { return ready(s) && s.Current.Labels == 0 && len(s.Current.Scene.Draws) > 0 })
	pixel(20, [3]int{255, 0, 0})
	pixel(140, [3]int{255, 0, 0}) // root half-world regression p5nh
	mu.Lock()
	initialBytes := stats.UploadedBytes
	mu.Unlock()
	close(leftGate)
	pump(func(s streamStatus) bool { return p.Status().Builds >= 2 && ready(s) })
	pixel(20, [3]int{255, 0, 0})
	mu.Lock()
	assert.Equal(t, initialBytes, stats.UploadedBytes)
	mu.Unlock()
	pauseAfterUpload.Store(true)
	close(rightGate)
	pump(func(streamStatus) bool { return pause.Load() })
	pixel(20, [3]int{255, 0, 0})
	pixel(140, [3]int{255, 0, 0})
	mu.Lock()
	uploadGeneration := status.Generation
	mu.Unlock()
	reset.Store(true) // old leases remain pinned across an interrupted upload
	pause.Store(false)
	pump(func(s streamStatus) bool {
		return s.Generation > uploadGeneration && ready(s) && len(s.Current.TileSpaces) == 2
	})
	pixel(20, [3]int{0, 255, 0})
	pixel(140, [3]int{0, 0, 255})
	mu.Lock()
	stableBytes := stats.UploadedBytes
	mu.Unlock()
	moved := camera.Panned(5, 0)
	request.Camera = moved
	submit()
	update.Store(&streamCamera{geographic: &moved, dpr: 1})
	pump(ready)
	mu.Lock()
	assert.Equal(t, stableBytes, stats.UploadedBytes)
	mu.Unlock()
	// Interrupt a two-mesh style replacement after its first upload. Each
	// replacement adds another fill layer so its geometry, and therefore its mesh
	// revisions, really change; a zoom-only recompile would keep every resident
	// version. All variants draw the same pixels.
	mu.Lock()
	pauseAtUpload.Store(stats.MeshUploads + 1)
	mu.Unlock()
	pauseAfterUpload.Store(true)
	fills := 1
	replaceStyle := func() {
		fills++
		var spec strings.Builder
		spec.WriteString(`{"version":8,"layers":[`)
		for i := range fills {
			if i > 0 {
				spec.WriteString(",")
			}
			fmt.Fprintf(&spec, `{"id":"land%d","type":"fill","source-layer":"land","paint":{"fill-color":["get","color"]}}`, i)
		}
		spec.WriteString("]}")
		layers, err := style.Parse([]byte(spec.String()))
		require.NoError(t, err)
		next := *request.Style
		next.Epoch++
		next.Layers = layers
		request.Style = &next
		submit()
	}
	replaceStyle()
	pump(func(streamStatus) bool { return pause.Load() })
	pixel(20, [3]int{0, 255, 0})
	pixel(140, [3]int{0, 0, 255})
	progress := bridge.Stats()
	replaceStyle()
	// Pumping prepare can complete the checked outstanding upload while Sync is
	// paused, but cannot upload another packet. The newer cover needs two uploads
	// while the exposed target needs one more, so it must wait rather than
	// abandon that progress.
	pump(func(streamStatus) bool { return bridge.Stats().Received > progress.Received })
	waiting := bridge.Stats()
	assert.Equal(t, progress.Superseded, waiting.Superseded, "a document needing more uploads than the target has left cannot supersede it")
	assert.Equal(t, progress.Targets, waiting.Targets)
	pause.Store(false)
	pump(ready)
	settled := bridge.Stats()
	assert.Equal(t, progress.Superseded, settled.Superseded)
	assert.Equal(t, progress.Targets+1, settled.Targets, "the newer cover is exposed once the interrupted one is Current")
	assert.Equal(t, progress.CurrentTargets+2, settled.CurrentTargets)
	pixel(20, [3]int{0, 255, 0})
	pixel(140, [3]int{0, 0, 255})
	// Re-load a root target, then cross its antimeridian under rotation. Copies
	// share resources and must cover both sides without another geometry upload.
	request.Targets = []view.TileID{{}}
	submit()
	pump(func(s streamStatus) bool { return ready(s) && len(s.Current.TileSpaces) == 1 })
	mu.Lock()
	stableBytes = stats.UploadedBytes
	mu.Unlock()
	seam := view.NewCamera(view.Coordinate{Longitude: 179.9}, 2, 37, 160, 120)
	request.Camera = seam
	submit()
	update.Store(&streamCamera{geographic: &seam, dpr: 1})
	pump(func(s streamStatus) bool { return ready(s) && len(s.Current.TileSpaces) == 2 })
	pixel(20, [3]int{255, 0, 0})
	pixel(140, [3]int{255, 0, 0})
	mu.Lock()
	assert.Equal(t, stableBytes, stats.UploadedBytes)
	mu.Unlock()
	request.Targets = []view.TileID{}
	submit()
	pump(func(s streamStatus) bool { return ready(s) && len(s.Current.Scene.Draws) == 0 })
	mu.Lock()
	generation := status.Generation
	mu.Unlock()
	reset.Store(true)
	pump(func(s streamStatus) bool { return s.Generation > generation && ready(s) })
	assert.False(t, earlyReady.Load(), "publication must await CPU frame selection and startup drains")
	mu.Lock()
	assert.GreaterOrEqual(t, stats.CompletionDrains, uint64(2))
	assert.Zero(t, status.BatchFailures)
	mu.Unlock()
	assert.LessOrEqual(t, p.Status().PeakLeases, 4)
	assert.LessOrEqual(t, p.Status().PeakJobs, 2)
}

func testViewerLiveCommand(t *testing.T, residentGeometry, residentSymbols, residentDashes bool) {
	t.Helper()
	fonts := os.Getenv("WHEREAMI_VECTOR_GLYPH_FIXTURE_DIR")
	if fonts == "" {
		t.Skip("set supplied glyph fixtures for live command integration")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(coloredMVT("#ff0000")) }))
	defer server.Close()
	output := filepath.Join(t.TempDir(), "live.png")
	err := run("", benchmarkOptions{duration: time.Second, foreground: true, screenshot: output, budget: retained.Budget{Bytes: 32 << 20, Resources: 32},
		liveOptions: liveOptions{enabled: true, template: server.URL + "/{z}/{x}/{y}.pbf", cache: t.TempDir(), glyphs: fonts, latitude: 0, longitude: 0, zoom: 2, cacheBytes: 256 << 20, residentGeometry: residentGeometry, residentSymbols: residentSymbols, residentDashes: residentDashes}})
	require.NoError(t, err)
	image := qt.NewQImage8(output)
	defer image.Delete()
	require.False(t, image.IsNull())
	assert.GreaterOrEqual(t, image.Width(), 800)
	assert.GreaterOrEqual(t, image.Height(), 600)
}
