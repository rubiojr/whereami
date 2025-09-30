package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rubiojr/whereami/pkg/gominatim"
	"github.com/rubiojr/whereami/pkg/logger"
	_ "modernc.org/sqlite"
)

//
// Shared / external symbols expected elsewhere in the project:
//
//   type Waypoint struct { Name string; Lat,Lon float64; Desc string; Bookmark bool; ... }
//   var (allWaypoints []Waypoint; allWaypointsMu sync.RWMutex)
//   func appendBookmark(path string, wp Waypoint) (Waypoint, error)
//   func deleteBookmark(path, name string, lat, lon float64) (bool, error)
//   func renameBookmark(path, oldName string, lat, lon float64, newName string) (bool, error)
//   var ErrDuplicate error
//   func DedupeWaypoints([]Waypoint) []Waypoint
//   func parseGPXFile(path string) ([]Waypoint, error)
//   var (dataDir string)
//   Location: locationMu, locationValid, currentLocation, InitLocationTracking(...)
//

// -------- Tile proxy configuration & metrics (globals retained for backward compatibility) -----

var locationOnce sync.Once

// Environment variable keys
var (
	tileCacheDirEnv          = "WHEREAMI_TILE_CACHE_DIR"
	tileCacheTTLEnv          = "WHEREAMI_TILE_CACHE_TTL"
	tileDiskTTLEnv           = "WHEREAMI_TILE_DISK_TTL"
	tileCacheMaxEntriesEnv   = "WHEREAMI_TILE_CACHE_MAX"
	tileUpstreamEnv          = "WHEREAMI_TILE_UPSTREAM"
	tileTimeoutEnv           = "WHEREAMI_TILE_TIMEOUT"
	tileDiskPruneIntervalEnv = "WHEREAMI_TILE_PRUNE_INTERVAL"
	tileCacheMaxBytesEnv     = "WHEREAMI_TILE_CACHE_MAX_BYTES"
)

// Defaults
const (
	defaultTileCacheMaxBytes int64 = 256 * 1024 * 1024
	defaultCacheTTL                = 1 * time.Hour
	defaultDiskTTL                 = 0 // Never expire disk cache (0 = infinite)
	defaultDiskPruneInterval       = 3 * time.Minute
	defaultMaxEntries              = 20000
	defaultUpstreamTemplate        = "https://cartodb-basemaps-a.global.ssl.fastly.net/rastertiles/voyager/%d/%d/%d@2x.png"
)

var (
	tileCacheDir                        = ""
	tileCacheTTL                        = defaultCacheTTL
	tileDiskTTL           time.Duration = defaultDiskTTL
	tileCacheMaxEntries                 = defaultMaxEntries
	tileDiskPruneInterval               = defaultDiskPruneInterval
	tileCacheMaxBytes                   = defaultTileCacheMaxBytes
	tileUpstreamTemplate                = defaultUpstreamTemplate
	tileHTTPClient                      = &http.Client{Timeout: 12 * time.Second}
)

// Metrics
var (
	tileHits    uint64 // memory+disk hits
	tileMisses  uint64 // upstream fetches initiated
	tileDiskHit uint64
	tileStored  uint64 // tiles written to disk
	tileErrors  uint64
	tileWaitHit uint64
	tileEvicts  uint64
)

// tileKey + cache entry
type tileKey struct {
	z, x, y int
}
type tileEntry struct {
	data      []byte
	timestamp time.Time
}

type resultTile struct {
	data []byte
	err  error
}

// tileProxy encapsulates state so handlers are small / testable.
type tileProxy struct {
	mu             sync.Mutex
	cache          map[tileKey]*tileEntry
	inFlight       map[tileKey][]chan resultTile
	upstreamFormat string
	ttl            time.Duration
	diskTTL        time.Duration
	maxEntries     int
	diskDir        string
	diskPruneEvery time.Duration
	maxBytes       int64
	client         *http.Client
	debug          bool
	prunerStarted  bool
}

var (
	tileProxyOnce sync.Once
	globalProxy   *tileProxy
)

// Tag database (separate lightweight SQLite store)
var (
	tagDB     *sql.DB
	tagDBOnce sync.Once

	// History database (stores every search query for recency list)
	historyDB     *sql.DB
	historyDBOnce sync.Once
)

// initHistoryDB initializes (idempotently) the persistent query history DB (history.sqlite).
func initHistoryDB() {
	historyDBOnce.Do(func() {
		dir := effectiveDataDir()
		if dir == "" {
			logger.Error("initHistoryDB: no data directory resolved")
			return
		}
		path := filepath.Join(dir, "history.sqlite")
		db, err := sql.Open("sqlite", path)
		if err != nil {
			logger.Error("initHistoryDB: open failed: %v", err)
			return
		}
		// Extended schema: added lat/lon (nullable) to allow revisiting coordinate-based history entries.
		if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS search_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			query TEXT NOT NULL,
			lat REAL,
			lon REAL,
			at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
			logger.Error("initHistoryDB: schema error: %v", err)
			_ = db.Close()
			return
		}
		// In case an older DB already existed without lat/lon, attempt to add them (ignore errors if they exist).
		_, _ = db.Exec(`ALTER TABLE search_history ADD COLUMN lat REAL`)
		_, _ = db.Exec(`ALTER TABLE search_history ADD COLUMN lon REAL`)
		// Performance indices (idempotent):
		// Speeds up:
		//  - Recent distinct queries (GROUP BY query, MAX(id))
		//  - Potential future lookups by timestamp
		_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_search_history_query_id ON search_history(query, id)`)
		_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_search_history_at ON search_history(at)`)
		historyDB = db
	})
}

// initialize tile proxy (idempotent via tileProxyOnce in RegisterAPI)
func initTileProxy(debug bool) *tileProxy {
	// Read env overrides (soft validation)
	if v := os.Getenv(tileCacheDirEnv); v != "" {
		tileCacheDir = v
	}
	if v := os.Getenv(tileCacheTTLEnv); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= time.Minute {
			tileCacheTTL = d
		}
	}
	if v := os.Getenv(tileDiskTTLEnv); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			tileDiskTTL = d
		}
	}
	if v := os.Getenv(tileCacheMaxEntriesEnv); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 100 {
			tileCacheMaxEntries = n
		}
	}
	if v := os.Getenv(tileDiskPruneIntervalEnv); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= time.Minute {
			tileDiskPruneInterval = d
		}
	}
	if v := os.Getenv(tileCacheMaxBytesEnv); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			tileCacheMaxBytes = n
		}
	}
	if v := os.Getenv(tileUpstreamEnv); v != "" {
		if strings.Count(v, "%d") == 3 {
			tileUpstreamTemplate = v
		}
	}
	if v := os.Getenv(tileTimeoutEnv); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			tileHTTPClient = &http.Client{Timeout: d}
		}
	}

	// Derive cache dir if still empty (use effective cache directory)
	if tileCacheDir == "" {
		tileCacheDir = filepath.Join(effectiveCacheDir(), "tiles")
	}
	if tileCacheDir != "" {
		_ = os.MkdirAll(tileCacheDir, 0o755)
	}

	return &tileProxy{
		cache:          make(map[tileKey]*tileEntry),
		inFlight:       make(map[tileKey][]chan resultTile),
		upstreamFormat: tileUpstreamTemplate,
		ttl:            tileCacheTTL,
		diskTTL:        tileDiskTTL,
		maxEntries:     tileCacheMaxEntries,
		diskDir:        tileCacheDir,
		diskPruneEvery: tileDiskPruneInterval,
		maxBytes:       tileCacheMaxBytes,
		client:         tileHTTPClient,
		debug:          debug,
	}
}

func (p *tileProxy) startPrunerOnce() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.prunerStarted || p.diskDir == "" {
		return
	}
	p.prunerStarted = true
	go p.pruneLoop()
}

func (p *tileProxy) pruneLoop() {
	ticker := time.NewTicker(p.diskPruneEvery)
	defer ticker.Stop()
	for range ticker.C {
		p.pruneDisk()
	}
}

