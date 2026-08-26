package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteBookmarksPreservesWaypointData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bookmarks.gpx")
	want := Waypoint{
		Name: "Summit",
		Lat:  12.1234567890123,
		Lon:  -45.9876543210987,
		Ele:  1234.56789,
		Time: "2026-08-26T12:34:56Z",
		Desc: "High point",
	}

	if err := writeBookmarks(path, []Waypoint{want}); err != nil {
		t.Fatalf("writeBookmarks() error = %v", err)
	}
	got, err := parseGPXFile(path)
	if err != nil {
		t.Fatalf("parseGPXFile() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("parseGPXFile() returned %d waypoints, want 1", len(got))
	}
	if got[0].Name != want.Name || got[0].Lat != want.Lat || got[0].Lon != want.Lon ||
		got[0].Ele != want.Ele || got[0].Time != want.Time || got[0].Desc != want.Desc {
		t.Errorf("round trip = %+v, want %+v", got[0], want)
	}
}

func TestAppendBookmarkDoesNotOverwriteMalformedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bookmarks.gpx")
	original := []byte(`<gpx><wpt lat="1" lon="2">`)
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatalf("write malformed GPX fixture: %v", err)
	}

	if _, err := appendBookmark(path, Waypoint{Name: "New", Lat: 3, Lon: 4}); err == nil {
		t.Fatal("appendBookmark() error = nil, want malformed GPX error")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read bookmarks after failed append: %v", err)
	}
	if string(got) != string(original) {
		t.Errorf("bookmarks changed after failed append: got %q, want %q", got, original)
	}
}

func TestCollectGPXWaypointsIgnoresImportStagingDirectories(t *testing.T) {
	root := t.TempDir()
	writeImportGPX(t, filepath.Join(root, "kept.gpx"), "kept", 41)
	writeImportGPX(t, filepath.Join(root, "imports", ".staging-orphan", "staged.gpx"), "staged", 42)

	waypoints, err := collectGPXWaypoints(root, true, "")
	if err != nil {
		t.Fatalf("collectGPXWaypoints() error = %v", err)
	}
	if len(waypoints) != 1 || waypoints[0].Name != "kept" {
		t.Fatalf("collectGPXWaypoints() = %+v, want only kept waypoint", waypoints)
	}
}
