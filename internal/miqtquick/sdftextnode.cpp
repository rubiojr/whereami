#include "sdftextnode.h"

#include <QByteArray>
#include <QImage>
#include <QQuickItem>
#include <QQuickWindow>
#include <QSGGeometry>
#include <QSGGeometryNode>
#include <QSGMaterial>
#include <QSGMaterialShader>
#include <QSGNode>
#include <QSGTexture>
#include <QtGlobal>
#include <QVector4D>

#include <algorithm>
#include <cstring>
#include <functional>
#include <iterator>

#if QT_VERSION < QT_VERSION_CHECK(6, 5, 0)
#error "The SDF text renderer requires Qt 6.5 or newer"
#endif

namespace {
constexpr int matrixOffset = 0;
constexpr int opacityOffset = 64;
constexpr int colorOffset = 80;
constexpr int haloColorOffset = 96;
constexpr int fontScaleOffset = 112;
constexpr int haloWidthOffset = 116;
constexpr int haloBlurOffset = 120;
constexpr int devicePixelRatioOffset = 124;
constexpr int uniformBufferSize = 128;

QVector4D normalizedColor(const int* color) {
    return QVector4D(
        std::clamp(color[0], 0, 255) / 255.0f,
        std::clamp(color[1], 0, 255) / 255.0f,
        std::clamp(color[2], 0, 255) / 255.0f,
        std::clamp(color[3], 0, 255) / 255.0f);
}

class SDFTextMaterial;

class SDFTextShader final : public QSGMaterialShader {
public:
    SDFTextShader() {
        setShaderFileName(VertexStage, QStringLiteral(":/shaders/sdftext.vert.qsb"));
        setShaderFileName(FragmentStage, QStringLiteral(":/shaders/sdftext.frag.qsb"));
    }

    bool updateUniformData(RenderState& state, QSGMaterial* newMaterial, QSGMaterial* oldMaterial) override;
    void updateSampledImage(
        RenderState& state,
        int binding,
        QSGTexture** texture,
        QSGMaterial* newMaterial,
        QSGMaterial* oldMaterial) override;
};

class SDFTextMaterial final : public QSGMaterial {
public:
    SDFTextMaterial(
        QSGTexture* texture,
        const QVector4D& color,
        const QVector4D& haloColor,
        float fontScale,
        float haloWidth,
        float haloBlur)
        : texture(texture)
        , color(color)
        , haloColor(haloColor)
        , fontScale(fontScale)
        , haloWidth(haloWidth)
        , haloBlur(haloBlur) {
        setFlag(Blending);
    }

    QSGMaterialType* type() const override {
        static QSGMaterialType type;
        return &type;
    }

    QSGMaterialShader* createShader(QSGRendererInterface::RenderMode) const override {
        return new SDFTextShader();
    }

    int compare(const QSGMaterial* material) const override {
        const auto* other = static_cast<const SDFTextMaterial*>(material);
        if (texture != other->texture)
            return std::less<QSGTexture*>{}(texture, other->texture) ? -1 : 1;
        const float values[] = {
            color.x(), color.y(), color.z(), color.w(),
            haloColor.x(), haloColor.y(), haloColor.z(), haloColor.w(),
            fontScale, haloWidth, haloBlur,
        };
        const float otherValues[] = {
            other->color.x(), other->color.y(), other->color.z(), other->color.w(),
            other->haloColor.x(), other->haloColor.y(), other->haloColor.z(), other->haloColor.w(),
            other->fontScale, other->haloWidth, other->haloBlur,
        };
        for (size_t index = 0; index < std::size(values); ++index) {
            if (values[index] < otherValues[index])
                return -1;
            if (values[index] > otherValues[index])
                return 1;
        }
        return 0;
    }

