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
	if item == nil || len(points) == 0 || len(points)%6 != 0 ||
		imageWidth <= 0 || imageHeight <= 0 || len(rgba) != imageWidth*imageHeight*4 ||
		patternWidth <= 0 || patternHeight <= 0 {
		return nil
	}
	vertices := make([]float32, len(points)*2)
	for source := 0; source < len(points); source += 2 {
		target := source * 2
		x, y := points[source], points[source+1]
		vertices[target] = x
		vertices[target+1] = y
		vertices[target+2] = (x + phaseX) / patternWidth
		vertices[target+3] = (y + phaseY) / patternHeight
	}
	node := C.QQuickItem_newPatternNode(
		(*C.QQuickItem)(item.UnsafePointer()),
		(*C.float)(unsafe.Pointer(&vertices[0])),
		C.int(len(points)/2),
		(*C.uchar)(unsafe.Pointer(&rgba[0])),
		C.int(imageWidth),
		C.int(imageHeight),
		C.float(opacity),
	)
	runtime.KeepAlive(item)
	runtime.KeepAlive(vertices)
	runtime.KeepAlive(rgba)
	return newQSGNode(node)
}
