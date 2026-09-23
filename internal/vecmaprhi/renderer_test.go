//go:build vecmap_rhi && integration

package vecmaprhi

import (
	"runtime"
	"testing"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/qml"
	rhi "github.com/rubiojr/whereami/internal/qtrhi"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderer(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	t.Setenv("QSG_RENDER_LOOP", "basic")
	app := qt.NewQApplication([]string{"rhi-test"})
	defer app.Delete()
	for range 3 {
		testRendererLifetime(t, false)
		testRendererLifetime(t, true)
	}
	testResourceStaging(t, false)
	testResourceStaging(t, true)
	testBatchExecution(t, false)
	testBatchExecution(t, true)
}

func testRendererLifetime(t *testing.T, indexed bool) {
	t.Helper()
	vertices := []scene.Vertex{{X: 0, Y: 0}, {X: 100, Y: 0}, {X: 0, Y: 80}, {X: 100, Y: 0}, {X: 100, Y: 80}, {X: 0, Y: 80}}
	s := &scene.Scene{Meshes: []scene.Mesh{{ID: 1, Revision: 1, Vertices: vertices}}, Draws: []scene.Draw{{Mesh: 1, Count: 6, Material: scene.Material{Color: [4]float32{1, 0, 0, 1}}}}}
	for i, kind := range []scene.Kind{scene.Image, scene.Pattern, scene.SDFFill} {
		id := uint64(i + 1)
		pixels := [][4]byte{{0, 255, 0, 255}, {0, 0, 255, 255}, {255, 255, 255, 255}}[i]
		s.Textures = append(s.Textures, scene.Texture{ID: id, Revision: 1, Width: 1, Height: 1, RGBA: pixels[:]})
		first := len(s.Meshes[0].Vertices)
		x := float32(10 + i*20)
		s.Meshes[0].Vertices = append(s.Meshes[0].Vertices, []scene.Vertex{{X: x, Y: 10}, {X: x + 15, Y: 10, U: 1}, {X: x, Y: 25, V: 1}, {X: x + 15, Y: 10, U: 1}, {X: x + 15, Y: 25, U: 1, V: 1}, {X: x, Y: 25, V: 1}}...)
		color := [4]float32{1, 1, 1, 1}
		if kind == scene.SDFFill {
			color = [4]float32{1, 1, 0, 1}
		}
		s.Draws = append(s.Draws, scene.Draw{Mesh: 1, First: uint32(first), Count: 6, Material: scene.Material{Kind: kind, Texture: id, Color: color, PatternSize: [2]float32{2, 2}, FontScale: 1}})
	}
	expandedVertices := s.Meshes[0].Vertices
	if indexed {
		mesh, err := scene.IndexMesh(s.Meshes[0])
		require.NoError(t, err)
		require.NotEmpty(t, mesh.Indices)
		s.Meshes[0] = mesh
	}
	require.NoError(t, s.Validate())
	frame := scene.Frame{Scene: s, Transforms: []scene.Affine{{M11: 1, M22: 1}}, DevicePixelRatio: 1}
	item := rhi.NewQQuickItem()
	defer item.Delete()
	item.SetFlag(rhi.QQuickItem__ItemHasContents)
	var renderer *Renderer
	var stats Stats
	invalidate := false
	item.OnUpdatePaintNode(func(_ func(*rhi.QSGNode, *rhi.QQuickItem__UpdatePaintNodeData) *rhi.QSGNode, old *rhi.QSGNode, _ *rhi.QQuickItem__UpdatePaintNodeData) *rhi.QSGNode {
		if old == nil {
			renderer = New(item, func(s Stats) { stats = s })
		}
		if invalidate {
			renderer.Node.ReleaseResources()
			invalidate = false
		}
		renderer.Sync(frame)
		return renderer.Node.QSGNode
	})
	engine := qml.NewQQmlApplicationEngine()
	engine.RootContext().SetContextProperty("testItem", item.QObject)
	engine.LoadData([]byte(`import QtQuick
import QtQuick.Window
Window { id:rootWindow; visible:true; width:160; height:120; color:"black"
 property real clipAngle:0
 property real mapOpacity:1
 Item { id:host; x:20; y:20; width:80; height:60; clip:true; rotation:rootWindow.clipAngle; opacity:rootWindow.mapOpacity
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
	require.Empty(t, stats.Error)
	require.Positive(t, stats.Frames)
	window := rhi.UnsafeNewQQuickItem(item.UnsafePointer()).Window()
	image := window.GrabWindow()
	require.False(t, image.IsNull())
	checkPixel(t, image, 25, 25, 255, 0, 0)
	checkPixel(t, image, 35, 35, 0, 255, 0)
	checkPixel(t, image, 55, 35, 0, 0, 255)
	checkPixel(t, image, 75, 35, 255, 255, 0)
	checkPixel(t, image, 105, 30, 0, 0, 0) // parent's scissor excludes our wider mesh
	// Compare the complete framebuffer across representations, not just a few
	// sampled pixels. This also exercises switching cached buffer layouts.
	comparison := *s
	comparison.Meshes = []scene.Mesh{{ID: 1, Revision: 2, Vertices: expandedVertices}}
	if !indexed {
		mesh, err := scene.IndexMesh(comparison.Meshes[0])
		require.NoError(t, err)
		comparison.Meshes[0] = mesh
	}
	require.NoError(t, comparison.Validate())
	frame.Scene = &comparison
	item.Update()
	qt.QCoreApplication_ProcessEvents()
	other := window.GrabWindow()
	assert.True(t, image.OperatorEqual(other), "indexed and expanded output differ")
	other.Delete()
	image.Delete()
	angle := qt.NewQVariant9(15)
	engine.RootObjects()[0].SetProperty("clipAngle", angle)
	angle.Delete()
	qt.QCoreApplication_ProcessEvents()
	image = window.GrabWindow()
	checkPixel(t, image, 60, 55, 255, 0, 0)
	checkPixel(t, image, 105, 30, 0, 0, 0)
	image.Delete()
	angle = qt.NewQVariant9(0)
	engine.RootObjects()[0].SetProperty("clipAngle", angle)
	angle.Delete()
	opacity := qt.NewQVariant9(0.5)
	engine.RootObjects()[0].SetProperty("mapOpacity", opacity)
	opacity.Delete()
	qt.QCoreApplication_ProcessEvents()
	image = window.GrabWindow()
	checkPixel(t, image, 30, 60, 128, 0, 0)
	image.Delete()
	opacity = qt.NewQVariant9(1)
	engine.RootObjects()[0].SetProperty("mapOpacity", opacity)
	opacity.Delete()
	uploads := stats.MeshUploads
	for i := range 30 {
		frame.Transforms = []scene.Affine{{M11: 1, M22: 1, DX: float32(i) / 10}}
		item.Update()
		qt.QCoreApplication_ProcessEvents()
		image = window.GrabWindow()
		image.Delete()
	}
	assert.Equal(t, uploads, stats.MeshUploads, "camera-only frames uploaded geometry")
	assert.Equal(t, uint64(4), stats.TextureUploads, "textures should remain resident")
	invalidate = true
	item.Update()
	qt.QCoreApplication_ProcessEvents()
	image = window.GrabWindow()
	checkPixel(t, image, 35, 35, 0, 255, 0)
	image.Delete()
	assert.Equal(t, uploads+1, stats.MeshUploads, "released resources must be reconstructed")
	assert.Equal(t, uint64(8), stats.TextureUploads)
	uploads = stats.MeshUploads
	replacement := &scene.Scene{Meshes: []scene.Mesh{{ID: 1, Revision: 3, Vertices: vertices}}, Draws: []scene.Draw{{Mesh: 1, Count: 6, Material: scene.Material{Color: [4]float32{0, 0, 1, 1}}}}}
	if indexed {
		mesh, err := scene.IndexMesh(replacement.Meshes[0])
		require.NoError(t, err)
		replacement.Meshes[0] = mesh
	}
	require.NoError(t, replacement.Validate())
	frame.Scene = replacement
	item.Update()
	qt.QCoreApplication_ProcessEvents()
	image = window.GrabWindow()
	checkPixel(t, image, 30, 30, 0, 0, 255)
	image.Delete()
	assert.Equal(t, uploads+1, stats.MeshUploads)
	assert.Equal(t, 1, stats.LiveTextures, "unused textures should be evicted")
	width := qt.NewQVariant4(200)
	engine.RootObjects()[0].SetProperty("width", width)
	width.Delete()
	deadline = time.Now().Add(2 * time.Second)
	for {
		qt.QCoreApplication_ProcessEvents()
		image = window.GrabWindow()
		resized := image.Width() == int(200*window.EffectiveDevicePixelRatio())
		image.Delete()
		if resized {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("window did not resize")
		}
		time.Sleep(time.Millisecond)
	}
	runtime.GC()
	runtime.GC()
	qt.QCoreApplication_ProcessEvents()
	engine.Delete()
	assert.Zero(t, stats.LiveMeshes)
	assert.Zero(t, stats.LiveTextures)
}

func checkPixel(t *testing.T, image *qt.QImage, x, y, red, green, blue int) {
	t.Helper()
	scale := float64(image.Width()) / 160
	x, y = int(float64(x)*scale), int(float64(y)*scale)
	color := image.PixelColor(x, y)
	defer runtime.KeepAlive(color)
	assert.InDelta(t, red, color.Red(), 2)
	assert.InDelta(t, green, color.Green(), 2)
	assert.InDelta(t, blue, color.Blue(), 2)
}
