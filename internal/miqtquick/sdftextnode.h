#pragma once

#include "gen_qquickitem.h"
#include "gen_qsgnode.h"

#ifdef __cplusplus
class QSGSDFAtlasNode;
extern "C" {
#else
typedef struct QSGSDFAtlasNode QSGSDFAtlasNode;
#endif

QSGSDFAtlasNode* QQuickItem_newSDFAtlasNode(
    QQuickItem* item,
    const unsigned char* pixels,
    int imageWidth,
    int imageHeight);

QSGNode* QSGSDFAtlasNode_node(QSGSDFAtlasNode* atlas);

QSGNode* QSGSDFAtlasNode_newTextNode(
    QSGSDFAtlasNode* atlas,
    const float* vertices,
    int pointCount,
    const int* color,
    const int* haloColor,
    float fontScale,
    float haloWidth,
    float haloBlur);

#ifdef __cplusplus
}
#endif
