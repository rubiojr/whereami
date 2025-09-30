package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

/*
GeoClue (geoclue2) location integration.

Overview:
  - On startup call:
        err := InitLocationTracking("whereami.desktop")
    This will:
      * Ensure a matching .desktop file exists (writes one into
        ~/.local/share/applications if missing).
      * Spawn a goroutine that connects to GeoClue on the system bus,
        creates a client, sets accuracy & thresholds, starts updates,
        and listens for property changes to keep the in‑memory location
        fresh.
  - Optionally call RegisterLocationAPI(http.DefaultServeMux) to expose
        GET /api/location  (200 JSON or 204 if unknown)

Data exposed:
  currentLocation   (guarded by locationMu)
  locationValid     (true once we have at least one fix)

Failure strategy:
  - If GeoClue is unavailable or permission denied, we log and
    continue (API will return 204 No Content).
  - The goroutine retries a few times initially, then backs off.

Security / Permissions:
  - GeoClue requires a valid DesktopId property that matches a
    .desktop file (basename) in XDG data dirs and contains
    X-Geoclue-2-Client=true.
  - Without it you'll usually get org.freedesktop.DBus.Error.AccessDenied
    or the Start call will silently not produce locations.

Adding dependency:
  - Ensure go.mod has:  require github.com/godbus/dbus/v5 latest
*/

const (
	geoService    = "org.freedesktop.GeoClue2"
	managerPath   = dbus.ObjectPath("/org/freedesktop/GeoClue2/Manager")
	managerIface  = "org.freedesktop.GeoClue2.Manager"
	clientIface   = "org.freedesktop.GeoClue2.Client"
	locationIface = "org.freedesktop.GeoClue2.Location"
	propsIface    = "org.freedesktop.DBus.Properties"
)

