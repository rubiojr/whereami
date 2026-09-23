//go:build vecmap_rhi && integration

package vecmaprhi

import (
	"errors"
	"runtime"
	"testing"
	"time"
	"unsafe"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/qml"
	rhi "github.com/rubiojr/whereami/internal/qtrhi"
	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testBatchExecution(t *testing.T, indexed bool) {
	t.Helper()
	first, replacement, abandoned := stagingScene(t, 1, indexed), stagingScene(t, 2, indexed), stagingScene(t, 3, indexed)
	replacement.Draws[0].Transform = 1
	planner, err := retained.NewPlanner(retained.ResidencyLimits{Bytes: 4096, Resources: 8})
	require.NoError(t, err)
	budget := retained.Budget{Bytes: 1024, Resources: 1}
	require.NoError(t, planner.SetTarget(first))
	item := rhi.NewQQuickItem()
	defer item.Delete()
	item.SetFlag(rhi.QQuickItem__ItemHasContents)
	var renderer *BatchRenderer
	var queued *retained.Batch
	var send bool
	frame := scene.Frame{Transforms: []scene.Affine{{M11: 1, M22: 1}}, DevicePixelRatio: 1}
	var results []BatchResult
	var syncError error
	item.OnUpdatePaintNode(func(_ func(*rhi.QSGNode, *rhi.QQuickItem__UpdatePaintNodeData) *rhi.QSGNode, old *rhi.QSGNode, _ *rhi.QQuickItem__UpdatePaintNodeData) *rhi.QSGNode {
		if old == nil {
			renderer = NewBatchRenderer(item, nil, func(result BatchResult) { results = append(results, result) })
			syncError = renderer.Sync(frame, nil)
		}
		if send {
			send = false
			syncError = renderer.Sync(frame, queued)
			queued = nil
		}
		return renderer.Node.QSGNode
	})
	engine := qml.NewQQmlApplicationEngine()
	engine.RootContext().SetContextProperty("testItem", item.QObject)
	engine.LoadData([]byte(`import QtQuick
import QtQuick.Window
Window { visible:true; width:160; height:120; color:"black"
 Item { id:host; anchors.fill:parent
  Binding {target:testItem;property:"parent";value:host}
  Binding {target:testItem;property:"width";value:100}
  Binding {target:testItem;property:"height";value:80}
 }
}`))
	require.Len(t, engine.RootObjects(), 1)
	deadline := time.Now().Add(5 * time.Second)
	for renderer == nil && time.Now().Before(deadline) {
		qt.QCoreApplication_ProcessEvents()
		time.Sleep(time.Millisecond)
	}
	require.NotNil(t, renderer)
	window := rhi.UnsafeNewQQuickItem(item.UnsafePointer()).Window()
	grab := func() *qt.QImage {
		item.Update()
		qt.QCoreApplication_ProcessEvents()
		image := window.GrabWindow()
		require.NoError(t, syncError)
		require.False(t, renderer.dead)
		require.Empty(t, renderer.r.stats.Error)
		return image
	}
	image := grab()
	image.Delete()
	// Exercise signal delivery from a different emitting thread. An Auto/queued
	// connection would run later on the GUI thread and cannot serve as a native
	// frame-lifetime boundary in Qt's threaded render loop.
	threadIDs := make(chan unsafe.Pointer, 1)
	emitProbe := true
	frameEnds := 0
	window.OnAfterFrameEnd(func() {
		if emitProbe {
			threadIDs <- qt.QThread_CurrentThreadId()
			return
		}
		if renderer.executed && !renderer.dead {
			frameEnds++
			assert.True(t, renderer.submitted)
			assert.Empty(t, results, "DeleteLater/endFrame alone cannot reclaim capacity")
		}
	})
	emitter := make(chan unsafe.Pointer, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		window.AfterFrameEnd()
		emitter <- qt.QThread_CurrentThreadId()
	}()
	expectedThread := <-emitter
	select {
	case got := <-threadIDs:
		assert.Equal(t, expectedThread, got)
	default:
		t.Fatal("afterFrameEnd callback was not direct")
	}
	emitProbe = false
	t.Logf("batch indexed=%t backend=%s device=%s", indexed, renderer.r.stats.Backend, renderer.r.stats.Device)
	// Observe before native frame submission: there must be no acknowledgement
	// even though allocations/commands already exist. Do not call Planner here.
	checks := 0
	renderer.r.observe = func(Stats) {
		if renderer.executed && !renderer.dead {
			checks++
			assert.Empty(t, results, "render callback precedes afterFrameEnd acknowledgement")
		}
	}
	step := func(success bool) *qt.QImage {
		t.Helper()
		batch, err := planner.Next(budget)
		require.NoError(t, err)
		require.NotNil(t, batch)
		assert.LessOrEqual(t, batch.Bytes, budget.Bytes)
		assert.LessOrEqual(t, len(batch.Uploads)+len(batch.Releases), budget.Resources)
		frame.Scene, queued, send = planner.Current(), batch, true
		frame.Transforms = []scene.Affine{{M11: 1, M22: 1}}
		if frame.Scene == replacement {
			frame.Transforms = append(frame.Transforms, scene.Affine{M11: 1, M22: 1})
		}
		before := renderer.r.stats
		image := grab()
		deadline := time.Now().Add(5 * time.Second)
		for len(results) == 0 && time.Now().Before(deadline) {
			item.Update()
			qt.QCoreApplication_ProcessEvents()
			time.Sleep(time.Millisecond)
		}
		require.Len(t, results, 1)
		result := results[0]
		results = nil
		assert.False(t, result.Reset)
		assert.Equal(t, batch.Ticket, result.Ticket)
		assert.Equal(t, success, result.Success)
		assert.LessOrEqual(t, renderer.r.stats.UploadedBytes-before.UploadedBytes, budget.Bytes)
		require.NoError(t, planner.Acknowledge(result.Ticket, result.Success))
		return image
	}
	image = step(true)
	checkPixel(t, image, 30, 30, 0, 0, 0)
	image.Delete()
	assert.Nil(t, planner.Current())
	image = step(true)
	checkPixel(t, image, 30, 30, 0, 0, 0)
	image.Delete()
	require.Same(t, first, planner.Current())
	frame.Scene, send = planner.Current(), true
	image = grab()
	checkPixel(t, image, 5, 5, 0, 255, 0)
	image.Delete()
	// A failed native allocation preserves current and every preceding batch.
	require.NoError(t, planner.SetTarget(replacement))
	failure := errors.New("injected batch texture failure")
	budget.Resources = 2
	renderer.textureFactory = func(scene.Texture) (gpuTexture, error) { return gpuTexture{}, failure }
	image = step(false)
	checkPixel(t, image, 5, 5, 0, 255, 0)
	image.Delete()
	assert.Len(t, renderer.r.meshes, 1, "fresh mesh from failed two-resource batch was discarded")
	assert.Len(t, renderer.r.textures, 2)
	budget.Resources = 1
	image = step(true)
	checkPixel(t, image, 5, 5, 0, 255, 0)
	image.Delete()
	renderer.textureFactory = func(scene.Texture) (gpuTexture, error) { return gpuTexture{}, failure }
	image = step(false)
	checkPixel(t, image, 5, 5, 0, 255, 0)
	image.Delete()
	require.Same(t, first, planner.Current())
	assert.Len(t, renderer.r.meshes, 2)
	renderer.textureFactory = renderer.r.createTexture
	image = step(true)
	checkPixel(t, image, 5, 5, 0, 255, 0)
	image.Delete()
	require.Same(t, replacement, planner.Current())
	before := renderer.r.stats
	// Selecting the new current and retiring the old mesh is one sync packet.
	image = step(true)
	checkPixel(t, image, 5, 5, 0, 0, 0)
	checkPixel(t, image, 30, 30, 0, 0, 255)
	image.Delete()
	image = step(true)
	image.Delete()
	assert.Equal(t, before.MeshUploads, renderer.r.stats.MeshUploads)
	assert.Equal(t, before.TextureUploads, renderer.r.stats.TextureUploads)
	assert.Len(t, renderer.r.meshes, 1)
	assert.Len(t, renderer.r.textures, 2)
	// Camera-only sync keeps current's transform namespace and does no upload.
	frame.Transforms = []scene.Affine{{M11: 1, M22: 1}, {M11: 1, M22: 1, DX: 20}}
	send = true
	image = grab()
	checkPixel(t, image, 15, 15, 0, 0, 0)
	checkPixel(t, image, 40, 30, 0, 0, 255)
	image.Delete()
	assert.Equal(t, before.UploadedBytes, renderer.r.stats.UploadedBytes)
	// Supersede a partially uploaded target; retire its mesh before new work.
	require.NoError(t, planner.SetTarget(abandoned))
	image = step(true)
	image.Delete()
	require.NoError(t, planner.SetTarget(replacement))
	image = step(true)
	checkPixel(t, image, 30, 30, 0, 0, 255)
	image.Delete()
	assert.Len(t, renderer.r.meshes, 1)
	// Empty publication continues to execute release batches without any draws.
	require.NoError(t, planner.SetTarget(nil))
	image = step(true)
	checkPixel(t, image, 30, 30, 0, 0, 0)
	image.Delete()
	image = step(true)
	image.Delete()
	assert.Empty(t, renderer.r.meshes)
	assert.Len(t, renderer.r.textures, 1)
	assert.Positive(t, checks)
	assert.Positive(t, frameEnds)
	// Teardown with a pending CPU batch invalidates the namespace, not just its
	// ticket; an old Planner must never be reused with a newly created renderer.
	require.NoError(t, planner.SetTarget(first))
	pending, err := planner.Next(budget)
	require.NoError(t, err)
	require.NotNil(t, pending)
	require.NoError(t, renderer.Sync(frame, pending))
	engine.Delete()
	require.Len(t, results, 1)
	assert.True(t, results[0].Reset)
	assert.Error(t, renderer.Sync(frame, nil))
}

func TestBatchGuards(t *testing.T) {
	var results []BatchResult
	r := &Renderer{frame: scene.Frame{Scene: &scene.Scene{}}, meshes: map[resourceKey]gpuMesh{{1, 1}: {}}, textures: map[resourceKey]gpuTexture{}}
	b := &BatchRenderer{r: r, notify: func(result BatchResult) { results = append(results, result) }}
	mesh := retained.Version{Kind: retained.MeshResource, ID: 1, Revision: 1}
	batch := &retained.Batch{Ticket: 1, Releases: []retained.Version{mesh}}
	require.NoError(t, b.Sync(scene.Frame{}, batch))
	batch.Releases[0].ID = 99
	assert.Equal(t, uint64(1), b.pending.Releases[0].ID)
	assert.ErrorIs(t, b.Sync(scene.Frame{}, nil), retained.ErrBusy)
	b.pending = nil
	assert.ErrorIs(t, b.Sync(scene.Frame{}, batch), retained.ErrTicket)
	batch.Ticket = 0
	assert.ErrorIs(t, b.Sync(scene.Frame{}, batch), retained.ErrTicket)
	assert.True(t, b.contains(mesh))
	assert.False(t, b.contains(retained.Version{}))
	assert.False(t, b.contains(retained.Version{Kind: 99, ID: 1}))
	assert.False(t, b.contains(retained.Version{Kind: retained.TextureResource, ID: 1}))
	assert.Error(t, r.requireResident(&scene.Scene{Meshes: []scene.Mesh{{ID: 2}}}))
	assert.Error(t, r.requireResident(&scene.Scene{Textures: []scene.Texture{{ID: 2}}}))
	r.frame.Scene.Meshes = []scene.Mesh{{ID: 1, Revision: 1}}
	assert.ErrorIs(t, b.releaseBatch(&retained.Batch{Releases: []retained.Version{mesh}}), retained.ErrInput)
	r.frame.Scene = &scene.Scene{}
	assert.ErrorIs(t, b.releaseBatch(&retained.Batch{Releases: []retained.Version{mesh, mesh}}), retained.ErrInput)
	assert.ErrorIs(t, b.releaseBatch(&retained.Batch{Releases: []retained.Version{mesh, {ID: 2}}}), retained.ErrInput)
	assert.ErrorIs(t, b.releaseBatch(&retained.Batch{Uploads: []retained.Resource{{}}}), retained.ErrInput)
	assert.Len(t, r.meshes, 1, "preflight failure must not partially release")
	assert.ErrorIs(t, b.uploadBatch(&retained.Batch{Uploads: []retained.Resource{{Version: retained.Version{Kind: 99}}}}), retained.ErrInput)
	b.afterFrame() // no batch executed
	assert.Empty(t, results)
	b.executed = true
	b.afterFrame() // absent context invalidates, without a successful/failed ack
	require.Len(t, results, 1)
	assert.True(t, results[0].Reset)
	b.afterFrame()
	b.prepare()
	b.invalidate(errors.New("again"))
	assert.Len(t, results, 1)
	assert.Error(t, b.Sync(scene.Frame{}, nil))
}
