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

// testPackedSymbols renders two textured, rotated quads, one map-aligned, from
// full vertices and from packed symbol vertices drawn through short indices
// with a vertex base. Rounded anchors, offsets and texture coordinates may flip
// a few edge pixels at most.
func testPackedSymbols(t *testing.T) {
	t.Helper()
	// Four colored quadrants, so texture coordinates matter.
	texture := scene.Texture{ID: 1, Revision: 1, Width: 4, Height: 4, RGBA: make([]byte, 4*4*4)}
	for y := range 4 {
		for x := range 4 {
			pixel := texture.RGBA[(y*4+x)*4:]
			pixel[0], pixel[1], pixel[2], pixel[3] = byte(255*(x/2)), byte(255*(y/2)), byte(255*(1-x/2)), 255
		}
	}
	full := &scene.Scene{Meshes: []scene.Mesh{{ID: 1, Revision: 1}}, Textures: []scene.Texture{texture}}
	packed := &scene.Scene{Meshes: []scene.Mesh{{ID: 2, Revision: 1}}, Textures: []scene.Texture{texture}}
	sin, cos := math.Sincos(0.4)
	for i, anchor := range []geometry.Point{{X: 22.3, Y: 18.7}, {X: 46.1, Y: 34.9}} {
		quad := geometry.TextQuad(-6.3, -4.1, 6.2, 4.4, 0, 0, 1, 1)
		material := scene.Material{Kind: scene.Image, Texture: 1, Color: [4]float32{1, 1, 1, 1}, MapAligned: i == 1, OffsetScale: 2.5}
		for _, index := range []int{0, 1, 2, 0, 2, 3} {
			full.Meshes[0].Vertices = append(full.Meshes[0].Vertices, geometry.TransformTextVertex(anchor, quad[index], geometry.Point{X: 3.3, Y: -2.2}, sin, cos))
		}
		full.Draws = append(full.Draws, scene.Draw{Mesh: 1, First: uint32(i * 6), Count: 6, Material: material})
		for _, corner := range quad {
			v, ok := scene.PackSymbol(geometry.TransformTextVertex(anchor, corner, geometry.Point{X: 3.3, Y: -2.2}, sin, cos))
			require.True(t, ok)
			packed.Meshes[0].PackedSymbols = append(packed.Meshes[0].PackedSymbols, v)
		}
		packed.Meshes[0].ShortIndices = append(packed.Meshes[0].ShortIndices, 0, 1, 2, 0, 2, 3)
		packed.Draws = append(packed.Draws, scene.Draw{Mesh: 2, First: uint32(i * 6), Count: 6, Material: material, Layout: scene.PackedSymbolLayout, Base: uint32(i * 4)})
	}
	require.NoError(t, full.Validate())
	require.NoError(t, packed.Validate())
	require.Less(t, packed.Meshes[0].BufferBytes(), full.Meshes[0].BufferBytes())

	sin, cos = math.Sincos(12 * math.Pi / 180)
	transform := scene.Affine{M11: float32(2 * cos), M12: float32(-2 * sin), DX: 20, M21: float32(2 * sin), M22: float32(2 * cos), DY: 4}
	grab, _, done := sceneGrabber(t, full, transform)
	defer done()
	reference := grab(full)
	defer reference.Delete()
	got := grab(packed)
	defer got.Delete()
	drawn, different, largest := 0, 0, 0
	colors := map[[3]int]bool{}
	for y := range reference.Height() {
		for x := range reference.Width() {
			a, b := reference.PixelColor(x, y), got.PixelColor(x, y)
			if a.Red() > 127 || a.Green() > 127 || a.Blue() > 127 {
				drawn++
				colors[[3]int{a.Red() / 128, a.Green() / 128, a.Blue() / 128}] = true
			}
			d := max(abs(a.Red()-b.Red()), abs(a.Green()-b.Green()), abs(a.Blue()-b.Blue()))
			if d > 2 {
				different++
			}
			largest = max(largest, d)
		}
	}
	t.Logf("packed symbols: %d of %d drawn pixels differ, by at most %d", different, drawn, largest)
	require.Greater(t, drawn, 1000, "both quads are drawn")
	for _, quadrant := range [][3]int{{0, 0, 1}, {1, 0, 0}, {0, 1, 1}, {1, 1, 0}} {
		assert.True(t, colors[quadrant], "quadrant %v of the texture shows", quadrant)
	}
	// Edges are not antialiased, so a pixel whose centre lies within the
	// rounding of an edge flips. Offsets round to 1/64 pixel, magnified here by
	// the offset scale: a few of the quads' 200 edge pixels.
	assert.LessOrEqual(t, different, drawn/100, "rounding moves at most a few edge pixels")
}

func abs(v int) int { return max(v, -v) }
