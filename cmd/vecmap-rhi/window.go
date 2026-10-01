//go:build vecmap_rhi

package main

import (
	"fmt"
	"sync"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/qml"
	rhi "github.com/rubiojr/whereami/internal/qtrhi"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
)

func createBenchmarkWindow(document scene.Document, item *rhi.QQuickItem, options benchmarkOptions) (*qml.QQmlApplicationEngine, error) {
	engine := qml.NewQQmlApplicationEngine()
	engine.RootContext().SetContextProperty("mapItem", item.QObject)
	engine.LoadData([]byte(fmt.Sprintf(`import QtQuick
import QtQuick.Window
Window {id:window; visible:false; width:%d; height:%d; color:"#f8f4f0"; title:"vecmap RHI — retained scene prototype"
 %s
 Item {id:host; anchors.fill:parent; clip:true
  Binding {target:mapItem;property:"parent";value:host}
  Binding {target:mapItem;property:"width";value:host.width}
  Binding {target:mapItem;property:"height";value:host.height}
 }
}`, document.Width, document.Height, swapDiagnostics(options))))
	if len(engine.RootObjects()) == 0 {
		engine.Delete()
		return nil, fmt.Errorf("load QML window")
	}
	showBenchmarkWindow(engine.RootObjects()[0], item.Window(), options)
	return engine, nil
}

// showBenchmarkWindow configures and shows a loaded window the same way for
// every renderer the viewer measures.
func showBenchmarkWindow(root *qt.QObject, window *rhi.QQuickWindow, options benchmarkOptions) {
	graphics := rhi.NewQQuickGraphicsConfiguration()
	graphics.SetTimestamps(true)
	window.SetGraphicsConfiguration(graphics)
	graphics.Delete()
	if options.foreground {
		window.SetFlag(qt.WindowStaysOnTopHint)
	}
	visible := qt.NewQVariant8(true)
	root.SetProperty("visible", visible)
	visible.Delete()
	if options.foreground {
		window.Raise()
		window.RequestActivate()
	}
}

// swapDiagnostics counts frameSwapped in the QML window for -diagnostics.
func swapDiagnostics(options benchmarkOptions) string {
	if options.diagnostics {
		return "property int swapCount:0\nonFrameSwapped: swapCount += 1"
	}
	return ""
}

// connectSwapchainWaits times swapchain calls from render-thread signals.
// Disconnect the subscriptions on the GUI thread before deleting the window.
func connectSwapchainWaits(window *rhi.QQuickWindow, mu *sync.Mutex, waits *swapchainWaits) []*rhi.SignalConnection {
	at := func(record func(*swapchainWaits, time.Time)) func() {
		return func() {
			now := time.Now()
			mu.Lock()
			defer mu.Unlock()
			record(waits, now)
		}
	}
	return []*rhi.SignalConnection{
		window.OnBeforeFrameBegin(at((*swapchainWaits).beforeFrameBegin)),
		window.OnBeforeSynchronizing(at((*swapchainWaits).frameBegun)),
		window.OnBeforeRendering(at((*swapchainWaits).frameBegun)),
		window.OnAfterRendering(at((*swapchainWaits).afterRendering)),
		window.OnAfterFrameEnd(at((*swapchainWaits).afterFrameEnd)),
	}
}
