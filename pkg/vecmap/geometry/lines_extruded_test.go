package geometry

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requireExtrudedEquivalent(t *testing.T, paths [][]Point, style LineStyle, indexed bool) ExtrudedMesh {
	t.Helper()
	baked, err := TessellateLines(paths, style, MaxLineTriangles, indexed)
	require.NoError(t, err)
	extruded, err := TessellateExtrudedLines(paths, ExtrudedLineStyle{Cap: style.Cap, Join: style.Join}, MaxLineTriangles, indexed)
	require.NoError(t, err)
	require.Equal(t, baked.Indices, extruded.Indices, "topology must not depend on width")
	require.Len(t, extruded.Vertices, len(baked.Vertices))
	for i, vertex := range extruded.Vertices {
		position := vertex.Position(style.Width / 2)
		tolerance := 1e-9 * math.Max(1, math.Max(math.Abs(baked.Vertices[i].X), math.Abs(baked.Vertices[i].Y)))
		require.InDelta(t, baked.Vertices[i].X, position.X, tolerance, "vertex %d", i)
		require.InDelta(t, baked.Vertices[i].Y, position.Y, tolerance, "vertex %d", i)
	}
	return extruded
}

func TestExtrudedLinesMatchBakedGeometry(t *testing.T) {
	paths := [][]Point{
		{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 3, Y: 17}},
		{{X: 1, Y: 1}, {X: 1, Y: 1}, {X: 5, Y: 4}},               // duplicate point
		{{X: 0, Y: 0}, {X: 8, Y: 0}, {X: 0, Y: 0.25}},            // sharp turn beyond the miter limit
		{{X: 0, Y: 0}, {X: 4, Y: 0}, {X: 8, Y: 0}},               // collinear: no miter
		{{X: 2, Y: 2}, {X: 6, Y: 2}, {X: 6, Y: 6}, {X: 2, Y: 2}}, // closed ring: no round caps
		{{X: 7, Y: 7}},
		nil,
		// Distinct points closer than Epsilon: no segment and no miter, as baked.
		{{X: 20, Y: 20}, {X: 20 + Epsilon/2, Y: 20}, {X: 24, Y: 21}, {X: 24, Y: 21 + Epsilon/2}},
	}
	for _, indexed := range []bool{false, true} {
		for _, cap := range []string{"butt", "square", "round", "unknown"} {
			for _, join := range []string{"miter", "round", "unknown"} {
				var previous ExtrudedMesh
				for i, width := range []float64{0.03125, 1, 2.5, 40} {
					extruded := requireExtrudedEquivalent(t, paths, LineStyle{Width: width, Cap: cap, Join: join}, indexed)
					if i > 0 {
						assert.Equal(t, previous, extruded, "one mesh serves every width")
					}
					previous = extruded
				}
			}
		}
	}
}

func TestExtrudedLinesRandomPaths(t *testing.T) {
	random := rand.New(rand.NewPCG(1, 2))
	for range 200 {
		paths := make([][]Point, 1+random.IntN(4))
		for i := range paths {
			paths[i] = make([]Point, random.IntN(12))
			for j := range paths[i] {
				paths[i][j] = Point{X: math.Round(random.Float64()*4096) / 16, Y: math.Round(random.Float64()*4096) / 16}
			}
		}
		style := LineStyle{Width: 0.05 + random.Float64()*30, Cap: []string{"butt", "square", "round"}[random.IntN(3)], Join: []string{"miter", "round"}[random.IntN(2)]}
		requireExtrudedEquivalent(t, paths, style, random.IntN(2) == 0)
	}
}

