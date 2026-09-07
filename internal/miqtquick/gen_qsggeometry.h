#pragma once
#ifndef MIQT_QT6_QUICK_GEN_QSGGEOMETRY_H
#define MIQT_QT6_QUICK_GEN_QSGGEOMETRY_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#pragma GCC diagnostic ignored "-Wdeprecated-declarations"

#ifdef __cplusplus
extern "C" {
#endif

#ifdef __cplusplus
class QSGGeometry;
#if defined(WORKAROUND_INNER_CLASS_DEFINITION_QSGGeometry__AttributeSet)
typedef QSGGeometry::AttributeSet QSGGeometry__AttributeSet;
#else
class QSGGeometry__AttributeSet;
#endif
#if defined(WORKAROUND_INNER_CLASS_DEFINITION_QSGGeometry__Point2D)
typedef QSGGeometry::Point2D QSGGeometry__Point2D;
#else
class QSGGeometry__Point2D;
#endif
#else
typedef struct QSGGeometry QSGGeometry;
typedef struct QSGGeometry__AttributeSet QSGGeometry__AttributeSet;
typedef struct QSGGeometry__Point2D QSGGeometry__Point2D;
#endif

QSGGeometry* QSGGeometry_new(QSGGeometry__AttributeSet* attribs, int vertexCount);
QSGGeometry* QSGGeometry_new2(QSGGeometry__AttributeSet* attribs, int vertexCount, int indexCount);
QSGGeometry* QSGGeometry_new3(QSGGeometry__AttributeSet* attribs, int vertexCount, int indexCount, int indexType);
QSGGeometry__AttributeSet* QSGGeometry_defaultAttributes_Point2D();
void QSGGeometry_setDrawingMode(QSGGeometry* self, unsigned int mode);
QSGGeometry__Point2D* QSGGeometry_vertexDataAsPoint2D(QSGGeometry* self);
QSGGeometry__Point2D* QSGGeometry_vertexDataAsPoint2D2(const QSGGeometry* self);
void QSGGeometry_markVertexDataDirty(QSGGeometry* self);

void QSGGeometry_delete(QSGGeometry* self);

float QSGGeometry__Point2D_x(const QSGGeometry__Point2D* self);
void QSGGeometry__Point2D_setX(QSGGeometry__Point2D* self, float x);
float QSGGeometry__Point2D_y(const QSGGeometry__Point2D* self);
void QSGGeometry__Point2D_setY(QSGGeometry__Point2D* self, float y);
void QSGGeometry__Point2D_set(QSGGeometry__Point2D* self, float nx, float ny);

#ifdef __cplusplus
} /* extern C */
#endif

#endif
