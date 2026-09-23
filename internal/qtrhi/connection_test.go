//go:build vecmap_rhi && integration

package qtrhi

import (
	"runtime"
	"testing"
	"time"
	"weak"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/qml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Larger than Go's tiny allocator block, with no finalizer of its own. If a
// native subscription leaks its cgo.Handle, its captured probe cannot be collected.
type signalProbe [64]byte

func probeSubscription(window *QQuickWindow, callback func()) (*SignalConnection, weak.Pointer[signalProbe]) {
	probe := &signalProbe{}
	connection := window.OnAfterFrameEnd(func() { callback(); runtime.KeepAlive(probe) })
	return connection, weak.Make(probe)
}

func selfSubscription(window *QQuickWindow, calls *int) (*SignalConnection, weak.Pointer[signalProbe]) {
	probe := &signalProbe{}
	var connection *SignalConnection
	connection = window.OnAfterFrameEnd(func() {
		*calls++
		connection.Disconnect()
		runtime.KeepAlive(probe)
	})
	return connection, weak.Make(probe)
}

func TestSignalConnectionLifetime(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	app := qt.NewQApplication([]string{"connection-test"})
	defer app.Delete()
	item := NewQQuickItem()
	defer item.Delete()
	engine := qml.NewQQmlApplicationEngine()
	defer func() {
		if engine != nil {
			engine.Delete()
		}
	}()
	engine.RootContext().SetContextProperty("testItem", item.QObject)
	engine.LoadData([]byte(`import QtQuick
import QtQuick.Window
Window { visible:false; width:64; height:64
 Item {id:host; Binding {target:testItem; property:"parent"; value:host} }
}`))
	require.Len(t, engine.RootObjects(), 1)
	window := item.Window()
	require.NotNil(t, window)
	var empty *SignalConnection
	assert.False(t, empty.Disconnect())

	stableCalls := 0
	stable := window.OnAfterFrameEnd(func() { stableCalls++ })
	probes := make([]weak.Pointer[signalProbe], 0, 1000)
	calls := 0
	for range 1000 {
		connection, probe := probeSubscription(window, func() { calls++ })
		probes = append(probes, probe)
		window.AfterFrameEnd()
		require.True(t, connection.Disconnect())
		assert.False(t, connection.Disconnect())
		window.AfterFrameEnd()
	}
	assert.Equal(t, 1000, calls, "retired subscriptions cannot fire on a long-lived window")
	assert.Equal(t, 2000, stableCalls, "disconnect must not remove another subscriber")
	require.True(t, stable.Disconnect())
	selfCalls := 0
	self, selfProbe := selfSubscription(window, &selfCalls)
	window.AfterFrameEnd()
	window.AfterFrameEnd()
	assert.Equal(t, 1, selfCalls)
	assert.False(t, self.Disconnect())
	probes = append(probes, selfProbe)
	require.Eventually(t, func() bool {
		runtime.GC()
		for _, probe := range probes {
			if probe.Value() != nil {
				return false
			}
		}
		return true
	}, 5*time.Second, time.Millisecond, "disconnect must free callback handles before window destruction")

	remaining, senderProbe := probeSubscription(window, func() {})
	runtime.GC()
	require.NotNil(t, senderProbe.Value(), "live subscription retains its callback")
	engine.Delete()
	engine = nil
	require.Eventually(t, func() bool { runtime.GC(); return senderProbe.Value() == nil }, 5*time.Second, time.Millisecond)
	assert.False(t, remaining.Disconnect(), "connection copy is safe after sender destruction")
}
