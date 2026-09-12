#pragma once
#ifndef MIQT_QTRHI_GEN_QQUICKITEM_H
#define MIQT_QTRHI_GEN_QQUICKITEM_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#pragma GCC diagnostic ignored "-Wdeprecated-declarations"

#include "../../vendor/github.com/mappu/miqt/libmiqt/libmiqt.h"

#ifdef __cplusplus
extern "C" {
#endif

#ifdef __cplusplus
class QObject;
class QQmlParserStatus;
class QQuickItem;
#if defined(WORKAROUND_INNER_CLASS_DEFINITION_QQuickItem__UpdatePaintNodeData)
typedef QQuickItem::UpdatePaintNodeData QQuickItem__UpdatePaintNodeData;
#else
class QQuickItem__UpdatePaintNodeData;
#endif
class QQuickWindow;
class QSGNode;
#else
typedef struct QObject QObject;
typedef struct QQmlParserStatus QQmlParserStatus;
typedef struct QQuickItem QQuickItem;
typedef struct QQuickItem__UpdatePaintNodeData QQuickItem__UpdatePaintNodeData;
typedef struct QQuickWindow QQuickWindow;
typedef struct QSGNode QSGNode;
#endif

QQuickItem* qtrhi_QQuickItem_new();
QQuickItem* qtrhi_QQuickItem_new2(QQuickItem* parent);
void qtrhi_QQuickItem_virtbase(QQuickItem* src, QObject** outptr_QObject, QQmlParserStatus** outptr_QQmlParserStatus);
QQuickWindow* qtrhi_QQuickItem_window(const QQuickItem* self);
double qtrhi_QQuickItem_width(const QQuickItem* self);
void qtrhi_QQuickItem_setWidth(QQuickItem* self, double width);
double qtrhi_QQuickItem_height(const QQuickItem* self);
void qtrhi_QQuickItem_setHeight(QQuickItem* self, double height);
void qtrhi_QQuickItem_setFlag(QQuickItem* self, int flag);
void qtrhi_QQuickItem_update(QQuickItem* self);
QSGNode* qtrhi_QQuickItem_updatePaintNode(QQuickItem* self, QSGNode* param1, QQuickItem__UpdatePaintNodeData* param2);
void qtrhi_QQuickItem_setFlag2(QQuickItem* self, int flag, bool enabled);

bool qtrhi_QQuickItem_override_virtual_updatePaintNode(void* self, intptr_t slot);
QSGNode* qtrhi_QQuickItem_virtualbase_updatePaintNode(void* self, QSGNode* param1, QQuickItem__UpdatePaintNodeData* param2);

void qtrhi_QQuickItem_delete(QQuickItem* self);

void qtrhi_QQuickItem__UpdatePaintNodeData_delete(QQuickItem__UpdatePaintNodeData* self);

#ifdef __cplusplus
} /* extern C */
#endif

#endif
