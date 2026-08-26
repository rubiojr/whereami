package observations

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepositoryPreservesDuplicateWaypointsBySourceAndOrdinal(t *testing.T) {
	dataRoot := t.TempDir()
	writeGPX(t, filepath.Join(dataRoot, "one.gpx"), waypoint("same", "10", "20", "2024-01-01T00:00:00Z")+
		waypoint("same", "10", "20", "2024-01-02T00:00:00Z"))
	writeGPX(t, filepath.Join(dataRoot, "nested", "two.gpx"),
		waypoint("same", "10", "20", "2024-01-01T00:00:00Z"))

	repository := openTestRepository(t)
	require.NoError(t, repository.Rebuild(dataRoot, filepath.Join(dataRoot, "bookmarks.gpx")))
	snapshot := openTestSnapshot(t, repository)

	observations := scanPeriod(t, snapshot, mustTime(t, "2024-01-01T00:00:00Z"), mustTime(t, "2024-01-03T00:00:00Z"))
	require.Len(t, observations, 3)
	assert.Equal(t, []string{"nested/two.gpx", "one.gpx", "one.gpx"}, observationSources(observations))
	assert.Equal(t, []int{0, 0, 1}, observationOrdinals(observations))
	for _, observation := range observations {
		assert.Equal(t, "same", observation.Name)
		assert.Equal(t, 10.0, observation.Latitude)
		assert.Equal(t, 20.0, observation.Longitude)
		assert.True(t, observation.CoordinatesValid)
	}
}

func TestRepositoryRecursesAndExcludesExplicitBookmarks(t *testing.T) {
	dataRoot := t.TempDir()
	bookmarksPath := filepath.Join(dataRoot, "private", "saved.gpx")
	writeGPX(t, bookmarksPath, waypoint("bookmark", "1", "2", "2024-01-01T00:00:00Z"))
	writeGPX(t, filepath.Join(dataRoot, "imports", "day", "track.GPX"),
		waypoint("import", "3", "4", "2024-01-01T00:00:00Z"))
	writeGPX(t, filepath.Join(dataRoot, "not-gpx.xml"),
		waypoint("ignored", "5", "6", "2024-01-01T00:00:00Z"))

	repository := openTestRepository(t)
	require.NoError(t, repository.Rebuild(dataRoot, bookmarksPath))
	snapshot := openTestSnapshot(t, repository)
	observations := scanPeriod(t, snapshot, mustTime(t, "2023-01-01T00:00:00Z"), mustTime(t, "2025-01-01T00:00:00Z"))

	require.Len(t, observations, 1)
	assert.Equal(t, "import", observations[0].Name)
	assert.Equal(t, "imports/day/track.GPX", observations[0].Source)
}

func TestScanPeriodUsesExactUTCBoundaries(t *testing.T) {
	dataRoot := t.TempDir()
	writeGPX(t, filepath.Join(dataRoot, "times.gpx"),
		waypoint("before", "1", "2", "2024-02-28T23:59:59.999999999Z")+
			waypoint("start-offset", "1", "2", "2024-02-29T01:00:00+01:00")+
			waypoint("fraction", "1", "2", "2024-02-29T00:00:00.123456789Z")+
			waypoint("leap-day", "1", "2", "2024-02-29T23:59:59-02:00")+
			waypoint("end-offset", "1", "2", "2024-03-01T03:00:00+01:00"))

	repository := openTestRepository(t)
	require.NoError(t, repository.Rebuild(dataRoot, ""))
	snapshot := openTestSnapshot(t, repository)
	start := mustTime(t, "2024-02-29T00:00:00Z")
	end := mustTime(t, "2024-03-01T02:00:00Z")
	observations := scanPeriod(t, snapshot, start, end)

	require.Len(t, observations, 3)
	assert.Equal(t, []string{"start-offset", "fraction", "leap-day"}, observationNames(observations))
	assert.Equal(t, start, observations[0].Time)
	assert.Equal(t, 123456789, observations[1].Time.Nanosecond())
	for _, observation := range observations {
		assert.Equal(t, time.UTC, observation.Time.Location())
	}

	fractionOnly := scanPeriod(t, snapshot,
		mustTime(t, "2024-02-29T00:00:00.123456789Z"),
		mustTime(t, "2024-02-29T00:00:00.123456790Z"))
	require.Len(t, fractionOnly, 1)
	assert.Equal(t, "fraction", fractionOnly[0].Name)
}

