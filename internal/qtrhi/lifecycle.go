//go:build vecmap_rhi

package qtrhi

/*
#include <stdint.h>
*/
import "C"

import (
	"runtime/cgo"
	"sync"
	"unsafe"
)

var destructionCallbacks sync.Map

// OnDestroyed registers render-thread cleanup for a generated native subclass.
// It must be installed before the object is published to the scene graph.
func OnDestroyed(pointer unsafe.Pointer, callback func()) {
	if _, loaded := destructionCallbacks.LoadOrStore(pointer, callback); loaded {
		panic("qtrhi: duplicate destruction callback")
	}
}

//export qtrhi_native_destroyed
func qtrhi_native_destroyed(pointer unsafe.Pointer) {
	if callback, exists := destructionCallbacks.LoadAndDelete(pointer); exists {
		callback.(func())()
	}
}

//export qtrhi_callback_released
func qtrhi_callback_released(handle C.intptr_t) { cgo.Handle(handle).Delete() }
