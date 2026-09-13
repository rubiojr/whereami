// Package geometry prepares toolkit-neutral map meshes using the existing
// bounded polygon, line and symbol-quad algorithms, including Go Earcut.
package geometry

import (
	"errors"
	"fmt"
	"math"

	"github.com/rubiojr/whereami/internal/earcut"
)

const (
	Epsilon             = 1e-9
	MaxPolygonPoints    = 32_768
	MaxPolygonTriangles = 500_000
)

var ErrResourceLimit = errors.New("MVT polygon resource limit exceeded")

// Point is a tile-local position, retaining float64 precision during preparation.
type Point struct{ X, Y float64 }

// Mesh preserves construction order without expanding shared vertices.
// The slices are owned by the result; input rings are never modified or retained.
// Treat published results as immutable. Non-nil Indices address Vertices;
// a nil index slice represents an expanded triangle list.
type Mesh struct {
	Vertices []Point
	Indices  []uint32
}

// Budget is a shared preparation work allowance. It is not safe for concurrent
// use. Like the existing compiler budget, Earcut work is charged conservatively
// before calling it, rather than instrumenting the triangulator's inner loop.
type Budget struct{ Remaining int }

func (b *Budget) Consume(amount int) error {
	if amount == 0 {
		return nil
	}
	if b == nil || amount < 0 || b.Remaining < amount {
		return ErrResourceLimit
	}
	b.Remaining -= amount
	return nil
}

// TriangulatePolygon cleans the exterior and holes, applies the compiler's
// resource limits, and retains the existing Earcut result as uint32 indices.
// Rings must contain finite tile-local coordinates, as validated by the decoder.
// Degenerate exteriors produce an empty mesh; degenerate holes are ignored.
func TriangulatePolygon(exterior []Point, holes [][]Point, budget *Budget) (Mesh, error) {
	mesh, err := triangulatePolygon(exterior, holes, budget)
	if err != nil {
		return Mesh{}, err
	}
	if mesh.indices == nil {
		return Mesh{}, nil
	}
	indices := make([]uint32, len(mesh.indices))
	for i, index := range mesh.indices {
		indices[i] = uint32(index)
	}
	return Mesh{Vertices: mesh.vertices, Indices: indices}, nil
}

// TriangulatePolygonExpanded is the compatibility path for triangle-list
// consumers. It expands Earcut indices directly, without allocating an
// intermediate uint32 index buffer. Both entry points use identical preparation.
func TriangulatePolygonExpanded(exterior []Point, holes [][]Point, budget *Budget) ([]Point, error) {
	mesh, err := triangulatePolygon(exterior, holes, budget)
	if err != nil {
		return nil, err
	}
	if mesh.indices == nil {
		return nil, nil
	}
	triangles := make([]Point, len(mesh.indices))
	for i, index := range mesh.indices {
		triangles[i] = mesh.vertices[index]
	}
	return triangles, nil
}

type polygonMesh struct {
	vertices []Point
	indices  []int
}

func triangulatePolygon(exterior []Point, holes [][]Point, budget *Budget) (polygonMesh, error) {
	rawCount := len(exterior)
	if rawCount > MaxPolygonPoints {
		return polygonMesh{}, pointLimit()
	}
	for _, hole := range holes {
		if len(hole) > MaxPolygonPoints-rawCount {
			return polygonMesh{}, pointLimit()
		}
		rawCount += len(hole)
	}
	if err := budget.Consume(rawCount); err != nil {
		return polygonMesh{}, err
	}
	cleanExterior, err := CleanRing(exterior, budget)
	if err != nil {
		return polygonMesh{}, err
	}
	if len(cleanExterior) < 3 || math.Abs(SignedRingArea(cleanExterior)) <= Epsilon {
		return polygonMesh{}, nil
	}
	cleanHoles, err := cleanPolygonHoles(holes, budget)
	if err != nil {
		return polygonMesh{}, err
	}
	return triangulateCleanPolygon(cleanExterior, cleanHoles, budget)
}

func pointLimit() error {
	return fmt.Errorf("%w: styled polygon exceeds %d-point limit", ErrResourceLimit, MaxPolygonPoints)
}

func cleanPolygonHoles(holes [][]Point, budget *Budget) ([][]Point, error) {
	cleaned := make([][]Point, 0, len(holes))
	for _, raw := range holes {
		hole, err := CleanRing(raw, budget)
		if err != nil {
			return nil, err
		}
		if len(hole) < 3 || math.Abs(SignedRingArea(hole)) <= Epsilon {
			continue
		}
		cleaned = append(cleaned, hole)
	}
	return cleaned, nil
}

func triangulateCleanPolygon(exterior []Point, holes [][]Point, budget *Budget) (polygonMesh, error) {
	vertexCount := len(exterior)
	for _, hole := range holes {
		vertexCount += len(hole)
	}
	expectedTriangles := vertexCount + 2*len(holes) - 2
	if expectedTriangles > MaxPolygonTriangles {
		return polygonMesh{}, fmt.Errorf("%w: styled polygon exceeds %d-triangle limit", ErrResourceLimit, MaxPolygonTriangles)
	}
	vertices := make([]Point, 0, vertexCount)
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
	if err := budget.Consume(earcutBudgetCost(vertexCount, len(holes))); err != nil {
		return polygonMesh{}, err
	}
	indices, err := earcut.Earcut(coordinates, holeIndices, 2)
	if err != nil {
		return polygonMesh{}, fmt.Errorf("MVT polygon triangulation failed: %w", err)
	}
	if err := budget.Consume(len(indices)); err != nil {
		return polygonMesh{}, err
	}
	if err := checkIndices(indices, len(vertices), expectedTriangles); err != nil {
		return polygonMesh{}, err
	}
	return polygonMesh{vertices: vertices, indices: indices}, nil
}

func checkIndices(indices []int, vertexCount, expectedTriangles int) error {
	if len(indices)%3 != 0 {
		return errors.New("MVT polygon triangulation returned an incomplete triangle")
	}
	if len(indices) > expectedTriangles*3 {
		return fmt.Errorf("MVT polygon triangulation returned %d triangles, maximum is %d", len(indices)/3, expectedTriangles)
	}
	for _, vertexIndex := range indices {
		if vertexIndex < 0 || vertexIndex >= vertexCount {
			return fmt.Errorf("MVT polygon triangulation returned out-of-range vertex index %d", vertexIndex)
		}
	}
	return nil
}

func earcutBudgetCost(vertexCount, holeCount int) int {
	levels := 1
	for remaining := vertexCount; remaining > 1; remaining = (remaining + 1) / 2 {
		levels++
	}
	return vertexCount * (levels + holeCount)
}

func SignedRingArea(ring []Point) float64 {
	area := 0.0
	for index, point := range ring {
		next := ring[(index+1)%len(ring)]
		area += point.X*next.Y - point.Y*next.X
	}
	return area / 2
}

// CleanRing returns an owned copy without closing duplicates or collinear
// points. Cleanup order and operation accounting match the original compiler.
func CleanRing(ring []Point, budget *Budget) ([]Point, error) {
	points := make([]Point, 0, len(ring))
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
			if err := budget.Consume(1); err != nil {
				return nil, err
			}
			previous := points[(index+len(points)-1)%len(points)]
			current := points[index]
			next := points[(index+1)%len(points)]
			if math.Abs(Cross(previous, current, next)) > Epsilon {
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

func Cross(first, second, third Point) float64 {
	return (second.X-first.X)*(third.Y-first.Y) - (second.Y-first.Y)*(third.X-first.X)
}
