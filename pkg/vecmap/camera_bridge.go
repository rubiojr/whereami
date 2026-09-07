package vecmap

/*
#cgo pkg-config: Qt6Qml Qt6Quick
#cgo CXXFLAGS: -std=c++17
#cgo LDFLAGS: -lstdc++

#include "camera_bridge.h"
*/
import "C"

import (
	"runtime/cgo"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/qml"
	quick "github.com/rubiojr/whereami/internal/miqtquick"
)

func connectQMLCamera(properties *qml.QQmlPropertyMap, camera *qmlCamera) {
	handle := cgo.NewHandle(camera)
	C.vecmap_qml_camera_connect(
		(*C.QQmlPropertyMap)(properties.UnsafePointer()),
		C.uintptr_t(handle),
	)
}

func queueQuickItemUpdate(item *quick.QQuickItem) {
	if item != nil {
		qt.QMetaObject_InvokeMethod3(item.QObject, "update", qt.QueuedConnection)
	}
}

//export vecmap_go_qml_camera_value_changed
func vecmap_go_qml_camera_value_changed(handle C.uintptr_t, key *C.char, value C.double) {
	camera, ok := cgo.Handle(handle).Value().(*qmlCamera)
	if ok {
		camera.handleValueChanged(C.GoString(key), float64(value))
	}
}

//export vecmap_go_qml_camera_callback_destroyed
func vecmap_go_qml_camera_callback_destroyed(handle C.uintptr_t) {
	cgo.Handle(handle).Delete()
}
