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

// testResidentSymbols renders a viewport-aligned and a map-aligned quad baked at
// a size and as unit quads scaled per draw, under a rotated, scaled transform.
// The pictures must agree, and a size change must reuse the resident mesh.
func testResidentSymbols(t *testing.T) {
	t.Helper()
	const size = 2.5
	anchors := []geometry.Point{{X: 22, Y: 18}, {X: 46, Y: 34}}
	quads := func(id uint64, factor float64, scale float32) *scene.Scene {
		s := &scene.Scene{Meshes: []scene.Mesh{{ID: id, Revision: 1}}, Textures: []scene.Texture{{ID: 1, Revision: 1, Width: 1, Height: 1, RGBA: []byte{255, 0, 0, 255}}}}
		sin, cos := math.Sincos(0.4)
		for i, anchor := range anchors {
			quad := geometry.TextQuad(-6*factor, -4*factor, 6*factor, 4*factor, 0, 0, 1, 1)
			for _, index := range []int{0, 1, 2, 0, 2, 3} {
				s.Meshes[0].Vertices = append(s.Meshes[0].Vertices, geometry.TransformTextVertex(anchor, quad[index], geometry.Point{X: 3 * factor, Y: -2 * factor}, sin, cos))
			}
			material := scene.Material{Kind: scene.Image, Texture: 1, Color: [4]float32{1, 1, 1, 1}, MapAligned: i == 1, OffsetScale: scale}
			s.Draws = append(s.Draws, scene.Draw{Mesh: id, First: uint32(i * 6), Count: 6, Material: material})
		}
		require.NoError(t, s.Validate())
		return s
	}
	baked, unit, larger := quads(1, size, 0), quads(2, 1, size), quads(2, 1, 2*size)
	assert.Equal(t, unit.Meshes, larger.Meshes)
	sin, cos := math.Sincos(12 * math.Pi / 180)
	transform := scene.Affine{M11: float32(2 * cos), M12: float32(-2 * sin), DX: 20, M21: float32(2 * sin), M22: float32(2 * cos), DY: 4}
	grab, stats, done := sceneGrabber(t, baked, transform)
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
	reference := grab(baked)
	defer reference.Delete()
	uploads := stats.MeshUploads
	scaled := grab(unit)
	defer scaled.Delete()
	assert.Equal(t, uploads+1, stats.MeshUploads)
	covered, different := compare(reference, scaled)
	require.Greater(t, covered, 1000, "the reference must draw both quads")
	assert.LessOrEqual(t, different, covered/200, "scaled and baked quads must agree within edge rounding")

	// Doubling the size is a draw-only change covering four times the area.
	doubled := grab(larger)
	defer doubled.Delete()
	assert.Equal(t, uploads+1, stats.MeshUploads, "a size change must reuse the resident mesh")
	coveredLarger, _ := compare(doubled, scaled)
	assert.Greater(t, coveredLarger, covered*3)
}
