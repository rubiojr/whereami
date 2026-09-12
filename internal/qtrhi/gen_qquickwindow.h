#pragma once
#ifndef MIQT_QTRHI_GEN_QQUICKWINDOW_H
#define MIQT_QTRHI_GEN_QQUICKWINDOW_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#pragma GCC diagnostic ignored "-Wdeprecated-declarations"

#include "../../vendor/github.com/mappu/miqt/libmiqt/libmiqt.h"

#ifdef __cplusplus
extern "C" {
#endif

#ifdef __cplusplus
class QImage;
class QQuickGraphicsConfiguration;
class QQuickWindow;
class QRhi;
#else
typedef struct QImage QImage;
typedef struct QQuickGraphicsConfiguration QQuickGraphicsConfiguration;
typedef struct QQuickWindow QQuickWindow;
typedef struct QRhi QRhi;
#endif

QImage* qtrhi_QQuickWindow_grabWindow(QQuickWindow* self);
double qtrhi_QQuickWindow_effectiveDevicePixelRatio(const QQuickWindow* self);
void qtrhi_QQuickWindow_setGraphicsConfiguration(QQuickWindow* self, QQuickGraphicsConfiguration* config);
QRhi* qtrhi_QQuickWindow_rhi(const QQuickWindow* self);


#ifdef __cplusplus
} /* extern C */
#endif

#endif
