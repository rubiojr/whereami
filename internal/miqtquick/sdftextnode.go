package quick

/*
#include "sdftextnode.h"
*/
import "C"

import (
	"runtime"
	"unsafe"
)

// QSGSDFAtlas owns one immutable SDF texture on Qt's render thread.
type QSGSDFAtlas struct {
	h    *C.QSGSDFAtlasNode
	node *QSGNode
}

// NewQSGSDFAtlas uploads a single-channel SDF atlas for reuse by text nodes.
func NewQSGSDFAtlas(item *QQuickItem, pixels []byte, width, height int) *QSGSDFAtlas {
	const maximumInt = int(^uint(0) >> 1)
	if item == nil || width <= 0 || height <= 0 || width > maximumInt/height || len(pixels) != width*height {
		return nil
	}
	atlas := C.QQuickItem_newSDFAtlasNode(
		(*C.QQuickItem)(item.UnsafePointer()),
		(*C.uchar)(unsafe.Pointer(&pixels[0])),
		C.int(width),
		C.int(height),
	)
	runtime.KeepAlive(item)
	runtime.KeepAlive(pixels)
	if atlas == nil {
		return nil
	}
	return &QSGSDFAtlas{h: atlas, node: newQSGNode(C.QSGSDFAtlasNode_node(atlas))}
}

// QSGNode returns the atlas lifetime node. Attach it after every text node that
// references the atlas so Qt destroys the texture last.
func (a *QSGSDFAtlas) QSGNode() *QSGNode {
	if a == nil {
		return nil
	}
	return a.node
}

// NewTextNode creates textured SDF triangles using this atlas.
func (a *QSGSDFAtlas) NewTextNode(
	vertices []float32,
	color, haloColor [4]int,
	fontScale, haloWidth, haloBlur float32,
) *QSGNode {
	if a == nil || a.h == nil || len(vertices) == 0 || len(vertices)%24 != 0 || fontScale <= 0 {
		return nil
	}
	colorValues := [4]C.int{C.int(color[0]), C.int(color[1]), C.int(color[2]), C.int(color[3])}
	haloValues := [4]C.int{C.int(haloColor[0]), C.int(haloColor[1]), C.int(haloColor[2]), C.int(haloColor[3])}
	node := C.QSGSDFAtlasNode_newTextNode(
		a.h,
		(*C.float)(unsafe.Pointer(&vertices[0])),
		C.int(len(vertices)/4),
		(*C.int)(unsafe.Pointer(&colorValues[0])),
		(*C.int)(unsafe.Pointer(&haloValues[0])),
		C.float(fontScale),
		C.float(haloWidth),
		C.float(haloBlur),
	)
	runtime.KeepAlive(a)
	runtime.KeepAlive(vertices)
	runtime.KeepAlive(colorValues)
	runtime.KeepAlive(haloValues)
	return newQSGNode(node)
}
