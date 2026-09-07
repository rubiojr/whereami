package main

import (
	_ "embed"
	"errors"
	"flag"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/qml"
	"github.com/rubiojr/whereami/internal/admincache"
	"github.com/rubiojr/whereami/internal/admingeo"
	"github.com/rubiojr/whereami/internal/geodata"
	"github.com/rubiojr/whereami/pkg/logger"
	"github.com/rubiojr/whereami/pkg/vecmap"
)

//go:embed bookmarks.gpx
var embeddedBookmarks []byte

// Global application directories (resolved at startup).
// Set once in main() via command-line flags or XDG rules.
var dataDir string
var configDir string
var cacheDir string

// Global live waypoint store (bookmarks + other GPX waypoints).
var allWaypoints []Waypoint
var allWaypointsMu sync.RWMutex

func main() {
	runtime.LockOSThread()

	// Command-line flags
	debugFlag := flag.Bool("debug", false, "enable debug logging")
	themeFlag := flag.String("theme", "", "theme variant (orange|green|purple|adwaita-dark|nord-polar|nord-frost)")
	dataDirFlag := flag.String("data-dir", "", "custom data directory (overrides XDG_DATA_HOME)")
	configDirFlag := flag.String("config-dir", "", "custom config directory (overrides XDG_CONFIG_HOME)")
	cacheDirFlag := flag.String("cache-dir", "", "custom cache directory (overrides XDG_CACHE_HOME)")
	geodataManifestFlag := flag.String("geodata-manifest", "", "local development geodata manifest override")
	legacyMapRendererFlag := flag.Bool("legacy-map-renderer", false, "use the legacy QtLocation MapLibre renderer")
	vectorPrototypeFlag := flag.Bool("vector-prototype", false, "force the Go vector renderer and enable prototype smoke options")
	vectorPrototypeDurationFlag := flag.Duration("vector-prototype-duration", 0, "quit after this duration for renderer smoke tests")
	vectorPrototypeZoomFlag := flag.Float64("vector-prototype-zoom", 9, "initial zoom for the vector prototype")
	flag.Parse()
	debug := *debugFlag
	themeVariant := *themeFlag

	// Set debug logging
	logger.SetDebug(debug)

	// Fixed loopback API port shared with the QML configuration.
	const apiPort = 43098
	apiToken, err := newAPIToken()
	if err != nil {
		logger.Fatalf("API token generation failed: %v", err)
	}

	// Determine data directory for persistent app storage (bookmarks, imported GPX, databases).
	// Precedence: --data-dir flag > $XDG_DATA_HOME > $HOME/.local/share/whereami > CWD fallback.
	// Set global directory variables based on flags or XDG defaults
	if *dataDirFlag != "" {
		dataDir = *dataDirFlag
	} else {
		dataDir = filepath.Join(xdgDataDir(), "whereami")
	}
	if err := ensureDir(dataDir); err != nil {
		logger.Error("Failed to create data dir %s: %v", dataDir, err)
	}

	if *configDirFlag != "" {
		configDir = *configDirFlag
	} else {
		configDir = filepath.Join(xdgConfigDir(), "whereami")
	}
	if err := ensureDir(configDir); err != nil {
		logger.Error("Failed to create config dir %s: %v", configDir, err)
	}

	if *cacheDirFlag != "" {
		cacheDir = *cacheDirFlag
	} else {
		cacheDir = filepath.Join(xdgCacheDir(), "whereami")
	}
	if err := ensureDir(cacheDir); err != nil {
		logger.Error("Failed to create cache dir %s: %v", cacheDir, err)
	}

	// Canonical bookmarks path (migrated from legacy per-flag directory location).
	bookmarksPath := filepath.Join(dataDir, "bookmarks.gpx")

	// Copy embedded bookmarks.gpx to data directory if it doesn't exist
	if !fileExists(bookmarksPath) {
		if err := copyEmbeddedBookmarks(bookmarksPath); err != nil {
			logger.Error("Failed to copy default bookmarks to %s: %v", bookmarksPath, err)
		} else {
			logger.Debug("Copied default bookmarks to %s", bookmarksPath)
		}
	}
	if err := openObservationIndex(dataDir, bookmarksPath); err != nil {
		logger.Error("Failed to initialize observation index: %v", err)
	} else {
		defer closeObservationIndex()
	}
	var geoService *geodataService
	if *geodataManifestFlag == "" {
		geoService, err = openGeodataService(filepath.Join(dataDir, "geodata", "admin"))
	} else {
		geoService, err = openGeodataServiceFile(filepath.Join(dataDir, "geodata", "admin"), *geodataManifestFlag)
	}
	if err != nil {
		logger.Error("Failed to initialize administrative geodata: %v", err)
	} else {
		defer geoService.Close()
	}
	var timelineService *timelineService
	if observationRepo != nil && geoService != nil {
		var resolutionCache *admincache.Store
		resolutionCache, err = admincache.Open(filepath.Join(cacheDir, "observations", "administrative.sqlite"))
		if err != nil {
			logger.Error("Failed to initialize administrative resolution cache: %v", err)
		} else {
			defer resolutionCache.Close()
		}
		var resolutionWarmer *admincache.Warmer
		if resolutionCache != nil {
			resolutionWarmer, err = admincache.NewWarmer(observationRepo, resolutionCache, func() (admingeo.Resolver, error) {
				lease, acquireErr := geoService.manager.Acquire()
				if acquireErr != nil {
					if errors.Is(acquireErr, geodata.ErrNoActiveGeneration) {
						return nil, nil
					}
					return nil, acquireErr
				}
				return lease, nil
			}, func() []admingeo.DatasetVersion {
				status := geoService.manager.Status()
				versions := make([]admingeo.DatasetVersion, 0, 2)
				if status.Current.Valid {
					versions = append(versions, status.Current.DatasetVersion)
				}
				if status.Previous.Valid {
					versions = append(versions, status.Previous.DatasetVersion)
				}
				return versions
			}, func(warmErr error) {
				logger.Error("Administrative resolution cache warming failed: %v", warmErr)
			})
			if err != nil {
				logger.Error("Failed to start administrative resolution cache warmer: %v", err)
			} else {
				defer resolutionWarmer.Close()
				observationIndexRebuilt = resolutionWarmer.Trigger
				geoService.SetActivationCallback(resolutionWarmer.Trigger)
				resolutionWarmer.Trigger()
			}
		}
		timelineService = newTimelineService(observationRepo, geoService.manager, resolutionCache, resolutionWarmer)
		defer timelineService.Close()
	}

	// Register HTTP API handlers.
	RegisterAPI(http.DefaultServeMux, bookmarksPath)
	RegisterGeodataAPI(http.DefaultServeMux, geoService)
	RegisterTimelineAPI(http.DefaultServeMux, timelineService)

	// Build initial waypoint list (bookmarks + imported GPX) using centralized dedupe helper.
	initial := RebuildAllWaypoints(bookmarksPath, dataDir)

	allWaypointsMu.Lock()
	allWaypoints = initial
	allWaypointsMu.Unlock()

	// Bind before starting Qt so a conflicting instance cannot leave a broken UI running.
	addr := "127.0.0.1:43098"
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		logger.Fatalf("Bookmark API server bind error on %s: %v", addr, err)
	}
	server := &http.Server{
		Handler:           secureAPI(apiToken, http.DefaultServeMux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			logger.Error("Bookmark API server error on %s: %v", addr, err)
		}
	}()

	// Prepare arguments for Qt; append a synthetic --theme=<variant> so QML can always detect it
	qtArgs := os.Args
	if themeVariant != "" {
		qtArgs = append(qtArgs, "--theme="+themeVariant)
	}

	// Set Material theme to dark mode
	os.Setenv("QT_QUICK_CONTROLS_STYLE", "Material")
	os.Setenv("QT_QUICK_CONTROLS_MATERIAL_THEME", "Dark")
	qt.QCoreApplication_SetApplicationName("io.github.rubiojr.whereami")

	qt.NewQApplication(qtArgs)
	engine := qml.NewQQmlApplicationEngine()
	engine.RootContext().SetContextProperty2("whereamiApiToken", qt.NewQVariant14(apiToken))
	engine.RootContext().SetContextProperty2("whereamiMapCacheDir", qt.NewQVariant14(mapCacheDir()))
	type vectorMapInstance struct {
		name string
		item *vecmap.Item
	}
	var vectorMaps []vectorMapInstance
	goVectorMaps := !*legacyMapRendererFlag || *vectorPrototypeFlag
	if goVectorMaps {
		vectorCacheDir := filepath.Join(effectiveCacheDir(), "vector")
		initialVectorCamera := vecmap.NewCamera(
			vecmap.Coordinate{Latitude: 40.4168, Longitude: -3.7038},
			9,
			0,
			0,
			0,
		)
		vectorOptions := vecmap.Options{CacheDir: vectorCacheDir, InitialCamera: &initialVectorCamera}
		mainMap, mainErr := vecmap.NewWithOptions(vectorOptions)
		var timelineMap *vecmap.Item
		if mainErr == nil {
			timelineMap, err = vecmap.NewWithOptions(vectorOptions)
		} else {
			err = mainErr
		}
		if err != nil {
			if mainMap != nil {
				mainMap.Close()
			}
			logger.Error("Go vector map initialization failed, using legacy renderer: %v", err)
			goVectorMaps = false
		} else {
			if *vectorPrototypeFlag {
				mainMap.SetZoom(*vectorPrototypeZoomFlag)
			}
			vectorMaps = []vectorMapInstance{{name: "main", item: mainMap}, {name: "timeline", item: timelineMap}}
			qml.QJSEngine_SetObjectOwnership(mainMap.QObject(), qml.QJSEngine__CppOwnership)
			qml.QJSEngine_SetObjectOwnership(mainMap.CameraQObject(), qml.QJSEngine__CppOwnership)
			qml.QJSEngine_SetObjectOwnership(timelineMap.QObject(), qml.QJSEngine__CppOwnership)
			qml.QJSEngine_SetObjectOwnership(timelineMap.CameraQObject(), qml.QJSEngine__CppOwnership)
			engine.RootContext().SetContextProperty("whereamiMainVectorItem", mainMap.QObject())
			engine.RootContext().SetContextProperty("whereamiMainVectorCamera", mainMap.CameraQObject())
			engine.RootContext().SetContextProperty("whereamiTimelineVectorItem", timelineMap.QObject())
			engine.RootContext().SetContextProperty("whereamiTimelineVectorCamera", timelineMap.CameraQObject())
			if *vectorPrototypeFlag {
				engine.RootContext().SetContextProperty2("whereamiVectorPrototypeInitialZoom", qt.NewQVariant9(*vectorPrototypeZoomFlag))
			}
		}
	}
	engine.RootContext().SetContextProperty2("whereamiGoVectorMaps", qt.NewQVariant8(goVectorMaps))

	// Load QML from Qt resources (qrc:/)
	engine.Load(qt.NewQUrl3("qrc:/components/Main.qml"))
	if len(engine.RootObjects()) == 0 {
		logger.Fatal("QML load failed: no root objects (check QML errors / Qt Location).")
	}
	var vectorPrototypeTimer *qt.QTimer
	if *vectorPrototypeDurationFlag > 0 {
		vectorPrototypeTimer = qt.NewQTimer()
		vectorPrototypeTimer.SetSingleShot(true)
		vectorPrototypeTimer.OnTimeout(qt.QCoreApplication_Quit)
		vectorPrototypeTimer.Start(int(vectorPrototypeDurationFlag.Milliseconds()))
	}
	logger.Debug("Bookmark API fixed port: http://127.0.0.1:%d/api/bookmarks", apiPort)
	qt.QApplication_Exec()
	if vectorPrototypeTimer != nil {
		vectorPrototypeTimer.Delete()
	}
	engine.Delete()
	for _, vectorMap := range vectorMaps {
		vectorMap.item.Close()
		stats := vectorMap.item.Stats()
		logger.Debug(
			"Go vector %s map: paint updates=%d geometry builds=%d geometry updates=%d geometry removals=%d transform updates=%d tiles requested=%d loaded=%d fallback=%d loading=%d errors=%d Liberty layers=%d triangles=%d symbols=%d SDF labels=%d atlas glyphs=%d rasters=%d land features=%d triangles=%d water features=%d triangles=%d road features=%d segments=%d",
			vectorMap.name,
			stats.PaintNodeUpdates,
			stats.GeometryBuilds,
			stats.GeometryUpdates,
			stats.GeometryRemovals,
			stats.TransformUpdates,
			stats.TilesRequested,
			stats.TilesLoaded,
			stats.FallbackTiles,
			stats.TilesLoading,
			stats.TileErrors,
			stats.LibertyLayers,
			stats.LibertyTriangles,
			stats.SymbolCandidates,
			stats.SDFLabels,
			stats.SDFAtlasGlyphs,
			stats.RasterTiles,
			stats.LandFeatures,
			stats.LandTriangles,
			stats.WaterFeatures,
			stats.WaterTriangles,
			stats.RoadFeatures,
			stats.RoadSegments,
		)
		if stats.TileError != "" {
			logger.Error("Go vector %s map tile load failed: %s", vectorMap.name, stats.TileError)
		}
	}
}

// mapCacheDir resolves the directory holding MapLibre's persistent vector map
// cache. It keeps the map cache under the effective cache directory so
// --cache-dir governs it like every other cached artifact. An empty result
// leaves the renderer with an in-memory cache for the session.
func mapCacheDir() string {
	dir := filepath.Join(effectiveCacheDir(), "maplibre")
	if err := ensureDir(dir); err != nil {
		logger.Error("Failed to create map cache dir %s: %v", dir, err)
		return ""
	}
	return dir
}

// copyEmbeddedBookmarks writes the embedded bookmarks.gpx to the specified path.
func copyEmbeddedBookmarks(destPath string) error {
	// Ensure the parent directory exists
	if err := ensureDir(filepath.Dir(destPath)); err != nil {
		return err
	}

	// Create the destination file
	file, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Copy the embedded content
	_, err = io.Copy(file, strings.NewReader(string(embeddedBookmarks)))
	return err
}
