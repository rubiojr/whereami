#include <QSGBasicGeometryNode>
#include <QSGGeometry>
#include <QSGGeometryNode>
#include <QSGMaterial>
#include <QSGNode>
#include <qsgnode.h>
#include "gen_qsgnode.h"

#ifdef __cplusplus
extern "C" {
#endif

#ifdef __cplusplus
} /* extern C */
#endif

void QSGNode_markDirty(QSGNode* self, int bits) {
	self->markDirty(static_cast<QSGNode::DirtyState>(bits));
}

void QSGNode_setFlag(QSGNode* self, int param1) {
	self->setFlag(static_cast<QSGNode::Flag>(param1));
}

void QSGNode_setFlag2(QSGNode* self, int param1, bool param2) {
	self->setFlag(static_cast<QSGNode::Flag>(param1), param2);
}

void QSGNode_delete(QSGNode* self) {
	delete self;
}
void QSGBasicGeometryNode_virtbase(QSGBasicGeometryNode* src, QSGNode** outptr_QSGNode) {
	*outptr_QSGNode = static_cast<QSGNode*>(src);
}

void QSGBasicGeometryNode_setGeometry(QSGBasicGeometryNode* self, QSGGeometry* geometry) {
	self->setGeometry(geometry);
}

QSGGeometry* QSGBasicGeometryNode_geometry(const QSGBasicGeometryNode* self) {
	return (QSGGeometry*) self->geometry();
}

QSGGeometry* QSGBasicGeometryNode_geometry2(QSGBasicGeometryNode* self) {
	return self->geometry();
}

void QSGBasicGeometryNode_delete(QSGBasicGeometryNode* self) {
	delete self;
}

QSGGeometryNode* QSGGeometryNode_new() {
	return new (std::nothrow) QSGGeometryNode();
}

void QSGGeometryNode_virtbase(QSGGeometryNode* src, QSGBasicGeometryNode** outptr_QSGBasicGeometryNode) {
	*outptr_QSGBasicGeometryNode = static_cast<QSGBasicGeometryNode*>(src);
}

void QSGGeometryNode_setMaterial(QSGGeometryNode* self, QSGMaterial* material) {
	self->setMaterial(material);
}

QSGMaterial* QSGGeometryNode_material(const QSGGeometryNode* self) {
	return self->material();
}

void QSGGeometryNode_delete(QSGGeometryNode* self) {
	delete self;
}
