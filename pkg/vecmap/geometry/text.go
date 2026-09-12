package geometry

import "github.com/rubiojr/whereami/pkg/vecmap/scene"

// TextVertex stores the existing float32 glyph/icon offset and atlas coordinates.
type TextVertex struct{ X, Y, U, V float32 }

func TextQuad(x1, y1, x2, y2, u1, v1, u2, v2 float64) [4]TextVertex {
	return [4]TextVertex{
		{float32(x1), float32(y1), float32(u1), float32(v1)},
		{float32(x2), float32(y1), float32(u2), float32(v1)},
		{float32(x2), float32(y2), float32(u2), float32(v2)},
		{float32(x1), float32(y2), float32(u1), float32(v2)},
	}
}

// TransformTextVertex keeps offsets in screen pixels relative to a tile anchor.
// sin and cos are computed once per label from its existing placement angle.
func TransformTextVertex(anchor Point, vertex TextVertex, offset Point, sin, cos float64) scene.Vertex {
	x, y := float64(vertex.X)+offset.X, float64(vertex.Y)+offset.Y
	return scene.Vertex{X: float32(anchor.X), Y: float32(anchor.Y), OffsetX: float32(x*cos - y*sin), OffsetY: float32(x*sin + y*cos), U: vertex.U, V: vertex.V}
}

// Quad preserves the existing glyph/background diagonal and winding.
func (b *Builder[T]) Quad(first, second, third, fourth T) error {
	return b.Append([]T{first, second, third, fourth}, []uint32{0, 1, 2, 0, 2, 3})
}
