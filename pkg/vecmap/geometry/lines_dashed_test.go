package geometry

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type dashInterval struct{ from, to float64 }

// requireDashedEquivalent proves that the fragments a DashPattern covers on the
// dashed mesh are the area TessellateLines bakes: every baked dash lies on one
// segment quad with the same lateral extent, and sampled centerline distances
// are covered exactly where a baked dash is. Samples within tolerance of a dash
// end are skipped; both forms round there.
func requireDashedEquivalent(t *testing.T, path []Point, dashes []float64, width float64, indexed bool) DashedMesh {
	t.Helper()
	pattern, ok := NewDashPattern(dashes, width)
	require.True(t, ok)
	baked, err := TessellateLines([][]Point{path}, LineStyle{Width: width, Dashes: dashes}, MaxLineTriangles, false)
	require.NoError(t, err)
	mesh, err := TessellateDashedLines([][]Point{path}, MaxLineTriangles, indexed)
	require.NoError(t, err)
	vertex := func(quad, corner int) DashedVertex {
		if indexed {
			return mesh.Vertices[quad*4+corner]
		}
		return mesh.Vertices[quad*6+[...]int{0, 1, 2, 5}[corner]]
	}
	quads := len(mesh.Vertices) / 6
	if indexed {
		quads = len(mesh.Vertices) / 4
		require.Len(t, mesh.Indices, quads*6)
	}
	const tolerance = 1e-6
	intervals := make([][]dashInterval, quads)
	quad := 0
	for piece := 0; piece < len(baked.Vertices); piece += 6 {
		first, second, third, fourth := baked.Vertices[piece], baked.Vertices[piece+1], baked.Vertices[piece+2], baked.Vertices[piece+5]
		start := Point{X: (first.X + second.X) / 2, Y: (first.Y + second.Y) / 2}
		end := Point{X: (third.X + fourth.X) / 2, Y: (third.Y + fourth.Y) / 2}
		var from, to, length float64
		for ; ; quad++ {
			require.Less(t, quad, quads, "a baked dash lies on no segment")
			a, b := vertex(quad, 0), vertex(quad, 2)
			length = b.Distance - a.Distance
			tangent := Point{X: (b.Anchor.X - a.Anchor.X) / length, Y: (b.Anchor.Y - a.Anchor.Y) / length}
			from = (start.X-a.Anchor.X)*tangent.X + (start.Y-a.Anchor.Y)*tangent.Y
			to = (end.X-a.Anchor.X)*tangent.X + (end.Y-a.Anchor.Y)*tangent.Y
			across := (start.X-a.Anchor.X)*tangent.Y - (start.Y-a.Anchor.Y)*tangent.X
			// A path may fold back onto itself, so a dash also runs along its quad.
			if math.Abs(across) < tolerance && from > -tolerance && from < length-Epsilon/2 && to > from && to < length+tolerance {
				break
			}
		}
		a := vertex(quad, 0)
		require.InDelta(t, first.X-start.X, a.Direction.X*width/2, tolerance)
		require.InDelta(t, first.Y-start.Y, a.Direction.Y*width/2, tolerance)
		require.InDelta(t, second.X-start.X, vertex(quad, 1).Direction.X*width/2, tolerance)
		require.InDelta(t, second.Y-start.Y, vertex(quad, 1).Direction.Y*width/2, tolerance)
		intervals[quad] = append(intervals[quad], dashInterval{a.Distance + from, a.Distance + to})
	}
	for quad := range quads {
		a, b := vertex(quad, 0), vertex(quad, 2)
		require.Equal(t, a.Distance, vertex(quad, 1).Distance)
		require.Equal(t, b.Distance, vertex(quad, 3).Distance)
		require.Equal(t, a.Anchor, vertex(quad, 1).Anchor)
		require.Equal(t, Point{X: -a.Direction.X, Y: -a.Direction.Y}, vertex(quad, 1).Direction)
		require.InDelta(t, 1, math.Hypot(a.Direction.X, a.Direction.Y), 1e-12)
		const samples = 512
		for sample := range samples {
			distance := a.Distance + (float64(sample)+0.5)/samples*(b.Distance-a.Distance)
			inside, near := false, false
			for _, interval := range intervals[quad] {
				inside = inside || (distance > interval.from && distance < interval.to)
				near = near || math.Abs(distance-interval.from) < tolerance || math.Abs(distance-interval.to) < tolerance
			}
			// A gap may end within tolerance of the sample without a baked dash.
			position := math.Mod(distance/width, pattern.Length())
			for _, edge := range []float64{0, pattern[0], pattern[0] + pattern[1], pattern[0] + pattern[1] + pattern[2], pattern.Length()} {
				near = near || math.Abs(position-edge)*width < tolerance
			}
			if !near {
				require.Equal(t, inside, pattern.Covers(distance, width), "quad %d distance %v", quad, distance)
			}
		}
	}
	return mesh
}

