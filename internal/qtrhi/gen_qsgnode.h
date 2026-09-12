#pragma once
#ifndef MIQT_QTRHI_GEN_QSGNODE_H
#define MIQT_QTRHI_GEN_QSGNODE_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#pragma GCC diagnostic ignored "-Wdeprecated-declarations"

#include "../../vendor/github.com/mappu/miqt/libmiqt/libmiqt.h"

#ifdef __cplusplus
extern "C" {
#endif

#ifdef __cplusplus
class QSGNode;
#else
typedef struct QSGNode QSGNode;
#endif

QSGNode* qtrhi_QSGNode_new();
void qtrhi_QSGNode_delete(QSGNode* self);

#ifdef __cplusplus
} /* extern C */
#endif

#endif
