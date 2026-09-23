//go:build vecmap_rhi

package qtrhi

import (
	"unsafe"

	qt "github.com/mappu/miqt/qt6"
)

// SignalConnection owns a generated direct-signal subscription. Call Disconnect
// on its owning thread before dropping it, even if the sender has already died.
// It must not be copied or used concurrently. No native cleanup uses finalizers.
// Qt owns callback lifetime: an in-progress invocation may finish after disconnect.
type SignalConnection struct {
	native *qt.QMetaObject__Connection
}

func newSignalConnection(pointer unsafe.Pointer) *SignalConnection {
	return &SignalConnection{native: qt.UnsafeNewQMetaObject__Connection(pointer)}
}

// Disconnect removes this subscription and deletes its native connection copy.
// It is idempotent, and may be called from its own callback. False means Qt had
// already disconnected it (including sender destruction), or it was called twice.
func (c *SignalConnection) Disconnect() bool {
	if c == nil || c.native == nil {
		return false
	}
	native := c.native
	c.native = nil
	disconnected := qt.QObject_DisconnectWithQMetaObjectConnection(native)
	native.Delete()
	return disconnected
}
