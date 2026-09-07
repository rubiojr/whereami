package vecmap

import (
	"errors"
	"fmt"
	"math"

	"github.com/rubiojr/whereami/internal/earcut"
)

const polygonEpsilon = 1e-9

var errPolygonResourceLimit = errors.New("MVT polygon resource limit exceeded")

type triangulationBudget struct {
	remaining int
}

func fillTriangleCount(bucket fillBucket) int {
	return (len(bucket.triangles) + len(bucket.cutouts)) / 3
}

func signedRingArea(ring []roadPoint) float64 {
	area := 0.0
	for index, point := range ring {
		next := ring[(index+1)%len(ring)]
		area += point.X*next.Y - point.Y*next.X
	}
	return area / 2
}

func triangulateRingBounded(ring []roadPoint, budget *triangulationBudget) ([]roadPoint, error) {
	points, err := cleanRing(ring, budget)
	if err != nil {
		return nil, err
	}
	if len(points) < 3 {
		return nil, nil
	}
	area := signedRingArea(points)
	if math.Abs(area) <= polygonEpsilon {
		return nil, nil
	}
	if area < 0 {
		reversePoints(points)
	}

	indices := make([]int, len(points))
	for index := range indices {
		indices[index] = index
	}
	triangles := make([]roadPoint, 0, (len(points)-2)*3)
	for len(indices) > 3 {
		ear := -1
		for index := range indices {
			if err := budget.consume(); err != nil {
				return nil, err
			}
			previous := indices[(index+len(indices)-1)%len(indices)]
			current := indices[index]
			next := indices[(index+1)%len(indices)]
			if pointCross(points[previous], points[current], points[next]) <= polygonEpsilon {
				continue
			}
			contains, err := triangleContainsOtherPoint(points, indices, previous, current, next, budget)
			if err != nil {
				return nil, err
			}
			if contains {
				continue
			}
			ear = index
			triangles = append(triangles, points[previous], points[current], points[next])
			break
		}
		if ear < 0 {
			return nil, errors.New("MVT polygon ring cannot be triangulated")
		}
		indices = append(indices[:ear], indices[ear+1:]...)
	}
	triangles = append(triangles, points[indices[0]], points[indices[1]], points[indices[2]])
	return triangles, nil
}

func triangulatePolygonBounded(polygon vectorPolygon, budget *triangulationBudget) ([]roadPoint, error) {
	rawVertexCount := len(polygon.exterior)
	if rawVertexCount > maxFillRingPoints {
		return nil, fmt.Errorf("%w: styled polygon exceeds %d-point limit", errPolygonResourceLimit, maxFillRingPoints)
	}
	for _, hole := range polygon.holes {
		if len(hole) > maxFillRingPoints-rawVertexCount {
			return nil, fmt.Errorf("%w: styled polygon exceeds %d-point limit", errPolygonResourceLimit, maxFillRingPoints)
		}
		rawVertexCount += len(hole)
	}
	if err := budget.consumeN(rawVertexCount); err != nil {
		return nil, err
	}

	exterior, err := cleanRing(polygon.exterior, budget)
	if err != nil {
		return nil, err
	}
	if len(exterior) < 3 || math.Abs(signedRingArea(exterior)) <= polygonEpsilon {
		return nil, nil
	}
	holes := make([][]roadPoint, 0, len(polygon.holes))
	for _, rawHole := range polygon.holes {
		hole, err := cleanRing(rawHole, budget)
		if err != nil {
			return nil, err
		}
		if len(hole) < 3 || math.Abs(signedRingArea(hole)) <= polygonEpsilon {
			continue
		}
		holes = append(holes, hole)
	}

	vertexCount := len(exterior)
	for _, hole := range holes {
		vertexCount += len(hole)
	}
	expectedTriangleCount := vertexCount + 2*len(holes) - 2
	if expectedTriangleCount > maxTileStyleTriangles {
		return nil, fmt.Errorf("%w: styled polygon exceeds %d-triangle limit", errPolygonResourceLimit, maxTileStyleTriangles)
	}

	vertices := make([]roadPoint, 0, vertexCount)
	vertices = append(vertices, exterior...)
	holeIndices := make([]int, 0, len(holes))
	for _, hole := range holes {
		holeIndices = append(holeIndices, len(vertices))
		vertices = append(vertices, hole...)
	}
	coordinates := make([]float64, 0, len(vertices)*2)
	for _, point := range vertices {
		coordinates = append(coordinates, point.X, point.Y)
	}

	if err := budget.consumeN(polygonEarcutBudgetCost(vertexCount, len(holes))); err != nil {
		return nil, err
	}
	indices, err := earcut.Earcut(coordinates, holeIndices, 2)
	if err != nil {
		return nil, fmt.Errorf("MVT polygon triangulation failed: %w", err)
	}
	if err := budget.consumeN(len(indices)); err != nil {
		return nil, err
	}
	if len(indices)%3 != 0 {
		return nil, errors.New("MVT polygon triangulation returned an incomplete triangle")
	}
	if len(indices) > expectedTriangleCount*3 {
		return nil, fmt.Errorf(
			"MVT polygon triangulation returned %d triangles, maximum is %d",
			len(indices)/3,
			expectedTriangleCount,
		)
	}

	triangles := make([]roadPoint, len(indices))
	for index, vertexIndex := range indices {
		if vertexIndex < 0 || vertexIndex >= len(vertices) {
			return nil, fmt.Errorf("MVT polygon triangulation returned out-of-range vertex index %d", vertexIndex)
		}
		triangles[index] = vertices[vertexIndex]
	}
	return triangles, nil
}

