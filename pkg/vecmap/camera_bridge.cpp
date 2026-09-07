#include "camera_bridge.h"

#include <QByteArray>
#include <QObject>
#include <QQmlPropertyMap>
#include <QString>
#include <QVariant>

extern "C" void vecmap_go_qml_camera_value_changed(
    uintptr_t handle,
    const char* key,
    double value);
extern "C" void vecmap_go_qml_camera_callback_destroyed(uintptr_t handle);

class CameraCallbackGuard final : public QObject {
public:
    CameraCallbackGuard(uintptr_t handle, QObject* parent)
        : QObject(parent), handle_(handle) {}

    ~CameraCallbackGuard() override {
        vecmap_go_qml_camera_callback_destroyed(handle_);
    }

private:
    uintptr_t handle_;
};

void vecmap_qml_camera_connect(QQmlPropertyMap* properties, uintptr_t handle) {
    auto* guard = new CameraCallbackGuard(handle, properties);
    QObject::connect(
        properties,
        &QQmlPropertyMap::valueChanged,
        guard,
        [handle](const QString& key, const QVariant& value) {
            const QByteArray utf8 = key.toUtf8();
            vecmap_go_qml_camera_value_changed(handle, utf8.constData(), value.toDouble());
        });
}
