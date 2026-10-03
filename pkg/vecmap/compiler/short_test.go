package compiler

import (
	"fmt"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/mvt"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShortIndicesDrawTheSameGeometry(t *testing.T) {
	for _, split := range []bool{false, true} {
		for _, resident := range []bool{false, true} {
			for _, packed := range []bool{false, true} {
				name := fmt.Sprintf("split=%t resident=%t packed=%t", split, resident, packed)
				long, longSources := packShort(t, true, split, resident, true, packed, true, false)
				for _, reserve := range []bool{false, true} {
					short, sources := packShort(t, true, split, resident, true, packed, reserve, true)
					assert.Equal(t, expand(t, long), expand(t, short), name)
					assert.Equal(t, longSources, sources)
					require.Equal(t, len(long.Meshes), len(short.Meshes))
					for i, mesh := range short.Meshes {
						assert.Empty(t, mesh.Indices, name)
						assert.Equal(t, len(mesh.ShortIndices), cap(mesh.ShortIndices), "published at their exact length")
						assert.Equal(t, long.Meshes[i].IndexBytes(), 2*mesh.IndexBytes(), name)
						assert.Equal(t, long.Meshes[i].VertexBytes(), mesh.VertexBytes(), "vertices are unchanged")
					}
				}
			}
		}
	}
	expanded := NewSceneBuilder(false, 0)
	expanded.ShortIndices()
	_, err := expanded.Finish()
	assert.ErrorIs(t, err, ErrPackingInput, "short indices need an indexed builder")
}

// strip is a triangle strip of n vertices along x, each triangle sharing two
// vertices with the one before.
func strip(n int, y float64) geometry.Mesh {
	mesh := geometry.Mesh{Vertices: make([]geometry.Point, n)}
	for i := range mesh.Vertices {
		mesh.Vertices[i] = geometry.Point{X: float64(i/2) / 1024, Y: y + float64(i%2)}
	}
	for i := 0; i+2 < n; i++ {
		mesh.Indices = append(mesh.Indices, uint32(i), uint32(i+1), uint32(i+2))
	}
	return mesh
}

// triangles flattens draws into one vertex list per material, in draw order.
func triangles(t *testing.T, s *scene.Scene) map[scene.Material][]scene.Vertex {
	t.Helper()
	result := make(map[scene.Material][]scene.Vertex)
	for _, d := range expand(t, s) {
		result[d.material] = append(result[d.material], d.vertices...)
	}
	return result
}

// Geometry larger than a segment is drawn in several draws, and geometry that
// doesn't fit after earlier geometry starts a segment of its own.
func TestShortIndicesSplitLargeGeometry(t *testing.T) {
	red, blue := scene.Material{Color: [4]float32{1, 0, 0, 1}}, scene.Material{Color: [4]float32{0, 0, 1, 1}}
	pack := func(short bool) *scene.Scene {
		b := NewSceneBuilder(true, 0)
		b.CompactVertices()
		if short {
			b.ShortIndices()
		}
		b.Geometry(strip(50_000, 0), red, [4]float32{})
		b.Geometry(strip(150_000, 2), blue, [4]float32{})
		b.Geometry(strip(4, 4), red, [4]float32{})
		result, err := b.Finish()
		require.NoError(t, err)
		return result
	}
	long, short := pack(false), pack(true)
	assert.Equal(t, triangles(t, long), triangles(t, short))
	require.Len(t, short.Meshes, 1)
	mesh := short.Meshes[0]
	var bases []uint32
	for _, draw := range short.Draws {
		bases = append(bases, draw.Base)
		assert.LessOrEqual(t, int(draw.Base)+int(maxIndex(mesh.ShortIndices[draw.First:draw.First+draw.Count])), mesh.Len(draw.Layout)-1)
	}
	// The red strip, the blue strip in a new segment and split in three
	// (copying the two vertices shared across each split; a split segment
	// keeps room for a whole triangle), then the last strip in the blue
	// strip's last segment.
	assert.Equal(t, []uint32{0, 50_000, 50_000 + 65_534, 50_000 + 2*65_534, 50_000 + 2*65_534}, bases)
	assert.Len(t, long.Draws, 3)
	assert.Equal(t, len(long.Meshes[0].Positions)+2*2, len(mesh.Positions))
	assert.Less(t, mesh.BufferBytes(), long.Meshes[0].BufferBytes())
}

func maxIndex(indices []uint16) uint16 {
	var largest uint16
	for _, index := range indices {
		largest = max(largest, index)
	}
	return largest
}

// A stable run that spans segments is borrowed with every one of its draws.
func TestShortIndicesBorrowRunsAcrossSegments(t *testing.T) {
	layers, _ := planLayers()
	// Enough small polygons that the fill batch and its outline exceed a
	// segment.
	features := make(mvt.FeatureSlice, 0, 20_000)
	for i := range 20_000 {
		x, y := float64(i%200), float64(i/200)
		points := []geometry.Point{{X: x, Y: y}, {X: x + 0.5, Y: y}, {X: x, Y: y + 0.5}}
		features = append(features, mvt.Feature{GeometryType: mvt.PolygonType, Polygons: []mvt.Polygon{{Exterior: points, Vertices: points}}})
	}
	sources := map[string]mvt.FeatureSlice{"source": features}
	for _, packed := range []bool{false, true} {
		modes := planModes{indexed: true, compact: true, packed: packed, short: true}
		base, err := planBuildModes(layers, sources, 9, modes, nil, dotsLookup(true))
		require.NoError(t, err)
		segments := 0
		for _, draw := range base.scene.Draws {
			if draw.Base != 0 {
				segments++
			}
		}
		require.NotZero(t, segments, "some draws count from a later segment")
		require.Greater(t, len(base.plan.draws), len(base.plan.runs), "a run has several draws")
		want, err := planBuildModes(layers, sources, 10, modes, nil, dotsLookup(true))
		require.NoError(t, err)
		got, err := planBuildModes(layers, sources, 10, modes, base, dotsLookup(true))
		require.NoError(t, err)
		assert.Equal(t, want.scene, got.scene, "packed=%t", packed)
		assert.Equal(t, want.sources, got.sources)
		assert.Equal(t, want.plan, got.plan)
	}
}