func polygonEarcutBudgetCost(vertexCount, holeCount int) int {
	// Account for indexed ear clipping and a conservative full scan per hole.
	levels := 1
	for remaining := vertexCount; remaining > 1; remaining = (remaining + 1) / 2 {
		levels++
	}
	return vertexCount * (levels + holeCount)
}

func cleanRing(ring []roadPoint, budget *triangulationBudget) ([]roadPoint, error) {
	points := make([]roadPoint, 0, len(ring))
	for _, point := range ring {
		if len(points) == 0 || points[len(points)-1] != point {
			points = append(points, point)
		}
	}
	if len(points) > 1 && points[0] == points[len(points)-1] {
		points = points[:len(points)-1]
	}
	for len(points) >= 3 {
		removed := false
		for index := range points {
			if err := budget.consume(); err != nil {
				return nil, err
			}
			previous := points[(index+len(points)-1)%len(points)]
			current := points[index]
			next := points[(index+1)%len(points)]
			if math.Abs(pointCross(previous, current, next)) > polygonEpsilon {
				continue
			}
			points = append(points[:index], points[index+1:]...)
			removed = true
			break
		}
		if !removed {
			break
		}
	}
	return points, nil
}

func reversePoints(points []roadPoint) {
	for left, right := 0, len(points)-1; left < right; left, right = left+1, right-1 {
		points[left], points[right] = points[right], points[left]
	}
}

func pointCross(first, second, third roadPoint) float64 {
	return (second.X-first.X)*(third.Y-first.Y) - (second.Y-first.Y)*(third.X-first.X)
}

func triangleContainsOtherPoint(
	points []roadPoint,
	indices []int,
	first, second, third int,
	budget *triangulationBudget,
) (bool, error) {
	for _, index := range indices {
		if index == first || index == second || index == third {
			continue
		}
		if err := budget.consume(); err != nil {
			return false, err
		}
		point := points[index]
		if point == points[first] || point == points[second] || point == points[third] {
			continue
		}
		if pointCross(points[first], points[second], point) >= -polygonEpsilon &&
			pointCross(points[second], points[third], point) >= -polygonEpsilon &&
			pointCross(points[third], points[first], point) >= -polygonEpsilon {
			return true, nil
		}
	}
	return false, nil
}

func (b *triangulationBudget) consume() error {
	return b.consumeN(1)
}

func (b *triangulationBudget) consumeN(amount int) error {
	if amount == 0 {
		return nil
	}
	if b == nil || amount < 0 || b.remaining < amount {
		return errPolygonResourceLimit
	}
	b.remaining -= amount
	return nil
}