    QSGTexture* texture;
    QVector4D color;
    QVector4D haloColor;
    float fontScale;
    float haloWidth;
    float haloBlur;
};

bool SDFTextShader::updateUniformData(RenderState& state, QSGMaterial* newMaterial, QSGMaterial* oldMaterial) {
    Q_UNUSED(oldMaterial);
    QByteArray* buffer = state.uniformData();
    Q_ASSERT(buffer->size() >= uniformBufferSize);
    if (buffer->size() < uniformBufferSize)
        return false;
    if (state.isMatrixDirty()) {
        const QMatrix4x4 matrix = state.combinedMatrix();
        std::memcpy(buffer->data() + matrixOffset, matrix.constData(), 64);
    }
    if (state.isOpacityDirty()) {
        const float opacity = state.opacity();
        std::memcpy(buffer->data() + opacityOffset, &opacity, sizeof(opacity));
    }
    const auto* material = static_cast<const SDFTextMaterial*>(newMaterial);
    const float color[] = {
        material->color.x(), material->color.y(), material->color.z(), material->color.w(),
    };
    const float haloColor[] = {
        material->haloColor.x(), material->haloColor.y(), material->haloColor.z(), material->haloColor.w(),
    };
    std::memcpy(buffer->data() + colorOffset, color, sizeof(color));
    std::memcpy(buffer->data() + haloColorOffset, haloColor, sizeof(haloColor));
    std::memcpy(buffer->data() + fontScaleOffset, &material->fontScale, sizeof(material->fontScale));
    std::memcpy(buffer->data() + haloWidthOffset, &material->haloWidth, sizeof(material->haloWidth));
    std::memcpy(buffer->data() + haloBlurOffset, &material->haloBlur, sizeof(material->haloBlur));
    const float devicePixelRatio = std::max(1.0f, state.devicePixelRatio());
    std::memcpy(buffer->data() + devicePixelRatioOffset, &devicePixelRatio, sizeof(devicePixelRatio));
    return true;
}

void SDFTextShader::updateSampledImage(
    RenderState& state,
    int binding,
    QSGTexture** texture,
    QSGMaterial* newMaterial,
    QSGMaterial*) {
    if (binding != 1)
        return;
    auto* materialTexture = static_cast<SDFTextMaterial*>(newMaterial)->texture;
    materialTexture->commitTextureOperations(state.rhi(), state.resourceUpdateBatch());
    *texture = materialTexture;
}

QSGGeometryNode* newSDFTextGeometry(
    QSGTexture* texture,
    const float* verticesData,
    int pointCount,
    const QVector4D& color,
    const QVector4D& haloColor,
    float fontScale,
    float haloWidth,
    float haloBlur) {
    auto* geometry = new QSGGeometry(QSGGeometry::defaultAttributes_TexturedPoint2D(), pointCount);
    geometry->setDrawingMode(QSGGeometry::DrawTriangles);
    geometry->setVertexDataPattern(QSGGeometry::StaticPattern);
    QSGGeometry::TexturedPoint2D* vertices = geometry->vertexDataAsTexturedPoint2D();
    for (int index = 0; index < pointCount; ++index) {
        vertices[index].set(
            verticesData[index * 4],
            verticesData[index * 4 + 1],
            verticesData[index * 4 + 2],
            verticesData[index * 4 + 3]);
    }
    auto* material = new SDFTextMaterial(
        texture,
        color,
        haloColor,
        fontScale,
        std::max(0.0f, haloWidth),
        std::max(0.0f, haloBlur));
    auto* node = new QSGGeometryNode();
    node->setGeometry(geometry);
    node->setFlag(QSGNode::OwnsGeometry);
    node->setMaterial(material);
    node->setFlag(QSGNode::OwnsMaterial);
    return node;
}
}

class QSGSDFAtlasNode final : public QSGNode {
public:
    explicit QSGSDFAtlasNode(QSGTexture* texture)
        : texture(texture) {}

    ~QSGSDFAtlasNode() override {
        delete texture;
    }

    QSGTexture* texture;
};

QSGSDFAtlasNode* QQuickItem_newSDFAtlasNode(
    QQuickItem* item,
    const unsigned char* pixels,
    int imageWidth,
    int imageHeight) {
    if (item == nullptr || pixels == nullptr || imageWidth <= 0 || imageHeight <= 0 || item->window() == nullptr)
        return nullptr;
    QImage borrowed(pixels, imageWidth, imageHeight, imageWidth, QImage::Format_Grayscale8);
    QSGTexture* texture = item->window()->createTextureFromImage(borrowed.copy());
    if (texture == nullptr)
        return nullptr;
    texture->setFiltering(QSGTexture::Linear);
    texture->setHorizontalWrapMode(QSGTexture::ClampToEdge);
    texture->setVerticalWrapMode(QSGTexture::ClampToEdge);
    return new QSGSDFAtlasNode(texture);
}

QSGNode* QSGSDFAtlasNode_node(QSGSDFAtlasNode* atlas) {
    return atlas;
}

QSGNode* QSGSDFAtlasNode_newTextNode(
    QSGSDFAtlasNode* atlas,
    const float* verticesData,
    int pointCount,
    const int* color,
    const int* haloColor,
    float fontScale,
    float haloWidth,
    float haloBlur) {
    if (atlas == nullptr || atlas->texture == nullptr || verticesData == nullptr || pointCount <= 0 ||
        pointCount % 3 != 0 || color == nullptr || haloColor == nullptr || fontScale <= 0)
        return nullptr;

    const QVector4D normalizedFill = normalizedColor(color);
    QVector4D normalizedHalo = normalizedColor(haloColor);
    if (haloWidth <= 0)
        normalizedHalo.setW(0);
    const QVector4D transparent(0, 0, 0, 0);
    if (normalizedHalo.w() <= 0) {
        return newSDFTextGeometry(
            atlas->texture,
            verticesData,
            pointCount,
            normalizedFill,
            transparent,
            fontScale,
            0,
            0);
    }

    // Draw all halo fragments first, then all fills. This prevents neighboring
    // glyph quads from painting halo over a previous glyph's fill.
    auto* root = new QSGNode();
    root->appendChildNode(newSDFTextGeometry(
        atlas->texture,
        verticesData,
        pointCount,
        transparent,
        normalizedHalo,
        fontScale,
        haloWidth,
        haloBlur));
    root->appendChildNode(newSDFTextGeometry(
        atlas->texture,
        verticesData,
        pointCount,
        normalizedFill,
        transparent,
        fontScale,
        0,
        0));
    return root;
}
