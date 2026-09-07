#pragma once

#include "gen_qquickitem.h"
#include "gen_qsgnode.h"

#ifdef __cplusplus
extern "C" {
#endif

QSGNode* QQuickItem_newRGBAImageNode(
    QQuickItem* item,
    const unsigned char* data,
    int imageWidth,
    int imageHeight,
    float x,
    float y,
    float width,
    float height,
    float opacity);

#ifdef __cplusplus
}
#endif
