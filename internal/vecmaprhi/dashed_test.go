//go:build vecmap_rhi && integration

package vecmaprhi

import (
	"math"
	"testing"

	qt "github.com/mappu/miqt/qt6"
	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testDashedLines renders one path with baked dashes and with dashes cut by the
// fragment shader under a rotated, scaled transform. The pictures must agree,
// and a change of width or pattern must reuse the resident mesh.
func testDashedLines(t *testing.T, indexed bool) {
	t.Helper()
	const scale, width = 2.0, 3.0 // six logical pixels on screen
	dashes := []float64{2, 1, 0.5, 1}
	paths := [][]geometry.Point{{{X: 8, Y: 10}, {X: 44, Y: 12}, {X: 50, Y: 40}}, {{X: 6, Y: 44}, {X: 40, Y: 28}}}
	baked, err := geometry.TessellateLines(paths, geometry.LineStyle{Width: width, Dashes: dashes}, 1024, indexed)
	require.NoError(t, err)
	mesh, err := geometry.TessellateDashedLines(paths, 1024, indexed)
	require.NoError(t, err)
	pattern, ok := geometry.NewDashPattern(dashes, width)
	require.True(t, ok)
	elements := func(vertices, indices int) uint32 {
		if indexed {
			return uint32(indices)
		}
		return uint32(vertices)
	}
	red := [4]float32{1, 0, 0, 1}
	bakedScene := &scene.Scene{Meshes: []scene.Mesh{{ID: 1, Revision: 1, Indices: baked.Indices}}, Draws: []scene.Draw{{Mesh: 1, Count: elements(len(baked.Vertices), len(baked.Indices)), Material: scene.Material{Color: red}}}}
	for _, point := range baked.Vertices {
		bakedScene.Meshes[0].Vertices = append(bakedScene.Meshes[0].Vertices, scene.Vertex{X: float32(point.X), Y: float32(point.Y)})
	}
	dashed := func(width float64, pattern geometry.DashPattern) *scene.Scene {
		material := scene.Material{Kind: scene.Dashed, Color: red, MapAligned: true, OffsetScale: float32(width / 2 * scale), DashUnit: float32(width)}
		for i, dash := range pattern {
			material.Dashes[i] = float32(dash)
		}
		s := &scene.Scene{Meshes: []scene.Mesh{{ID: 2, Revision: 1, Indices: mesh.Indices}}, Draws: []scene.Draw{{Mesh: 2, Count: elements(len(mesh.Vertices), len(mesh.Indices)), Material: material}}}
		for _, vertex := range mesh.Vertices {
			s.Meshes[0].Vertices = append(s.Meshes[0].Vertices, scene.Vertex{X: float32(vertex.Anchor.X), Y: float32(vertex.Anchor.Y),
				OffsetX: float32(vertex.Direction.X), OffsetY: float32(vertex.Direction.Y), U: float32(vertex.Distance)})
		}
		require.NoError(t, s.Validate())
		return s
	}
	shaderScene, solid := dashed(width, pattern), dashed(width, geometry.DashPattern{1})
	require.NoError(t, bakedScene.Validate())
	sin, cos := math.Sincos(12 * math.Pi / 180)
	transform := scene.Affine{M11: float32(scale * cos), M12: float32(-scale * sin), DX: 20, M21: float32(scale * sin), M22: float32(scale * cos), DY: 4}
	grab, stats, done := sceneGrabber(t, bakedScene, transform)
	defer done()
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
	at := func(distance float64) (int, int) {
		x, y := 8+distance*36/math.Hypot(36, 2), 10+distance*2/math.Hypot(36, 2)
		return int(float64(transform.M11)*x + float64(transform.M12)*y + float64(transform.DX)), int(float64(transform.M21)*x + float64(transform.M22)*y + float64(transform.DY))
	}
	reference := grab(bakedScene)
	defer reference.Delete()
	// The first dash covers distances up to six tile units and its gap up to nine.
	dashX, dashY := at(3)
	gapX, gapY := at(7.5)
	checkPixel(t, reference, dashX, dashY, 255, 0, 0)
	checkPixel(t, reference, gapX, gapY, 0, 0, 0)
	uploads := stats.MeshUploads
	shader := grab(shaderScene)
	defer shader.Delete()
	assert.Equal(t, uploads+1, stats.MeshUploads)
	covered, different := compare(reference, shader)
	require.Greater(t, covered, 500, "the reference must draw the dashes")
	// Dash ends are cut per fragment from an interpolated float32 distance, and
	// long edges are extruded in the vertex shader; both round at sample points.
	assert.LessOrEqual(t, different, covered/200, "shader and baked dashes must agree within edge rounding")
	checkPixel(t, shader, dashX, dashY, 255, 0, 0)
	checkPixel(t, shader, gapX, gapY, 0, 0, 0)

	// A pattern change is a draw-only change: the gap fills without an upload.
	filled := grab(solid)
	defer filled.Delete()
	assert.Equal(t, uploads+1, stats.MeshUploads, "a pattern change must reuse the resident mesh")
	checkPixel(t, filled, gapX, gapY, 255, 0, 0)
	coveredSolid, _ := compare(filled, shader)
	assert.Greater(t, coveredSolid, covered*5/4)
}
