package quick

/*
#include "patternnode.h"
*/
import "C"

import (
	"runtime"
	"unsafe"
)

// NewQSGPatternNode creates textured triangles with a repeating RGBA image.
func NewQSGPatternNode(
	item *QQuickItem,
	points []float32,
	rgba []byte,
	imageWidth, imageHeight int,
	patternWidth, patternHeight, phaseX, phaseY, opacity float32,
) *QSGNode {
	if item == nil || len(points) == 0 || len(points)%6 != 0 || len(points)/2 > maxGeometryVertices ||
		!validImageSize(len(rgba), imageWidth, imageHeight, 4) ||
		patternWidth <= 0 || patternHeight <= 0 {
		return nil
	}
	var vertices *C.float
	node := C.QQuickItem_newPatternNode(
		(*C.QQuickItem)(item.UnsafePointer()),
		&vertices,
		C.int(len(points)/2),
		(*C.uchar)(unsafe.Pointer(&rgba[0])),
		C.int(imageWidth),
		C.int(imageHeight),
		C.float(opacity),
	)
	if node != nil {
		// The node owns this buffer. Populate it before publishing the node to Qt;
		// no Go pointer is retained by C++ and no intermediate vertex slice is needed.
		writePatternVertices(unsafe.Slice((*float32)(unsafe.Pointer(vertices)), len(points)*2),
			points, patternWidth, patternHeight, phaseX, phaseY)
	}
	runtime.KeepAlive(item)
	runtime.KeepAlive(points)
	runtime.KeepAlive(rgba)
	return newQSGNode(node)
}
