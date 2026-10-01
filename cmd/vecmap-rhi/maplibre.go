//go:build vecmap_rhi

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/qml"
	rhi "github.com/rubiojr/whereami/internal/qtrhi"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
)

// mapLibreQuiet is how long the final camera must go without a frame before
// MapLibre Native counts as settled.
const mapLibreQuiet = time.Second

// frameEnds records when the render loop finished frames.
type frameEnds struct {
	count     uint64
	last      time.Time
	intervals []time.Duration
}

func (f *frameEnds) add(now time.Time) {
	if !f.last.IsZero() && len(f.intervals) < 60000 {
		f.intervals = append(f.intervals, now.Sub(f.last))
	}
	f.count++
	f.last = now
}

// mapLibreSettled reports whether the map went quiet after the trace ended, and
// how long after the trace end its last frame finished.
func mapLibreSettled(now, traceEnd, lastFrame time.Time) (bool, time.Duration) {
	after := max(0, lastFrame.Sub(traceEnd))
	return now.Sub(traceEnd.Add(after)) >= mapLibreQuiet, after
}

// displayMapLibre replays the live trace in QtLocation's MapLibre Native map,
// in the window, timer, trace and diagnostics vecmap uses, so both renderers are
// measured the same way. QtLocation's map renders only while MapLibre has work,
// so the last frame after the trace ends marks settlement.
func displayMapLibre(options benchmarkOptions) error {
	if os.Getenv("QSG_RHI_BACKEND") != "opengl" {
		return fmt.Errorf("MapLibre Native Qt renders with OpenGL; set QSG_RHI_BACKEND=opengl")
	}
	style, err := filepath.Abs(options.mapLibreStyle)
	if err != nil {
		return err
	}
	if _, err := os.Stat(style); err != nil {
		return fmt.Errorf("MapLibre style: %w", err)
	}
	live := options.liveOptions
	camera := view.NewCamera(view.Coordinate{Latitude: live.latitude, Longitude: live.longitude}, live.zoom, 0, 800, 600)
	document := scene.Document{Width: 800, Height: 600, Camera: &camera, TileSpaces: []scene.TileSpace{{}}}
	app := qt.NewQApplication([]string{"vecmap-rhi"})
	defer app.Delete()
	engine := qml.NewQQmlApplicationEngine()
	engine.LoadData([]byte(fmt.Sprintf(`import QtQuick
import QtQuick.Window
import QtLocation
import QtPositioning
Window {id:window; visible:false; width:%d; height:%d; color:"#f8f4f0"; title:"MapLibre Native — vecmap comparison"
 property real latitude:%g; property real longitude:%g; property real zoom:%g; property real bearing:0
 %s
 Map {anchors.fill:parent; copyrightsVisible:false
  plugin: Plugin {name:"maplibre"; PluginParameter {name:"maplibre.map.styles"; value:%s}}
  center: QtPositioning.coordinate(window.latitude, window.longitude); zoomLevel:window.zoom; bearing:window.bearing
 }
}`, document.Width, document.Height, live.latitude, live.longitude, live.zoom, swapDiagnostics(options), strconv.Quote("file://"+filepath.ToSlash(style)))))
	if len(engine.RootObjects()) == 0 {
		engine.Delete()
		return fmt.Errorf("load MapLibre QML window; is the maplibre geoservice plugin on QT_PLUGIN_PATH?")
	}
	root := engine.RootObjects()[0]
	window := rhi.UnsafeNewQQuickWindow(root.UnsafePointer())
	showBenchmarkWindow(root, window, options)
	var mu sync.Mutex
	var frames frameEnds
	var pacing pacingSamples
	var waits swapchainWaits
	connections := []*rhi.SignalConnection{window.OnAfterFrameEnd(func() {
		now := time.Now()
		mu.Lock()
		defer mu.Unlock()
		frames.add(now)
	})}
	if options.diagnostics {
		connections = append(connections, connectSwapchainWaits(window, &mu, &waits)...)
	}
	setReal := func(name string, value float64) {
		v := qt.NewQVariant9(value)
		root.SetProperty(name, v)
		v.Delete()
	}
	start := time.Now()
	var result error
	var settledAfter time.Duration
	timer := qt.NewQTimer()
	timer.OnTimeout(func() {
		now := time.Now()
		mu.Lock()
		lastFrame, count := frames.last, frames.count
		mu.Unlock()
		elapsed := now.Sub(start)
		// After the trace the map stops rendering once loaded; vecmap's item keeps
		// redrawing. That idle wait for settlement is not a pacing gap.
		idle := options.duration > 0 && elapsed >= options.duration && now.Sub(lastFrame) >= 100*time.Millisecond
		if options.diagnostics && !idle {
			state := windowState{Visible: window.IsVisible(), Active: window.IsActive(), Exposed: window.IsExposed()}
			swaps := root.Property("swapCount")
			state.Swaps = swaps.ToInt()
			mu.Lock()
			pacing.add(now, lastFrame, count, state, waits.takeLongest())
			mu.Unlock()
		}
		traceTime := elapsed.Seconds()
		if options.duration > 0 {
			traceTime = min(traceTime, options.duration.Seconds())
		}
		geographic := traceCamera(document, traceTime, options.animate).geographic
		setReal("latitude", geographic.Center.Latitude)
		setReal("longitude", geographic.Center.Longitude)
		setReal("zoom", geographic.Zoom)
		setReal("bearing", geographic.Bearing)
		if options.duration <= 0 || elapsed < options.duration {
			return
		}
		settled, after := mapLibreSettled(now, start.Add(options.duration), lastFrame)
		if !settled && elapsed < options.duration+10*time.Second {
			return
		}
		settledAfter = after
		if !settled {
			result = fmt.Errorf("MapLibre did not settle: still rendering %s after the trace", elapsed-options.duration)
		} else if options.screenshot != "" {
			image := window.GrabWindow()
			if image.IsNull() || !image.Save(options.screenshot) {
				result = fmt.Errorf("save screenshot %q", options.screenshot)
			}
			image.Delete()
		}
		qt.QCoreApplication_Quit()
	})
	timer.Start(8)
	qt.QApplication_Exec()
	timer.Delete()
	for _, connection := range connections {
		connection.Disconnect()
	}
	costs, costsErr := readProcessCosts() // before teardown closes DRM clients
	engine.Delete()
	mu.Lock()
	defer mu.Unlock()
	fmt.Printf("platform=%s foreground=%t renderer=maplibre style=%s\n", qt.QGuiApplication_PlatformName(), options.foreground, style)
	if options.diagnostics {
		pacing.report()
		waits.report()
	}
	fmt.Printf("frames=%d settled_after=%s\n", frames.count, settledAfter)
	if values := slices.Clone(frames.intervals); len(values) > 0 {
		slices.Sort(values)
		fmt.Printf("frame_end_interval samples=%d p50=%s p95=%s p99=%s\n", len(values), values[len(values)/2], values[min(len(values)-1, len(values)*95/100)], values[min(len(values)-1, len(values)*99/100)])
	}
	if costsErr == nil {
		costs.report()
	}
	if frames.count == 0 {
		return fmt.Errorf("MapLibre produced no frames")
	}
	return result
}
