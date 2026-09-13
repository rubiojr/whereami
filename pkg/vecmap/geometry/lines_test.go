package geometry

import (
	"math"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func lineArea(mesh Mesh) float64 {
	area := 0.0
	for i := 0; i < len(mesh.Vertices); i += 3 {
		area += math.Abs(SignedRingArea(mesh.Vertices[i : i+3]))
	}
	return area
}

// Port the original width/dash/miter regressions to the headless engine.
func TestTessellateLinesStyles(t *testing.T) {
	path := [][]Point{{{X: 0, Y: 0}, {X: 20, Y: 0}}}
	solid, err := TessellateLines(path, LineStyle{Width: 4, Cap: "butt"}, 100, false)
	require.NoError(t, err)
	assert.InDelta(t, 80, lineArea(solid), 1e-9)
	dashed, err := TessellateLines(path, LineStyle{Width: 2, Dashes: []float64{2, 2}, Cap: "butt"}, 100, false)
	require.NoError(t, err)
	assert.Less(t, lineArea(dashed), lineArea(solid))
	for _, style := range []LineStyle{{Width: 1e-300, Dashes: []float64{1, 1}}, {Width: 2, Dashes: []float64{0, 2}}} {
		mesh, err := TessellateLines(path, style, 100, true)
		require.NoError(t, err)
		assert.Empty(t, mesh.Vertices)
		assert.Empty(t, mesh.Indices)
	}
	zero, err := TessellateLines(path, LineStyle{Width: 2, Dashes: []float64{0, 0}}, 100, false)
	require.NoError(t, err)
	assert.NotEmpty(t, zero.Vertices)
	miter, err := TessellateLines([][]Point{{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}}}, LineStyle{Width: 4, Join: "miter"}, 100, false)
	require.NoError(t, err)
	assert.Len(t, miter.Vertices, 18)
}

func TestTessellateLinesTopologyAndOwnership(t *testing.T) {
	paths := [][]Point{
		{{X: math.Copysign(0, -1)}, {X: 4}, {X: 4}, {X: 8, Y: 3}, {X: 4, Y: 3.001}},
		{{}, {X: 10}, {X: 10, Y: 10}, {Y: 10}, {}},
		nil, {{X: 1}}, {{X: 2}, {X: 2}},
	}
	original := make([][]Point, len(paths))
	for i := range paths {
		original[i] = slices.Clone(paths[i])
	}
	for _, cap := range []string{"butt", "square", "round"} {
		for _, join := range []string{"miter", "round"} {
			for _, dashes := range [][]float64{nil, {1, 2, 3}, {0, 1}, {0, 0}} {
				style := LineStyle{Width: 2, Offset: -1.5, Cap: cap, Join: join, Dashes: dashes}
				expanded, err := TessellateLines(paths, style, 10000, false)
				require.NoError(t, err)
				indexed, err := TessellateLines(paths, style, 10000, true)
				require.NoError(t, err)
				require.Len(t, indexed.Indices, len(expanded.Vertices))
				for i, index := range indexed.Indices {
					want, got := expanded.Vertices[i], indexed.Vertices[index]
					assert.Equal(t, math.Float64bits(want.X), math.Float64bits(got.X))
					assert.Equal(t, math.Float64bits(want.Y), math.Float64bits(got.Y))
				}
				assert.Equal(t, original, paths)
				if len(indexed.Vertices) > 0 {
					indexed.Vertices[0].X = 999
					assert.Equal(t, original, paths)
				}
			}
		}
	}
}

func TestLineDashPhaseAndLimits(t *testing.T) {
	segments, err := dashedSegments([]Point{{}, {X: 3}, {X: 10}}, []float64{4, 2}, 1, 100)
	require.NoError(t, err)
	require.Len(t, segments, 3)
	for i, endpoints := range [][2]float64{{0, 3}, {3, 4}, {6, 10}} {
		assert.InDelta(t, endpoints[0], segments[i].Start.X, 1e-12)
		assert.InDelta(t, endpoints[1], segments[i].End.X, 1e-12)
	}
	path := []Point{{}, {X: 30}}
	odd, err := dashedSegments(path, []float64{1, 2, 3}, 1, 100)
	require.NoError(t, err)
	even, err := dashedSegments(path, []float64{1, 2, 3, 1, 2, 3}, 1, 100)
	require.NoError(t, err)
	assert.Equal(t, even, odd)
	for _, dash := range []float64{-1, math.NaN(), math.Inf(1)} {
		_, err := dashedSegments(path, []float64{dash}, 1, 100)
		assert.ErrorIs(t, err, ErrLinePattern)
	}
	_, err = dashedSegments(path, []float64{0, 2e-9}, 1, 2)
	assert.ErrorIs(t, err, ErrGeometryLimit)
	assert.ErrorContains(t, err, "iteration limit")
	_, err = dashedSegments([]Point{{}, {X: 300000}}, []float64{1, 1}, 1, MaxLineTriangles)
	assert.ErrorIs(t, err, ErrGeometryLimit)
	assert.ErrorContains(t, err, "100000-segment limit")
}

