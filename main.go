package main

// NOTE: Location tracking desktop file id updated to io.github.rubiojr.whereami.desktop (actual InitLocationTracking call now lives in api.go)

import (
	_ "embed"
	"flag"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/qml"
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
// Waypoint type & persistence helpers now live in storage.go.
var allWaypoints []Waypoint
var allWaypointsMu sync.RWMutex

func main() {
	// Command-line flags
	debugFlag := flag.Bool("debug", false, "enable debug logging (verbose tile proxy requests)")
	themeFlag := flag.String("theme", "", "theme variant (orange|green|purple|adwaita-dark|nord-polar|nord-frost)")
	dataDirFlag := flag.String("data-dir", "", "custom data directory (overrides XDG_DATA_HOME)")
	configDirFlag := flag.String("config-dir", "", "custom config directory (overrides XDG_CONFIG_HOME)")
	cacheDirFlag := flag.String("cache-dir", "", "custom cache directory (overrides XDG_CACHE_HOME)")
	flag.Parse()
	debug := *debugFlag
	themeVariant := *themeFlag

	// Set debug logging
	logger.SetDebug(debug)

	// Hardcoded API port (as requested)
	const apiPort = 43098

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

	// Legacy bookmark migration removed; using only XDG dataDir location now.

	// Register HTTP API handlers (moved to api.go)
	RegisterAPI(http.DefaultServeMux, bookmarksPath, debug)

	// /api/location endpoint moved to api.go (lazy initialization handled there)

	// (Removed HTTP /qml/ handler — using local temp materialization instead)

	// Start server on fixed port 43098
	go func() {
		addr := "127.0.0.1:43098"
		if err := http.ListenAndServe(addr, nil); err != nil {
			logger.Error("Bookmark API server error on %s: %v", addr, err)
		}
	}()

	// Build initial waypoint list (bookmarks + imported GPX) using centralized dedupe helper.
	initial := RebuildAllWaypoints(bookmarksPath, dataDir)

	allWaypointsMu.Lock()
	allWaypoints = initial
	allWaypointsMu.Unlock()

	// Prepare arguments for Qt; append a synthetic --theme=<variant> so QML can always detect it
	qtArgs := os.Args
	if themeVariant != "" {
		qtArgs = append(qtArgs, "--theme="+themeVariant)
	}
	qt.QCoreApplication_SetApplicationName("io.github.rubiojr.whereami")
	qt.NewQApplication(qtArgs)
	engine := qml.NewQQmlApplicationEngine()

	// Load QML from Qt resources (qrc:/)
	engine.Load(qt.NewQUrl3("qrc:/components/MapView.qml"))
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
