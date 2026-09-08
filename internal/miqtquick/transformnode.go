package quick

/*
#include "transformnode.h"
*/
import "C"

import "unsafe"

// QSGTransformNode applies a matrix to its child scene-graph nodes.
type QSGTransformNode struct {
	h *C.QSGTransformNode
	*QSGNode
	m11, m12, m21, m22, dx, dy float32
	affineKnown                bool
}

// QSGClipNode clips its child scene-graph nodes to local geometry.
type QSGClipNode struct {
	h *C.QSGClipNode
	*QSGNode
}

func newQSGClipNode(handle *C.QSGClipNode) *QSGClipNode {
	if handle == nil {
		return nil
	}
	var node *C.QSGNode
	C.QSGClipNode_virtbase(handle, &node)
	return &QSGClipNode{h: handle, QSGNode: newQSGNode(node)}
}

func newQSGTransformNode(handle *C.QSGTransformNode) *QSGTransformNode {
	if handle == nil {
		return nil
	}
	var node *C.QSGNode
	C.QSGTransformNode_virtbase(handle, &node)
	return &QSGTransformNode{h: handle, QSGNode: newQSGNode(node), m11: 1, m22: 1}
}

// NewQSGTransformNode constructs a public Qt scene-graph transform node.
func NewQSGTransformNode() *QSGTransformNode {
	node := newQSGTransformNode(C.QSGTransformNode_new())
	if node != nil {
		node.affineKnown = true
	}
	return node
}

// NewQSGNode constructs a public Qt scene-graph node.
func NewQSGNode() *QSGNode {
	return newQSGNode(C.QSGNode_new())
}

// NewQSGClipNode constructs a rectangular public Qt scene-graph clip node.
func NewQSGClipNode() *QSGClipNode {
	return newQSGClipNode(C.QSGClipNode_new())
}

// UnsafeNewQSGTransformNode wraps a known QSGTransformNode pointer.
func UnsafeNewQSGTransformNode(handle unsafe.Pointer) *QSGTransformNode {
	return newQSGTransformNode((*C.QSGTransformNode)(handle))
}

// SetAffine sets the node's two-dimensional affine transform.
func (node *QSGTransformNode) SetAffine(m11, m12, m21, m22, dx, dy float32) {
	if node == nil || node.h == nil {
		return
	}
	if node.affineKnown && node.m11 == m11 && node.m12 == m12 && node.m21 == m21 && node.m22 == m22 && node.dx == dx && node.dy == dy {
		return
	}
	C.QSGTransformNode_setAffine(
		node.h,
		C.float(m11),
		C.float(m12),
		C.float(m21),
		C.float(m22),
		C.float(dx),
		C.float(dy),
	)
	node.m11, node.m12 = m11, m12
	node.m21, node.m22 = m21, m22
	node.dx, node.dy = dx, dy
	node.affineKnown = true
}

// MapPoint maps a point through the node matrix.
func (node *QSGTransformNode) MapPoint(x, y float32) (float32, float32) {
	return node.m11*x + node.m12*y + node.dx, node.m21*x + node.m22*y + node.dy
}

// SetRect sets the rectangular local clip.
func (node *QSGClipNode) SetRect(x, y, width, height float32) {
	if node == nil || node.h == nil {
		return
	}
	C.QSGClipNode_setRect(
		node.h,
		C.float(x),
		C.float(y),
		C.float(width),
		C.float(height),
	)
}

// AppendChildNode transfers child scene-graph ownership to this node.
func (node *QSGNode) AppendChildNode(child *QSGNode) {
	if node == nil || node.h == nil || child == nil || child.h == nil {
		return
	}
	C.QSGNode_appendChildNode(node.h, child.h)
}

// RemoveChildNode detaches a child without deleting it.
func (node *QSGNode) RemoveChildNode(child *QSGNode) {
	if node == nil || node.h == nil || child == nil || child.h == nil {
		return
	}
	C.QSGNode_removeChildNode(node.h, child.h)
}

// ChildCount returns the number of direct child nodes.
func (node *QSGNode) ChildCount() int {
	return int(C.QSGNode_childCount(node.h))
}
