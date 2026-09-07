#pragma once
#ifndef MIQT_QT6_QUICK_GEN_QSGNODE_H
#define MIQT_QT6_QUICK_GEN_QSGNODE_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#pragma GCC diagnostic ignored "-Wdeprecated-declarations"

#ifdef __cplusplus
extern "C" {
#endif

#ifdef __cplusplus
class QSGBasicGeometryNode;
class QSGGeometry;
class QSGGeometryNode;
class QSGMaterial;
class QSGNode;
#else
typedef struct QSGBasicGeometryNode QSGBasicGeometryNode;
typedef struct QSGGeometry QSGGeometry;
typedef struct QSGGeometryNode QSGGeometryNode;
typedef struct QSGMaterial QSGMaterial;
typedef struct QSGNode QSGNode;
#endif

void QSGNode_markDirty(QSGNode* self, int bits);
void QSGNode_setFlag(QSGNode* self, int param1);
void QSGNode_setFlag2(QSGNode* self, int param1, bool param2);

void QSGNode_delete(QSGNode* self);

void QSGBasicGeometryNode_virtbase(QSGBasicGeometryNode* src, QSGNode** outptr_QSGNode);
void QSGBasicGeometryNode_setGeometry(QSGBasicGeometryNode* self, QSGGeometry* geometry);
QSGGeometry* QSGBasicGeometryNode_geometry(const QSGBasicGeometryNode* self);
QSGGeometry* QSGBasicGeometryNode_geometry2(QSGBasicGeometryNode* self);

void QSGBasicGeometryNode_delete(QSGBasicGeometryNode* self);

QSGGeometryNode* QSGGeometryNode_new();
void QSGGeometryNode_virtbase(QSGGeometryNode* src, QSGBasicGeometryNode** outptr_QSGBasicGeometryNode);
void QSGGeometryNode_setMaterial(QSGGeometryNode* self, QSGMaterial* material);
QSGMaterial* QSGGeometryNode_material(const QSGGeometryNode* self);

void QSGGeometryNode_delete(QSGGeometryNode* self);

#ifdef __cplusplus
} /* extern C */
#endif

#endif
