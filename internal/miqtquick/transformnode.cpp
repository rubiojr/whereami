#include "transformnode.h"

#include <new>
#include <QMatrix4x4>
#include <QRectF>
#include <QSGGeometry>
#include <QSGNode>
#include <QSGClipNode>
#include <QSGTransformNode>

QSGTransformNode* QSGTransformNode_new() {
    return new (std::nothrow) QSGTransformNode();
}

QSGClipNode* QSGClipNode_new() {
    return new (std::nothrow) QSGClipNode();
}

QSGNode* QSGNode_new() {
    return new (std::nothrow) QSGNode();
}

void QSGTransformNode_virtbase(QSGTransformNode* source, QSGNode** node) {
    *node = static_cast<QSGNode*>(source);
}

void QSGTransformNode_setAffine(
    QSGTransformNode* self,
    float m11,
    float m12,
    float m21,
    float m22,
    float dx,
    float dy) {
    QMatrix4x4 matrix;
    matrix(0, 0) = m11;
    matrix(0, 1) = m12;
    matrix(0, 3) = dx;
    matrix(1, 0) = m21;
    matrix(1, 1) = m22;
    matrix(1, 3) = dy;
    self->setMatrix(matrix);
}

void QSGClipNode_virtbase(QSGClipNode* source, QSGNode** node) {
    *node = static_cast<QSGNode*>(source);
}

void QSGClipNode_setRect(QSGClipNode* self, float x, float y, float width, float height) {
    QSGGeometry* geometry = self->geometry();
    if (geometry == nullptr) {
        geometry = new (std::nothrow) QSGGeometry(QSGGeometry::defaultAttributes_Point2D(), 4);
        if (geometry != nullptr) {
            geometry->setDrawingMode(QSGGeometry::DrawTriangleStrip);
            self->setGeometry(geometry);
            self->setFlag(QSGNode::OwnsGeometry);
        }
    }
    if (geometry != nullptr) {
        QSGGeometry::Point2D* points = geometry->vertexDataAsPoint2D();
        points[0].set(x, y);
        points[1].set(x, y + height);
        points[2].set(x + width, y);
        points[3].set(x + width, y + height);
        self->markDirty(QSGNode::DirtyGeometry);
    }
    self->setIsRectangular(true);
    self->setClipRect(QRectF(x, y, width, height));
}

void QSGNode_appendChildNode(QSGNode* self, QSGNode* child) {
    self->appendChildNode(child);
}

void QSGNode_removeChildNode(QSGNode* self, QSGNode* child) {
    self->removeChildNode(child);
}

int QSGNode_childCount(const QSGNode* self) {
    int count = 0;
    for (const QSGNode* child = self->firstChild(); child != nullptr; child = child->nextSibling()) {
        ++count;
    }
    return count;
}