func TestTimeStatusesAndCoordinateValidationRetainObservations(t *testing.T) {
	dataRoot := t.TempDir()
	writeGPX(t, filepath.Join(dataRoot, "quality.gpx"),
		waypoint("valid", "45", "90", "2024-01-01T00:00:00Z")+
			waypoint("missing", "46", "91", "")+
			waypoint("invalid-time", "47", "92", "not-a-time")+
			waypoint("bad-range", "91", "181", "2024-01-01T00:00:01Z")+
			waypoint("not-finite", "NaN", "-Inf", "2024-01-01T00:00:02Z"))

	repository := openTestRepository(t)
	require.NoError(t, repository.Rebuild(dataRoot, ""))
	snapshot := openTestSnapshot(t, repository)

	counts, err := snapshot.TimeStatusCounts()
	require.NoError(t, err)
	assert.Equal(t, TimeStatusCounts{Valid: 3, Missing: 1, Invalid: 1}, counts)

	observations := scanPeriod(t, snapshot, mustTime(t, "2024-01-01T00:00:00Z"), mustTime(t, "2024-01-02T00:00:00Z"))
	require.Len(t, observations, 3)
	assert.True(t, observations[0].CoordinatesValid)
	assert.False(t, observations[1].CoordinatesValid)
	assert.Equal(t, 91.0, observations[1].Latitude)
	assert.Equal(t, 181.0, observations[1].Longitude)
	assert.False(t, observations[2].CoordinatesValid)
	assert.Equal(t, "NaN", observations[2].RawLatitude)
	assert.Equal(t, "-Inf", observations[2].RawLongitude)
}

func TestRebuildReplacesChangedSourcesAndRemovesDeletedSources(t *testing.T) {
	dataRoot := t.TempDir()
	firstPath := filepath.Join(dataRoot, "first.gpx")
	deletedPath := filepath.Join(dataRoot, "deleted.gpx")
	writeGPX(t, firstPath, waypoint("old", "1", "2", "2024-01-01T00:00:00Z"))
	writeGPX(t, deletedPath, waypoint("deleted", "3", "4", "2024-01-01T00:00:00Z"))

	repository := openTestRepository(t)
	require.NoError(t, repository.Rebuild(dataRoot, ""))
	oldSnapshot := openTestSnapshot(t, repository)

	writeGPX(t, firstPath,
		waypoint("new-one", "5", "6", "2024-01-02T00:00:00Z")+
			waypoint("new-two", "7", "8", "2024-01-03T00:00:00Z"))
	require.NoError(t, os.Remove(deletedPath))
	require.NoError(t, repository.Rebuild(dataRoot, ""))

	oldObservations := scanPeriod(t, oldSnapshot, mustTime(t, "2024-01-01T00:00:00Z"), mustTime(t, "2024-01-04T00:00:00Z"))
	assert.Equal(t, []string{"deleted", "old"}, observationNames(oldObservations))

	newSnapshot := openTestSnapshot(t, repository)
	newObservations := scanPeriod(t, newSnapshot, mustTime(t, "2024-01-01T00:00:00Z"), mustTime(t, "2024-01-04T00:00:00Z"))
	assert.Equal(t, []string{"new-one", "new-two"}, observationNames(newObservations))
	assert.NotEqual(t, oldSnapshot.Revision(), newSnapshot.Revision())
}

func TestRebuildRollsBackAllChangesWhenAChangedSourceIsMalformed(t *testing.T) {
	dataRoot := t.TempDir()
	firstPath := filepath.Join(dataRoot, "a.gpx")
	writeGPX(t, firstPath, waypoint("old", "1", "2", "2024-01-01T00:00:00Z"))

	repository := openTestRepository(t)
	require.NoError(t, repository.Rebuild(dataRoot, ""))
	originalRevision := snapshotRevision(t, repository)

	writeGPX(t, firstPath, waypoint("new", "3", "4", "2024-01-02T00:00:00Z"))
	writeGPX(t, filepath.Join(dataRoot, "z-broken.gpx"), `<wpt lat="5"`)
	assert.ErrorContains(t, repository.Rebuild(dataRoot, ""), `parse observation source "z-broken.gpx"`)

	snapshot := openTestSnapshot(t, repository)
	assert.Equal(t, originalRevision, snapshot.Revision())
	observations := scanPeriod(t, snapshot, mustTime(t, "2024-01-01T00:00:00Z"), mustTime(t, "2024-01-03T00:00:00Z"))
	require.Len(t, observations, 1)
	assert.Equal(t, "old", observations[0].Name)
}

