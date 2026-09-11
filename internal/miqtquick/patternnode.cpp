#include "patternnode.h"

#include <QImage>
#include <QQuickItem>
#include <QQuickWindow>
#include <QSGGeometry>
#include <QSGGeometryNode>
#include <QSGNode>
#include <QSGTexture>
#include <QSGTextureMaterial>
#include <cstddef>

// Go writes interleaved x/y/u/v directly into this public Qt vertex layout.
static_assert(sizeof(QSGGeometry::TexturedPoint2D) == 4 * sizeof(float));
static_assert(offsetof(QSGGeometry::TexturedPoint2D, tx) == 2 * sizeof(float));
static_assert(offsetof(QSGGeometry::TexturedPoint2D, ty) == 3 * sizeof(float));

class OwnedTextureMaterial final : public QSGTextureMaterial {
public:
    ~OwnedTextureMaterial() override {
        delete texture();
    }
};

QSGNode* QQuickItem_newPatternNode(
    QQuickItem* item,
    float** verticesData,
    int pointCount,
    const unsigned char* rgba,
    int imageWidth,
    int imageHeight,
    float opacity) {
    if (item == nullptr || verticesData == nullptr || pointCount <= 0 || pointCount % 3 != 0 || rgba == nullptr ||
        imageWidth <= 0 || imageHeight <= 0 || item->window() == nullptr) {
        return nullptr;
    }

    QImage borrowed(rgba, imageWidth, imageHeight, imageWidth * 4, QImage::Format_RGBA8888);
    QSGTexture* texture = item->window()->createTextureFromImage(borrowed.copy());
    if (texture == nullptr) {
        return nullptr;
    }
    auto* geometry = new QSGGeometry(QSGGeometry::defaultAttributes_TexturedPoint2D(), pointCount);
    geometry->setDrawingMode(QSGGeometry::DrawTriangles);
    geometry->setVertexDataPattern(QSGGeometry::StaticPattern);
    *verticesData = static_cast<float*>(geometry->vertexData());

    auto* material = new OwnedTextureMaterial();
    material->setTexture(texture);
    material->setFiltering(QSGTexture::Linear);
    material->setHorizontalWrapMode(QSGTexture::Repeat);
    material->setVerticalWrapMode(QSGTexture::Repeat);
    auto* geometryNode = new QSGGeometryNode();
    geometryNode->setGeometry(geometry);
    geometryNode->setFlag(QSGNode::OwnsGeometry);
    geometryNode->setMaterial(material);
    geometryNode->setFlag(QSGNode::OwnsMaterial);

    auto* opacityNode = new QSGOpacityNode();
    opacityNode->setOpacity(qBound(0.0f, opacity, 1.0f));
    opacityNode->appendChildNode(geometryNode);
    return opacityNode;
}
