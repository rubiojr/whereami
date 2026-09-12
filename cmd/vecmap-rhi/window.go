//go:build vecmap_rhi

package main

import (
	"fmt"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/qml"
	rhi "github.com/rubiojr/whereami/internal/qtrhi"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
)

func createBenchmarkWindow(document scene.Document, item *rhi.QQuickItem, options benchmarkOptions) (*qml.QQmlApplicationEngine, error) {
	engine := qml.NewQQmlApplicationEngine()
	engine.RootContext().SetContextProperty("mapItem", item.QObject)
	swapDiagnostics := ""
	if options.diagnostics {
		swapDiagnostics = "property int swapCount:0\nonFrameSwapped: swapCount += 1"
	}
	engine.LoadData([]byte(fmt.Sprintf(`import QtQuick
import QtQuick.Window
Window {id:window; visible:false; width:%d; height:%d; color:"#f8f4f0"; title:"vecmap RHI — retained scene prototype"
 %s
 Item {id:host; anchors.fill:parent; clip:true
  Binding {target:mapItem;property:"parent";value:host}
  Binding {target:mapItem;property:"width";value:host.width}
  Binding {target:mapItem;property:"height";value:host.height}
 }
}`, document.Width, document.Height, swapDiagnostics)))
	if len(engine.RootObjects()) == 0 {
		engine.Delete()
		return nil, fmt.Errorf("load QML window")
	}
	window := item.Window()
	graphics := rhi.NewQQuickGraphicsConfiguration()
	graphics.SetTimestamps(true)
	window.SetGraphicsConfiguration(graphics)
	graphics.Delete()
	if options.foreground {
		window.SetFlag(qt.WindowStaysOnTopHint)
	}
	visible := qt.NewQVariant8(true)
	engine.RootObjects()[0].SetProperty("visible", visible)
	visible.Delete()
	if options.foreground {
		window.Raise()
		window.RequestActivate()
	}
	return engine, nil
}