func TestRevisionIsDeterministicAndContentBased(t *testing.T) {
	rootOne := t.TempDir()
	rootTwo := t.TempDir()
	contentA := waypoint("a", "1", "2", "2024-01-01T00:00:00Z")
	contentB := waypoint("b", "3", "4", "2024-01-02T00:00:00Z")
	writeGPX(t, filepath.Join(rootOne, "z.gpx"), contentB)
	writeGPX(t, filepath.Join(rootOne, "nested", "a.gpx"), contentA)
	writeGPX(t, filepath.Join(rootTwo, "nested", "a.gpx"), contentA)
	writeGPX(t, filepath.Join(rootTwo, "z.gpx"), contentB)

	repositoryOne := openTestRepository(t)
	require.NoError(t, repositoryOne.Rebuild(rootOne, ""))
	revisionOne := snapshotRevision(t, repositoryOne)
	require.Len(t, revisionOne, 64)

	require.NoError(t, repositoryOne.Rebuild(rootOne, ""))
	assert.Equal(t, revisionOne, snapshotRevision(t, repositoryOne))

	repositoryTwo := openTestRepository(t)
	require.NoError(t, repositoryTwo.Rebuild(rootTwo, ""))
	assert.Equal(t, revisionOne, snapshotRevision(t, repositoryTwo))

	writeGPX(t, filepath.Join(rootOne, "z.gpx"), waypoint("changed", "3", "4", "2024-01-02T00:00:00Z"))
	require.NoError(t, repositoryOne.Rebuild(rootOne, ""))
	assert.NotEqual(t, revisionOne, snapshotRevision(t, repositoryOne))

	writeGPX(t, filepath.Join(rootOne, "z.gpx"), contentB)
	require.NoError(t, repositoryOne.Rebuild(rootOne, ""))
	assert.Equal(t, revisionOne, snapshotRevision(t, repositoryOne))
}

func TestDatabasePermissions(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "private", "index")
	dbPath := filepath.Join(parent, "observations.sqlite")
	repository, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, repository.Close()) })

	parentInfo, err := os.Stat(parent)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), parentInfo.Mode().Perm())
	dbInfo, err := os.Stat(dbPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), dbInfo.Mode().Perm())
}

func openTestRepository(t *testing.T) *Repository {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "db", "observations.sqlite")
	repository, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, repository.Close()) })
	return repository
}

func openTestSnapshot(t *testing.T, repository *Repository) *Snapshot {
	t.Helper()
	snapshot, err := repository.Snapshot()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, snapshot.Close()) })
	return snapshot
}

func snapshotRevision(t *testing.T, repository *Repository) string {
	t.Helper()
	snapshot, err := repository.Snapshot()
	require.NoError(t, err)
	defer func() { require.NoError(t, snapshot.Close()) }()
	return snapshot.Revision()
}

func scanPeriod(t *testing.T, snapshot *Snapshot, start, end time.Time) []Observation {
	t.Helper()
	var observations []Observation
	require.NoError(t, snapshot.ScanPeriod(start, end, func(observation Observation) error {
		observations = append(observations, observation)
		return nil
	}))
	return observations
}

func writeGPX(t *testing.T, path, waypoints string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	content := `<?xml version="1.0" encoding="UTF-8"?>
<gpx version="1.1" xmlns="http://www.topografix.com/GPX/1/1">` + waypoints + `</gpx>`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func waypoint(name, latitude, longitude, timestamp string) string {
	timeElement := ""
	if timestamp != "" {
		timeElement = fmt.Sprintf("<time>%s</time>", timestamp)
	}
	return fmt.Sprintf(`<wpt lat="%s" lon="%s"><name>%s</name>%s</wpt>`,
		latitude, longitude, name, timeElement)
}

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	require.NoError(t, err)
	return parsed
}

func observationNames(observations []Observation) []string {
	names := make([]string, 0, len(observations))
	for _, observation := range observations {
		names = append(names, observation.Name)
	}
	return names
}

func observationSources(observations []Observation) []string {
	sources := make([]string, 0, len(observations))
	for _, observation := range observations {
		sources = append(sources, observation.Source)
	}
	return sources
}

func observationOrdinals(observations []Observation) []int {
	ordinals := make([]int, 0, len(observations))
	for _, observation := range observations {
		ordinals = append(ordinals, observation.Ordinal)
	}
	return ordinals
}
