package vecmap

import (
	"math"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTriangulateConcaveRing(t *testing.T) {
	ring := []roadPoint{
		{X: 0, Y: 0},
		{X: 4, Y: 0},
		{X: 4, Y: 4},
		{X: 2, Y: 2},
		{X: 0, Y: 4},
	}

	triangles, err := testTriangulateRing(ring)

	require.NoError(t, err)
	assert.Len(t, triangles, 9)
	assert.InDelta(t, math.Abs(signedRingArea(ring)), triangleMeshArea(triangles), 1e-9)
}

func TestTriangulateRingNormalizesWindingAndCollinearPoints(t *testing.T) {
	ring := []roadPoint{
		{X: 0, Y: 0},
		{X: 0, Y: 4},
		{X: 2, Y: 4},
		{X: 4, Y: 4},
		{X: 4, Y: 0},
		{X: 0, Y: 0},
	}

	triangles, err := testTriangulateRing(ring)

	require.NoError(t, err)
	assert.Len(t, triangles, 6)
	assert.InDelta(t, 16, triangleMeshArea(triangles), 1e-9)
}

func TestTriangulateDegenerateRing(t *testing.T) {
	triangles, err := testTriangulateRing([]roadPoint{{X: 0, Y: 0}, {X: 1, Y: 1}, {X: 2, Y: 2}})
	require.NoError(t, err)
	assert.Empty(t, triangles)
}

func TestTriangulateRingEnforcesOperationBudget(t *testing.T) {
	budget := triangulationBudget{}
	_, err := triangulateRingBounded([]roadPoint{
		{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 1}, {X: 0, Y: 1},
	}, &budget)
	assert.ErrorIs(t, err, errPolygonResourceLimit)
}

func TestTriangulatePolygonBoundedPreservesHoles(t *testing.T) {
	polygon := vectorPolygon{
		Exterior: []roadPoint{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}},
		Holes:    [][]roadPoint{{{X: 3, Y: 3}, {X: 3, Y: 7}, {X: 7, Y: 7}, {X: 7, Y: 3}}},
	}
	budget := triangulationBudget{remaining: 10_000}

	triangles, err := triangulatePolygonBounded(polygon, &budget)

	require.NoError(t, err)
	assert.Len(t, triangles, 24)
	assert.InDelta(t, 84, triangleMeshArea(triangles), 1e-9)
}

func TestTriangulatePolygonBoundedHandlesLargePolygonWithinBudget(t *testing.T) {
	const pointCount = 4096
	exterior := make([]roadPoint, pointCount)
	for index := range exterior {
		angle := 2 * math.Pi * float64(index) / pointCount
		exterior[index] = roadPoint{X: math.Cos(angle), Y: math.Sin(angle)}
	}
	budget := triangulationBudget{remaining: pointCount * 20}

	triangles, err := triangulatePolygonBounded(vectorPolygon{Exterior: exterior}, &budget)

	require.NoError(t, err)
	assert.Len(t, triangles, (pointCount-2)*3)
	assert.Positive(t, budget.remaining)
}

func TestPolygonAdapterPreservesLimitsAndSpentBudget(t *testing.T) {
	assert.Equal(t, maxFillRingPoints, geometry.MaxPolygonPoints)
	assert.Equal(t, maxTileStyleTriangles, geometry.MaxPolygonTriangles)
	polygon := vectorPolygon{Exterior: []roadPoint{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}}}
	budget := triangulationBudget{remaining: 5}
	_, err := triangulatePolygonBounded(polygon, &budget)
	assert.ErrorIs(t, err, errPolygonResourceLimit)
	assert.Zero(t, budget.remaining, "failed cleanup must retain the work already charged")
	_, err = triangulatePolygonBounded(polygon, nil)
	assert.ErrorIs(t, err, errPolygonResourceLimit)
}

func triangleMeshArea(triangles []roadPoint) float64 {
	area := 0.0
	for index := 0; index < len(triangles); index += 3 {
		area += math.Abs(signedRingArea(triangles[index : index+3]))
	}
	return area
}

func testTriangulateRing(ring []roadPoint) ([]roadPoint, error) {
	budget := triangulationBudget{remaining: maxTriangulationOps}
	return triangulateRingBounded(ring, &budget)
}
