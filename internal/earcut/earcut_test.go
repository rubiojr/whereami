package earcut

import (
	"math"
	"testing"
)

func TestEarcutLargePolygonUsesAccurateZOrder(t *testing.T) {
	const vertices = 256
	data := make([]float64, 0, vertices*2)
	for index := range vertices {
		angle := float64(index) * 2 * math.Pi / vertices
		data = append(data, math.Cos(angle), math.Sin(angle))
	}

	triangles, err := Earcut(data, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(triangles) != (vertices-2)*3 {
		t.Fatalf("got %d triangle indices, want %d", len(triangles), (vertices-2)*3)
	}
	if deviation := Deviation(data, nil, 2, triangles); deviation > 1e-12 {
		t.Fatalf("triangulation deviation %g exceeds tolerance", deviation)
	}
	if first, last := zOrder(-1, -1, -1, -1, 0.5), zOrder(1, 1, -1, -1, 0.5); first == last {
		t.Fatal("z-order quantization collapsed distinct bounds")
	}
}

func TestEarcutSkipsEmptyHoles(t *testing.T) {
	data := []float64{0, 0, 10, 0, 10, 10, 0, 10}
	triangles, err := Earcut(data, []int{4, 4}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(triangles) != 6 {
		t.Fatalf("got %d triangle indices, want 6", len(triangles))
	}
}
