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
	"strings"
	"sync"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/qml"
	"github.com/rubiojr/whereami/internal/admincache"
	"github.com/rubiojr/whereami/internal/admingeo"
	"github.com/rubiojr/whereami/internal/geodata"
	"github.com/rubiojr/whereami/pkg/logger"
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
	// Command-line flags
	debugFlag := flag.Bool("debug", false, "enable debug logging (verbose tile proxy requests)")
	themeFlag := flag.String("theme", "", "theme variant (orange|green|purple|adwaita-dark|nord-polar|nord-frost)")
	dataDirFlag := flag.String("data-dir", "", "custom data directory (overrides XDG_DATA_HOME)")
	configDirFlag := flag.String("config-dir", "", "custom config directory (overrides XDG_CONFIG_HOME)")
	cacheDirFlag := flag.String("cache-dir", "", "custom cache directory (overrides XDG_CACHE_HOME)")
	geodataManifestFlag := flag.String("geodata-manifest", "", "local development geodata manifest override")
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
	var reportService *placeReportService
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
		reportService = newPlaceReportService(observationRepo, geoService.manager, resolutionCache, resolutionWarmer)
		defer reportService.Close()
	}

	// Register HTTP API handlers.
	RegisterAPI(http.DefaultServeMux, bookmarksPath, debug)
	RegisterGeodataAPI(http.DefaultServeMux, geoService)
	RegisterPlaceReportAPI(http.DefaultServeMux, reportService)

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

	// Load QML from Qt resources (qrc:/)
	engine.Load(qt.NewQUrl3("qrc:/components/Main.qml"))
	if len(engine.RootObjects()) == 0 {
		logger.Fatal("QML load failed: no root objects (check QML errors / Qt Location).")
	}
	logger.Debug("Bookmark API fixed port: http://127.0.0.1:%d/api/bookmarks", apiPort)
	qt.QApplication_Exec()
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
