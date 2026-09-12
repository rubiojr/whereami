package geometry

import (
	"fmt"
	"math"
	"slices"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPolygonTopology(t *testing.T) {
	exterior := []Point{{0, 0}, {10, 0}, {10, 10}, {0, 10}}
	tests := []struct {
		name      string
		exterior  []Point
		holes     [][]Point
		area      float64
		triangles int
	}{
		{"square", exterior, nil, 100, 2},
		{"hole", exterior, [][]Point{{{3, 3}, {3, 7}, {7, 7}, {7, 3}}}, 84, 8},
		{"concave", []Point{{0, 0}, {4, 0}, {4, 4}, {2, 2}, {0, 4}}, nil, 12, 3},
		{"cleaned", []Point{{0, 0}, {0, 4}, {2, 4}, {2, 4}, {4, 4}, {4, 0}, {0, 0}}, nil, 16, 2},
		{"degenerate", []Point{{0, 0}, {1, 1}, {2, 2}}, nil, 0, 0},
		{"degenerate-hole", exterior, [][]Point{nil, {{1, 1}, {2, 2}, {3, 3}}}, 100, 2},
		{"empty", nil, nil, 0, 0},
		{"signed-zero", []Point{{math.Copysign(0, -1), 0}, {10, 0}, {10, 10}, {0, 10}}, nil, 100, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := slices.Clone(tt.exterior)
			budget, expandedBudget := Budget{Remaining: 10_000}, Budget{Remaining: 10_000}
			mesh, err := TriangulatePolygon(tt.exterior, tt.holes, &budget)
			require.NoError(t, err)
			expanded, err := TriangulatePolygonExpanded(tt.exterior, tt.holes, &expandedBudget)
			require.NoError(t, err)
			require.Len(t, mesh.Indices, tt.triangles*3)
			require.Len(t, expanded, len(mesh.Indices))
			assert.Equal(t, budget, expandedBudget)
			assert.Equal(t, original, tt.exterior)
			area := 0.0
			for i, index := range mesh.Indices {
				require.Less(t, int(index), len(mesh.Vertices))
				point := mesh.Vertices[index]
				assert.Equal(t, math.Float64bits(expanded[i].X), math.Float64bits(point.X))
				assert.Equal(t, math.Float64bits(expanded[i].Y), math.Float64bits(point.Y))
				if i%3 == 0 {
					area += math.Abs(SignedRingArea(expanded[i : i+3]))
				}
			}
			assert.InDelta(t, tt.area, area, Epsilon)
			if tt.name == "square" {
				assert.Equal(t, []uint32{2, 3, 0, 0, 1, 2}, mesh.Indices)
			}
			if tt.name == "signed-zero" {
				assert.True(t, math.Signbit(mesh.Vertices[0].X))
			}
			if len(mesh.Vertices) != 0 {
				mesh.Vertices[0].X = 12345
				assert.Equal(t, original, tt.exterior, "result must not alias input rings")
			}
		})
	}
}

func TestPolygonBudgetAndLimits(t *testing.T) {
	exterior := []Point{{0, 0}, {10, 0}, {10, 10}, {0, 10}}
	holes := [][]Point{{{3, 3}, {3, 7}, {7, 7}, {7, 3}}}
	for remaining := 0; remaining < 120; remaining++ {
		budget, legacyBudget := Budget{Remaining: remaining}, Budget{Remaining: remaining}
		mesh, err := TriangulatePolygon(exterior, holes, &budget)
		_, legacyErr := TriangulatePolygonExpanded(exterior, holes, &legacyBudget)
		assert.Equal(t, legacyBudget, budget)
		assert.Equal(t, legacyErr, err)
		if err != nil {
			assert.ErrorIs(t, err, ErrResourceLimit)
			assert.Empty(t, mesh)
		}
	}
	_, err := TriangulatePolygon(exterior, nil, nil)
	assert.ErrorIs(t, err, ErrResourceLimit)
	mesh, err := TriangulatePolygon(nil, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, mesh)
	for _, rings := range [][][]Point{
		{make([]Point, MaxPolygonPoints+1)},
		{exterior, make([]Point, MaxPolygonPoints)},
	} {
		budget := Budget{Remaining: 1_000_000}
		_, err := TriangulatePolygon(rings[0], rings[1:], &budget)
		assert.ErrorIs(t, err, ErrResourceLimit)
		assert.Equal(t, 1_000_000, budget.Remaining, "reject before cleanup and allocation")
	}
	budget := Budget{Remaining: 4096 * 20}
	large, err := TriangulatePolygon(circle(4096), nil, &budget)
	require.NoError(t, err)
	assert.Len(t, large.Vertices, 4096)
	assert.Len(t, large.Indices, (4096-2)*3)
	assert.Positive(t, budget.Remaining)
}

func TestCheckedEarcutIndices(t *testing.T) {
	for _, tt := range []struct {
		indices             []int
		vertices, triangles int
		message             string
	}{
		{[]int{0, 1}, 3, 1, "incomplete triangle"},
		{[]int{0, 1, 2}, 3, 0, "maximum is 0"},
		{[]int{0, -1, 2}, 3, 1, "out-of-range"},
		{[]int{0, 1, 3}, 3, 1, "out-of-range"},
	} {
		assert.ErrorContains(t, checkIndices(tt.indices, tt.vertices, tt.triangles), tt.message)
	}
	assert.NoError(t, checkIndices([]int{2, 0, 1}, 3, 1))
	assert.ErrorIs(t, (&Budget{Remaining: 1}).Consume(-1), ErrResourceLimit)
}

func circle(count int) []Point {
	points := make([]Point, count)
	for i := range points {
		angle := 2 * math.Pi * float64(i) / float64(count)
		points[i] = Point{X: math.Cos(angle) * 100, Y: math.Sin(angle) * 100}
	}
	return points
}

// Include triangulation, float32 scene packing, and (for post-indexed) hashing.
// This is polygon preparation, not a full tile/compiler or GPU benchmark.
func BenchmarkPolygonPreparation(b *testing.B) {
	for _, count := range []int{16, 4096} {
		points := circle(count)
		for _, mode := range []string{"expanded", "direct", "post-indexed"} {
			b.Run(fmt.Sprintf("%d/%s", count, mode), func(b *testing.B) {
				b.ReportAllocs()
				var mesh scene.Mesh
				for b.Loop() {
					budget := Budget{Remaining: 50_000_000}
					var vertices []Point
					var indices []uint32
					var err error
					if mode == "direct" {
						var result Mesh
						result, err = TriangulatePolygon(points, nil, &budget)
						vertices, indices = result.Vertices, result.Indices
					} else {
						vertices, err = TriangulatePolygonExpanded(points, nil, &budget)
					}
					if err != nil {
						b.Fatal(err)
					}
					mesh = scene.Mesh{ID: 1, Revision: 1, Vertices: make([]scene.Vertex, len(vertices)), Indices: indices}
					for i, point := range vertices {
						mesh.Vertices[i] = scene.Vertex{X: float32(point.X), Y: float32(point.Y)}
					}
					if mode == "post-indexed" {
						mesh, err = scene.IndexMesh(mesh)
						if err != nil {
							b.Fatal(err)
						}
					}
				}
				b.ReportMetric(float64(len(mesh.Vertices)*24+len(mesh.Indices)*4), "geometry-B")
			})
		}
	}
}
