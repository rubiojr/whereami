#include <QObject>
#include <QQmlParserStatus>
#include <QQuickItem>
#define WORKAROUND_INNER_CLASS_DEFINITION_QQuickItem__UpdatePaintNodeData
#include <QSGNode>
#include <qquickitem.h>
#include "gen_qquickitem.h"

#ifdef __cplusplus
extern "C" {
#endif

QSGNode* miqt_exec_callback_QQuickItem_updatePaintNode(QQuickItem*, intptr_t, QSGNode*, QQuickItem__UpdatePaintNodeData*);
void miqt_exec_callback_QQuickItem_destroyed(intptr_t);
#ifdef __cplusplus
} /* extern C */
#endif

class MiqtVirtualQQuickItem final : public QQuickItem {
public:

	MiqtVirtualQQuickItem(): QQuickItem() {}
	MiqtVirtualQQuickItem(QQuickItem* parent): QQuickItem(parent) {}

	virtual ~MiqtVirtualQQuickItem() override {
		miqt_exec_callback_QQuickItem_destroyed(handle__updatePaintNode);
	}

	// cgo.Handle value for overwritten implementation
	intptr_t handle__updatePaintNode = 0;

	// Subclass to allow providing a Go implementation
	virtual QSGNode* updatePaintNode(QSGNode* param1, QQuickItem::UpdatePaintNodeData* param2) override {
		if (handle__updatePaintNode == 0) {
			return QQuickItem::updatePaintNode(param1, param2);
		}

		QSGNode* sigval1 = param1;
		QQuickItem__UpdatePaintNodeData* sigval2 = param2;
		QSGNode* callback_return_value = miqt_exec_callback_QQuickItem_updatePaintNode(this, handle__updatePaintNode, sigval1, sigval2);
		return callback_return_value;
	}

	friend QSGNode* QQuickItem_virtualbase_updatePaintNode(void* self, QSGNode* param1, QQuickItem__UpdatePaintNodeData* param2);

};

QQuickItem* QQuickItem_new() {
	return new (std::nothrow) MiqtVirtualQQuickItem();
}

QQuickItem* QQuickItem_new2(QQuickItem* parent) {
	return new (std::nothrow) MiqtVirtualQQuickItem(parent);
}

void QQuickItem_virtbase(QQuickItem* src, QObject** outptr_QObject, QQmlParserStatus** outptr_QQmlParserStatus) {
	*outptr_QObject = static_cast<QObject*>(src);
	*outptr_QQmlParserStatus = static_cast<QQmlParserStatus*>(src);
}

double QQuickItem_width(const QQuickItem* self) {
	return self->width();
}

void QQuickItem_setWidth(QQuickItem* self, double width) {
	self->setWidth(static_cast<qreal>(width));
}

double QQuickItem_height(const QQuickItem* self) {
	return self->height();
}

void QQuickItem_setHeight(QQuickItem* self, double height) {
	self->setHeight(static_cast<qreal>(height));
}

void QQuickItem_setFlag(QQuickItem* self, int flag) {
	self->setFlag(static_cast<QQuickItem::Flag>(flag));
}

void QQuickItem_update(QQuickItem* self) {
	self->update();
}

void QQuickItem_setFlag2(QQuickItem* self, int flag, bool enabled) {
	self->setFlag(static_cast<QQuickItem::Flag>(flag), enabled);
}

bool QQuickItem_override_virtual_updatePaintNode(void* self, intptr_t slot) {
	MiqtVirtualQQuickItem* self_cast = dynamic_cast<MiqtVirtualQQuickItem*>( (QQuickItem*)(self) );
	if (self_cast == nullptr || self_cast->handle__updatePaintNode != 0) {
		return false;
	}

	self_cast->handle__updatePaintNode = slot;
	return true;
}

QSGNode* QQuickItem_virtualbase_updatePaintNode(void* self, QSGNode* param1, QQuickItem__UpdatePaintNodeData* param2) {
	return static_cast<MiqtVirtualQQuickItem*>(self)->QQuickItem::updatePaintNode(param1, param2);
}

void QQuickItem_delete(QQuickItem* self) {
	delete self;
}
