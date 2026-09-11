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

// Allocates native-only nodes (no Go callbacks). Copies passCount blocks of 11
// material floats and returns their writable vertex buffers. The caller fills
// those buffers before attaching the returned node to the scene graph.
QSGNode* QSGSDFAtlasNode_newTextNode(
    QSGSDFAtlasNode* atlas,
    float** vertices,
    int pointCount,
    const float* materials,
    int passCount);

#ifdef __cplusplus
}
#endif
