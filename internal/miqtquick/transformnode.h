#pragma once

#include "gen_qsgnode.h"

#ifdef __cplusplus
class QSGTransformNode;
class QSGClipNode;
extern "C" {
#else
typedef struct QSGTransformNode QSGTransformNode;
typedef struct QSGClipNode QSGClipNode;
#endif

QSGTransformNode* QSGTransformNode_new();
QSGClipNode* QSGClipNode_new();
QSGNode* QSGNode_new();
void QSGTransformNode_virtbase(QSGTransformNode* source, QSGNode** node);
void QSGTransformNode_setAffine(
    QSGTransformNode* self,
    float m11,
    float m12,
    float m21,
    float m22,
    float dx,
    float dy);
void QSGClipNode_virtbase(QSGClipNode* source, QSGNode** node);
void QSGClipNode_setRect(QSGClipNode* self, float x, float y, float width, float height);
void QSGNode_appendChildNode(QSGNode* self, QSGNode* child);
void QSGNode_removeChildNode(QSGNode* self, QSGNode* child);
int QSGNode_childCount(const QSGNode* self);

#ifdef __cplusplus
}
#endif
