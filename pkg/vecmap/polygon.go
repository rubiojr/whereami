package vecmap

import (
	"errors"
	"math"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
)

const polygonEpsilon = geometry.Epsilon

var errPolygonResourceLimit = geometry.ErrResourceLimit

type triangulationBudget struct {
	remaining int
}

func fillTriangleCount(bucket fillBucket) int {
	return (len(bucket.triangles) + len(bucket.cutouts)) / 3
}

func signedRingArea(ring []roadPoint) float64 {
	return geometry.SignedRingArea(ring)
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
	core := budget.geometryBudget()
	triangles, err := geometry.TriangulatePolygonExpanded(polygon.exterior, polygon.holes, core)
	budget.updateGeometryBudget(core)
	return triangles, err
}

func cleanRing(ring []roadPoint, budget *triangulationBudget) ([]roadPoint, error) {
	core := budget.geometryBudget()
	points, err := geometry.CleanRing(ring, core)
	budget.updateGeometryBudget(core)
	return points, err
}

func (b *triangulationBudget) geometryBudget() *geometry.Budget {
	if b == nil {
		return nil
	}
	return &geometry.Budget{Remaining: b.remaining}
}

func (b *triangulationBudget) updateGeometryBudget(core *geometry.Budget) {
	if b != nil {
		b.remaining = core.Remaining
	}
}

func reversePoints(points []roadPoint) {
	for left, right := 0, len(points)-1; left < right; left, right = left+1, right-1 {
		points[left], points[right] = points[right], points[left]
	}
}

func pointCross(first, second, third roadPoint) float64 {
	return geometry.Cross(first, second, third)
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
