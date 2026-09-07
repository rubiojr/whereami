#pragma once

#include "gen_qquickitem.h"
#include "gen_qsgnode.h"

#ifdef __cplusplus
extern "C" {
#endif

QSGNode* QQuickItem_newPatternNode(
    QQuickItem* item,
    const float* vertices,
    int pointCount,
    const unsigned char* rgba,
    int imageWidth,
    int imageHeight,
    float opacity);

#ifdef __cplusplus
}
#endif
