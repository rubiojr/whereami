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
class QWindow;
#else
typedef struct QImage QImage;
typedef struct QQuickGraphicsConfiguration QQuickGraphicsConfiguration;
typedef struct QQuickWindow QQuickWindow;
typedef struct QRhi QRhi;
typedef struct QWindow QWindow;
#endif

void qtrhi_QQuickWindow_virtbase(QQuickWindow* src, QWindow** outptr_QWindow);
QImage* qtrhi_QQuickWindow_grabWindow(QQuickWindow* self);
double qtrhi_QQuickWindow_effectiveDevicePixelRatio(const QQuickWindow* self);
void qtrhi_QQuickWindow_setGraphicsConfiguration(QQuickWindow* self, QQuickGraphicsConfiguration* config);
QRhi* qtrhi_QQuickWindow_rhi(const QQuickWindow* self);
void qtrhi_QQuickWindow_beforeSynchronizing(QQuickWindow* self);
void* qtrhi_QQuickWindow_connect_beforeSynchronizing(QQuickWindow* self, intptr_t slot);
void qtrhi_QQuickWindow_beforeRendering(QQuickWindow* self);
void* qtrhi_QQuickWindow_connect_beforeRendering(QQuickWindow* self, intptr_t slot);
void qtrhi_QQuickWindow_afterRendering(QQuickWindow* self);
void* qtrhi_QQuickWindow_connect_afterRendering(QQuickWindow* self, intptr_t slot);
void qtrhi_QQuickWindow_beforeFrameBegin(QQuickWindow* self);
void* qtrhi_QQuickWindow_connect_beforeFrameBegin(QQuickWindow* self, intptr_t slot);
void qtrhi_QQuickWindow_afterFrameEnd(QQuickWindow* self);
void* qtrhi_QQuickWindow_connect_afterFrameEnd(QQuickWindow* self, intptr_t slot);


#ifdef __cplusplus
} /* extern C */
#endif

#endif
