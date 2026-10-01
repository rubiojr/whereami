#pragma once
#ifndef MIQT_QTRHI_GEN_QLIBRARY_H
#define MIQT_QTRHI_GEN_QLIBRARY_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#pragma GCC diagnostic ignored "-Wdeprecated-declarations"

#include "../../vendor/github.com/mappu/miqt/libmiqt/libmiqt.h"

#ifdef __cplusplus
extern "C" {
#endif

#ifdef __cplusplus
class QLibrary;
class QObject;
#else
typedef struct QLibrary QLibrary;
typedef struct QObject QObject;
#endif

void qtrhi_QLibrary_virtbase(QLibrary* src, QObject** outptr_QObject);
bool qtrhi_QLibrary_resolve(struct miqt_string fileName, int verNum, const char* symbol);


#ifdef __cplusplus
} /* extern C */
#endif

#endif