func (p *tileProxy) pruneDisk() {
	if p.diskDir == "" || p.diskTTL == 0 {
		return // No disk cache or never expire
	}
	// Remove expired
	_ = filepath.WalkDir(p.diskDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			if time.Since(info.ModTime()) > p.diskTTL {
				_ = os.Remove(path)
			}
		}
		return nil
	})
	// Collect paths
	var paths []string
	_ = filepath.WalkDir(p.diskDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		paths = append(paths, path)
		return nil
	})

	// Trim count
	if len(paths) > p.maxEntries {
		type ft struct {
			path string
			t    time.Time
		}
		var list []ft
		for _, pth := range paths {
			if fi, err := os.Stat(pth); err == nil {
				list = append(list, ft{pth, fi.ModTime()})
			}
		}
		// selection sort oldest first
		for i := 0; i < len(list)-1; i++ {
			min := i
			for j := i + 1; j < len(list); j++ {
				if list[j].t.Before(list[min].t) {
					min = j
				}
			}
			if min != i {
				list[i], list[min] = list[min], list[i]
			}
		}
		excess := len(list) - p.maxEntries
		for i := 0; i < excess; i++ {
			_ = os.Remove(list[i].path)
		}
	}

	// Enforce size
	var total int64
	type ft2 struct {
		path string
		t    time.Time
		sz   int64
	}
	var list2 []ft2
	for _, pth := range paths {
		if fi, err := os.Stat(pth); err == nil {
			total += fi.Size()
			list2 = append(list2, ft2{pth, fi.ModTime(), fi.Size()})
		}
	}
	if total <= p.maxBytes {
		return
	}
	// sort oldest first
	for i := 0; i < len(list2)-1; i++ {
		min := i
		for j := i + 1; j < len(list2); j++ {
			if list2[j].t.Before(list2[min].t) {
				min = j
			}
		}
		if min != i {
			list2[i], list2[min] = list2[min], list2[i]
		}
	}
	for _, e := range list2 {
		if total <= p.maxBytes {
			break
		}
		_ = os.Remove(e.path)
		total -= e.sz
	}
}

func (p *tileProxy) evictIfNeeded() {
	if len(p.cache) <= p.maxEntries {
		return
	}
	var oldest tileKey
	var oldestTime time.Time
	first := true
	for k, v := range p.cache {
		if first || v.timestamp.Before(oldestTime) {
			first = false
			oldestTime = v.timestamp
			oldest = k
		}
	}
	if !first { // means we found something
		delete(p.cache, oldest)
		atomic.AddUint64(&tileEvicts, 1)
	}
}

