#pragma once
#ifndef MIQT_QTRHI_GEN_QSHADER_H
#define MIQT_QTRHI_GEN_QSHADER_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#pragma GCC diagnostic ignored "-Wdeprecated-declarations"

#include "../../vendor/github.com/mappu/miqt/libmiqt/libmiqt.h"

#ifdef __cplusplus
extern "C" {
#endif

#ifdef __cplusplus
class QShader;
#else
typedef struct QShader QShader;
#endif

QShader* qtrhi_QShader_new();
QShader* qtrhi_QShader_new2(QShader* other);
bool qtrhi_QShader_isValid(const QShader* self);
QShader* qtrhi_QShader_fromSerialized(struct miqt_string data);

void qtrhi_QShader_delete(QShader* self);

#ifdef __cplusplus
} /* extern C */
#endif

#endif
