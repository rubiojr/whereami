package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"
	"runtime"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/qml"
	"github.com/rubiojr/whereami/pkg/vecmap"
)

func main() {
	runtime.LockOSThread()
	duration := flag.Duration("duration", 0, "quit after this duration")
	flag.Parse()
	qt.QCoreApplication_SetApplicationName("vecmap-example")
	qt.NewQApplication(os.Args)

	camera := vecmap.NewCamera(
		vecmap.Coordinate{Latitude: 51.5074, Longitude: -0.1278},
		10, 0, 0, 0,
	)
	item, err := vecmap.NewWithOptions(vecmap.Options{
		CacheDir:      filepath.Join(qt.QStandardPaths_WritableLocation(qt.QStandardPaths__CacheLocation), "vector"),
		InitialCamera: &camera,
	})
	if err != nil {
		log.Fatal(err)
	}

	engine := qml.NewQQmlApplicationEngine()
	engine.RootContext().SetContextProperty("mapItem", item.QObject())
	engine.RootContext().SetContextProperty("mapCamera", item.CameraQObject())
	engine.Load(qt.NewQUrl3("qrc:/Main.qml"))
	if len(engine.RootObjects()) == 0 {
		log.Fatal("failed to load Main.qml")
	}
	var timer *qt.QTimer
	if *duration > 0 {
		timer = qt.NewQTimer()
		timer.SetSingleShot(true)
		timer.OnTimeout(qt.QCoreApplication_Quit)
		timer.Start(int(duration.Milliseconds()))
	}

	qt.QApplication_Exec()
	if timer != nil {
		timer.Delete()
	}
	engine.Delete()
	item.Close()
	stats := item.Stats()
	log.Printf(
		"tiles=%d/%d loading=%d errors=%d labels=%d basemap_nodes=%d/%d",
		stats.TilesLoaded,
		stats.TilesRequested,
		stats.TilesLoading,
		stats.TileErrors,
		stats.SDFLabels,
		stats.BasemapNodeBuilds,
		stats.BasemapNodeRemovals,
	)
}
