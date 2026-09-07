#pragma once
#ifndef MIQT_QT6_QUICK_GEN_QQUICKITEM_H
#define MIQT_QT6_QUICK_GEN_QQUICKITEM_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#pragma GCC diagnostic ignored "-Wdeprecated-declarations"

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
class QSGNode;
#else
typedef struct QObject QObject;
typedef struct QQmlParserStatus QQmlParserStatus;
typedef struct QQuickItem QQuickItem;
typedef struct QQuickItem__UpdatePaintNodeData QQuickItem__UpdatePaintNodeData;
typedef struct QSGNode QSGNode;
#endif

QQuickItem* QQuickItem_new();
QQuickItem* QQuickItem_new2(QQuickItem* parent);
void QQuickItem_virtbase(QQuickItem* src, QObject** outptr_QObject, QQmlParserStatus** outptr_QQmlParserStatus);
double QQuickItem_width(const QQuickItem* self);
void QQuickItem_setWidth(QQuickItem* self, double width);
double QQuickItem_height(const QQuickItem* self);
void QQuickItem_setHeight(QQuickItem* self, double height);
void QQuickItem_setFlag(QQuickItem* self, int flag);
void QQuickItem_update(QQuickItem* self);
void QQuickItem_setFlag2(QQuickItem* self, int flag, bool enabled);

bool QQuickItem_override_virtual_updatePaintNode(void* self, intptr_t slot);
QSGNode* QQuickItem_virtualbase_updatePaintNode(void* self, QSGNode* param1, QQuickItem__UpdatePaintNodeData* param2);

void QQuickItem_delete(QQuickItem* self);

#ifdef __cplusplus
} /* extern C */
#endif

#endif
