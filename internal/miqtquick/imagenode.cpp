#include "imagenode.h"

#include <QImage>
#include <QQuickItem>
#include <QQuickWindow>
#include <QSGImageNode>
#include <QSGNode>
#include <QSGTexture>

QSGNode* QQuickItem_newRGBAImageNode(
    QQuickItem* item,
    const unsigned char* data,
    int imageWidth,
    int imageHeight,
    float x,
    float y,
    float width,
    float height,
    float opacity) {
    if (item == nullptr || data == nullptr || imageWidth <= 0 || imageHeight <= 0 || item->window() == nullptr) {
        return nullptr;
    }
    QImage borrowed(data, imageWidth, imageHeight, imageWidth * 4, QImage::Format_RGBA8888);
    QSGTexture* texture = item->window()->createTextureFromImage(borrowed.copy());
    QSGImageNode* imageNode = item->window()->createImageNode();
    if (texture == nullptr || imageNode == nullptr) {
        delete texture;
        delete imageNode;
        return nullptr;
    }
    imageNode->setRect(x, y, width, height);
    imageNode->setTexture(texture);
    imageNode->setOwnsTexture(true);
    imageNode->setFiltering(QSGTexture::Linear);

    auto* opacityNode = new QSGOpacityNode();
    opacityNode->setOpacity(qBound(0.0f, opacity, 1.0f));
    opacityNode->appendChildNode(imageNode);
    return opacityNode;
}
