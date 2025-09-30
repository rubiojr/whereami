package main

import (
	"fmt"
	"math"
	"strconv"

	"github.com/rubiojr/whereami/pkg/logger"
)

// waypointEpsilon defines the coordinate precision threshold for considering two
// waypoints identical. Keep this consistent across add/delete/rename/dedupe logic.
const waypointEpsilon = 1e-6

// waypointKeyPrecision is the number of decimal places we normalize to when
// constructing a stable dedupe key. Chosen to align with waypointEpsilon.
const waypointKeyPrecision = 6

// waypointKey returns a stable identity key for a waypoint combining name and
// normalized coordinates. Name participates fully in identity (two different
// names at same coordinates are considered distinct).
func waypointKey(w Waypoint) string {
	lat := strconv.FormatFloat(roundTo(w.Lat, waypointKeyPrecision), 'f', waypointKeyPrecision, 64)
	lon := strconv.FormatFloat(roundTo(w.Lon, waypointKeyPrecision), 'f', waypointKeyPrecision, 64)
	return fmt.Sprintf("%s|%s|%s", w.Name, lat, lon)
}

// roundTo rounds v to 'places' decimal digits using standard rounding.
func roundTo(v float64, places int) float64 {
	p := math.Pow10(places)
	return math.Round(v*p) / p
}

// DedupeWaypoints returns a new slice with duplicate waypoints (same name +
// coordinates within waypointEpsilon) removed, preserving the first occurrence
// order. The input slice is not modified.
func DedupeWaypoints(in []Waypoint) []Waypoint {
	if len(in) <= 1 {
		// Nothing to dedupe.
		return append([]Waypoint(nil), in...)
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]Waypoint, 0, len(in))
	for _, w := range in {
		k := waypointKey(w)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, w)
	}
	return out
}

// MergeAndDedupe merges multiple waypoint slices and returns a deduplicated
// result. Later duplicates are discarded (first occurrence wins).
func MergeAndDedupe(slices ...[]Waypoint) []Waypoint {
	totalCap := 0
	for _, s := range slices {
		totalCap += len(s)
	}
	tmp := make([]Waypoint, 0, totalCap)
	for _, s := range slices {
		tmp = append(tmp, s...)
	}
	return DedupeWaypoints(tmp)
}

// RebuildAllWaypoints reconstructs the in-memory waypoint store from persistent
// sources (bookmarks file + all GPX files under dataDir) applying the unified
// deduplication logic. This centralizes the logic currently mirrored in main.go
// and intended to be invoked from the /api/import handler.
//
// NOTE: This function does not mutate global state directly; callers should
// acquire allWaypointsMu and assign the returned slice.
//
// Usage pattern:
//
//	rebuilt := RebuildAllWaypoints(bookmarksPath, dataDir)
//	allWaypointsMu.Lock()
//	allWaypoints = rebuilt
//	allWaypointsMu.Unlock()
func RebuildAllWaypoints(bookmarksPath, dataDir string) []Waypoint {
	var bookmarks []Waypoint
	if fileExists(bookmarksPath) {
		if bms, err := parseGPXFile(bookmarksPath); err == nil {
			for i := range bms {
				bms[i].Bookmark = true
			}
			bookmarks = bms
		}
	}

	others, err := collectGPXWaypoints(dataDir, true, bookmarksPath)
	if err != nil {
		// Non-fatal: log to stderr; keep what we have.
		logger.Error("collectGPXWaypoints error: %v", err)
	}

	return MergeAndDedupe(bookmarks, others)
}

// And in main.go (startup) similarly switch to:
//
//   initial := RebuildAllWaypoints(bookmarksPath, dataDir)
//   allWaypointsMu.Lock()
//   allWaypoints = initial
//   allWaypointsMu.Unlock()
//
// This consolidates deduplication logic in one place and ensures consistent behavior
// between startup and on-demand imports.
