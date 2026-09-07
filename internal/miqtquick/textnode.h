#pragma once

#include "gen_qquickitem.h"
#include "gen_qsgnode.h"

#ifdef __cplusplus
class QColor;
class QTextLayout;
extern "C" {
#else
typedef struct QColor QColor;
typedef struct QTextLayout QTextLayout;
#endif

QSGNode* QQuickItem_newTextNode(
    QQuickItem* item,
    QTextLayout* layout,
    float x,
    float y,
    const QColor* color,
    const QColor* haloColor);

#ifdef __cplusplus
}
#endif
