#pragma once
#ifndef MIQT_QTRHI_GEN_QSGRENDERNODE_H
#define MIQT_QTRHI_GEN_QSGRENDERNODE_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#pragma GCC diagnostic ignored "-Wdeprecated-declarations"

#include "../../vendor/github.com/mappu/miqt/libmiqt/libmiqt.h"

#ifdef __cplusplus
extern "C" {
#endif

#ifdef __cplusplus
class QMatrix4x4;
class QRect;
class QRhiCommandBuffer;
class QRhiRenderTarget;
class QSGNode;
class QSGRenderNode;
#if defined(WORKAROUND_INNER_CLASS_DEFINITION_QSGRenderNode__RenderState)
typedef QSGRenderNode::RenderState QSGRenderNode__RenderState;
#else
class QSGRenderNode__RenderState;
#endif
#else
typedef struct QMatrix4x4 QMatrix4x4;
typedef struct QRect QRect;
typedef struct QRhiCommandBuffer QRhiCommandBuffer;
typedef struct QRhiRenderTarget QRhiRenderTarget;
typedef struct QSGNode QSGNode;
typedef struct QSGRenderNode QSGRenderNode;
typedef struct QSGRenderNode__RenderState QSGRenderNode__RenderState;
#endif

QSGRenderNode* qtrhi_QSGRenderNode_new();
void qtrhi_QSGRenderNode_virtbase(QSGRenderNode* src, QSGNode** outptr_QSGNode);
int qtrhi_QSGRenderNode_changedStates(const QSGRenderNode* self);
void qtrhi_QSGRenderNode_prepare(QSGRenderNode* self);
void qtrhi_QSGRenderNode_render(QSGRenderNode* self, QSGRenderNode__RenderState* state);
void qtrhi_QSGRenderNode_releaseResources(QSGRenderNode* self);
int qtrhi_QSGRenderNode_flags(const QSGRenderNode* self);
QMatrix4x4* qtrhi_QSGRenderNode_projectionMatrix(const QSGRenderNode* self);
QMatrix4x4* qtrhi_QSGRenderNode_projectionMatrixWithIndex(const QSGRenderNode* self, ptrdiff_t index);
QMatrix4x4* qtrhi_QSGRenderNode_matrix(const QSGRenderNode* self);
double qtrhi_QSGRenderNode_inheritedOpacity(const QSGRenderNode* self);
QRhiRenderTarget* qtrhi_QSGRenderNode_renderTarget(const QSGRenderNode* self);
QRhiCommandBuffer* qtrhi_QSGRenderNode_commandBuffer(const QSGRenderNode* self);

bool qtrhi_QSGRenderNode_override_virtual_changedStates(void* self, intptr_t slot);
int qtrhi_QSGRenderNode_virtualbase_changedStates(const void* self);
bool qtrhi_QSGRenderNode_override_virtual_prepare(void* self, intptr_t slot);
void qtrhi_QSGRenderNode_virtualbase_prepare(void* self);
bool qtrhi_QSGRenderNode_override_virtual_render(void* self, intptr_t slot);
void qtrhi_QSGRenderNode_virtualbase_render(void* self, QSGRenderNode__RenderState* state);
bool qtrhi_QSGRenderNode_override_virtual_releaseResources(void* self, intptr_t slot);
void qtrhi_QSGRenderNode_virtualbase_releaseResources(void* self);
bool qtrhi_QSGRenderNode_override_virtual_flags(void* self, intptr_t slot);
int qtrhi_QSGRenderNode_virtualbase_flags(const void* self);

void qtrhi_QSGRenderNode_delete(QSGRenderNode* self);

QRect* qtrhi_QSGRenderNode__RenderState_scissorRect(const QSGRenderNode__RenderState* self);
bool qtrhi_QSGRenderNode__RenderState_scissorEnabled(const QSGRenderNode__RenderState* self);
int qtrhi_QSGRenderNode__RenderState_stencilValue(const QSGRenderNode__RenderState* self);
bool qtrhi_QSGRenderNode__RenderState_stencilEnabled(const QSGRenderNode__RenderState* self);

void qtrhi_QSGRenderNode__RenderState_delete(QSGRenderNode__RenderState* self);

#ifdef __cplusplus
} /* extern C */
#endif

#endif
