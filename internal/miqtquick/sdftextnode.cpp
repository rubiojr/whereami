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

#include <algorithm>
#include <array>
#include <cstddef>
#include <cstring>
#include <functional>

#if QT_VERSION < QT_VERSION_CHECK(6, 5, 0)
#error "The SDF text renderer requires Qt 6.5 or newer"
#endif

namespace {
constexpr int matrixOffset = 0;
constexpr int opacityOffset = 64;
constexpr int colorOffset = 80;
constexpr int devicePixelRatioOffset = 124;
constexpr int uniformBufferSize = 128;

static_assert(sizeof(QSGGeometry::TexturedPoint2D) == 4 * sizeof(float));
static_assert(offsetof(QSGGeometry::TexturedPoint2D, tx) == 2 * sizeof(float));
static_assert(offsetof(QSGGeometry::TexturedPoint2D, ty) == 3 * sizeof(float));

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
    SDFTextMaterial(QSGTexture* texture, const float* data)
        : texture(texture) {
        std::copy_n(data, values.size(), values.begin());
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
        for (size_t index = 0; index < values.size(); ++index) {
            if (values[index] < other->values[index])
                return -1;
            if (values[index] > other->values[index])
                return 1;
        }
        return 0;
    }

    QSGTexture* texture;
    std::array<float, 11> values;
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
    std::memcpy(buffer->data() + colorOffset, material->values.data(), 11 * sizeof(float));
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
    float** verticesData,
    int pointCount,
    const float* materialData) {
    auto* geometry = new QSGGeometry(QSGGeometry::defaultAttributes_TexturedPoint2D(), pointCount);
    geometry->setDrawingMode(QSGGeometry::DrawTriangles);
    geometry->setVertexDataPattern(QSGGeometry::StaticPattern);
    *verticesData = static_cast<float*>(geometry->vertexData());
    auto* material = new SDFTextMaterial(texture, materialData);
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
    float** verticesData,
    int pointCount,
    const float* materials,
    int passCount) {
    if (atlas == nullptr || atlas->texture == nullptr || verticesData == nullptr || pointCount <= 0 ||
        pointCount % 3 != 0 || materials == nullptr || passCount < 1 || passCount > 2)
        return nullptr;

    if (passCount == 1)
        return newSDFTextGeometry(atlas->texture, verticesData, pointCount, materials);
    auto* root = new QSGNode();
    for (int pass = 0; pass < passCount; ++pass)
        root->appendChildNode(newSDFTextGeometry(
            atlas->texture, verticesData + pass, pointCount, materials + pass * 11));
    return root;
}