// LocationFix holds the last known position.
type LocationFix struct {
	Latitude  float64   `json:"lat"`
	Longitude float64   `json:"lon"`
	Accuracy  float64   `json:"accuracy_m,omitempty"`
	Altitude  float64   `json:"altitude_m,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// Shared state.
var (
	locationMu      sync.RWMutex
	currentLocation LocationFix
	locationValid   bool

	locationCancel context.CancelFunc
)

// InitLocationTracking ensures a .desktop file is present and starts GeoClue client tracking.
func InitLocationTracking(desktopID string) error {
	if err := ensureDesktopFile(desktopID); err != nil {
		// Non-fatal but inform user.
		log.Printf("location: failed to ensure desktop file: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	locationCancel = cancel
	go runGeoClueLoop(ctx, desktopID)
	return nil
}

// StopLocationTracking stops the background loop (optional).
func StopLocationTracking() {
	if locationCancel != nil {
		locationCancel()
	}
}

// RegisterLocationAPI registers /api/location endpoint.
func RegisterLocationAPI(mux *http.ServeMux) {
	if mux == nil {
		mux = http.DefaultServeMux
	}
	mux.HandleFunc("/api/location", func(w http.ResponseWriter, r *http.Request) {
		locationMu.RLock()
		defer locationMu.RUnlock()
		if !locationValid {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(currentLocation)
	})
}

// ensureDesktopFile writes a minimal desktop file if it does not already exist.
// Returns nil if the file already exists.
func ensureDesktopFile(desktopID string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	appsDir := filepath.Join(home, ".local", "share", "applications")
	if err := os.MkdirAll(appsDir, 0o755); err != nil {
		return err
	}
	dest := filepath.Join(appsDir, desktopID)
	if _, err := os.Stat(dest); err == nil {
		// Exists; do not overwrite to allow user customization.
		return nil
	}
	content := `[Desktop Entry]
Type=Application
Name=WhereAmI
Comment=Waypoint viewer (GeoClue client)
Exec=whereami
Icon=whereami
Terminal=false
Categories=Utility;
X-Geoclue-2-Client=true
X-Geoclue-2-Access-Fine=true
`
	return os.WriteFile(dest, []byte(content), 0o644)
}

// -- GeoClue integration internals --

type geoClient struct {
	path dbus.ObjectPath
	bus  *dbus.Conn
}

// runGeoClueLoop keeps trying to establish location updates until context cancelled.
func runGeoClueLoop(ctx context.Context, desktopID string) {
	const (
		maxInitialRetries = 5
		retryBaseDelay    = 2 * time.Second
		requestedAccuracy = uint32(5)  // "exact"
		distanceThreshold = uint32(25) // meters between updates
		timeThreshold     = uint32(5)  // seconds between updates
	)

	var attempt int
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		err := func() error {
			cl, err := newGeoClueClient(desktopID, requestedAccuracy, distanceThreshold, timeThreshold)
			if err != nil {
				return err
			}
			defer cl.close()
			if err := cl.start(); err != nil {
				return err
			}
			// Get initial fix (if any)
			cl.fetchInitialLocation()
			// Subscribe to updates (blocks until context canceled or bus error)
			return cl.runSignalLoop(ctx)
		}()
		if err == nil {
			return
		}
		attempt++
		var delay time.Duration
		if attempt <= maxInitialRetries {
			delay = retryBaseDelay * time.Duration(attempt)
		} else {
			delay = 30 * time.Second
		}
		log.Printf("location: retrying after error (%v), attempt=%d delay=%s", err, attempt, delay)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		}
	}
}

func newGeoClueClient(desktopID string, acc, dist, sec uint32) (*geoClient, error) {
	bus, err := dbus.SystemBus()
	if err != nil {
		return nil, err
	}
	manager := bus.Object(geoService, managerPath)

	var clientPath dbus.ObjectPath
	if call := manager.Call(managerIface+".CreateClient", 0); call.Err != nil {
		return nil, call.Err
	} else if err := call.Store(&clientPath); err != nil {
		return nil, err
	}
	clientObj := bus.Object(geoService, clientPath)

	// Helper to set property.
	setProp := func(name string, val interface{}) error {
		call := clientObj.Call(propsIface+".Set", 0, clientIface, name, dbus.MakeVariant(val))
		return call.Err
	}

	if err := setProp("DesktopId", desktopID); err != nil {
		return nil, fmt.Errorf("set DesktopId: %w", err)
	}
	if err := setProp("RequestedAccuracyLevel", acc); err != nil {
		return nil, fmt.Errorf("set accuracy: %w", err)
	}
	_ = setProp("DistanceThreshold", dist)
	_ = setProp("TimeThreshold", sec)

	return &geoClient{path: clientPath, bus: bus}, nil
}

func (c *geoClient) start() error {
	call := c.bus.Object(geoService, c.path).Call(clientIface+".Start", 0)
	return call.Err
}

func (c *geoClient) close() {
	_ = c.bus.Object(geoService, c.path).Call(clientIface+".Stop", 0)
	c.bus.Close()
}

func (c *geoClient) fetchInitialLocation() {
	locPath, err := c.getLocationPath()
	if err != nil || locPath == "" {
		return
	}
	c.readAndStoreLocation(locPath)
}

func (c *geoClient) getLocationPath() (dbus.ObjectPath, error) {
	var variant dbus.Variant
	call := c.bus.Object(geoService, c.path).Call(propsIface+".Get", 0, clientIface, "Location")
	if call.Err != nil {
		return "", call.Err
	}
	if err := call.Store(&variant); err != nil {
		return "", err
	}
	locPath, _ := variant.Value().(dbus.ObjectPath)
	return locPath, nil
}

func (c *geoClient) runSignalLoop(ctx context.Context) error {
	// Match rule for PropertiesChanged on the client path
	matchRule := fmt.Sprintf("type='signal',interface='%s',path='%s'", propsIface, c.path)
	if call := c.bus.BusObject().Call("org.freedesktop.DBus.AddMatch", 0, matchRule); call.Err != nil {
		return call.Err
	}
	sigCh := make(chan *dbus.Signal, 10)
	c.bus.Signal(sigCh)

	for {
		select {
		case <-ctx.Done():
			return nil
		case sig := <-sigCh:
			if sig == nil {
				return errors.New("dbus signal channel closed")
			}
			if sig.Name == propsIface+".PropertiesChanged" && sig.Path == c.path {
				// Body[1] should be changed map[string]Variant
				if len(sig.Body) >= 2 {
					if changed, ok := sig.Body[1].(map[string]dbus.Variant); ok {
						if v, ok := changed["Location"]; ok {
							if lp, ok := v.Value().(dbus.ObjectPath); ok && lp != "" {
								c.readAndStoreLocation(lp)
							}
						}
					}
				}
			}
		}
	}
}

func (c *geoClient) readAndStoreLocation(locPath dbus.ObjectPath) {
	locObj := c.bus.Object(geoService, locPath)
	var props map[string]dbus.Variant
	call := locObj.Call(propsIface+".GetAll", 0, locationIface)
	if call.Err != nil {
		return
	}
	if err := call.Store(&props); err != nil {
		return
	}

	getF64 := func(key string) (float64, bool) {
		if v, ok := props[key]; ok {
			if f, ok2 := v.Value().(float64); ok2 {
				return f, true
			}
		}
		return 0, false
	}

	lat, _ := getF64("Latitude")
	lon, _ := getF64("Longitude")
	acc, _ := getF64("Accuracy")
	alt, _ := getF64("Altitude")

	if lat == 0 && lon == 0 {
		return // ignore obviously invalid fix
	}

	locationMu.Lock()
	currentLocation = LocationFix{
		Latitude:  lat,
		Longitude: lon,
		Accuracy:  acc,
		Altitude:  alt,
		Timestamp: time.Now().UTC(),
	}
	locationValid = true
	locationMu.Unlock()
}

// Helper so other packages (or QML integration wrappers later) can get current fix.
func GetCurrentLocation() (LocationFix, bool) {
	locationMu.RLock()
	defer locationMu.RUnlock()
	if !locationValid {
		return LocationFix{}, false
	}
	return currentLocation, true
}
