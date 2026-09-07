#include <QColor>
#include <QSGFlatColorMaterial>
#include <QSGMaterial>
#include <qsgflatcolormaterial.h>
#include "gen_qsgflatcolormaterial.h"

#ifdef __cplusplus
extern "C" {
#endif

#ifdef __cplusplus
} /* extern C */
#endif

QSGFlatColorMaterial* QSGFlatColorMaterial_new() {
	return new (std::nothrow) QSGFlatColorMaterial();
}

void QSGFlatColorMaterial_virtbase(QSGFlatColorMaterial* src, QSGMaterial** outptr_QSGMaterial) {
	*outptr_QSGMaterial = static_cast<QSGMaterial*>(src);
}

void QSGFlatColorMaterial_setColor(QSGFlatColorMaterial* self, QColor* color) {
	self->setColor(*color);
}

void QSGFlatColorMaterial_delete(QSGFlatColorMaterial* self) {
	delete self;
}
