#pragma once

#include "gen_qquickitem.h"
#include "gen_qsgnode.h"

#ifdef __cplusplus
extern "C" {
#endif

// On success, vertices receives the node-owned x/y/u/v buffer. The caller must
// populate pointCount vertices before publishing the node to the scene graph.
QSGNode* QQuickItem_newPatternNode(
    QQuickItem* item,
    float** vertices,
    int pointCount,
    const unsigned char* rgba,
    int imageWidth,
    int imageHeight,
    float opacity);

#ifdef __cplusplus
}
#endif
