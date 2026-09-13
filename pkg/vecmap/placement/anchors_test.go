package placement

import (
	"math"
	"slices"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/mvt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFeatureAnchorsSelectionOrderAndOwnership(t *testing.T) {
	feature := mvt.Feature{
		Points:   []geometry.Point{{X: 8, Y: 9}, {X: 3, Y: 4}},
		Polygons: []mvt.Polygon{{Exterior: []geometry.Point{{}, {X: 6}, {Y: 6}}, Holes: [][]geometry.Point{{{X: 1, Y: 1}}}}},
		Lines:    [][]geometry.Point{{{}, {X: 100}}, {{X: 10, Y: 20}, {X: 30, Y: 20}}, nil},
	}
	for _, mode := range []string{"point", "unknown", ""} {
		assert.Equal(t, []Anchor{{Point: geometry.Point{X: 8, Y: 9}}, {Point: geometry.Point{X: 3, Y: 4}}, {Point: geometry.Point{X: 2, Y: 2}}}, FeatureAnchors(feature, mode, 30))
	}
	center := FeatureAnchors(feature, "line-center", 30)
	assert.Equal(t, []Anchor{{Point: geometry.Point{X: 50}}, {Point: geometry.Point{X: 20, Y: 20}}}, center)
	repeated := FeatureAnchors(feature, "line", 30)
	require.Len(t, repeated, 4)
	assert.InDelta(t, 100.0/6, repeated[0].Point.X, 1e-12)
	assert.Equal(t, geometry.Point{X: 20, Y: 20}, repeated[3].Point)
	feature.Points, feature.Polygons = nil, nil
	assert.Equal(t, center, FeatureAnchors(feature, "point", 30))
	feature.Lines[0][1].X = 999
	assert.Equal(t, 50.0, center[0].Point.X)
	center[0].Point.X = -100
	assert.Equal(t, 999.0, feature.Lines[0][1].X)
	// Even an empty polygon contributes the legacy origin and prevents fallback.
	feature.Polygons = []mvt.Polygon{{}}
	assert.Equal(t, []Anchor{{}}, FeatureAnchors(feature, "point", 0))
	assert.Empty(t, FeatureAnchors(mvt.Feature{}, "point", 0))
}

func TestLineAnchorClampingAndAngles(t *testing.T) {
	line := []geometry.Point{{}, {}, {X: 10}, {X: 10, Y: 10}}
	for _, tt := range []struct {
		fraction float64
		point    geometry.Point
		angle    float64
	}{
		{-1, geometry.Point{}, 0}, {math.Inf(-1), geometry.Point{}, 0},
		{0.25, geometry.Point{X: 5}, 0}, {0.5, geometry.Point{X: 10}, 0},
		{0.75, geometry.Point{X: 10, Y: 5}, math.Pi / 2},
		{2, geometry.Point{X: 10, Y: 10}, math.Pi / 2}, {math.Inf(1), geometry.Point{X: 10, Y: 10}, math.Pi / 2},
	} {
		anchor, ok := LineAnchor(line, tt.fraction)
		require.True(t, ok)
		assert.Equal(t, tt.point, anchor.Point)
		assert.Equal(t, tt.angle, anchor.RawAngle)
		assert.Equal(t, tt.angle, anchor.Angle)
	}
	for _, tt := range []struct {
		end          geometry.Point
		raw, upright float64
	}{
		{geometry.Point{X: -10}, math.Pi, 2 * math.Pi},
		{geometry.Point{X: -10, Y: -10}, -3 * math.Pi / 4, math.Pi / 4},
		{geometry.Point{Y: -10}, -math.Pi / 2, -math.Pi / 2},
	} {
		anchor, ok := LineAnchor([]geometry.Point{{}, tt.end}, 0.5)
		require.True(t, ok)
		assert.Equal(t, tt.raw, anchor.RawAngle)
		assert.Equal(t, tt.upright, anchor.Angle)
	}
	anchor, ok := LineAnchor(line, math.NaN())
	assert.False(t, ok)
	assert.Equal(t, Anchor{}, anchor)
}

func TestRepeatedAnchorsClampBeforeIntegerConversion(t *testing.T) {
	line := []geometry.Point{{}, {X: 100}}
	for _, tt := range []struct {
		spacing float64
		count   int
	}{
		{-1, 1}, {0, 1}, {math.Inf(1), 1}, {101, 1}, {100, 1}, {30, 3}, {1, 16},
		{1e-10, 16}, {math.SmallestNonzeroFloat64, 16},
	} {
		anchors := RepeatedLineAnchors(line, tt.spacing)
		require.Len(t, anchors, tt.count)
		for index, anchor := range anchors {
			assert.InDelta(t, 100*(float64(index)+0.5)/float64(tt.count), anchor.Point.X, 1e-12)
		}
	}
	assert.Nil(t, RepeatedLineAnchors(line, math.NaN()))
}

func TestDegenerateAndNonfiniteLines(t *testing.T) {
	tiny := []geometry.Point{{}, {X: geometry.Epsilon * 0.75}, {X: geometry.Epsilon * 1.5}}
	for _, line := range [][]geometry.Point{nil, {{}}, {{}, {}}, {{}, {X: geometry.Epsilon}}, tiny,
		{{}, {X: math.NaN()}}, {{}, {Y: math.Inf(1)}}, {{X: -math.MaxFloat64}, {X: math.MaxFloat64}}} {
		anchor, ok := LineAnchor(line, 1)
		assert.False(t, ok)
		assert.Equal(t, Anchor{}, anchor)
		assert.Empty(t, RepeatedLineAnchors(line, 0))
		assert.Empty(t, RepeatedLineAnchors(line, geometry.Epsilon/10))
	}
	assert.Empty(t, FeatureAnchors(mvt.Feature{Lines: [][]geometry.Point{tiny}}, "line-center", 0))
}

func TestPolygonCentroidPreservesExistingArithmetic(t *testing.T) {
	triangle := []geometry.Point{{}, {X: 6}, {Y: 6}}
	assert.Equal(t, geometry.Point{X: 2, Y: 2}, PolygonCentroid(triangle))
	reversed := slices.Clone(triangle)
	slices.Reverse(reversed)
	assert.Equal(t, PolygonCentroid(triangle), PolygonCentroid(reversed))
	assert.Equal(t, geometry.Point{}, PolygonCentroid(nil))
	assert.Equal(t, geometry.Point{X: 2, Y: 3}, PolygonCentroid([]geometry.Point{{X: 2, Y: 3}}))
	assert.Equal(t, geometry.Point{X: 2}, PolygonCentroid([]geometry.Point{{}, {X: 2}, {X: 4}}))
	// Preserve the legacy zero-area fallback, including its residual numerator.
	assert.Equal(t, geometry.Point{X: 1, Y: 3}, PolygonCentroid([]geometry.Point{{}, {X: 2, Y: 2}, {Y: 2}, {X: 2}}))
	centroid := PolygonCentroid(triangle)
	triangle[0].X = 99
	assert.Equal(t, geometry.Point{X: 2, Y: 2}, centroid)
}

func FuzzLineAnchors(f *testing.F) {
	f.Add(0.0, 0.0, 100.0, 0.0, 30.0, 0.5)
	f.Add(10.0, 2.0, -20.0, -3.0, math.SmallestNonzeroFloat64, 1.0)
	f.Fuzz(func(t *testing.T, x1, y1, x2, y2, spacing, fraction float64) {
		line := []geometry.Point{{X: x1, Y: y1}, {X: x2, Y: y2}}
		anchors := RepeatedLineAnchors(line, spacing)
		require.LessOrEqual(t, len(anchors), MaxLineAnchors)
		anchor, ok := LineAnchor(line, fraction)
		if ok {
			anchors = append(anchors, anchor)
		} else {
			require.Equal(t, Anchor{}, anchor)
		}
		for _, anchor := range anchors {
			require.True(t, finite(anchor.Point.X) && finite(anchor.Point.Y))
			require.True(t, finite(anchor.Angle) && finite(anchor.RawAngle))
		}
	})
}