func (p *tileProxy) serveTile(w http.ResponseWriter, r *http.Request) {
	// Add CORS headers for QML map compatibility
	corsHeaders(w)

	// Expected path: /api/tiles/{z}/{x}/{y}.png  (stats handled by dedicated handler)
	if r.URL.Path == "/api/tiles/stats" {
		// Should be caught by stats handler; defensive.
		p.serveStats(w, r)
		return
	}
	trim := strings.TrimPrefix(r.URL.Path, "/api/tiles/")
	parts := strings.Split(trim, "/")
	if len(parts) != 3 || !strings.HasSuffix(parts[2], ".png") {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	yStr := strings.TrimSuffix(parts[2], ".png")
	z, err1 := strconv.Atoi(parts[0])
	x, err2 := strconv.Atoi(parts[1])
	y, err3 := strconv.Atoi(yStr)
	if err1 != nil || err2 != nil || err3 != nil || z < 0 || x < 0 || y < 0 {
		http.Error(w, "invalid coords", http.StatusBadRequest)
		return
	}
	key := tileKey{z, x, y}

	start := time.Now()
	p.mu.Lock()
	// Memory hit
	if ent, ok := p.cache[key]; ok && time.Since(ent.timestamp) < p.ttl {
		data := ent.data
		p.mu.Unlock()
		atomic.AddUint64(&tileHits, 1)
		logger.Debug("TILE mem-hit z=%d x=%d y=%d age=%v", z, x, y, time.Since(ent.timestamp))
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=120")
		_, _ = w.Write(data)
		return
	}
	// Disk hit (with detailed miss diagnostics when debug enabled)
	if p.diskDir != "" {
		diskPath := filepath.Join(p.diskDir, fmt.Sprintf("%d", z), fmt.Sprintf("%d", x), fmt.Sprintf("%d.png", y))
		if fi, err := os.Stat(diskPath); err == nil {
			age := time.Since(fi.ModTime())
			// Check if disk cache never expires (diskTTL == 0) or is still valid
			if p.diskTTL == 0 || age < p.diskTTL {
				if data, err := os.ReadFile(diskPath); err == nil {
					p.mu.Unlock()
					atomic.AddUint64(&tileHits, 1)
					atomic.AddUint64(&tileDiskHit, 1)
					logger.Debug("TILE disk-hit z=%d x=%d y=%d age=%v", z, x, y, age)
					w.Header().Set("Content-Type", "image/png")
					w.Header().Set("Cache-Control", "public, max-age=120")
					_, _ = w.Write(data)
					return
				} else {
					logger.Debug("TILE disk-miss z=%d x=%d y=%d reason=read-error err=%v", z, x, y, err)
				}
			} else {
				logger.Debug("TILE disk-miss z=%d x=%d y=%d reason=expired age=%v diskTTL=%v", z, x, y, age, p.diskTTL)
			}
		} else {
			logger.Debug("TILE disk-miss z=%d x=%d y=%d reason=not-found err=%v", z, x, y, err)
		}
	}
	// In-flight wait
	if waiters, ok := p.inFlight[key]; ok {
		ch := make(chan resultTile, 1)
		p.inFlight[key] = append(waiters, ch)
		p.mu.Unlock()
		res := <-ch
		if res.err != nil {
			logger.Debug("TILE wait-hit upstream error z=%d x=%d y=%d err=%v", z, x, y, res.err)
			http.Error(w, "upstream error", http.StatusBadGateway)
			return
		}
		atomic.AddUint64(&tileWaitHit, 1)
		logger.Debug("TILE wait-hit z=%d x=%d y=%d waited=%v", z, x, y, time.Since(start))
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=120")
		_, _ = w.Write(res.data)
		return
	}
	// Miss path: record + mark inflight
	atomic.AddUint64(&tileMisses, 1)
	mainCh := make(chan resultTile, 1)
	p.inFlight[key] = []chan resultTile{mainCh}
	p.mu.Unlock()

	upURL := fmt.Sprintf(p.upstreamFormat, z, x, y)
	if _, err := url.Parse(upURL); err != nil {
		p.mu.Lock()
		delete(p.inFlight, key)
		p.mu.Unlock()
		atomic.AddUint64(&tileErrors, 1)
		logger.Debug("TILE bad-upstream-url z=%d x=%d y=%d url=%s err=%v", z, x, y, upURL, err)
		http.Error(w, "bad upstream url", http.StatusInternalServerError)
		return
	}
	logger.Debug("TILE miss -> upstream fetch z=%d x=%d y=%d url=%s", z, x, y, upURL)
	req, _ := http.NewRequest(http.MethodGet, upURL, nil)
	req.Header.Set("User-Agent", "WhereAmI Tile Proxy/1.0")
	resp, err := p.client.Do(req)
	if err != nil {
		p.finishInflightWithError(key, err)
		atomic.AddUint64(&tileErrors, 1)
		logger.Debug("TILE fetch-error z=%d x=%d y=%d err=%v", z, x, y, err)
		http.Error(w, "fetch error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		p.finishInflightWithError(key, fmt.Errorf("status %d", resp.StatusCode))
		atomic.AddUint64(&tileErrors, 1)
		logger.Debug("TILE upstream-status z=%d x=%d y=%d status=%d", z, x, y, resp.StatusCode)
		http.Error(w, "upstream status", http.StatusBadGateway)
		return
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		p.finishInflightWithError(key, err)
		atomic.AddUint64(&tileErrors, 1)
		logger.Debug("TILE read-error z=%d x=%d y=%d err=%v", z, x, y, err)
		http.Error(w, "read error", http.StatusBadGateway)
		return
	}

	// Store + persist (best effort)
	p.mu.Lock()
	p.cache[key] = &tileEntry{data: body, timestamp: time.Now()}
	if p.diskDir != "" {
		dir := filepath.Join(p.diskDir, fmt.Sprintf("%d", z), fmt.Sprintf("%d", x))
		_ = os.MkdirAll(dir, 0o755)
		final := filepath.Join(dir, fmt.Sprintf("%d.png", y))
		tmp := final + ".tmp"
		if err := os.WriteFile(tmp, body, 0o644); err == nil {
			if err := os.Rename(tmp, final); err == nil {
				atomic.AddUint64(&tileStored, 1)
				logger.Debug("TILE stored z=%d x=%d y=%d size=%dB path=%s", z, x, y, len(body), final)
			}
		}
	}
	p.evictIfNeeded()
	waiters := p.inFlight[key]
	delete(p.inFlight, key)
	p.mu.Unlock()

	for _, ch := range waiters {
		ch <- resultTile{data: body, err: nil}
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=120")
	logger.Debug("TILE upstream-success z=%d x=%d y=%d size=%dB elapsed=%v", z, x, y, len(body), time.Since(start))
	_, _ = w.Write(body)
}

func (p *tileProxy) finishInflightWithError(key tileKey, err error) {
	p.mu.Lock()
	waiters := p.inFlight[key]
	delete(p.inFlight, key)
	p.mu.Unlock()
	for _, ch := range waiters {
		ch <- resultTile{nil, err}
	}
}

func (p *tileProxy) serveStats(w http.ResponseWriter, _ *http.Request) {
	p.mu.Lock()
	memEntries := len(p.cache)
	p.mu.Unlock()
	diskTTLSeconds := int(p.diskTTL.Seconds())
	if p.diskTTL == 0 {
		diskTTLSeconds = -1 // Indicate never expires
	}
	stats := map[string]any{
		"memory_cache_entries":     memEntries,
		"memory_cache_ttl_seconds": int(p.ttl.Seconds()),
		"memory_cache_max_entries": p.maxEntries,
		"disk_cache_dir":           p.diskDir,
		"disk_cache_ttl_seconds":   diskTTLSeconds,
		"disk_cache_max_entries":   p.maxEntries,
		"disk_cache_max_bytes":     p.maxBytes,
		"cache_hits":               atomic.LoadUint64(&tileHits),
		"cache_disk_hits":          atomic.LoadUint64(&tileDiskHit),
		"cache_misses":             atomic.LoadUint64(&tileMisses),
		"cache_wait_hit":           atomic.LoadUint64(&tileWaitHit),
		"tiles_stored":             atomic.LoadUint64(&tileStored),
		"errors":                   atomic.LoadUint64(&tileErrors),
		"evictions":                atomic.LoadUint64(&tileEvicts),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stats)
}

// ----------------- Bookmark Handlers -----------------

func handlePostBookmark(bookmarksPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		corsHeaders(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var req struct {
			Name string   `json:"name"`
			Lat  float64  `json:"lat"`
			Lon  float64  `json:"lon"`
			Desc string   `json:"desc,omitempty"`
			Tags []string `json:"tags,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		logger.Debug("POST /api/bookmarks decode ok name=%q lat=%.6f lon=%.6f tags=%d descLen=%d",
			req.Name, req.Lat, req.Lon, len(req.Tags), len(req.Desc))
		if strings.TrimSpace(req.Name) == "" {
			http.Error(w, "name required", http.StatusBadRequest)
			return
		}
		wp := Waypoint{Name: req.Name, Lat: req.Lat, Lon: req.Lon, Desc: req.Desc}
		saved, err := appendBookmark(bookmarksPath, wp)
		if err != nil {
			if errors.Is(err, ErrDuplicate) {
				http.Error(w, "duplicate", http.StatusConflict)
				return
			}
			http.Error(w, "save error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		saved.Bookmark = true
		allWaypointsMu.Lock()
		allWaypoints = append(allWaypoints, saved)
		allWaypointsMu.Unlock()

		// Persist tags (best‑effort; non-fatal on error)
		if len(req.Tags) > 0 {
			logger.Debug("POST /api/bookmarks persisting %d tag(s) for %q", len(req.Tags), req.Name)
			if err := addTagsToDB(req.Name, req.Lat, req.Lon, req.Tags); err != nil {
				logger.Debug("tag insert error for %q: %v", req.Name, err)
			} else {
				logger.Debug("tag insert success for %q", req.Name)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if len(req.Tags) > 0 {
			resp := map[string]any{
				"name":     saved.Name,
				"lat":      saved.Lat,
				"lon":      saved.Lon,
				"ele":      saved.Ele,
				"time":     saved.Time,
				"desc":     saved.Desc,
				"bookmark": true,
				"tags":     req.Tags,
			}
			_ = json.NewEncoder(w).Encode(resp)
		} else {
			_ = json.NewEncoder(w).Encode(saved)
		}
	}
}

func handlePatchBookmark(bookmarksPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			OldName string  `json:"oldName"`
			Lat     float64 `json:"lat"`
			Lon     float64 `json:"lon"`
			NewName string  `json:"newName"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.OldName) == "" || strings.TrimSpace(req.NewName) == "" {
			http.Error(w, "oldName and newName required", http.StatusBadRequest)
			return
		}
		found, err := renameBookmark(bookmarksPath, req.OldName, req.Lat, req.Lon, req.NewName)
		if err != nil {
			http.Error(w, "rename error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if !found {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		allWaypointsMu.Lock()
		for i := range allWaypoints {
			if allWaypoints[i].Name == req.OldName &&
				math.Abs(allWaypoints[i].Lat-req.Lat) < 1e-9 &&
				math.Abs(allWaypoints[i].Lon-req.Lon) < 1e-9 {
				allWaypoints[i].Name = req.NewName
				break
			}
		}
		allWaypointsMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"renamed": true,
			"oldName": req.OldName,
			"newName": req.NewName,
			"lat":     req.Lat,
			"lon":     req.Lon,
		})
	}
}

func handleDeleteBookmark(bookmarksPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		name := q.Get("name")
		if name == "" {
			http.Error(w, "name required", http.StatusBadRequest)
			return
		}
		lat, err1 := strconv.ParseFloat(q.Get("lat"), 64)
		lon, err2 := strconv.ParseFloat(q.Get("lon"), 64)
		if err1 != nil || err2 != nil {
			http.Error(w, "invalid lat/lon", http.StatusBadRequest)
			return
		}
		found, err := deleteBookmark(bookmarksPath, name, lat, lon)
		if err != nil {
			http.Error(w, "delete error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if !found {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		allWaypointsMu.Lock()
		for i := 0; i < len(allWaypoints); i++ {
			if allWaypoints[i].Name == name &&
				math.Abs(allWaypoints[i].Lat-lat) < 1e-9 &&
				math.Abs(allWaypoints[i].Lon-lon) < 1e-9 &&
				allWaypoints[i].Bookmark {
				allWaypoints = append(allWaypoints[:i], allWaypoints[i+1:]...)
				break
			}
		}
		allWaypointsMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"deleted": true,
			"name":    name,
			"lat":     lat,
			"lon":     lon,
		})
	}
}

func corsHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "POST, PATCH, DELETE, OPTIONS")
}

// ---------------- Waypoints & Clustering ----------------

func handleGetWaypoints(w http.ResponseWriter, r *http.Request) {
	// Copy snapshot under lock first (avoid holding lock while querying tag DB)
	allWaypointsMu.RLock()
	snap := make([]Waypoint, len(allWaypoints))
	copy(snap, allWaypoints)
	allWaypointsMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")

	useEmoji := false
	if r != nil && strings.EqualFold(r.URL.Query().Get("emoji"), "true") {
		useEmoji = true
	}

	// If tag DB not initialized just return the raw snapshot (cannot enrich)
	if tagDB == nil {
		_ = json.NewEncoder(w).Encode(snap)
		return
	}

	out := make([]map[string]any, 0, len(snap))
	for _, wp := range snap {
		obj := map[string]any{
			"name":     wp.Name,
			"lat":      wp.Lat,
			"lon":      wp.Lon,
			"bookmark": wp.Bookmark,
		}
		if wp.Ele != 0 {
			obj["ele"] = wp.Ele
		}
		if wp.Time != "" {
			obj["time"] = wp.Time
		}
		if wp.Desc != "" {
			obj["desc"] = wp.Desc
		}
		if wp.Name != "" {
			if tags, err := getTagsFor(wp.Name, wp.Lat, wp.Lon); err == nil && len(tags) > 0 {
				if useEmoji {
					unified := unifyDistinctTags(tags)
					enriched := make([]TagDTO, 0, len(unified))
					for _, t := range unified {
						enriched = append(enriched, enrichTag(t))
					}
					obj["tags"] = enriched
				} else {
					obj["tags"] = tags
				}
			}
		}
		out = append(out, obj)
	}

	_ = json.NewEncoder(w).Encode(out)
}

