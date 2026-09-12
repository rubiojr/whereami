#pragma once
#ifndef MIQT_QTRHI_GEN_QRHI_H
#define MIQT_QTRHI_GEN_QRHI_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#pragma GCC diagnostic ignored "-Wdeprecated-declarations"

#include "../../vendor/github.com/mappu/miqt/libmiqt/libmiqt.h"

#ifdef __cplusplus
extern "C" {
#endif

#ifdef __cplusplus
class QImage;
class QPoint;
class QRhi;
class QRhiBuffer;
class QRhiCommandBuffer;
class QRhiDriverInfo;
class QRhiGraphicsPipeline;
#if defined(WORKAROUND_INNER_CLASS_DEFINITION_QRhiGraphicsPipeline__StencilOpState)
typedef QRhiGraphicsPipeline::StencilOpState QRhiGraphicsPipeline__StencilOpState;
#else
class QRhiGraphicsPipeline__StencilOpState;
#endif
#if defined(WORKAROUND_INNER_CLASS_DEFINITION_QRhiGraphicsPipeline__TargetBlend)
typedef QRhiGraphicsPipeline::TargetBlend QRhiGraphicsPipeline__TargetBlend;
#else
class QRhiGraphicsPipeline__TargetBlend;
#endif
class QRhiRenderPassDescriptor;
class QRhiRenderTarget;
class QRhiResource;
class QRhiResourceUpdateBatch;
class QRhiSampler;
class QRhiScissor;
class QRhiShaderResourceBinding;
class QRhiShaderResourceBindings;
class QRhiShaderStage;
class QRhiTexture;
class QRhiTextureSubresourceUploadDescription;
class QRhiTextureUploadDescription;
class QRhiTextureUploadEntry;
class QRhiVertexInputAttribute;
class QRhiVertexInputBinding;
class QRhiVertexInputLayout;
class QRhiViewport;
class QShader;
class QSize;
#else
typedef struct QImage QImage;
typedef struct QPoint QPoint;
typedef struct QRhi QRhi;
typedef struct QRhiBuffer QRhiBuffer;
typedef struct QRhiCommandBuffer QRhiCommandBuffer;
typedef struct QRhiDriverInfo QRhiDriverInfo;
typedef struct QRhiGraphicsPipeline QRhiGraphicsPipeline;
typedef struct QRhiGraphicsPipeline__StencilOpState QRhiGraphicsPipeline__StencilOpState;
typedef struct QRhiGraphicsPipeline__TargetBlend QRhiGraphicsPipeline__TargetBlend;
typedef struct QRhiRenderPassDescriptor QRhiRenderPassDescriptor;
typedef struct QRhiRenderTarget QRhiRenderTarget;
typedef struct QRhiResource QRhiResource;
typedef struct QRhiResourceUpdateBatch QRhiResourceUpdateBatch;
typedef struct QRhiSampler QRhiSampler;
typedef struct QRhiScissor QRhiScissor;
typedef struct QRhiShaderResourceBinding QRhiShaderResourceBinding;
typedef struct QRhiShaderResourceBindings QRhiShaderResourceBindings;
typedef struct QRhiShaderStage QRhiShaderStage;
typedef struct QRhiTexture QRhiTexture;
typedef struct QRhiTextureSubresourceUploadDescription QRhiTextureSubresourceUploadDescription;
typedef struct QRhiTextureUploadDescription QRhiTextureUploadDescription;
typedef struct QRhiTextureUploadEntry QRhiTextureUploadEntry;
typedef struct QRhiVertexInputAttribute QRhiVertexInputAttribute;
typedef struct QRhiVertexInputBinding QRhiVertexInputBinding;
typedef struct QRhiVertexInputLayout QRhiVertexInputLayout;
typedef struct QRhiViewport QRhiViewport;
typedef struct QShader QShader;
typedef struct QSize QSize;
#endif

QRhiViewport* qtrhi_QRhiViewport_new();
QRhiViewport* qtrhi_QRhiViewport_new2(float x, float y, float w, float h);
QRhiViewport* qtrhi_QRhiViewport_new3(QRhiViewport* param1);
QRhiViewport* qtrhi_QRhiViewport_new4(float x, float y, float w, float h, float minDepth);
QRhiViewport* qtrhi_QRhiViewport_new5(float x, float y, float w, float h, float minDepth, float maxDepth);
void qtrhi_QRhiViewport_delete(QRhiViewport* self);

QRhiScissor* qtrhi_QRhiScissor_new();
QRhiScissor* qtrhi_QRhiScissor_new2(int x, int y, int w, int h);
QRhiScissor* qtrhi_QRhiScissor_new3(QRhiScissor* param1);
void qtrhi_QRhiScissor_delete(QRhiScissor* self);

QRhiVertexInputBinding* qtrhi_QRhiVertexInputBinding_new();
QRhiVertexInputBinding* qtrhi_QRhiVertexInputBinding_new2(uint32_t stride);
QRhiVertexInputBinding* qtrhi_QRhiVertexInputBinding_new3(QRhiVertexInputBinding* param1);
QRhiVertexInputBinding* qtrhi_QRhiVertexInputBinding_new4(uint32_t stride, int cls);
QRhiVertexInputBinding* qtrhi_QRhiVertexInputBinding_new5(uint32_t stride, int cls, uint32_t stepRate);
void qtrhi_QRhiVertexInputBinding_delete(QRhiVertexInputBinding* self);

QRhiVertexInputAttribute* qtrhi_QRhiVertexInputAttribute_new();
QRhiVertexInputAttribute* qtrhi_QRhiVertexInputAttribute_new2(int binding, int location, int format, uint32_t offset);
QRhiVertexInputAttribute* qtrhi_QRhiVertexInputAttribute_new3(QRhiVertexInputAttribute* param1);
QRhiVertexInputAttribute* qtrhi_QRhiVertexInputAttribute_new4(int binding, int location, int format, uint32_t offset, int matrixSlice);
void qtrhi_QRhiVertexInputAttribute_delete(QRhiVertexInputAttribute* self);

QRhiVertexInputLayout* qtrhi_QRhiVertexInputLayout_new();
QRhiVertexInputLayout* qtrhi_QRhiVertexInputLayout_new2(QRhiVertexInputLayout* param1);
void qtrhi_QRhiVertexInputLayout_setBindings(QRhiVertexInputLayout* self, struct miqt_array /* of QRhiVertexInputBinding* */  list);
void qtrhi_QRhiVertexInputLayout_setAttributes(QRhiVertexInputLayout* self, struct miqt_array /* of QRhiVertexInputAttribute* */  list);

void qtrhi_QRhiVertexInputLayout_delete(QRhiVertexInputLayout* self);

QRhiShaderStage* qtrhi_QRhiShaderStage_new();
QRhiShaderStage* qtrhi_QRhiShaderStage_new2(int type, QShader* shader);
QRhiShaderStage* qtrhi_QRhiShaderStage_new3(QRhiShaderStage* param1);
QRhiShaderStage* qtrhi_QRhiShaderStage_new4(int type, QShader* shader, int v);
void qtrhi_QRhiShaderStage_delete(QRhiShaderStage* self);

QRhiShaderResourceBinding* qtrhi_QRhiShaderResourceBinding_new();
QRhiShaderResourceBinding* qtrhi_QRhiShaderResourceBinding_new2(QRhiShaderResourceBinding* param1);
QRhiShaderResourceBinding* qtrhi_QRhiShaderResourceBinding_uniformBufferWithDynamicOffset(int binding, int stage, QRhiBuffer* buf, uint32_t size);
QRhiShaderResourceBinding* qtrhi_QRhiShaderResourceBinding_sampledTexture(int binding, int stage, QRhiTexture* tex, QRhiSampler* sampler);

void qtrhi_QRhiShaderResourceBinding_delete(QRhiShaderResourceBinding* self);

QRhiTextureSubresourceUploadDescription* qtrhi_QRhiTextureSubresourceUploadDescription_new();
QRhiTextureSubresourceUploadDescription* qtrhi_QRhiTextureSubresourceUploadDescription_new2(QImage* image);
QRhiTextureSubresourceUploadDescription* qtrhi_QRhiTextureSubresourceUploadDescription_new3(const void* data, uint32_t size);
QRhiTextureSubresourceUploadDescription* qtrhi_QRhiTextureSubresourceUploadDescription_new4(struct miqt_string data);
QRhiTextureSubresourceUploadDescription* qtrhi_QRhiTextureSubresourceUploadDescription_new5(QRhiTextureSubresourceUploadDescription* param1);
void qtrhi_QRhiTextureSubresourceUploadDescription_setDestinationTopLeft(QRhiTextureSubresourceUploadDescription* self, QPoint* p);
void qtrhi_QRhiTextureSubresourceUploadDescription_setSourceSize(QRhiTextureSubresourceUploadDescription* self, QSize* size);

void qtrhi_QRhiTextureSubresourceUploadDescription_delete(QRhiTextureSubresourceUploadDescription* self);

QRhiTextureUploadEntry* qtrhi_QRhiTextureUploadEntry_new();
QRhiTextureUploadEntry* qtrhi_QRhiTextureUploadEntry_new2(int layer, int level, QRhiTextureSubresourceUploadDescription* desc);
QRhiTextureUploadEntry* qtrhi_QRhiTextureUploadEntry_new3(QRhiTextureUploadEntry* param1);
void qtrhi_QRhiTextureUploadEntry_delete(QRhiTextureUploadEntry* self);

QRhiTextureUploadDescription* qtrhi_QRhiTextureUploadDescription_new();
QRhiTextureUploadDescription* qtrhi_QRhiTextureUploadDescription_new2(QRhiTextureUploadEntry* entry);
QRhiTextureUploadDescription* qtrhi_QRhiTextureUploadDescription_new3(QRhiTextureUploadDescription* param1);
void qtrhi_QRhiTextureUploadDescription_setEntries(QRhiTextureUploadDescription* self, struct miqt_array /* of QRhiTextureUploadEntry* */  list);

void qtrhi_QRhiTextureUploadDescription_delete(QRhiTextureUploadDescription* self);

void qtrhi_QRhiResource_destroy(QRhiResource* self);
void qtrhi_QRhiResource_deleteLater(QRhiResource* self);
void qtrhi_QRhiResource_setName(QRhiResource* self, struct miqt_string name);

void qtrhi_QRhiResource_delete(QRhiResource* self);

void qtrhi_QRhiBuffer_virtbase(QRhiBuffer* src, QRhiResource** outptr_QRhiResource);
uint32_t qtrhi_QRhiBuffer_size(const QRhiBuffer* self);
bool qtrhi_QRhiBuffer_create(QRhiBuffer* self);

void qtrhi_QRhiBuffer_delete(QRhiBuffer* self);

void qtrhi_QRhiTexture_virtbase(QRhiTexture* src, QRhiResource** outptr_QRhiResource);
QSize* qtrhi_QRhiTexture_pixelSize(const QRhiTexture* self);
bool qtrhi_QRhiTexture_create(QRhiTexture* self);

void qtrhi_QRhiTexture_delete(QRhiTexture* self);

void qtrhi_QRhiSampler_virtbase(QRhiSampler* src, QRhiResource** outptr_QRhiResource);
bool qtrhi_QRhiSampler_create(QRhiSampler* self);

void qtrhi_QRhiSampler_delete(QRhiSampler* self);

void qtrhi_QRhiRenderPassDescriptor_virtbase(QRhiRenderPassDescriptor* src, QRhiResource** outptr_QRhiResource);
bool qtrhi_QRhiRenderPassDescriptor_isCompatible(const QRhiRenderPassDescriptor* self, QRhiRenderPassDescriptor* other);
struct miqt_array /* of uint32_t */  qtrhi_QRhiRenderPassDescriptor_serializedFormat(const QRhiRenderPassDescriptor* self);

void qtrhi_QRhiRenderPassDescriptor_delete(QRhiRenderPassDescriptor* self);

void qtrhi_QRhiRenderTarget_virtbase(QRhiRenderTarget* src, QRhiResource** outptr_QRhiResource);
QSize* qtrhi_QRhiRenderTarget_pixelSize(const QRhiRenderTarget* self);
int qtrhi_QRhiRenderTarget_sampleCount(const QRhiRenderTarget* self);
QRhiRenderPassDescriptor* qtrhi_QRhiRenderTarget_renderPassDescriptor(const QRhiRenderTarget* self);

void qtrhi_QRhiRenderTarget_delete(QRhiRenderTarget* self);

void qtrhi_QRhiShaderResourceBindings_virtbase(QRhiShaderResourceBindings* src, QRhiResource** outptr_QRhiResource);
void qtrhi_QRhiShaderResourceBindings_setBindings(QRhiShaderResourceBindings* self, struct miqt_array /* of QRhiShaderResourceBinding* */  list);
bool qtrhi_QRhiShaderResourceBindings_create(QRhiShaderResourceBindings* self);

void qtrhi_QRhiShaderResourceBindings_delete(QRhiShaderResourceBindings* self);

void qtrhi_QRhiGraphicsPipeline_virtbase(QRhiGraphicsPipeline* src, QRhiResource** outptr_QRhiResource);
void qtrhi_QRhiGraphicsPipeline_setFlags(QRhiGraphicsPipeline* self, int f);
void qtrhi_QRhiGraphicsPipeline_setTargetBlends(QRhiGraphicsPipeline* self, struct miqt_array /* of QRhiGraphicsPipeline__TargetBlend* */  list);
void qtrhi_QRhiGraphicsPipeline_setDepthTest(QRhiGraphicsPipeline* self, bool enable);
void qtrhi_QRhiGraphicsPipeline_setDepthWrite(QRhiGraphicsPipeline* self, bool enable);
void qtrhi_QRhiGraphicsPipeline_setStencilTest(QRhiGraphicsPipeline* self, bool enable);
void qtrhi_QRhiGraphicsPipeline_setStencilFront(QRhiGraphicsPipeline* self, QRhiGraphicsPipeline__StencilOpState* state);
void qtrhi_QRhiGraphicsPipeline_setStencilBack(QRhiGraphicsPipeline* self, QRhiGraphicsPipeline__StencilOpState* state);
void qtrhi_QRhiGraphicsPipeline_setStencilReadMask(QRhiGraphicsPipeline* self, uint32_t mask);
void qtrhi_QRhiGraphicsPipeline_setStencilWriteMask(QRhiGraphicsPipeline* self, uint32_t mask);
void qtrhi_QRhiGraphicsPipeline_setSampleCount(QRhiGraphicsPipeline* self, int s);
void qtrhi_QRhiGraphicsPipeline_setShaderStages(QRhiGraphicsPipeline* self, struct miqt_array /* of QRhiShaderStage* */  list);
void qtrhi_QRhiGraphicsPipeline_setVertexInputLayout(QRhiGraphicsPipeline* self, QRhiVertexInputLayout* layout);
void qtrhi_QRhiGraphicsPipeline_setShaderResourceBindings(QRhiGraphicsPipeline* self, QRhiShaderResourceBindings* srb);
void qtrhi_QRhiGraphicsPipeline_setRenderPassDescriptor(QRhiGraphicsPipeline* self, QRhiRenderPassDescriptor* desc);
bool qtrhi_QRhiGraphicsPipeline_create(QRhiGraphicsPipeline* self);

void qtrhi_QRhiGraphicsPipeline_delete(QRhiGraphicsPipeline* self);

void qtrhi_QRhiCommandBuffer_virtbase(QRhiCommandBuffer* src, QRhiResource** outptr_QRhiResource);
void qtrhi_QRhiCommandBuffer_resourceUpdate(QRhiCommandBuffer* self, QRhiResourceUpdateBatch* resourceUpdates);
void qtrhi_QRhiCommandBuffer_setGraphicsPipeline(QRhiCommandBuffer* self, QRhiGraphicsPipeline* ps);
void qtrhi_QRhiCommandBuffer_setShaderResources(QRhiCommandBuffer* self);
void qtrhi_QRhiCommandBuffer_setVertexInput(QRhiCommandBuffer* self, int startBinding, int bindingCount, struct miqt_map /* tuple of QRhiBuffer* and uint32_t */  bindings);
void qtrhi_QRhiCommandBuffer_setViewport(QRhiCommandBuffer* self, QRhiViewport* viewport);
void qtrhi_QRhiCommandBuffer_setScissor(QRhiCommandBuffer* self, QRhiScissor* scissor);
void qtrhi_QRhiCommandBuffer_setStencilRef(QRhiCommandBuffer* self, uint32_t refValue);
void qtrhi_QRhiCommandBuffer_draw(QRhiCommandBuffer* self, uint32_t vertexCount);
void qtrhi_QRhiCommandBuffer_drawIndexed(QRhiCommandBuffer* self, uint32_t indexCount);
double qtrhi_QRhiCommandBuffer_lastCompletedGpuTime(QRhiCommandBuffer* self);
void qtrhi_QRhiCommandBuffer_setShaderResourcesWithSrb(QRhiCommandBuffer* self, QRhiShaderResourceBindings* srb);
void qtrhi_QRhiCommandBuffer_setShaderResources2(QRhiCommandBuffer* self, QRhiShaderResourceBindings* srb, int dynamicOffsetCount);
void qtrhi_QRhiCommandBuffer_setShaderResources3(QRhiCommandBuffer* self, QRhiShaderResourceBindings* srb, int dynamicOffsetCount, struct miqt_map /* tuple of int and uint32_t */  dynamicOffsets);
void qtrhi_QRhiCommandBuffer_setVertexInput2(QRhiCommandBuffer* self, int startBinding, int bindingCount, struct miqt_map /* tuple of QRhiBuffer* and uint32_t */  bindings, QRhiBuffer* indexBuf);
void qtrhi_QRhiCommandBuffer_setVertexInput3(QRhiCommandBuffer* self, int startBinding, int bindingCount, struct miqt_map /* tuple of QRhiBuffer* and uint32_t */  bindings, QRhiBuffer* indexBuf, uint32_t indexOffset);
void qtrhi_QRhiCommandBuffer_setVertexInput4(QRhiCommandBuffer* self, int startBinding, int bindingCount, struct miqt_map /* tuple of QRhiBuffer* and uint32_t */  bindings, QRhiBuffer* indexBuf, uint32_t indexOffset, int indexFormat);
void qtrhi_QRhiCommandBuffer_draw2(QRhiCommandBuffer* self, uint32_t vertexCount, uint32_t instanceCount);
void qtrhi_QRhiCommandBuffer_draw3(QRhiCommandBuffer* self, uint32_t vertexCount, uint32_t instanceCount, uint32_t firstVertex);
void qtrhi_QRhiCommandBuffer_draw4(QRhiCommandBuffer* self, uint32_t vertexCount, uint32_t instanceCount, uint32_t firstVertex, uint32_t firstInstance);
void qtrhi_QRhiCommandBuffer_drawIndexed2(QRhiCommandBuffer* self, uint32_t indexCount, uint32_t instanceCount);
void qtrhi_QRhiCommandBuffer_drawIndexed3(QRhiCommandBuffer* self, uint32_t indexCount, uint32_t instanceCount, uint32_t firstIndex);
void qtrhi_QRhiCommandBuffer_drawIndexed4(QRhiCommandBuffer* self, uint32_t indexCount, uint32_t instanceCount, uint32_t firstIndex, int32_t vertexOffset);
void qtrhi_QRhiCommandBuffer_drawIndexed5(QRhiCommandBuffer* self, uint32_t indexCount, uint32_t instanceCount, uint32_t firstIndex, int32_t vertexOffset, uint32_t firstInstance);

void qtrhi_QRhiCommandBuffer_delete(QRhiCommandBuffer* self);

void qtrhi_QRhiResourceUpdateBatch_release(QRhiResourceUpdateBatch* self);
void qtrhi_QRhiResourceUpdateBatch_updateDynamicBuffer(QRhiResourceUpdateBatch* self, QRhiBuffer* buf, uint32_t offset, uint32_t size, const void* data);
void qtrhi_QRhiResourceUpdateBatch_updateDynamicBuffer2(QRhiResourceUpdateBatch* self, QRhiBuffer* buf, uint32_t offset, struct miqt_string data);
void qtrhi_QRhiResourceUpdateBatch_uploadStaticBuffer(QRhiResourceUpdateBatch* self, QRhiBuffer* buf, uint32_t offset, uint32_t size, const void* data);
void qtrhi_QRhiResourceUpdateBatch_uploadStaticBuffer2(QRhiResourceUpdateBatch* self, QRhiBuffer* buf, uint32_t offset, struct miqt_string data);
void qtrhi_QRhiResourceUpdateBatch_uploadStaticBuffer3(QRhiResourceUpdateBatch* self, QRhiBuffer* buf, const void* data);
void qtrhi_QRhiResourceUpdateBatch_uploadStaticBuffer4(QRhiResourceUpdateBatch* self, QRhiBuffer* buf, struct miqt_string data);
void qtrhi_QRhiResourceUpdateBatch_uploadTexture(QRhiResourceUpdateBatch* self, QRhiTexture* tex, QRhiTextureUploadDescription* desc);
void qtrhi_QRhiResourceUpdateBatch_uploadTexture2(QRhiResourceUpdateBatch* self, QRhiTexture* tex, QImage* image);

void qtrhi_QRhiResourceUpdateBatch_delete(QRhiResourceUpdateBatch* self);

QRhiDriverInfo* qtrhi_QRhiDriverInfo_new(QRhiDriverInfo* param1);
QRhiDriverInfo* qtrhi_QRhiDriverInfo_new2();
struct miqt_string qtrhi_QRhiDriverInfo_deviceName(const QRhiDriverInfo* self);
void qtrhi_QRhiDriverInfo_setDeviceName(QRhiDriverInfo* self, struct miqt_string deviceName);
uint64_t qtrhi_QRhiDriverInfo_deviceId(const QRhiDriverInfo* self);
void qtrhi_QRhiDriverInfo_setDeviceId(QRhiDriverInfo* self, uint64_t deviceId);
uint64_t qtrhi_QRhiDriverInfo_vendorId(const QRhiDriverInfo* self);
void qtrhi_QRhiDriverInfo_setVendorId(QRhiDriverInfo* self, uint64_t vendorId);
int qtrhi_QRhiDriverInfo_deviceType(const QRhiDriverInfo* self);
void qtrhi_QRhiDriverInfo_setDeviceType(QRhiDriverInfo* self, int deviceType);

void qtrhi_QRhiDriverInfo_delete(QRhiDriverInfo* self);

const char* qtrhi_QRhi_backendName(const QRhi* self);
const char* qtrhi_QRhi_backendNameWithImpl(int impl);
QRhiDriverInfo* qtrhi_QRhi_driverInfo(const QRhi* self);
QRhiGraphicsPipeline* qtrhi_QRhi_newGraphicsPipeline(QRhi* self);
QRhiShaderResourceBindings* qtrhi_QRhi_newShaderResourceBindings(QRhi* self);
QRhiBuffer* qtrhi_QRhi_newBuffer(QRhi* self, int type, int usage, uint32_t size);
QRhiTexture* qtrhi_QRhi_newTexture(QRhi* self, int format, QSize* pixelSize);
QRhiTexture* qtrhi_QRhi_newTexture2(QRhi* self, int format, int width, int height, int depth);
QRhiSampler* qtrhi_QRhi_newSampler(QRhi* self, int magFilter, int minFilter, int mipmapMode, int addressU, int addressV);
QRhiResourceUpdateBatch* qtrhi_QRhi_nextResourceUpdateBatch(QRhi* self);
int qtrhi_QRhi_ubufAlignment(const QRhi* self);
int qtrhi_QRhi_ubufAligned(const QRhi* self, int v);
bool qtrhi_QRhi_isYUpInFramebuffer(const QRhi* self);
QRhiTexture* qtrhi_QRhi_newTexture3(QRhi* self, int format, QSize* pixelSize, int sampleCount);
QRhiTexture* qtrhi_QRhi_newTexture4(QRhi* self, int format, QSize* pixelSize, int sampleCount, int flags);
QRhiTexture* qtrhi_QRhi_newTexture5(QRhi* self, int format, int width, int height, int depth, int sampleCount);
QRhiTexture* qtrhi_QRhi_newTexture6(QRhi* self, int format, int width, int height, int depth, int sampleCount, int flags);
QRhiSampler* qtrhi_QRhi_newSampler2(QRhi* self, int magFilter, int minFilter, int mipmapMode, int addressU, int addressV, int addressW);

void qtrhi_QRhi_delete(QRhi* self);

QRhiGraphicsPipeline__TargetBlend* qtrhi_QRhiGraphicsPipeline__TargetBlend_new();
QRhiGraphicsPipeline__TargetBlend* qtrhi_QRhiGraphicsPipeline__TargetBlend_new2(QRhiGraphicsPipeline__TargetBlend* param1);
int qtrhi_QRhiGraphicsPipeline__TargetBlend_colorWrite(const QRhiGraphicsPipeline__TargetBlend* self);
void qtrhi_QRhiGraphicsPipeline__TargetBlend_setColorWrite(QRhiGraphicsPipeline__TargetBlend* self, int colorWrite);
bool qtrhi_QRhiGraphicsPipeline__TargetBlend_enable(const QRhiGraphicsPipeline__TargetBlend* self);
void qtrhi_QRhiGraphicsPipeline__TargetBlend_setEnable(QRhiGraphicsPipeline__TargetBlend* self, bool enable);
int qtrhi_QRhiGraphicsPipeline__TargetBlend_srcColor(const QRhiGraphicsPipeline__TargetBlend* self);
void qtrhi_QRhiGraphicsPipeline__TargetBlend_setSrcColor(QRhiGraphicsPipeline__TargetBlend* self, int srcColor);
int qtrhi_QRhiGraphicsPipeline__TargetBlend_dstColor(const QRhiGraphicsPipeline__TargetBlend* self);
void qtrhi_QRhiGraphicsPipeline__TargetBlend_setDstColor(QRhiGraphicsPipeline__TargetBlend* self, int dstColor);
int qtrhi_QRhiGraphicsPipeline__TargetBlend_opColor(const QRhiGraphicsPipeline__TargetBlend* self);
void qtrhi_QRhiGraphicsPipeline__TargetBlend_setOpColor(QRhiGraphicsPipeline__TargetBlend* self, int opColor);
int qtrhi_QRhiGraphicsPipeline__TargetBlend_srcAlpha(const QRhiGraphicsPipeline__TargetBlend* self);
void qtrhi_QRhiGraphicsPipeline__TargetBlend_setSrcAlpha(QRhiGraphicsPipeline__TargetBlend* self, int srcAlpha);
int qtrhi_QRhiGraphicsPipeline__TargetBlend_dstAlpha(const QRhiGraphicsPipeline__TargetBlend* self);
void qtrhi_QRhiGraphicsPipeline__TargetBlend_setDstAlpha(QRhiGraphicsPipeline__TargetBlend* self, int dstAlpha);
int qtrhi_QRhiGraphicsPipeline__TargetBlend_opAlpha(const QRhiGraphicsPipeline__TargetBlend* self);
void qtrhi_QRhiGraphicsPipeline__TargetBlend_setOpAlpha(QRhiGraphicsPipeline__TargetBlend* self, int opAlpha);

void qtrhi_QRhiGraphicsPipeline__TargetBlend_delete(QRhiGraphicsPipeline__TargetBlend* self);

QRhiGraphicsPipeline__StencilOpState* qtrhi_QRhiGraphicsPipeline__StencilOpState_new();
QRhiGraphicsPipeline__StencilOpState* qtrhi_QRhiGraphicsPipeline__StencilOpState_new2(QRhiGraphicsPipeline__StencilOpState* param1);
int qtrhi_QRhiGraphicsPipeline__StencilOpState_failOp(const QRhiGraphicsPipeline__StencilOpState* self);
void qtrhi_QRhiGraphicsPipeline__StencilOpState_setFailOp(QRhiGraphicsPipeline__StencilOpState* self, int failOp);
int qtrhi_QRhiGraphicsPipeline__StencilOpState_depthFailOp(const QRhiGraphicsPipeline__StencilOpState* self);
void qtrhi_QRhiGraphicsPipeline__StencilOpState_setDepthFailOp(QRhiGraphicsPipeline__StencilOpState* self, int depthFailOp);
int qtrhi_QRhiGraphicsPipeline__StencilOpState_passOp(const QRhiGraphicsPipeline__StencilOpState* self);
void qtrhi_QRhiGraphicsPipeline__StencilOpState_setPassOp(QRhiGraphicsPipeline__StencilOpState* self, int passOp);
int qtrhi_QRhiGraphicsPipeline__StencilOpState_compareOp(const QRhiGraphicsPipeline__StencilOpState* self);
void qtrhi_QRhiGraphicsPipeline__StencilOpState_setCompareOp(QRhiGraphicsPipeline__StencilOpState* self, int compareOp);

void qtrhi_QRhiGraphicsPipeline__StencilOpState_delete(QRhiGraphicsPipeline__StencilOpState* self);

#ifdef __cplusplus
} /* extern C */
#endif

#endif
