package quick

/*
#include "imagenode.h"
*/
import "C"

import (
	"runtime"
	"unsafe"
)

// NewQSGRGBAImageNode creates an image node from tightly packed RGBA pixels.
func NewQSGRGBAImageNode(
	item *QQuickItem,
	data []byte,
	imageWidth, imageHeight int,
	x, y, width, height, opacity float32,
) *QSGNode {
	if item == nil || imageWidth <= 0 || imageHeight <= 0 || len(data) != imageWidth*imageHeight*4 {
		return nil
	}
	node := C.QQuickItem_newRGBAImageNode(
		(*C.QQuickItem)(item.UnsafePointer()),
		(*C.uchar)(unsafe.Pointer(&data[0])),
		C.int(imageWidth),
		C.int(imageHeight),
		C.float(x),
		C.float(y),
		C.float(width),
		C.float(height),
		C.float(opacity),
	)
	runtime.KeepAlive(item)
	runtime.KeepAlive(data)
	return newQSGNode(node)
}
