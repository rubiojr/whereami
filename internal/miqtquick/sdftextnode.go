package quick

/*
#cgo noescape QSGSDFAtlasNode_newTextNode
#cgo nocallback QSGSDFAtlasNode_newTextNode
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
	if item == nil || !validImageSize(len(pixels), width, height, 1) {
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
	if a == nil || a.h == nil || len(vertices) == 0 || len(vertices)%24 != 0 ||
		len(vertices)/4 > maxGeometryVertices || fontScale <= 0 {
		return nil
	}
	passes, count := sdfTextPasses(color, haloColor, fontScale, haloWidth, haloBlur)
	var buffers [2]*C.float
	node := C.QSGSDFAtlasNode_newTextNode(
		a.h,
		&buffers[0],
		C.int(len(vertices)/4),
		(*C.float)(unsafe.Pointer(&passes[0][0])),
		C.int(count),
	)
	if node != nil {
		// Qt owns the buffers, but cannot render them until this call returns.
		// Go's bulk copy uses the runtime's architecture-specific implementation.
		for _, buffer := range buffers[:count] {
			copy(unsafe.Slice((*float32)(unsafe.Pointer(buffer)), len(vertices)), vertices)
		}
	}
	runtime.KeepAlive(a)
	runtime.KeepAlive(vertices)
	runtime.KeepAlive(passes)
	return newQSGNode(node)
}