func TestDashedLinesMatchBakedCoverage(t *testing.T) {
	paths := [][]Point{
		{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 3, Y: 17}},
		{{X: 1, Y: 1}, {X: 1, Y: 1}, {X: 5, Y: 4}},
		{{X: 0, Y: 0}, {X: 8, Y: 0}, {X: 0, Y: 0.25}},
		{{X: 2, Y: 2}, {X: 6, Y: 2}, {X: 6, Y: 6}, {X: 2, Y: 2}},
		{{X: 0, Y: 0}, {X: 0.01, Y: 0}, {X: 0.01, Y: 0.02}, {X: 40, Y: 0.02}},
		{{X: 20, Y: 20}, {X: 20 + Epsilon/2, Y: 20}, {X: 24, Y: 21}, {X: 24, Y: 21 + Epsilon/2}},
	}
	patterns := [][]float64{{1, 1.5}, {0.5, 0.25}, {0.2, 8}, {1, 0}, {3}, {1, 2, 3, 4}, {2, 0, 1, 1}, {1, 1, 0, 2}}
	for _, indexed := range []bool{false, true} {
		for _, path := range paths {
			for _, dashes := range patterns {
				var previous DashedMesh
				for i, width := range []float64{0.03125, 1, 2.5, 40} {
					mesh := requireDashedEquivalent(t, path, dashes, width, indexed)
					if i > 0 {
						assert.Equal(t, previous, mesh, "one mesh serves every width and pattern scale")
					}
					previous = mesh
				}
			}
		}
	}
}

func TestDashedLinesRandomPaths(t *testing.T) {
	random := rand.New(rand.NewPCG(3, 4))
	for range 200 {
		path := make([]Point, random.IntN(12))
		for j := range path {
			path[j] = Point{X: math.Round(random.Float64()*4096) / 16, Y: math.Round(random.Float64()*4096) / 16}
		}
		dashes := make([]float64, []int{1, 2, 4}[random.IntN(3)])
		for j := range dashes {
			dashes[j] = 0.1 + random.Float64()*8
		}
		requireDashedEquivalent(t, path, dashes, 0.05+random.Float64()*30, random.IntN(2) == 0)
	}
}

func TestDashedMeshLayout(t *testing.T) {
	paths := [][]Point{{{X: 0, Y: 0}, {X: 4, Y: 0}, {X: 4, Y: 3}}, {{X: 1, Y: 1}}, nil, {{X: 0, Y: 8}, {X: 3, Y: 12}}}
	mesh, err := TessellateDashedLines(paths, 16, true)
	require.NoError(t, err)
	require.Len(t, mesh.Vertices, 12)
	assert.Equal(t, []uint32{0, 1, 2, 2, 1, 3, 4, 5, 6, 6, 5, 7, 8, 9, 10, 10, 9, 11}, mesh.Indices)
	assert.Equal(t, DashedVertex{Anchor: Point{}, Direction: Point{X: 0, Y: 1}}, mesh.Vertices[0])
	assert.Equal(t, DashedVertex{Anchor: Point{X: 4}, Direction: Point{X: 0, Y: -1}, Distance: 4}, mesh.Vertices[3])
	assert.Equal(t, DashedVertex{Anchor: Point{X: 4, Y: 3}, Direction: Point{X: -1}, Distance: 7}, mesh.Vertices[6])
	assert.Zero(t, mesh.Vertices[8].Distance, "distances restart at each path")
	assert.Equal(t, 5.0, mesh.Vertices[11].Distance)
	assert.Equal(t, Point{X: 4, Y: -2}, mesh.Vertices[3].Position(2))
	expanded, err := TessellateDashedLines(paths, 16, false)
	require.NoError(t, err)
	require.Len(t, expanded.Vertices, 18)
	assert.Nil(t, expanded.Indices)
	for i, index := range mesh.Indices {
		assert.Equal(t, mesh.Vertices[index], expanded.Vertices[i])
	}
}

func TestDashedLineLimitsAndOwnership(t *testing.T) {
	paths := [][]Point{{{X: 0, Y: 0}, {X: 4, Y: 0}, {X: 4, Y: 4}}}
	for _, limit := range []int{-1, MaxLineTriangles + 1, 3} {
		mesh, err := TessellateDashedLines(paths, limit, true)
		assert.ErrorIs(t, err, ErrGeometryLimit)
		assert.Equal(t, DashedMesh{}, mesh, "errors discard partial output")
	}
	mesh, err := TessellateDashedLines(paths, 4, true)
	require.NoError(t, err)
	paths[0][0] = Point{X: 99, Y: 99}
	assert.Equal(t, Point{}, mesh.Vertices[0].Anchor, "input is not retained")
	empty, err := TessellateDashedLines(nil, 0, false)
	require.NoError(t, err)
	assert.Empty(t, empty.Vertices)
}

