package main

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rubiojr/whereami/pkg/logger"
)

// Persistence / storage layer for waypoints & bookmarks.
//
// Responsibilities:
//   - Parse GPX files into in‑memory `Waypoint` slices.
//   - Collect waypoints from a directory tree (optionally recursive).
//   - Append/delete bookmark waypoints with duplicate prevention and atomic writes.
//   - Serialize bookmarks back to GPX safely.
//   - Rename bookmark waypoints.
//
// Concurrency:
//   - `bookmarkMu` guards writes to the bookmarks GPX file.
//   - Callers that mutate in‑memory global slices (e.g. `allWaypoints`) must
//     still use their own synchronization (`allWaypointsMu` in main.go).
//
// Atomic write pattern:
//   - Write to `file.tmp` then `os.Rename` over the original to avoid partial files.
//
// Duplicate detection:
//   - Name equality + lat/lon within epsilon (1e-6) is considered a duplicate.
//
// NOTE: After moving these helpers here, remove their counterparts from main.go
// to avoid duplicate symbol compilation errors.

// Sentinel error for duplicate bookmarks.
var ErrDuplicate = errors.New("duplicate bookmark")

// Guards concurrent writes to bookmarks.gpx (file-level serialization).
var bookmarkMu sync.Mutex

// Waypoint represents a GPX waypoint (<wpt>).
type Waypoint struct {
	Name     string  `xml:"name" json:"name,omitempty"`
	Lat      float64 `xml:"lat,attr" json:"lat"`
	Lon      float64 `xml:"lon,attr" json:"lon"`
	Ele      float64 `xml:"ele" json:"ele,omitempty"`
	Time     string  `xml:"time" json:"time,omitempty"`
	Desc     string  `xml:"desc" json:"desc,omitempty"`
	Bookmark bool    `xml:"-" json:"bookmark,omitempty"` // true if sourced from / destined to bookmarks.gpx
	Deleted  bool    `xml:"-" json:"-"`                  // internal helper (soft delete when rewriting)
}

// gpxRoot is the root structure used for GPX (de)serialization.
type gpxRoot struct {
	Waypoints []Waypoint `xml:"wpt"`
}

// parseGPXFile loads a GPX file and returns normalized waypoints (timestamps -> RFC3339 UTC).
func parseGPXFile(path string) ([]Waypoint, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var root gpxRoot
	if err := xml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	for i := range root.Waypoints {
		if ts := root.Waypoints[i].Time; ts != "" {
			if t, err := time.Parse(time.RFC3339, ts); err == nil {
				root.Waypoints[i].Time = t.UTC().Format(time.RFC3339)
			}
		}
	}
	return root.Waypoints, nil
}

// collectGPXWaypoints walks a directory collecting waypoints from *.gpx files,
// optionally recursively. It skips the `exclude` path if provided.
func collectGPXWaypoints(dir string, recursive bool, exclude string) ([]Waypoint, error) {
	var all []Waypoint
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if !recursive && p != dir {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Clean(p) == filepath.Clean(exclude) {
			return nil
		}
		if strings.EqualFold(filepath.Ext(d.Name()), ".gpx") {
			wps, err := parseGPXFile(p)
			if err != nil {
				logger.Error("Skipping %s: %v", p, err)
				return nil
			}
			all = append(all, wps...)
		}
		return nil
	})
	return all, err
}

// writeBookmarks rewrites the bookmark list (skipping Deleted entries) to path using
// an atomic temp-file + rename pattern. Caller must hold bookmarkMu.
func writeBookmarks(path string, wps []Waypoint) error {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<gpx version="1.1" creator="whereami" xmlns="http://www.topografix.com/GPX/1/1">` + "\n")
	for _, e := range wps {
		if e.Deleted {
			continue
		}
		fmt.Fprintf(&b, "  <wpt lat=\"%f\" lon=\"%f\">\n", e.Lat, e.Lon)
		if e.Time != "" {
			fmt.Fprintf(&b, "    <time>%s</time>\n", e.Time)
		}
		if e.Name != "" {
			name := escapeXML(e.Name)
			fmt.Fprintf(&b, "    <name>%s</name>\n", name)
		}
		if e.Desc != "" {
			desc := escapeXML(e.Desc)
			fmt.Fprintf(&b, "    <desc>%s</desc>\n", desc)
		}
		b.WriteString("  </wpt>\n")
	}
	b.WriteString("</gpx>\n")

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// appendBookmark adds a new waypoint into bookmarks.gpx (creating or extending
// the existing list) while preventing duplicates. Returns the waypoint (with
// Bookmark flag set) or ErrDuplicate.
func appendBookmark(bookmarksPath string, wp Waypoint) (Waypoint, error) {
	bookmarkMu.Lock()
	defer bookmarkMu.Unlock()

	if wp.Time == "" {
		wp.Time = time.Now().UTC().Format(time.RFC3339)
	}

	var existing []Waypoint
	if fi, err := os.Stat(bookmarksPath); err == nil && fi.Size() > 0 {
		if wps, err := parseGPXFile(bookmarksPath); err == nil {
			existing = wps
		}
	}

	const eps = 1e-6
	for _, e := range existing {
		if e.Name == wp.Name &&
			abs(e.Lat-wp.Lat) < eps &&
			abs(e.Lon-wp.Lon) < eps {
			return wp, ErrDuplicate
		}
	}

	existing = append(existing, wp)
	if err := writeBookmarks(bookmarksPath, existing); err != nil {
		return wp, err
	}
	wp.Bookmark = true
	return wp, nil
}

// deleteBookmark marks a waypoint (by name + lat/lon within epsilon) as deleted
// and rewrites the file. Returns (found, error).
func deleteBookmark(bookmarksPath, name string, lat, lon float64) (bool, error) {
	bookmarkMu.Lock()
	defer bookmarkMu.Unlock()

	wps, err := parseGPXFile(bookmarksPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	const eps = 1e-6
	found := false
	for i := range wps {
		if wps[i].Name == name &&
			abs(wps[i].Lat-lat) < eps &&
			abs(wps[i].Lon-lon) < eps {
			wps[i].Deleted = true
			found = true
		}
	}
	if !found {
		return false, nil
	}
	if err := writeBookmarks(bookmarksPath, wps); err != nil {
		return false, err
	}
	return true, nil
}

// renameBookmark changes the name of a bookmark matched by (oldName, lat, lon) within epsilon.
// Returns (found, error). Duplicate name (same name+coords already present) is treated as success noop.
func renameBookmark(bookmarksPath, oldName string, lat, lon float64, newName string) (bool, error) {
	bookmarkMu.Lock()
	defer bookmarkMu.Unlock()

	if strings.TrimSpace(newName) == "" || newName == oldName {
		// Nothing to do; treat as not-found only if oldName missing later.
	}

	wps, err := parseGPXFile(bookmarksPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	const eps = 1e-6
	found := false
	for i := range wps {
		if wps[i].Name == oldName &&
			abs(wps[i].Lat-lat) < eps &&
			abs(wps[i].Lon-lon) < eps {
			found = true
			// If already same new name, break (idempotent).
			if wps[i].Name == newName {
				break
			}
			wps[i].Name = newName
			break
		}
	}
	if !found {
		return false, nil
	}
	if err := writeBookmarks(bookmarksPath, wps); err != nil {
		return false, err
	}
	return true, nil
}

// escapeXML performs minimal escaping for XML content nodes (not attributes).
func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// abs returns absolute value of a float64.
func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
