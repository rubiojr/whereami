#include <QSGGeometry>
#define WORKAROUND_INNER_CLASS_DEFINITION_QSGGeometry__AttributeSet
#define WORKAROUND_INNER_CLASS_DEFINITION_QSGGeometry__Point2D
#include <qsggeometry.h>
#include "gen_qsggeometry.h"

#ifdef __cplusplus
extern "C" {
#endif

#ifdef __cplusplus
} /* extern C */
#endif

QSGGeometry* QSGGeometry_new(QSGGeometry__AttributeSet* attribs, int vertexCount) {
	return new (std::nothrow) QSGGeometry(*attribs, static_cast<int>(vertexCount));
}

QSGGeometry* QSGGeometry_new2(QSGGeometry__AttributeSet* attribs, int vertexCount, int indexCount) {
	return new (std::nothrow) QSGGeometry(*attribs, static_cast<int>(vertexCount), static_cast<int>(indexCount));
}

QSGGeometry* QSGGeometry_new3(QSGGeometry__AttributeSet* attribs, int vertexCount, int indexCount, int indexType) {
	return new (std::nothrow) QSGGeometry(*attribs, static_cast<int>(vertexCount), static_cast<int>(indexCount), static_cast<int>(indexType));
}

QSGGeometry__AttributeSet* QSGGeometry_defaultAttributes_Point2D() {
	const QSGGeometry::AttributeSet& _ret = QSGGeometry::defaultAttributes_Point2D();
	// Cast returned reference into pointer
	return const_cast<QSGGeometry::AttributeSet*>(&_ret);
}

void QSGGeometry_setDrawingMode(QSGGeometry* self, unsigned int mode) {
	self->setDrawingMode(static_cast<unsigned int>(mode));
}

QSGGeometry__Point2D* QSGGeometry_vertexDataAsPoint2D(QSGGeometry* self) {
	return self->vertexDataAsPoint2D();
}

QSGGeometry__Point2D* QSGGeometry_vertexDataAsPoint2D2(const QSGGeometry* self) {
	return (QSGGeometry__Point2D*) self->vertexDataAsPoint2D();
}

void QSGGeometry_markVertexDataDirty(QSGGeometry* self) {
	self->markVertexDataDirty();
}

void QSGGeometry_delete(QSGGeometry* self) {
	delete self;
}

float QSGGeometry__Point2D_x(const QSGGeometry__Point2D* self) {
	return self->x;
}

void QSGGeometry__Point2D_setX(QSGGeometry__Point2D* self, float x) {
	self->x = static_cast<float>(x);
}

float QSGGeometry__Point2D_y(const QSGGeometry__Point2D* self) {
	return self->y;
}

void QSGGeometry__Point2D_setY(QSGGeometry__Point2D* self, float y) {
	self->y = static_cast<float>(y);
}

void QSGGeometry__Point2D_set(QSGGeometry__Point2D* self, float nx, float ny) {
	self->set(static_cast<float>(nx), static_cast<float>(ny));
}
