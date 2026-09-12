#pragma once
#ifndef MIQT_QTRHI_GEN_QQUICKGRAPHICSCONFIGURATION_H
#define MIQT_QTRHI_GEN_QQUICKGRAPHICSCONFIGURATION_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#pragma GCC diagnostic ignored "-Wdeprecated-declarations"

#include "../../vendor/github.com/mappu/miqt/libmiqt/libmiqt.h"

#ifdef __cplusplus
extern "C" {
#endif

#ifdef __cplusplus
class QQuickGraphicsConfiguration;
#else
typedef struct QQuickGraphicsConfiguration QQuickGraphicsConfiguration;
#endif

QQuickGraphicsConfiguration* qtrhi_QQuickGraphicsConfiguration_new();
QQuickGraphicsConfiguration* qtrhi_QQuickGraphicsConfiguration_new2(QQuickGraphicsConfiguration* other);
void qtrhi_QQuickGraphicsConfiguration_setTimestamps(QQuickGraphicsConfiguration* self, bool enable);

void qtrhi_QQuickGraphicsConfiguration_delete(QQuickGraphicsConfiguration* self);

#ifdef __cplusplus
} /* extern C */
#endif

#endif
