//go:build vecmap_rhi && integration

package vecmaprhi

import (
	"math"
	"testing"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/qml"
	rhi "github.com/rubiojr/whereami/internal/qtrhi"
	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testExtrudedLines renders one path baked at a width and extruded with the
// equivalent pixel half width under a rotated, scaled transform. The pictures
// must agree, and a width change must reuse the resident extruded mesh.
func testExtrudedLines(t *testing.T, indexed bool) {
	t.Helper()
	const scale, width = 2.0, 5.0 // ten logical pixels on screen
	paths := [][]geometry.Point{{{X: 12, Y: 12}, {X: 40, Y: 14}, {X: 44, Y: 36}}, {{X: 10, Y: 40}, {X: 30, Y: 30}}}
	style := geometry.LineStyle{Width: width, Cap: "round", Join: "round"}
	baked, err := geometry.TessellateLines(paths, style, 1024, indexed)
	require.NoError(t, err)
	extruded, err := geometry.TessellateExtrudedLines(paths, geometry.ExtrudedLineStyle{Cap: style.Cap, Join: style.Join}, 1024, indexed)
	require.NoError(t, err)
	count := len(baked.Vertices)
	if indexed {
		count = len(baked.Indices)
	}
	red := [4]float32{1, 0, 0, 1}
	bakedScene := &scene.Scene{Meshes: []scene.Mesh{{ID: 1, Revision: 1, Indices: baked.Indices}}, Draws: []scene.Draw{{Mesh: 1, Count: uint32(count), Material: scene.Material{Color: red}}}}
	for _, point := range baked.Vertices {
		bakedScene.Meshes[0].Vertices = append(bakedScene.Meshes[0].Vertices, scene.Vertex{X: float32(point.X), Y: float32(point.Y)})
	}
	material := scene.Material{Color: red, MapAligned: true, OffsetScale: float32(width / 2 * scale)}
	extrudedScene := &scene.Scene{Meshes: []scene.Mesh{{ID: 2, Revision: 1, Indices: extruded.Indices}}, Draws: []scene.Draw{{Mesh: 2, Count: uint32(count), Material: material}}}
	for _, vertex := range extruded.Vertices {
		extrudedScene.Meshes[0].Vertices = append(extrudedScene.Meshes[0].Vertices, scene.Vertex{X: float32(vertex.Anchor.X), Y: float32(vertex.Anchor.Y), OffsetX: float32(vertex.Direction.X), OffsetY: float32(vertex.Direction.Y)})
	}
	wide := *extrudedScene
	wide.Draws = []scene.Draw{{Mesh: 2, Count: uint32(count), Material: scene.Material{Color: red, MapAligned: true, OffsetScale: float32(width * scale)}}}
	for _, s := range []*scene.Scene{bakedScene, extrudedScene, &wide} {
		require.NoError(t, s.Validate())
	}
	sin, cos := math.Sincos(12 * math.Pi / 180)
	transform := scene.Affine{M11: float32(scale * cos), M12: float32(-scale * sin), DX: 20, M21: float32(scale * sin), M22: float32(scale * cos), DY: 4}
	frame := scene.Frame{Scene: bakedScene, Transforms: []scene.Affine{transform}, DevicePixelRatio: 1}

	item := rhi.NewQQuickItem()
	defer item.Delete()
	item.SetFlag(rhi.QQuickItem__ItemHasContents)
	var renderer *Renderer
	var stats Stats
	item.OnUpdatePaintNode(func(_ func(*rhi.QSGNode, *rhi.QQuickItem__UpdatePaintNodeData) *rhi.QSGNode, old *rhi.QSGNode, _ *rhi.QQuickItem__UpdatePaintNodeData) *rhi.QSGNode {
		if old == nil {
			renderer = New(item, func(s Stats) { stats = s })
		}
		renderer.Sync(frame)
		return renderer.Node.QSGNode
	})
	engine := qml.NewQQmlApplicationEngine()
	defer engine.Delete()
	engine.RootContext().SetContextProperty("testItem", item.QObject)
	engine.LoadData([]byte(`import QtQuick
import QtQuick.Window
Window { visible:true; width:160; height:120; color:"black"
 Item { id:host; width:160; height:120
  Binding {target:testItem;property:"parent";value:host}
  Binding {target:testItem;property:"width";value:160}
  Binding {target:testItem;property:"height";value:120}
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
	grab := func(s *scene.Scene) *qt.QImage {
		frame.Scene = s
		item.Update()
		qt.QCoreApplication_ProcessEvents()
		image := window.GrabWindow()
		require.False(t, image.IsNull())
		require.Empty(t, stats.Error)
		return image
	}
	// covered counts red pixels and those differing from other by more than
	// rounding. Edges may differ where float32 shader extrusion and float64 CPU
	// extrusion round a vertex across a sample point.
	compare := func(image, other *qt.QImage) (covered, different int) {
		for y := range image.Height() {
			for x := range image.Width() {
				a, b := image.PixelColor(x, y), other.PixelColor(x, y)
				if a.Red() > 127 {
					covered++
				}
				if d := a.Red() - b.Red(); d > 2 || d < -2 {
					different++
				}
			}
		}
		return covered, different
	}
	reference := grab(bakedScene)
	defer reference.Delete()
	// On-screen centerline point of the first segment's start.
	at := func(x, y float64) (int, int) {
		return int(float64(transform.M11)*x + float64(transform.M12)*y + float64(transform.DX)), int(float64(transform.M21)*x + float64(transform.M22)*y + float64(transform.DY))
	}
	x, y := at(26, 13)
	checkPixel(t, reference, x, y, 255, 0, 0)
	uploads := stats.MeshUploads
	shader := grab(extrudedScene)
	defer shader.Delete()
	assert.Equal(t, uploads+1, stats.MeshUploads)
	covered, different := compare(reference, shader)
	require.Greater(t, covered, 500, "the reference must draw the line")
	assert.LessOrEqual(t, different, covered/200, "extruded and baked lines must agree within edge rounding")
	checkPixel(t, shader, x, y, 255, 0, 0)

	// Doubling the width is a draw-only change: no geometry is uploaded, and
	// pixels between the old and new half widths become covered.
	// 7.5 pixels beside the first segment lies between the 5 and 10 pixel half widths.
	normalX, normalY := 7.5*-sin, 7.5*cos
	checkPixel(t, shader, x+int(normalX), y+int(normalY), 0, 0, 0)
	widened := grab(&wide)
	defer widened.Delete()
	assert.Equal(t, uploads+1, stats.MeshUploads, "a width change must reuse the resident mesh")
	checkPixel(t, widened, x+int(normalX), y+int(normalY), 255, 0, 0)
	coveredWide, _ := compare(widened, shader)
	assert.Greater(t, coveredWide, covered*3/2)
}