func TestExtrudedDirections(t *testing.T) {
	mesh, err := TessellateExtrudedLines([][]Point{{{X: 0, Y: 0}, {X: 4, Y: 0}}}, ExtrudedLineStyle{Cap: "square"}, 16, true)
	require.NoError(t, err)
	require.Len(t, mesh.Vertices, 4)
	assert.Equal(t, ExtrudedVertex{Anchor: Point{}, Direction: Point{X: -1, Y: 1}}, mesh.Vertices[0])
	assert.Equal(t, ExtrudedVertex{Anchor: Point{X: 4}, Direction: Point{X: 1, Y: -1}}, mesh.Vertices[3])
	round, err := TessellateExtrudedLines([][]Point{{{X: 0, Y: 0}, {X: 4, Y: 0}}}, ExtrudedLineStyle{Cap: "round"}, 32, true)
	require.NoError(t, err)
	require.Len(t, round.Vertices, 4+2*(DiskSections+1))
	assert.Equal(t, Point{}, round.Vertices[4].Direction, "disk centers are not extruded")
	for _, vertex := range round.Vertices[5 : 5+DiskSections] {
		assert.InDelta(t, 1, math.Hypot(vertex.Direction.X, vertex.Direction.Y), 1e-15)
	}
	// A rejected miter collapses to the anchor for every width.
	sharp, err := TessellateExtrudedLines([][]Point{{{X: 0, Y: 0}, {X: 8, Y: 0}, {X: 0, Y: 0.25}}}, ExtrudedLineStyle{}, 32, false)
	require.NoError(t, err)
	collapsed := 0
	for _, vertex := range sharp.Vertices {
		if vertex.Anchor == (Point{X: 8}) && vertex.Direction == (Point{}) {
			collapsed++
		}
		assert.LessOrEqual(t, math.Hypot(vertex.Direction.X, vertex.Direction.Y), 4.0)
	}
	assert.Equal(t, 2, collapsed, "both sides share the miter length and its limit")
}

func TestExtrudedLineLimitsAndOwnership(t *testing.T) {
	paths := [][]Point{{{X: 0, Y: 0}, {X: 4, Y: 0}, {X: 4, Y: 4}}}
	_, err := TessellateExtrudedLines(paths, ExtrudedLineStyle{}, -1, true)
	assert.ErrorIs(t, err, ErrGeometryLimit)
	_, err = TessellateExtrudedLines(paths, ExtrudedLineStyle{}, MaxLineTriangles+1, true)
	assert.ErrorIs(t, err, ErrGeometryLimit)
	for _, style := range []ExtrudedLineStyle{{}, {Join: "round"}, {Cap: "round"}} {
		for limit := range 16 {
			mesh, err := TessellateExtrudedLines(paths, style, limit, true)
			if err != nil {
				assert.ErrorIs(t, err, ErrGeometryLimit)
				assert.Equal(t, ExtrudedMesh{}, mesh, "errors discard partial output")
			}
		}
	}
	_, err = TessellateExtrudedLines(paths, ExtrudedLineStyle{Cap: "round"}, 4, true)
	assert.ErrorIs(t, err, ErrGeometryLimit, "the closing cap must respect the limit")
	mesh, err := TessellateExtrudedLines(paths, ExtrudedLineStyle{}, 16, true)
	require.NoError(t, err)
	paths[0][0] = Point{X: 99, Y: 99}
	assert.Equal(t, Point{}, mesh.Vertices[0].Anchor, "input is not retained")
	empty, err := TessellateExtrudedLines(nil, ExtrudedLineStyle{}, 0, false)
	require.NoError(t, err)
	assert.Empty(t, empty.Vertices)
}