func handleGetClusters(w http.ResponseWriter, r *http.Request) {
	zoom := 0
	if zStr := r.URL.Query().Get("zoom"); zStr != "" {
		if z, err := strconv.Atoi(zStr); err == nil {
			zoom = z
		}
	}
	if zoom < 0 {
		zoom = 0
	}
	grid := 60
	if gStr := r.URL.Query().Get("grid"); gStr != "" {
		if g, err := strconv.Atoi(gStr); err == nil && g >= 8 && g <= 512 {
			grid = g
		}
	}

	// Optional filter: only cluster bookmark waypoints if requested.
	bookmarksOnly := false
	if b := r.URL.Query().Get("bookmarksOnly"); b == "1" || strings.EqualFold(b, "true") {
		bookmarksOnly = true
	} else if b2 := r.URL.Query().Get("bookmarks"); b2 == "1" || strings.EqualFold(b2, "true") {
		// Support alternate param name ?bookmarks=1
		bookmarksOnly = true
	}
	logger.Debug("/api/clusters zoom=%d grid=%d bookmarksOnly=%v", zoom, grid, bookmarksOnly)

	allWaypointsMu.RLock()
	points := make([]Waypoint, len(allWaypoints))
	copy(points, allWaypoints)
	allWaypointsMu.RUnlock()

	type bucket struct {
		sumLat, sumLon float64
		minX, maxX     float64
		minY, maxY     float64
		count          int
		wps            []Waypoint
	}
	buckets := make(map[string]*bucket)

	for _, wp := range points {
		if bookmarksOnly && !wp.Bookmark {
			continue
		}
		lat := wp.Lat
		lon := wp.Lon
		sinLat := math.Sin(lat * math.Pi / 180)
		n := math.Exp2(float64(zoom))
		x := (lon + 180.0) / 360.0 * 256.0 * n
		y := (0.5 - math.Log((1+sinLat)/(1-sinLat))/(4*math.Pi)) * 256.0 * n
		bx := int(x / float64(grid))
		by := int(y / float64(grid))
		key := fmt.Sprintf("%d:%d", bx, by)
		b := buckets[key]
		if b == nil {
			b = &bucket{minX: x, maxX: x, minY: y, maxY: y}
			buckets[key] = b
		}
		if x < b.minX {
			b.minX = x
		}
		if x > b.maxX {
			b.maxX = x
		}
		if y < b.minY {
			b.minY = y
		}
		if y > b.maxY {
			b.maxY = y
		}
		b.sumLat += lat
		b.sumLon += lon
		b.count++
		b.wps = append(b.wps, wp)
	}

	var out []map[string]any
	for _, b := range buckets {
		if b.count == 1 {
			wp := b.wps[0]
			out = append(out, map[string]any{
				"type":     "waypoint",
				"lat":      wp.Lat,
				"lon":      wp.Lon,
				"name":     wp.Name,
				"bookmark": wp.Bookmark,
			})
		} else {
			centerX := (b.minX + b.maxX) / 2
			centerY := (b.minY + b.maxY) / 2
			scale := 256.0 * math.Exp2(float64(zoom))
			lon := (centerX/scale)*360.0 - 180.0
			normY := centerY / scale
			lat := math.Atan(math.Sinh(math.Pi*(1-2*normY))) * 180.0 / math.Pi
			out = append(out, map[string]any{
				"type":  "cluster",
				"lat":   lat,
				"lon":   lon,
				"count": b.count,
			})
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// --------------- Location ---------------

func handleGetLocation(w http.ResponseWriter, _ *http.Request) {
	locationOnce.Do(func() {
		if err := InitLocationTracking("io.github.rubiojr.whereami.desktop"); err != nil {
			logger.Error("Location init error: %v", err)
		}
	})
	locationMu.RLock()
	defer locationMu.RUnlock()
	if !locationValid {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(currentLocation)
}

// --------------- Import GPX ---------------

func handlePostImport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dir       string `json:"dir"`
		Recursive bool   `json:"recursive"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Dir == "" {
		http.Error(w, "dir required", http.StatusBadRequest)
		return
	}
	info, err := os.Stat(req.Dir)
	if err != nil || !info.IsDir() {
		http.Error(w, "not a directory", http.StatusBadRequest)
		return
	}
	dir := effectiveDataDir()
	if dir == "" {
		http.Error(w, "no data directory available", http.StatusInternalServerError)
		return
	}
	importBase := filepath.Join(dir, "imports")
	if err := os.MkdirAll(importBase, 0o755); err != nil {
		http.Error(w, "cannot create imports dir: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var importedFiles []string
	var skipped []string
	err = filepath.WalkDir(req.Dir, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if !req.Recursive && p != req.Dir {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(d.Name()), ".gpx") {
			return nil
		}
		destPath := filepath.Join(importBase, d.Name())
		if _, err := os.Stat(destPath); err == nil {
			skipped = append(skipped, d.Name())
			return nil
		}
		src, err := os.Open(p)
		if err != nil {
			return nil
		}
		defer src.Close()
		dst, err := os.Create(destPath)
		if err != nil {
			return nil
		}
		defer dst.Close()
		_, _ = io.Copy(dst, src)
		importedFiles = append(importedFiles, destPath)
		return nil
	})
	if err != nil {
		http.Error(w, "import error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var newly []Waypoint
	for _, f := range importedFiles {
		if wps, err := parseGPXFile(f); err == nil {
			newly = append(newly, wps...)
		}
	}

	var dedupCount int
	if len(newly) > 0 {
		allWaypointsMu.Lock()
		combined := append(allWaypoints, newly...)
		allWaypoints = DedupeWaypoints(combined)
		dedupCount = len(allWaypoints)
		allWaypointsMu.Unlock()
	} else {
		dedupCount = len(allWaypoints)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"imported":      true,
		"dir":           req.Dir,
		"count":         len(newly),
		"files":         len(importedFiles),
		"skipped_files": skipped,
		"skipped":       len(skipped),
		"dedup_count":   dedupCount,
	})
}

// --------------- Suggestions & Tags ---------------

// -------- Geocode / Suggestion Cache & Helpers --------

var (
	geoDBOnce           sync.Once
	geoDB               *sql.DB
	nominatimThrottleMu sync.Mutex
	nominatimLast       time.Time
	nominatimInitOnce   sync.Once
)

const nominatimMinInterval = 400 * time.Millisecond
const defaultNominatimServer = "https://nominatim.openstreetmap.org"

type suggestResult struct {
	Name   string  `json:"name"`
	Lat    float64 `json:"lat"`
	Lon    float64 `json:"lon"`
	Source string  `json:"source"`          // "bookmark" | "waypoint" | "geocode"
	Class  string  `json:"class,omitempty"` // nominatim
	Type   string  `json:"type,omitempty"`  // nominatim
}

// initGeocodeDB initializes the persistent SQLite cache (indefinite retention, no pruning).
func initGeocodeDB() {
	geoDBOnce.Do(func() {
		path := effectiveCacheDir()
		_ = ensureDir(path)
		dbPath := filepath.Join(path, "geocode.sqlite")
		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			logger.Error("geocode cache open failed: %v", err)
			return
		}
		// Index to support potential pruning / ordering by fetched_at (query already PRIMARY KEY)
		_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_geocode_cache_fetched_at ON geocode_cache(fetched_at)`)
		if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS geocode_cache (
			query TEXT PRIMARY KEY,
			json  TEXT NOT NULL,
			fetched_at TIMESTAMP NOT NULL
		)`); err != nil {
			logger.Error("geocode cache schema error: %v", err)
			_ = db.Close()
			return
		}
		geoDB = db
	})
}

// fetchGeocodeCached returns up to limit nominatim results, using indefinite sqlite caching.
// Adds lightweight retry for transient / truncated JSON errors (e.g. "unexpected end of JSON input", "EOF").
// We only cache successful (even if empty) responses; transient failures are not cached.
func fetchGeocodeCached(q string, limit int) []suggestResult {
	if limit <= 0 {
		return nil
	}
	initGeocodeDB()
	var rawJSON string
	if geoDB != nil {
		_ = geoDB.QueryRow(`SELECT json FROM geocode_cache WHERE query = ?`, q).Scan(&rawJSON)
	}

	var payload []map[string]any
	if rawJSON == "" {
		// ---- Cache miss: perform network fetch (with throttle + retry) ----
		nominatimThrottleMu.Lock()
		delta := time.Since(nominatimLast)
		if delta < nominatimMinInterval {
			time.Sleep(nominatimMinInterval - delta)
		}
		nominatimLast = time.Now()
		nominatimThrottleMu.Unlock()

		// One-time server init
		nominatimInitOnce.Do(func() {
			srv := os.Getenv("WHEREAMI_NOMINATIM_SERVER")
			if strings.TrimSpace(srv) == "" {
				srv = defaultNominatimServer
			}
			gominatim.SetServer(srv)
		})

		// Determine retry count (default 1 transient retry -> total attempts = 2)
		maxTransientRetries := 1
		if v := os.Getenv("WHEREAMI_NOMINATIM_RETRIES"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 5 {
				maxTransientRetries = n
			}
		}

		qObj := gominatim.SearchQuery{
			Q:     q,
			Limit: limit,
		}

		var res []gominatim.SearchResult
		var err error
		attempts := maxTransientRetries + 1
		for attempt := 1; attempt <= attempts; attempt++ {
			res, err = qObj.Get()
			if err == nil {
				if attempt > 1 {
					logger.Info("nominatim recovered after %d attempt(s) for %q", attempt, q)
				}
				break
			}
			errStr := err.Error()
			transient := strings.Contains(errStr, "unexpected end of JSON") || strings.Contains(errStr, "EOF")
			if !transient || attempt == attempts {
				logger.Error("nominatim search error (attempt %d/%d, query=%q): %v", attempt, attempts, q, err)
				return nil
			}
			logger.Error("transient nominatim error (attempt %d/%d, will retry) query=%q err=%v", attempt, attempts, q, err)
			time.Sleep(150 * time.Millisecond)
		}

		for _, r := range res {
			var lat, lon float64
			if r.Lat != "" {
				lat, _ = strconv.ParseFloat(r.Lat, 64)
			}
			if r.Lon != "" {
				lon, _ = strconv.ParseFloat(r.Lon, 64)
			}
			payload = append(payload, map[string]any{
				"display_name": r.DisplayName,
				"lat":          lat,
				"lon":          lon,
				"class":        r.Class,
				"type":         r.Type,
			})
			if len(payload) >= limit {
				break
			}
		}

		// Only cache successful fetches (even if empty slice).
		if geoDB != nil {
			b, _ := json.Marshal(payload)
			_, _ = geoDB.Exec(`INSERT OR REPLACE INTO geocode_cache(query, json, fetched_at) VALUES(?,?,CURRENT_TIMESTAMP)`, q, string(b))
		}
	} else {
		// ---- Cache hit ----
		if err := json.Unmarshal([]byte(rawJSON), &payload); err != nil {
			logger.Error("geocode cache unmarshal failed for %q: %v (ignoring)", q, err)
			payload = nil
		}
	}

	out := make([]suggestResult, 0, limit)
	for _, p := range payload {
		name, _ := p["display_name"].(string)
		lat, _ := p["lat"].(float64)
		lon, _ := p["lon"].(float64)
		class, _ := p["class"].(string)
		tp, _ := p["type"].(string)
		if name == "" {
			continue
		}
		out = append(out, suggestResult{
			Name:   name,
			Lat:    lat,
			Lon:    lon,
			Source: "geocode",
			Class:  class,
			Type:   tp,
		})
		if len(out) >= limit {
			break
		}
	}
	return out
}

// handleGetSuggest now returns structured suggestions:
// [
//
//	{ "name": "...", "lat": ..., "lon": ..., "source": "bookmark|waypoint|geocode", "class": "...", "type": "..." },
//
// Tags are not included in suggestions (would require extra lookups); clients can fetch via /api/tags.
//
//	...
//
// ]
// Combined limit: 8 (first waypoints/bookmarks, then geocode).
func handleGetSuggest(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		q = strings.TrimSpace(r.URL.Query().Get("query"))
	}
	logger.Debug("/api/suggest received q=%q", q)
	if q == "" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"query":       "",
			"suggestions": []suggestResult{},
		})
		return
	}
	qLower := strings.ToLower(q)

	// Boolean / single tag query branch
	if strings.HasPrefix(qLower, "tag:") {
		rawExpr := strings.TrimSpace(q[4:])
		// Strip optional surrounding quotes
		if len(rawExpr) >= 2 && rawExpr[0] == '"' && rawExpr[len(rawExpr)-1] == '"' {
			rawExpr = strings.TrimSpace(rawExpr[1 : len(rawExpr)-1])
		}
		mode := "single"
		var terms []string
		var singleTerm string

		upperExpr := strings.ToUpper(rawExpr)
		if strings.Contains(upperExpr, " AND ") {
			mode = "AND"
			parts := strings.Split(upperExpr, " AND ")
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					terms = append(terms, normalizeTagKey(p))
				}
			}
		} else if strings.Contains(upperExpr, " OR ") {
			mode = "OR"
			parts := strings.Split(upperExpr, " OR ")
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					terms = append(terms, normalizeTagKey(p))
				}
			}
		} else {
			mode = "single"
			singleTerm = normalizeTagKey(rawExpr)
		}

		var results []suggestResult
		if tagDB != nil {
			// Build waypoint -> normalized tag set
			type wkey struct {
				name     string
				lat, lon float64
			}
			wmap := make(map[wkey]map[string]struct{})
			rows, err := tagDB.Query(`SELECT name, lat, lon, tag FROM waypoint_tags`)
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var name, tagVal string
					var lat, lon float64
					if err := rows.Scan(&name, &lat, &lon, &tagVal); err == nil {
						k := wkey{name, lat, lon}
						norm := normalizeTagKey(tagVal)
						if _, ok := wmap[k]; !ok {
							wmap[k] = make(map[string]struct{})
						}
						wmap[k][norm] = struct{}{}
					}
				}
			}

			evalWaypoint := func(tags map[string]struct{}) bool {
				switch mode {
				case "single":
					if singleTerm == "" {
						return false
					}
					_, ok := tags[singleTerm]
					return ok
				case "AND":
					if len(terms) == 0 {
						return false
					}
					for _, t := range terms {
						if t == "" {
							continue
						}
						if _, ok := tags[t]; !ok {
							return false
						}
					}
					return true
				case "OR":
					if len(terms) == 0 {
						return false
					}
					for _, t := range terms {
						if t == "" {
							continue
						}
						if _, ok := tags[t]; ok {
							return true
						}
					}
					return false
				default:
					return false
				}
			}

			// Build suggestions
			for k, tagset := range wmap {
				if evalWaypoint(tagset) {
					src := "bookmark"
					allWaypointsMu.RLock()
					for _, wpt := range allWaypoints {
						if wpt.Name == k.name && math.Abs(wpt.Lat-k.lat) < 1e-9 && math.Abs(wpt.Lon-k.lon) < 1e-9 {
							if !wpt.Bookmark {
								src = "waypoint"
							}
							break
						}
					}
					allWaypointsMu.RUnlock()
					results = append(results, suggestResult{
						Name:   k.name,
						Lat:    k.lat,
						Lon:    k.lon,
						Source: src,
						Class:  "tag",
						Type:   mode,
					})
				}
			}
		}

		// Sort and cap (reuse normal suggest cap of 8)
		sort.Slice(results, func(i, j int) bool {
			ai := strings.ToLower(results[i].Name)
			aj := strings.ToLower(results[j].Name)
			if ai == aj {
				return results[i].Name < results[j].Name
			}
			return ai < aj
		})
		const maxTagSuggest = 8
		if len(results) > maxTagSuggest {
			results = results[:maxTagSuggest]
		}

		logger.Debug("/api/suggest tag query mode=%s terms=%v single=%q matches=%d", mode, terms, singleTerm, len(results))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"query":       q,
			"suggestions": results,
		})
		return
	}

	// Non-tag suggestion logic (original behavior)
	// Collect local waypoint matches (name contains query)
	var local []suggestResult
	allWaypointsMu.RLock()
	for _, wpt := range allWaypoints {
		if wpt.Name == "" {
			continue
		}
		if strings.Contains(strings.ToLower(wpt.Name), qLower) {
			src := "waypoint"
			if wpt.Bookmark {
				src = "bookmark"
			}
			local = append(local, suggestResult{
				Name:   wpt.Name,
				Lat:    wpt.Lat,
				Lon:    wpt.Lon,
				Source: src,
			})
		}
	}
	allWaypointsMu.RUnlock()

	sort.Slice(local, func(i, j int) bool {
		return strings.ToLower(local[i].Name) < strings.ToLower(local[j].Name)
	})

	const maxSuggestions = 8

	// If we still have capacity, fetch geocode suggestions (remaining slots)
	remaining := maxSuggestions - len(local)
	var combined []suggestResult
	if len(local) > 0 {
		if len(local) > maxSuggestions {
			combined = append(combined, local[:maxSuggestions]...)
			remaining = 0
		} else {
			combined = append(combined, local...)
		}
	}

	if remaining > 0 {
		geo := fetchGeocodeCached(q, remaining)
		combined = append(combined, geo...)
	}

	// Final cap (safety)
	if len(combined) > maxSuggestions {
		combined = combined[:maxSuggestions]
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"query":       q,
		"suggestions": combined,
	})
}

// (Removed stray duplicate code after handleGetSuggest)

// Recent search queries (distinct, most recent first). Returns legacy string list plus
// enriched entries with optional lat/lon:
//
//	{
//	  "queries": ["lastQuery","previousQuery", ...],
//	  "entries": [ { "query":"lastQuery","lat":..,"lon":.. }, ... ]
//	}
func handleGetRecentSuggest(w http.ResponseWriter, r *http.Request) {
	initHistoryDB()
	limitStr := r.URL.Query().Get("limit")
	limit := 10
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 && v <= 200 {
			limit = v
		}
	}
	logger.Debug("/api/recent_suggest requested limit=%d", limit)
	if historyDB == nil {
		logger.Debug("/api/recent_suggest history DB unavailable -> returning empty list")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"queries": []string{},
			"entries": []any{},
		})
		return
	}

	// For each distinct query, take the most recent row (max id) and include lat/lon if present.
	rows, err := historyDB.Query(`
		SELECT sh.query, sh.lat, sh.lon
		FROM search_history sh
		JOIN (
			SELECT query, MAX(id) AS max_id
			FROM search_history
			WHERE query <> ''
			GROUP BY query
		) latest ON latest.max_id = sh.id
		ORDER BY sh.id DESC
		LIMIT ?`, limit)
	if err != nil {
		http.Error(w, "query error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var recent []string
	var entries []map[string]any
	for rows.Next() {
		var q string
		var lat, lon sql.NullFloat64
		if err := rows.Scan(&q, &lat, &lon); err == nil {
			recent = append(recent, q)
			entry := map[string]any{"query": q}
			if lat.Valid {
				entry["lat"] = lat.Float64
			}
			if lon.Valid {
				entry["lon"] = lon.Float64
			}
			entries = append(entries, entry)
		}
	}
	logger.Debug("/api/recent_suggest returning %d distinct queries (limit=%d)", len(recent), limit)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"queries": recent,
		"entries": entries,
	})
}

// POST /api/history { "query": "...", "lat": <optional>, "lon": <optional> }
// or {"queries":["...","..."]} (multi insert without coordinates)
func handlePostHistory(w http.ResponseWriter, r *http.Request) {
	initHistoryDB()
	if historyDB == nil {
		http.Error(w, "history db unavailable", http.StatusServiceUnavailable)
		return
	}
	var payload struct {
		Query   string   `json:"query"`
		Queries []string `json:"queries"`
		Lat     *float64 `json:"lat"`
		Lon     *float64 `json:"lon"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	var inserted int
	insertOne := func(q string, latPtr, lonPtr *float64) {
		q = strings.TrimSpace(q)
		if q == "" {
			return
		}
		if latPtr != nil || lonPtr != nil {
			var latVal any
			var lonVal any
			if latPtr != nil {
				latVal = *latPtr
			}
			if lonPtr != nil {
				lonVal = *lonPtr
			}
			_, _ = historyDB.Exec(`INSERT INTO search_history(query, lat, lon) VALUES(?,?,?)`, q, latVal, lonVal)
		} else {
			_, _ = historyDB.Exec(`INSERT INTO search_history(query) VALUES(?)`, q)
		}
		inserted++
	}
	if len(payload.Queries) > 0 {
		for _, q := range payload.Queries {
			insertOne(q, nil, nil)
		}
	} else {
		insertOne(payload.Query, payload.Lat, payload.Lon)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"stored":   inserted,
		"multiple": len(payload.Queries) > 0,
	})
}

// ---------------- RegisterAPI (public) ----------------

// RegisterAPI wires all HTTP endpoints using Go 1.22 method-aware patterns.
// initTagDB opens (and creates if needed) the tag database in dataDir.
func initTagDB() {
	tagDBOnce.Do(func() {
		dir := effectiveDataDir()
		if dir == "" {
			logger.Error("initTagDB: no data directory resolved")
			return
		}
		path := filepath.Join(dir, "tags.sqlite")
		db, err := sql.Open("sqlite", path)
		logger.Debug("initTagDB opening %s", path)
		if err != nil {
			logger.Error("initTagDB: open failed: %v", err)
			return
		}
		if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS waypoint_tags (
			name TEXT NOT NULL,
			lat REAL NOT NULL,
			lon REAL NOT NULL,
			tag TEXT NOT NULL,
			PRIMARY KEY(name, lat, lon, tag)
		)`); err != nil {
			logger.Error("initTagDB: schema error: %v", err)
			_ = db.Close()
			return
		}
		tagDB = db
		logger.Debug("initTagDB ready (path=%s)", path)
	})
}

// addTagsToDB inserts tags (ignoring duplicates).
func addTagsToDB(name string, lat, lon float64, tags []string) error {
	logger.Debug("addTagsToDB name=%q lat=%.6f lon=%.6f tags=%v", name, lat, lon, tags)
	if tagDB == nil || len(tags) == 0 {
		return nil
	}
	tx, err := tagDB.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO waypoint_tags(name, lat, lon, tag) VALUES(?,?,?,?)`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, err := stmt.Exec(name, lat, lon, t); err != nil {
			tx.Rollback()
			return err
		}
	}
	err = tx.Commit()
	if err != nil {
		logger.Debug("addTagsToDB commit error for %q: %v", name, err)
	} else {
		logger.Debug("addTagsToDB commit ok for %q", name)
	}
	return err
}

// getTagsFor returns all tags for a waypoint.
func getTagsFor(name string, lat, lon float64) ([]string, error) {
	logger.Debug("getTagsFor name=%q lat=%.6f lon=%.6f", name, lat, lon)
	if tagDB == nil {
		return nil, nil
	}
	rows, err := tagDB.Query(`SELECT tag FROM waypoint_tags WHERE name = ? AND lat = ? AND lon = ? ORDER BY tag COLLATE NOCASE`, name, lat, lon)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	logger.Debug("getTagsFor name=%q found %d tag(s)", name, len(out))
	return out, nil
}

// deleteTag removes one tag for a waypoint.
func deleteTag(name string, lat, lon float64, tag string) error {
	logger.Debug("deleteTag name=%q lat=%.6f lon=%.6f tag=%q", name, lat, lon, tag)
	if tagDB == nil {
		return nil
	}
	_, err := tagDB.Exec(`DELETE FROM waypoint_tags WHERE name = ? AND lat = ? AND lon = ? AND tag = ?`, name, lat, lon, tag)
	return err
}

// Handlers for tag API (rewritten with backend enrichment & distinct mode)
//
// New modes:
//   GET /api/tags?name=&lat=&lon=&emoji=true        (per-waypoint, optional enrichment)
//   GET /api/tags?distinct=true&emoji=true          (global distinct tag list)
//   POST /api/tags?emoji=true                       (returns enriched list when requested)
//   DELETE /api/tags?name=&lat=&lon=&tag=&emoji=true
//
// When emoji=true the 'tags' array contains objects:
//   { "raw": "...", "emoji": "...", "name": "...", "display": "..." }
//
// Without emoji=true, 'tags' remains an array of raw strings (legacy shape).

// TagDTO represents an enriched tag (only when emoji=true).
type TagDTO struct {
	Raw     string `json:"raw"`
	Emoji   string `json:"emoji,omitempty"`
	Name    string `json:"name,omitempty"`
	Display string `json:"display"`
	Normal  string `json:"normal,omitempty"` // canonical lowercase / symbol-collapsed form (backend normalized)
}

// tagEmojiMap centralizes the mapping (word keys stored lowercase).
var tagEmojiMap = map[string]struct{ Emoji, Name string }{
	"*":          {"⭐", "star"},
	"$":          {"💲", "money"},
	"done":       {"✅", "done"},
	"todo":       {"☐", "todo"},
	"!":          {"❗", "important"},
	"?":          {"❓", "question"},
	"+":          {"➕", "plus"},
	"-":          {"➖", "minus"},
	"x":          {"❌", "x"},
	"@":          {"📧", "email"},
	"#":          {"🔖", "tag"},
	"%":          {"📊", "percent"},
	"&":          {"🔗", "link"},
	"home":       {"🏠", "home"},
	"work":       {"💼", "work"},
	"food":       {"🍽️", "food"},
	"gas":        {"⛽", "gas"},
	"coffee":     {"☕", "coffee"},
	"hotel":      {"🏨", "hotel"},
	"restaurant": {"🍴", "restaurant"},
	"shopping":   {"🛒", "shopping"},
	"park":       {"🌳", "park"},
	"beach":      {"🏖️", "beach"},
	"mountain":   {"⛰️", "mountain"},
	"hospital":   {"🏥", "hospital"},
	"school":     {"🏫", "school"},
	"church":     {"⛪", "church"},
	"bank":       {"🏦", "bank"},
	"urgent":     {"🚨", "urgent"},
	"favorite":   {"💖", "favorite"},
	"important":  {"⚡", "important"},
	"diving":     {"🤿", "diving"},
}

// enrichTag converts a raw tag to a TagDTO (adding emoji/name if known).
func enrichTag(raw string) TagDTO {
	r := strings.TrimSpace(raw)
	if r == "" {
		return TagDTO{Raw: raw, Display: raw, Normal: ""}
	}
	norm := normalizeTagKey(r)
	lower := strings.ToLower(r)

	// 1. Direct single-key mapping (full string matches a known key)
	if m, ok := tagEmojiMap[lower]; ok {
		// For purely symbolic single-key tags, display ONLY the emoji (no raw text).
		if len(r) == 1 {
			return TagDTO{
				Raw:     r,
				Emoji:   m.Emoji,
				Name:    m.Name,
				Display: m.Emoji,
				Normal:  norm,
			}
		}
		return TagDTO{
			Raw:     r,
			Emoji:   m.Emoji,
			Name:    m.Name,
			Display: m.Emoji + " " + r,
			Normal:  norm,
		}
	}
	if m, ok := tagEmojiMap[r]; ok { // exact (case sensitive) fallback
		if len(r) == 1 {
			return TagDTO{
				Raw:     r,
				Emoji:   m.Emoji,
				Name:    m.Name,
				Display: m.Emoji,
				Normal:  norm,
			}
		}
		return TagDTO{
			Raw:     r,
			Emoji:   m.Emoji,
			Name:    m.Name,
			Display: m.Emoji + " " + r,
			Normal:  norm,
		}
	}

	// 2. Repeated symbol sequences: if every rune is one symbol that has an emoji mapping, repeat the emoji.
	//    Example: "$$", "$$$" -> "💲💲", "***" -> "⭐⭐⭐"
	if len(r) > 1 {
		first := rune(r[0])
		allSame := true
		for _, rr := range r {
			if rr != first {
				allSame = false
				break
			}
		}
		if allSame {
			sym := string(first)
			// Accept either the exact symbol or its lowercase as a key in the map
			if m, ok := tagEmojiMap[sym]; ok {
				var b strings.Builder
				for range r {
					b.WriteString(m.Emoji)
				}
				repeated := b.String()
				return TagDTO{
					Raw:     r,
					Emoji:   m.Emoji, // base emoji (single)
					Name:    m.Name,
					Display: repeated, // ONLY repeated emojis (no raw text)
					Normal:  norm,
				}
			}
			if m, ok := tagEmojiMap[strings.ToLower(sym)]; ok {
				var b2 strings.Builder
				for range r {
					b2.WriteString(m.Emoji)
				}
				repeated := b2.String()
				return TagDTO{
					Raw:     r,
					Emoji:   m.Emoji,
					Name:    m.Name,
					Display: repeated, // ONLY repeated emojis
					Normal:  norm,
				}
			}
		}
	}

	// 3. Mixed content or no mapping: leave raw
	return TagDTO{
		Raw:     r,
		Display: r,
		Normal:  norm,
	}
}

// normalizeTagKey produces a canonical comparison key:
//   - lowercase
//   - replace emoji equivalents with their symbolic form (⭐->*, 💲->$)
//   - collapse repeated symbol runs (*+, $+) to a single character
//   - trim surrounding whitespace
func normalizeTagKey(s string) string {
	if s == "" {
		return ""
	}
	// Lowercase + trim first
	ls := strings.ToLower(strings.TrimSpace(s))

	// Map every emoji in tagEmojiMap back to its canonical key so emoji and textual forms normalize identically.
	if len(tagEmojiMap) > 0 {
		pairs := make([]string, 0, len(tagEmojiMap)*2)
		for k, v := range tagEmojiMap {
			if v.Emoji != "" {
				pairs = append(pairs, v.Emoji, k) // emoji -> canonical key
			}
		}
		if len(pairs) > 0 {
			replacer := strings.NewReplacer(pairs...)
			ls = replacer.Replace(ls)
		}
	}

	// IMPORTANT: Do NOT collapse repeated symbol runs anymore.
	// We intentionally preserve sequences (e.g. "**", "$$", "!!!!") so that
	// queries like tag:** do NOT match tag:* and tag:homeeee does NOT match tag:home.
	return ls
}

// unifyDistinctTags collapses raw distinct tags that normalize to the same key.
// Preference order for representative selection:
//  1. A tag whose normalized key has an emoji mapping (via tagEmojiMap)
//  2. Shortest raw representation
//  3. Lexicographic (stable fallback)
func unifyDistinctTags(raw []string) []string {
	chosen := make(map[string]string)
	for _, r := range raw {
		n := normalizeTagKey(r)
		if n == "" {
			continue
		}
		existing, ok := chosen[n]
		if !ok {
			chosen[n] = r
			continue
		}
		// Prefer mapped over unmapped
		_, existingMapped := tagEmojiMap[normalizeTagKey(existing)]
		_, newMapped := tagEmojiMap[normalizeTagKey(r)]
		if newMapped && !existingMapped {
			chosen[n] = r
			continue
		}
		// Prefer shorter
		if len(r) < len(existing) {
			chosen[n] = r
			continue
		}
		// Finally prefer lexicographically smaller (stable tie-breaker)
		if strings.ToLower(r) < strings.ToLower(existing) {
			chosen[n] = r
		}
	}
	out := make([]string, 0, len(chosen))
	for _, v := range chosen {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		ai := strings.ToLower(out[i])
		aj := strings.ToLower(out[j])
		if ai == aj {
			return out[i] < out[j]
		}
		return ai < aj
	})
	return out
}

// getDistinctTags returns unique raw tags sorted case-insensitively.
func getDistinctTags() ([]string, error) {
	if tagDB == nil {
		return nil, nil
	}
	rows, err := tagDB.Query(`SELECT DISTINCT tag FROM waypoint_tags ORDER BY tag COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// GET /api/tags (per-waypoint or distinct)
func handleGetTags(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	useEmoji := strings.EqualFold(q.Get("emoji"), "true")
	distinct := strings.EqualFold(q.Get("distinct"), "true")
	name := strings.TrimSpace(q.Get("name"))
	latStr := q.Get("lat")
	lonStr := q.Get("lon")
	w.Header().Set("Content-Type", "application/json")

	if distinct {
		raw, err := getDistinctTags()
		if err != nil {
			http.Error(w, "query error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		// Normalize & unify variants (e.g. ⭐, ** -> *) before responding
		raw = unifyDistinctTags(raw)
		if useEmoji {
			enriched := make([]TagDTO, 0, len(raw))
			for _, t := range raw {
				e := enrichTag(t)
				enriched = append(enriched, e)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"distinct": true,
				"tags":     enriched,
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"distinct": true,
			"tags":     raw,
		})
		return
	}

	// Per-waypoint mode
	if name == "" || latStr == "" || lonStr == "" {
		http.Error(w, "missing name/lat/lon or distinct=true", http.StatusBadRequest)
		return
	}
	lat, err1 := strconv.ParseFloat(latStr, 64)
	lon, err2 := strconv.ParseFloat(lonStr, 64)
	if err1 != nil || err2 != nil {
		http.Error(w, "invalid lat/lon", http.StatusBadRequest)
		return
	}
	rawTags, err := getTagsFor(name, lat, lon)
	if err != nil {
		http.Error(w, "query error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if useEmoji {
		// Normalize & unify variants for this waypoint before enrichment
		rawUnified := unifyDistinctTags(rawTags)
		enriched := make([]TagDTO, 0, len(rawUnified))
		for _, t := range rawUnified {
			enriched = append(enriched, enrichTag(t))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": name, "lat": lat, "lon": lon,
			"tags": enriched,
		})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"name": name, "lat": lat, "lon": lon,
		"tags": rawTags,
	})
}

// POST /api/tags?emoji=true  JSON: { name, lat, lon, tags: [] }
func handlePostTags(w http.ResponseWriter, r *http.Request) {
	useEmoji := strings.EqualFold(r.URL.Query().Get("emoji"), "true")
	var req struct {
		Name string   `json:"name"`
		Lat  float64  `json:"lat"`
		Lon  float64  `json:"lon"`
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Name) == "" || len(req.Tags) == 0 {
		http.Error(w, "name and tags required", http.StatusBadRequest)
		return
	}
	// Store tags verbatim (no frontend preprocessing anymore).
	if err := addTagsToDB(req.Name, req.Lat, req.Lon, req.Tags); err != nil {
		http.Error(w, "insert error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	raw, _ := getTagsFor(req.Name, req.Lat, req.Lon)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if useEmoji {
		enriched := make([]TagDTO, 0, len(raw))
		for _, t := range raw {
			enriched = append(enriched, enrichTag(t))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": req.Name, "lat": req.Lat, "lon": req.Lon,
			"tags": enriched,
		})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"name": req.Name, "lat": req.Lat, "lon": req.Lon,
		"tags": raw,
	})
}

// DELETE /api/tags?name=&lat=&lon=&tag=&emoji=true
func handleDeleteTag(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	useEmoji := strings.EqualFold(q.Get("emoji"), "true")
	name := strings.TrimSpace(q.Get("name"))
	tag := strings.TrimSpace(q.Get("tag"))
	latStr := q.Get("lat")
	lonStr := q.Get("lon")
	if name == "" || tag == "" || latStr == "" || lonStr == "" {
		http.Error(w, "name, lat, lon, tag required", http.StatusBadRequest)
		return
	}
	lat, err1 := strconv.ParseFloat(latStr, 64)
	lon, err2 := strconv.ParseFloat(lonStr, 64)
	if err1 != nil || err2 != nil {
		http.Error(w, "invalid lat/lon", http.StatusBadRequest)
		return
	}
	if err := deleteTag(name, lat, lon, tag); err != nil {
		http.Error(w, "delete error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	raw, _ := getTagsFor(name, lat, lon)
	w.Header().Set("Content-Type", "application/json")
	if useEmoji {
		enriched := make([]TagDTO, 0, len(raw))
		for _, t := range raw {
			enriched = append(enriched, enrichTag(t))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": name, "lat": lat, "lon": lon,
			"tags": enriched,
		})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"name": name, "lat": lat, "lon": lon,
		"tags": raw,
	})
}

func RegisterAPI(mux *http.ServeMux, bookmarksPath string, debug bool) {
	if mux == nil {
		mux = http.DefaultServeMux
	}
	// Initialize tag DB (idempotent)
	initTagDB()

	// Initialize tile proxy once
	tileProxyOnce.Do(func() {
		globalProxy = initTileProxy(debug)
		globalProxy.startPrunerOnce()
	})

	// Bookmarks (CORS)
	mux.HandleFunc("OPTIONS /api/bookmarks", handlePostBookmark(bookmarksPath))
	mux.HandleFunc("POST /api/bookmarks", handlePostBookmark(bookmarksPath))
	mux.HandleFunc("PATCH /api/bookmarks", handlePatchBookmark(bookmarksPath))
	mux.HandleFunc("DELETE /api/bookmarks", handleDeleteBookmark(bookmarksPath))

	// Waypoints & clusters
	mux.HandleFunc("GET /api/waypoints", handleGetWaypoints)
	mux.HandleFunc("GET /api/clusters", handleGetClusters)

	// Tiles
	mux.HandleFunc("GET /api/tiles/stats", globalProxy.serveStats)
	mux.HandleFunc("GET /api/tiles/", globalProxy.serveTile)

	// Location
	mux.HandleFunc("GET /api/location", handleGetLocation)

	// Import
	mux.HandleFunc("POST /api/import", handlePostImport)

	// Tag management
	mux.HandleFunc("GET /api/tags", handleGetTags)
	mux.HandleFunc("POST /api/tags", handlePostTags)
	mux.HandleFunc("DELETE /api/tags", handleDeleteTag)

	// Suggest & history
	mux.HandleFunc("GET /api/suggest", handleGetSuggest)
	mux.HandleFunc("GET /api/recent_suggest", handleGetRecentSuggest)
	mux.HandleFunc("POST /api/history", handlePostHistory)

	// Version info
	mux.HandleFunc("GET /api/version", handleGetVersion)
}

// handleGetVersion returns runtime version information
func handleGetVersion(w http.ResponseWriter, r *http.Request) {
	corsHeaders(w)

	versionInfo := map[string]interface{}{
		"go_version": runtime.Version(),
		"go_os":      runtime.GOOS,
		"go_arch":    runtime.GOARCH,
	}

	// Try to get build info
	if buildInfo, ok := debug.ReadBuildInfo(); ok {
		versionInfo["go_module"] = buildInfo.Path
		if buildInfo.Main.Version != "" && buildInfo.Main.Version != "(devel)" {
			versionInfo["app_version"] = buildInfo.Main.Version
		}

		// Extract build settings
		settings := make(map[string]string)
		for _, setting := range buildInfo.Settings {
			switch setting.Key {
			case "vcs.revision":
				settings["commit"] = setting.Value
				if len(setting.Value) > 7 {
					settings["commit_short"] = setting.Value[:7]
				}
			case "vcs.time":
				settings["build_time"] = setting.Value
			case "vcs.modified":
				settings["dirty"] = setting.Value
			}
		}
		if len(settings) > 0 {
			versionInfo["build_info"] = settings
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(versionInfo); err != nil {
		logger.Error("Failed to encode version info: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