func TestNewDashPattern(t *testing.T) {
	for _, test := range []struct {
		dashes []float64
		width  float64
		want   DashPattern
		ok     bool
	}{
		{[]float64{1, 1.5}, 2, DashPattern{1, 1.5}, true},
		{[]float64{3}, 2, DashPattern{3, 3}, true},
		{[]float64{1, 2, 3, 4}, 0.5, DashPattern{1, 2, 3, 4}, true},
		{[]float64{1, 0}, 1, DashPattern{1, 0}, true},
		{[]float64{2, 0, 1, 1}, 1, DashPattern{2, 0, 1, 1}, true},
		{nil, 1, DashPattern{}, false},
		{[]float64{1, 2, 3}, 1, DashPattern{}, false}, // six entries when repeated
		{[]float64{1, 2, 3, 4, 5, 6}, 1, DashPattern{}, false},
		{[]float64{0, 2}, 1, DashPattern{}, false}, // no dash at the path start
		{[]float64{0, 0}, 1, DashPattern{}, false},
		{[]float64{1, -1}, 1, DashPattern{}, false},
		{[]float64{1, math.NaN()}, 1, DashPattern{}, false},
		{[]float64{1, math.Inf(1)}, 1, DashPattern{}, false},
		{[]float64{math.MaxFloat64, 1}, 4, DashPattern{}, false},
		{[]float64{1, 1e-12}, 1, DashPattern{}, false},   // the baked walk skips it
		{[]float64{1, 1}, Epsilon, DashPattern{}, false}, // no geometry at this width
		{[]float64{1, 1}, 0, DashPattern{}, false},
		{[]float64{1, 1}, -1, DashPattern{}, false},
		{[]float64{1, 1}, math.NaN(), DashPattern{}, false},
		{[]float64{1, 1}, math.Inf(1), DashPattern{}, false},
		{[]float64{1, 1}, 1e39, DashPattern{}, false},     // beyond float32
		{[]float64{1e-46, 1}, 1e38, DashPattern{}, false}, // underflows float32
		{[]float64{1e39, 1}, 1, DashPattern{}, false},
	} {
		pattern, ok := NewDashPattern(test.dashes, test.width)
		assert.Equal(t, test.ok, ok, "%v at %v", test.dashes, test.width)
		assert.Equal(t, test.want, pattern, "%v at %v", test.dashes, test.width)
	}
	pattern := DashPattern{1, 2, 3, 4}
	assert.Equal(t, 10.0, pattern.Length())
	for distance, covered := range map[float64]bool{0: true, 1.9: true, 2: false, 5.9: false, 6: true, 11.9: true, 12: false, 19.9: false, 20: true, 26: true} {
		assert.Equal(t, covered, pattern.Covers(distance, 2), "distance %v", distance)
	}
	assert.False(t, DashPattern{}.Covers(1, 1))
	assert.False(t, pattern.Covers(1, 0))
	assert.False(t, pattern.Covers(1, math.NaN()))
}

func FuzzDashedLines(f *testing.F) {
	f.Add(0.0, 0.0, 10.0, 0.0, 10.0, 10.0, 2.0, 1.0, 1.5, false)
	f.Add(1.0, 1.0, 1.0, 1.0, 5.0, 4.0, 0.25, 0.2, 8.0, true)
	f.Add(0.0, 0.0, 8.0, 0.0, 0.0, 0.25, 30.0, 1.0, 0.0, true)
	f.Add(1.0, 1.0, 426.0, 1.0, 78.0, 1.0, 0.25, 0.02, 4.0, false)  // folds back onto itself
	f.Add(1640.0, 2.0, 1.0, 1.0, 5.0, 4.0, 0.0625, 0.2, 0.0, false) // more dashes than the baked limit
	f.Fuzz(func(t *testing.T, ax, ay, bx, by, cx, cy, width, dash, gap float64, indexed bool) {
		for _, value := range []float64{ax, ay, bx, by, cx, cy} {
			if math.IsNaN(value) || math.Abs(value) > 1<<12 {
				return
			}
		}
		// Keep dash counts within the sampling resolution of the proof.
		if !(width > 1e-3) || width > 1<<12 || !(dash > 1e-2) || dash > 1<<10 || !(gap >= 0) || gap > 1<<10 || (gap != 0 && gap < 1e-2) {
			return
		}
		path := []Point{{X: ax, Y: ay}, {X: bx, Y: by}, {X: cx, Y: cy}}
		total := 0.0
		for index := 1; index < len(path); index++ {
			length := math.Hypot(path[index].X-path[index-1].X, path[index].Y-path[index-1].Y)
			if length != 0 && length < 1e-3 {
				return
			}
			total += length
		}
		// The baked walk stops at 100,000 dashes per path; the mesh has no such limit.
		if total/((dash+gap)*width) > 50_000 {
			mesh, err := TessellateDashedLines([][]Point{path}, 4, indexed)
			require.NoError(t, err)
			require.NotEmpty(t, mesh.Vertices)
			return
		}
		requireDashedEquivalent(t, path, []float64{dash, gap}, width, indexed)
	})
}