func FuzzExtrudedLines(f *testing.F) {
	f.Add(0.0, 0.0, 10.0, 0.0, 10.0, 10.0, 2.0, uint8(0), false)
	f.Add(1.0, 1.0, 1.0, 1.0, 5.0, 4.0, 0.25, uint8(5), true)
	f.Add(0.0, 0.0, 8.0, 0.0, 0.0, 0.25, 30.0, uint8(2), true)
	f.Fuzz(func(t *testing.T, ax, ay, bx, by, cx, cy, width float64, style uint8, indexed bool) {
		for _, value := range []float64{ax, ay, bx, by, cx, cy} {
			if math.IsNaN(value) || math.Abs(value) > 1<<20 {
				return
			}
		}
		if !(width > Epsilon) || width > 1<<12 {
			return
		}
		paths := [][]Point{{{X: ax, Y: ay}, {X: bx, Y: by}, {X: cx, Y: cy}}}
		line := LineStyle{Width: width, Cap: []string{"butt", "square", "round"}[style%3], Join: []string{"miter", "round"}[style/3%2]}
		baked, err := TessellateLines(paths, line, 256, indexed)
		require.NoError(t, err)
		extruded, err := TessellateExtrudedLines(paths, ExtrudedLineStyle{Cap: line.Cap, Join: line.Join}, 256, indexed)
		require.NoError(t, err)
		require.Equal(t, baked.Indices, extruded.Indices)
		require.Len(t, extruded.Vertices, len(baked.Vertices))
		for i, vertex := range extruded.Vertices {
			position := vertex.Position(width / 2)
			// A miter within rounding of the four-half-width limit may be kept by
			// one form and collapsed by the other; both are valid renderings.
			if length := math.Hypot(vertex.Direction.X, vertex.Direction.Y); length == 0 || math.Abs(length-4) < 1e-6 {
				continue
			}
			scale := math.Max(1, math.Max(math.Abs(baked.Vertices[i].X), math.Abs(baked.Vertices[i].Y)))
			require.InDelta(t, baked.Vertices[i].X, position.X, 1e-7*scale+1e-9*width*4)
			require.InDelta(t, baked.Vertices[i].Y, position.Y, 1e-7*scale+1e-9*width*4)
		}
	})
}

func TestTessellateIntoReusesStorageWithTheSameResult(t *testing.T) {
	random := rand.New(rand.NewPCG(7, 11))
	path := func(n int) []Point {
		points := make([]Point, n)
		for i := range points {
			points[i] = Point{X: random.Float64() * 4096, Y: random.Float64() * 4096}
		}
		return points
	}
	small, large := [][]Point{path(3)}, [][]Point{path(40), path(25), {{X: 1, Y: 1}}}
	for _, indexed := range []bool{false, true} {
		var extruded ExtrudedMesh
		var dashed DashedMesh
		// Grow, shrink, then grow again into storage holding stale vertices.
		for _, paths := range [][][]Point{small, large, small, large, nil} {
			want, err := TessellateExtrudedLines(paths, ExtrudedLineStyle{Cap: "round", Join: "round"}, MaxLineTriangles, indexed)
			require.NoError(t, err)
			previous := extruded
			extruded, err = TessellateExtrudedLinesInto(extruded, paths, ExtrudedLineStyle{Cap: "round", Join: "round"}, MaxLineTriangles, indexed)
			require.NoError(t, err)
			assert.Equal(t, want, extruded)
			if cap(previous.Vertices) >= cap(extruded.Vertices) && len(extruded.Vertices) > 0 {
				assert.Same(t, &previous.Vertices[:1][0], &extruded.Vertices[0], "reused vertex storage")
			}

			wantDashed, err := TessellateDashedLines(paths, MaxLineTriangles, indexed)
			require.NoError(t, err)
			previousDashed := dashed
			dashed, err = TessellateDashedLinesInto(dashed, paths, MaxLineTriangles, indexed)
			require.NoError(t, err)
			assert.Equal(t, len(wantDashed.Vertices), len(dashed.Vertices))
			assert.Equal(t, len(wantDashed.Indices), len(dashed.Indices))
			if len(wantDashed.Vertices) > 0 {
				assert.Equal(t, wantDashed.Vertices, dashed.Vertices)
				assert.Equal(t, wantDashed.Indices, dashed.Indices)
			}
			if cap(previousDashed.Vertices) >= len(dashed.Vertices) && len(dashed.Vertices) > 0 {
				assert.Same(t, &previousDashed.Vertices[:1][0], &dashed.Vertices[0], "reused dashed storage")
			}
		}
	}
	_, err := TessellateExtrudedLinesInto(ExtrudedMesh{}, small, ExtrudedLineStyle{}, -1, true)
	assert.ErrorIs(t, err, ErrGeometryLimit)
	_, err = TessellateDashedLinesInto(DashedMesh{}, small, -1, true)
	assert.ErrorIs(t, err, ErrGeometryLimit)
}
