#pragma once
#ifndef MIQT_QT6_QUICK_GEN_QSGFLATCOLORMATERIAL_H
#define MIQT_QT6_QUICK_GEN_QSGFLATCOLORMATERIAL_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#pragma GCC diagnostic ignored "-Wdeprecated-declarations"

#ifdef __cplusplus
extern "C" {
#endif

#ifdef __cplusplus
class QColor;
class QSGFlatColorMaterial;
class QSGMaterial;
#else
typedef struct QColor QColor;
typedef struct QSGFlatColorMaterial QSGFlatColorMaterial;
typedef struct QSGMaterial QSGMaterial;
#endif

QSGFlatColorMaterial* QSGFlatColorMaterial_new();
void QSGFlatColorMaterial_virtbase(QSGFlatColorMaterial* src, QSGMaterial** outptr_QSGMaterial);
void QSGFlatColorMaterial_setColor(QSGFlatColorMaterial* self, QColor* color);

void QSGFlatColorMaterial_delete(QSGFlatColorMaterial* self);

#ifdef __cplusplus
} /* extern C */
#endif

#endif
