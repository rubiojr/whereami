package quick

/*
#include <stdint.h>
*/
import "C"

import "runtime/cgo"

//export miqt_exec_callback_QQuickItem_destroyed
func miqt_exec_callback_QQuickItem_destroyed(handle C.intptr_t) {
	if handle != 0 {
		cgo.Handle(handle).Delete()
	}
}