func TestLineErrorsDiscardPartialOutput(t *testing.T) {
	path := [][]Point{{{}, {X: 2}}}
	for _, indexed := range []bool{false, true} {
		for _, limit := range []int{-1, 0, 1, MaxLineTriangles + 1} {
			mesh, err := TessellateLines(path, LineStyle{Width: 2}, limit, indexed)
			assert.ErrorIs(t, err, ErrGeometryLimit)
			assert.Equal(t, Mesh{}, mesh)
		}
		mesh, err := TessellateLines(path, LineStyle{Width: 1, Cap: "round", Dashes: []float64{4, 2}}, 2, indexed)
		assert.ErrorIs(t, err, ErrGeometryLimit, "caps count against the same triangle limit")
		assert.Equal(t, Mesh{}, mesh)
		mesh, err = TessellateLines(path, LineStyle{Width: 1, Dashes: []float64{-1}}, 100, indexed)
		assert.ErrorIs(t, err, ErrLinePattern)
		assert.Equal(t, Mesh{}, mesh)
	}
}

func TestLineCapacitySaturates(t *testing.T) {
	assert.Equal(t, 6, addLineCapacity(0, math.MaxInt, 24, 6))
	path := make([]Point, 65536)
	paths := make([][]Point, 4096)
	for i := range paths {
		paths[i] = path
	}
	assert.Equal(t, MaxLineTriangles*3, lineVertexCapacity(paths, LineStyle{Width: 1, Join: "round"}, MaxLineTriangles))
}

func TestOffsetLineBounds(t *testing.T) {
	path := []Point{{}, {X: 10}, {X: 10, Y: 10}}
	borrowed := OffsetLine(path, 0)
	assert.Same(t, &path[0], &borrowed[0])
	offset := OffsetLine(path, 2)
	assert.InDelta(t, 8, offset[1].X, 1e-12)
	assert.InDelta(t, 2, offset[1].Y, 1e-12)
	for _, path := range [][]Point{
		{{}, {X: 10}, {X: 0.001, Y: 0.0001}},
		{{}, {}, {X: 10}}, {{}, {}},
	} {
		for _, distance := range []float64{-2, 2} {
			original := slices.Clone(path)
			offset := OffsetLine(path, distance)
			for i := range path {
				assert.LessOrEqual(t, math.Hypot(offset[i].X-path[i].X, offset[i].Y-path[i].Y), math.Abs(distance)*4+1e-9)
			}
			assert.Equal(t, original, path)
		}
	}
}

func TestClosedLineCapsAndMiterFallback(t *testing.T) {
	closed := [][]Point{{{}, {X: 10}, {X: 10, Y: 10}, {Y: 10}, {}}}
	butt, err := TessellateLines(closed, LineStyle{Width: 2, Cap: "butt", Join: "round"}, 100, false)
	require.NoError(t, err)
	round, err := TessellateLines(closed, LineStyle{Width: 2, Cap: "round", Join: "round"}, 100, false)
	require.NoError(t, err)
	assert.Equal(t, butt, round, "a closed path must not gain endpoint caps")
	mesh := NewBuilder[Point](false, 6)
	require.NoError(t, appendMiterJoin(Point{}, Point{X: 10}, Point{X: 0, Y: 0.01}, 1, &mesh))
	require.Len(t, mesh.Vertices, 6)
	assert.Equal(t, Point{X: 10}, mesh.Vertices[1])
	assert.Equal(t, Point{X: 10}, mesh.Vertices[4])
	for _, points := range [][3]Point{{{}, {}, {X: 1}}, {{}, {X: 1}, {X: 2}}} {
		empty := NewBuilder[Point](false, 0)
		require.NoError(t, appendMiterJoin(points[0], points[1], points[2], 1, &empty))
		assert.Empty(t, empty.Vertices)
	}
}

func BenchmarkTessellateLines(b *testing.B) {
	path := make([]Point, 1024)
	for i := range path {
		path[i] = Point{X: float64(i), Y: float64(i%7) * 5}
	}
	for _, kind := range []string{"solid", "dashed"} {
		style := LineStyle{Width: 2, Offset: 1, Cap: "round", Join: "round"}
		if kind == "dashed" {
			style.Dashes = []float64{2, 1}
		}
		for _, indexed := range []bool{false, true} {
			name := "expanded"
			if indexed {
				name = "indexed"
			}
			b.Run(kind+"/"+name, func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					_, err := TessellateLines([][]Point{path}, style, MaxLineTriangles, indexed)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
