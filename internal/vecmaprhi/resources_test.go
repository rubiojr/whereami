//go:build vecmap_rhi && integration

package vecmaprhi

import (
	"errors"
	"testing"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/qml"
	rhi "github.com/rubiojr/whereami/internal/qtrhi"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stagingScene(t *testing.T, revision uint64, indexed bool) *scene.Scene {
	t.Helper()
	x, y := float32(0), float32(0)
	color := []byte{0, 255, 0, 255}
	if revision > 1 {
		x, y = 10, 10
		color = []byte{0, 0, 255, 255}
	}
	s := &scene.Scene{
		Meshes:   []scene.Mesh{{ID: 1, Revision: revision, Vertices: []scene.Vertex{{X: x, Y: y}, {X: 100, Y: y, U: 1}, {X: x, Y: 80, V: 1}, {X: 100, Y: y, U: 1}, {X: 100, Y: 80, U: 1, V: 1}, {X: x, Y: 80, V: 1}}}},
		Textures: []scene.Texture{{ID: 1, Revision: revision, Width: 1, Height: 1, RGBA: color}},
		Draws:    []scene.Draw{{Mesh: 1, Count: 6, Material: scene.Material{Kind: scene.Image, Texture: 1, Color: [4]float32{1, 1, 1, 1}}}},
	}
	if indexed {
		mesh, err := scene.IndexMesh(s.Meshes[0])
		require.NoError(t, err)
		s.Meshes[0] = mesh
	}
	require.NoError(t, s.Validate())
	return s
}

// Called by TestRenderer on the QApplication's locked OS thread.
func testResourceStaging(t *testing.T, indexed bool) {
	t.Helper()
	first, replacement := stagingScene(t, 1, indexed), stagingScene(t, 2, indexed)
	frame := scene.Frame{Scene: first, Transforms: []scene.Affine{{M11: 1, M22: 1}}, DevicePixelRatio: 1}
	item := rhi.NewQQuickItem()
	defer item.Delete()
	item.SetFlag(rhi.QQuickItem__ItemHasContents)
	var renderer *Renderer
	var stats Stats
	var hook func() error
	var hookError error
	item.OnUpdatePaintNode(func(_ func(*rhi.QSGNode, *rhi.QQuickItem__UpdatePaintNodeData) *rhi.QSGNode, old *rhi.QSGNode, _ *rhi.QQuickItem__UpdatePaintNodeData) *rhi.QSGNode {
		if old == nil {
			renderer = newRenderer(item, func(s Stats) { stats = s })
			renderer.Node.OnPrepare(func(func()) {
				if hook != nil {
					fn := hook
					hook = nil
					hookError = fn()
				}
				renderer.prepare()
			})
		}
		renderer.Sync(frame)
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
	for stats.Frames == 0 && time.Now().Before(deadline) {
		qt.QCoreApplication_ProcessEvents()
		time.Sleep(time.Millisecond)
	}
	require.Positive(t, stats.Frames)
	require.Empty(t, stats.Error)
	t.Logf("staging indexed=%t backend=%s device=%s", indexed, stats.Backend, stats.Device)
	window := rhi.UnsafeNewQQuickItem(item.UnsafePointer()).Window()
	grab := func() *qt.QImage {
		item.Update()
		qt.QCoreApplication_ProcessEvents()
		image := window.GrabWindow()
		require.NoError(t, hookError)
		require.Empty(t, stats.Error)
		return image
	}
	// Grow a live uniform buffer and rebuild its bindings without reuploading.
	grown := *first
	grown.Draws = append([]scene.Draw{first.Draws[0]}, first.Draws[0])
	frame.Scene = &grown
	initialUploads := stats.MeshUploads
	grownImage := grab()
	checkPixel(t, grownImage, 30, 30, 0, 255, 0)
	grownImage.Delete()
	assert.Equal(t, initialUploads, stats.MeshUploads)

	// Inject failure after real mesh and texture creation, before any recording.
	failure := errors.New("injected final texture allocation failure")
	failedScene := *replacement
	failedScene.Textures = append([]scene.Texture{replacement.Textures[0]}, scene.Texture{ID: 2, Revision: 1, Width: 1, Height: 1, RGBA: []byte{255, 0, 0, 255}})
	var allocationError error
	var meshFailure, whiteFailure error
	meshAllocations, textureAllocations := 0, 0
	before := stats
	hook = func() error {
		_, meshFailure = renderer.allocateResources(replacement, func(scene.Mesh) (gpuMesh, error) {
			return gpuMesh{}, failure
		}, renderer.createTexture)
		probe := &Renderer{context: renderer.context, meshes: make(map[resourceKey]gpuMesh), textures: make(map[resourceKey]gpuTexture)}
		_, whiteFailure = probe.allocateResources(first, probe.createMesh, func(scene.Texture) (gpuTexture, error) {
			return gpuTexture{}, failure
		})
		_, allocationError = renderer.allocateResources(&failedScene, func(m scene.Mesh) (gpuMesh, error) {
			meshAllocations++
			return renderer.createMesh(m)
		}, func(s scene.Texture) (gpuTexture, error) {
			if s.ID == 2 {
				return gpuTexture{}, failure
			}
			textureAllocations++
			return renderer.createTexture(s)
		})
		return nil
	}
	image := grab()
	checkPixel(t, image, 30, 30, 0, 255, 0)
	image.Delete()
	assert.ErrorIs(t, allocationError, failure)
	assert.ErrorIs(t, meshFailure, failure)
	assert.ErrorIs(t, whiteFailure, failure)
	assert.Equal(t, 1, meshAllocations)
	assert.Equal(t, 1, textureAllocations)
	assert.Len(t, renderer.meshes, 1)
	assert.Len(t, renderer.textures, 2)
	assert.Equal(t, before.MeshUploads, stats.MeshUploads)
	assert.Equal(t, before.TextureUploads, stats.TextureUploads)

	// Submit replacement resources, but keep selecting/rendering revision one.
	hook = func() error {
		stage, err := renderer.allocateResources(replacement, renderer.createMesh, renderer.createTexture)
		if err != nil {
			return err
		}
		updates := renderer.context.NextResourceUpdateBatch()
		renderer.recordResources(stage, updates)
		stage.discard() // consumed stage must no longer own cached resources
		renderer.Node.CommandBuffer().ResourceUpdate(updates)
		return nil
	}
	image = grab()
	checkPixel(t, image, 5, 5, 0, 255, 0)
	checkPixel(t, image, 30, 30, 0, 255, 0)
	image.Delete()
	assert.Len(t, renderer.meshes, 2)
	assert.Len(t, renderer.textures, 3)
	assert.NotEqual(t, renderer.meshes[resourceKey{1, 1}].buffer.UnsafePointer(), renderer.meshes[resourceKey{1, 2}].buffer.UnsafePointer())
	assert.Equal(t, resourceKey{1, 1}, renderer.drawKeys[0].texture)
	before = stats
	frame.Scene = replacement
	image = grab()
	checkPixel(t, image, 5, 5, 0, 0, 0)
	checkPixel(t, image, 30, 30, 0, 0, 255)
	image.Delete()
	assert.Equal(t, before.MeshUploads, stats.MeshUploads, "activating staged geometry must not reupload")
	assert.Equal(t, before.TextureUploads, stats.TextureUploads, "activating staged texture must not reupload")
	assert.Len(t, renderer.meshes, 1)
	assert.Len(t, renderer.textures, 2)
	assert.Equal(t, resourceKey{1, 2}, renderer.drawKeys[0].texture)

	// Retire throwaway uploads while their commands belong to the current frame.
	// Wrappers must survive via DeleteLater until Qt submits endFrame.
	throwaway := stagingScene(t, 3, indexed)
	hook = func() error {
		stage, err := renderer.allocateResources(throwaway, renderer.createMesh, renderer.createTexture)
		if err != nil {
			return err
		}
		updates := renderer.context.NextResourceUpdateBatch()
		renderer.recordResources(stage, updates)
		renderer.selectResources(replacement)
		renderer.Node.CommandBuffer().ResourceUpdate(updates)
		return nil
	}
	image = grab()
	checkPixel(t, image, 30, 30, 0, 0, 255)
	image.Delete()
	assert.Len(t, renderer.meshes, 1)
	assert.Len(t, renderer.textures, 2)
	engine.Delete()
	assert.Zero(t, stats.LiveMeshes)
	assert.Zero(t, stats.LiveTextures)
}
