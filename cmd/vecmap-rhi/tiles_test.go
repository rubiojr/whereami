//go:build vecmap_rhi && integration

package main

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	qt "github.com/mappu/miqt/qt6"
	rhi "github.com/rubiojr/whereami/internal/qtrhi"
	"github.com/rubiojr/whereami/internal/vecmaprhi"
	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/tiles"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testTileComposition(t *testing.T) {
	t.Helper()
	set, err := tiles.New(retained.Limits{})
	require.NoError(t, err)
	left, right := view.TileID{X: 2, Y: 2, Z: 3}, view.TileID{X: 3, Y: 2, Z: 3}
	parent, _ := left.Parent()
	camera := view.NewCamera(view.TileCoordinate(left, view.ScreenPoint{X: 256, Y: 128}), 3, 0, 160, 120)
	fragment := func(color [4]float32) *tiles.Fragment {
		return &tiles.Fragment{Scene: &scene.Scene{
			Meshes: []scene.Mesh{{ID: 1, Vertices: []scene.Vertex{{}, {X: 256}, {Y: 256}, {X: 256}, {X: 256, Y: 256}, {Y: 256}}}},
			Draws:  []scene.Draw{{Mesh: 1, Count: 6, Clip: [4]float32{0, 0, 256, 256}, Material: scene.Material{Color: color}}},
		}, Draws: []tiles.Draw{{Layer: 0}}}
	}
	red, green, blue := [4]float32{1, 0, 0, 1}, [4]float32{0, 1, 0, 1}, [4]float32{0, 0, 1, 1}
	require.NoError(t, set.Apply([]tiles.Change{{Tile: parent, Fragment: fragment(red)}}))
	compose := func(previous []view.TileID) (*tiles.Snapshot, *scene.Document) {
		t.Helper()
		snapshot, err := set.Select([]view.TileID{left, right}, previous, camera, 0)
		require.NoError(t, err)
		document := &scene.Document{Scene: *snapshot.Scene, TileSpaces: snapshot.TileSpaces, Camera: &camera, Width: 160, Height: 120}
		require.NoError(t, document.Validate())
		return snapshot, document
	}
	coarse, document := compose(nil)
	worker, err := retained.NewWorkerWithData[*scene.Document](retained.ResidencyLimits{Bytes: 4096, Resources: 8}, retained.Budget{Bytes: 1024, Resources: 1})
	require.NoError(t, err)
	defer func() { worker.Close(); <-worker.Done() }()
	item := rhi.NewQQuickItem()
	defer item.Delete()
	item.SetFlag(rhi.QQuickItem__ItemHasContents)
	var request atomic.Pointer[streamUpdate]
	request.Store(&streamUpdate{target: document, camera: traceCamera(*document, 0, false)})
	var pause, pauseAfterUpload atomic.Bool
	var mu sync.Mutex
	var stats vecmaprhi.Stats
	var status streamStatus
	stream := &viewerStream{worker: worker,
		report: func(v streamStatus) { mu.Lock(); status = v; mu.Unlock() },
		observe: func(v vecmaprhi.Stats) {
			mu.Lock()
			stats = v
			mu.Unlock()
			if v.MeshUploads == 2 && pauseAfterUpload.CompareAndSwap(true, false) {
				pause.Store(true)
			}
		},
	}
	item.OnUpdatePaintNode(func(_ func(*rhi.QSGNode, *rhi.QQuickItem__UpdatePaintNodeData) *rhi.QSGNode, old *rhi.QSGNode, _ *rhi.QQuickItem__UpdatePaintNodeData) *rhi.QSGNode {
		if pause.Load() {
			return old
		}
		return stream.sync(item, old, *request.Load())
	})
	engine, err := createBenchmarkWindow(*document, item, benchmarkOptions{foreground: true})
	require.NoError(t, err)
	defer engine.Delete()
	pump := func(target *scene.Document, waitingForPause bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			item.Update()
			qt.QCoreApplication_ProcessEvents()
			mu.Lock()
			current := status
			mu.Unlock()
			require.NoError(t, current.Err)
			if (waitingForPause && pause.Load()) || (!waitingForPause && current.Ready && current.Current == target) {
				return
			}
			time.Sleep(8 * time.Millisecond)
		}
		t.Fatal("tile composition transition did not complete")
	}
	pixel := func(x int, expected [4]float32) {
		t.Helper()
		image := item.Window().GrabWindow()
		defer image.Delete()
		require.False(t, image.IsNull())
		scale := float64(image.Width()) / 160
		color := image.PixelColor(int(float64(x)*scale), int(30*scale))
		defer runtime.KeepAlive(color)
		assert.InDelta(t, expected[0]*255, color.Red(), 2)
		assert.InDelta(t, expected[1]*255, color.Green(), 2)
		assert.InDelta(t, expected[2]*255, color.Blue(), 2)
	}
	pump(document, false)
	pixel(20, red)
	pixel(140, red)
	mu.Lock()
	initialBytes := stats.UploadedBytes
	mu.Unlock()
	require.NoError(t, set.Apply([]tiles.Change{{Tile: left, Fragment: fragment(green)}}))
	_, partial := compose(coarse.Cover)
	request.Store(&streamUpdate{target: partial, camera: traceCamera(*partial, 0, false)})
	pump(partial, false)
	pixel(20, red)
	mu.Lock()
	assert.Equal(t, initialBytes, stats.UploadedBytes, "one prepared child cannot replace the parent")
	mu.Unlock()
	require.NoError(t, set.Apply([]tiles.Change{{Tile: right, Fragment: fragment(blue)}}))
	_, refined := compose(coarse.Cover)
	pauseAfterUpload.Store(true)
	request.Store(&streamUpdate{target: refined, camera: traceCamera(*refined, 0, false)})
	pump(refined, true)
	pixel(20, red)
	pixel(140, red)
	pause.Store(false)
	pump(refined, false)
	pixel(20, green)
	pixel(140, blue)
	mu.Lock()
	assert.Equal(t, uint64(3), stats.MeshUploads, "parent and two child meshes")
	assert.Zero(t, status.BatchFailures)
	mu.Unlock()
}
