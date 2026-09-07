#pragma once

#include <stdint.h>

#ifdef __cplusplus
class QQmlPropertyMap;
extern "C" {
#else
typedef struct QQmlPropertyMap QQmlPropertyMap;
#endif

void vecmap_qml_camera_connect(QQmlPropertyMap* properties, uintptr_t handle);

#ifdef __cplusplus
}
#endif
