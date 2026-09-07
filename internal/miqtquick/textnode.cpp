#include "textnode.h"

#include <QColor>
#include <QQuickItem>
#include <QQuickWindow>
#include <QSGTextNode>
#include <QTextLayout>

namespace {
// Keep map labels crisp when Qt generates distance-field glyphs on high-DPI displays.
constexpr int mapTextRenderQuality = 104;
}

QSGNode* QQuickItem_newTextNode(
    QQuickItem* item,
    QTextLayout* layout,
    float x,
    float y,
    const QColor* color,
    const QColor* haloColor) {
    if (item == nullptr || layout == nullptr || color == nullptr || item->window() == nullptr) {
        return nullptr;
    }

    QSGTextNode* node = item->window()->createTextNode();
    if (node == nullptr) {
        return nullptr;
    }
    node->setColor(*color);
    if (haloColor != nullptr) {
        node->setTextStyle(QSGTextNode::Outline);
        node->setStyleColor(*haloColor);
    }
    node->setRenderType(QSGTextNode::QtRendering);
    node->setRenderTypeQuality(mapTextRenderQuality);
    node->setFiltering(QSGTexture::Linear);
    node->addTextLayout(QPointF(x, y), layout);
    return node;
}
