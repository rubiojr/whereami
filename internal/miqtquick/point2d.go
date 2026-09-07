package quick

import (
	"runtime"
	"unsafe"
)

// NewQSGPointGeometry creates geometry from alternating x/y values.
// QSGGeometry::Point2D is a public pair of adjacent floats, so its C-owned
// vertex buffer can be populated directly without a per-vertex C++ bridge.
func NewQSGPointGeometry(coordinates []float32) *QSGGeometry {
	if len(coordinates) == 0 || len(coordinates)%2 != 0 {
		return nil
	}
	geometry := NewQSGGeometry(QSGGeometry_DefaultAttributes_Point2D(), len(coordinates)/2)
	if geometry == nil {
		return nil
	}
	vertices := geometry.VertexDataAsPoint2D()
	if vertices == nil {
		geometry.Delete()
		return nil
	}
	copy(unsafe.Slice((*float32)(vertices.UnsafePointer()), len(coordinates)), coordinates)
	runtime.KeepAlive(coordinates)
	runtime.KeepAlive(vertices)
	return geometry
}
