package timeline_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rubiojr/whereami/internal/admingeo"
	"github.com/rubiojr/whereami/internal/observations"
	"github.com/rubiojr/whereami/internal/timeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type timelineResolver struct{}

func (timelineResolver) Resolve(_ context.Context, coordinate admingeo.Coordinate) (admingeo.AdminPath, error) {
	switch coordinate.Longitude {
	case 2:
		return admingeo.AdminPath{Country: "Spain", CountryID: "ES", Region: "Catalonia", RegionID: "CAT", Locality: "Barcelona", LocalityID: "BCN"}, nil
	case 3:
		return admingeo.AdminPath{}, nil
	default:
		return admingeo.AdminPath{Country: "France", CountryID: "FR", Locality: "Paris", LocalityID: "PAR"}, nil
	}
}

func (timelineResolver) Version() admingeo.DatasetVersion { return "dataset-v1" }
func (timelineResolver) Close() error                     { return nil }

func TestGeneratePreservesRecordedObservationsAndOmitsSources(t *testing.T) {
	root := t.TempDir()
	writeTimelineGPX(t, filepath.Join(root, "one.gpx"),
		timelineWaypoint("first", "41", "2", "2024-01-01T00:00:00Z")+
			timelineWaypoint("duplicate", "41", "2", "2024-01-01T12:34:00Z")+
			timelineWaypoint("offset-still-utc-day", "41", "2", "2024-01-02T00:30:00+01:00")+
			timelineWaypoint("ocean", "0", "3", "2024-01-01T01:00:00Z")+
			timelineWaypoint("bad-coordinate", "91", "181", "2024-01-01T02:00:00Z")+
			timelineWaypoint("excluded-end", "41", "2", "2024-01-03T00:00:00Z")+
			timelineWaypoint("missing-time", "41", "2", ""))
	writeTimelineGPX(t, filepath.Join(root, "two.gpx"),
		timelineWaypoint("next-day", "41", "2", "2024-01-02T00:00:00Z"))

	repository, err := observations.Open(filepath.Join(t.TempDir(), "observations.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, repository.Close()) })
	require.NoError(t, repository.Rebuild(root, ""))
	snapshot, err := repository.Snapshot()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, snapshot.Close()) })

	var progress int64
	result, err := timeline.Generate(context.Background(), snapshot, timelineResolver{}, timeline.DatasetMetadata{
		DatasetVersion: "dataset-v1",
		SourceVersion:  "source-v1",
		Attribution:    "Test contributors",
		License:        "Test license",
	}, mustTimelineTime(t, "2024-01-01T00:00:00Z"), mustTimelineTime(t, "2024-01-03T00:00:00Z"), func(processed int64) {
		progress = processed
	})
	require.NoError(t, err)
	assert.Equal(t, int64(6), result.Summary.RecordedObservations)
	assert.Equal(t, int64(4), result.Summary.ResolvedObservations)
	assert.Equal(t, int64(1), result.Summary.UnresolvedObservations)
	assert.Equal(t, int64(1), result.Summary.InvalidCoordinates)
	assert.Equal(t, int64(7), result.Summary.IndexedValidTimes)
	assert.Equal(t, int64(1), result.Summary.IndexedMissingTimes)
	assert.Equal(t, int64(6), progress)
	require.Len(t, result.Places, 1)
	assert.Equal(t, int64(4), result.Places[0].RecordedObservations)
	assert.Equal(t, 2, result.Places[0].RecordedDays)
	assert.Equal(t, 2, result.Places[0].SourceFiles)
	assert.Equal(t, 2, result.Places[0].TimelineIndex)
	assert.Equal(t, 100.0, result.TimelineStopSeparationMeters)
	require.Len(t, result.Timeline, 3)
	assert.Equal(t, "2024-01-01", result.Timeline[0].DateUTC)
	assert.Equal(t, int64(1), result.Timeline[0].RecordedObservations)
	assert.Equal(t, "Barcelona", result.Timeline[0].Locality)
	assert.Equal(t, 41.0, result.Timeline[0].Latitude)
	assert.Equal(t, 2.0, result.Timeline[0].Longitude)
	assert.Empty(t, result.Timeline[1].Country)
	assert.Equal(t, 0.0, result.Timeline[1].Latitude)
	assert.Equal(t, 3.0, result.Timeline[1].Longitude)
	assert.Equal(t, "2024-01-02", result.Timeline[2].DateUTC)
	assert.Equal(t, int64(3), result.Timeline[2].RecordedObservations)
	assert.Equal(t, "2024-01-01T12:34:00Z", result.Timeline[2].FirstObservationUTC)
	assert.Equal(t, "2024-01-02T00:00:00Z", result.Timeline[2].LastObservationUTC)

	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	response := strings.ToLower(string(encoded))
	assert.Contains(t, response, `"latitude":41`)
	assert.Contains(t, response, `"longitude":2`)
	assert.Contains(t, response, `"timeline_index":2`)
	assert.NotContains(t, response, "one.gpx")
	assert.NotContains(t, response, "two.gpx")
}

func TestGenerateTimelineCollapsesMovementBelowOneHundredMeters(t *testing.T) {
	root := t.TempDir()
	writeTimelineGPX(t, filepath.Join(root, "timeline.gpx"),
		timelineWaypoint("start", "41", "2", "2024-01-01T00:00:00Z")+
			timelineWaypoint("nearby", "41", "2.0005", "2024-01-01T01:00:00Z")+
			timelineWaypoint("far", "41", "2.002", "2024-01-01T02:00:00Z"))

	repository, err := observations.Open(filepath.Join(t.TempDir(), "observations.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, repository.Close()) })
	require.NoError(t, repository.Rebuild(root, ""))
	snapshot, err := repository.Snapshot()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, snapshot.Close()) })

	result, err := timeline.Generate(context.Background(), snapshot, timelineResolver{}, timeline.DatasetMetadata{
		DatasetVersion: "dataset-v1",
	}, mustTimelineTime(t, "2024-01-01T00:00:00Z"), mustTimelineTime(t, "2024-01-02T00:00:00Z"), nil)
	require.NoError(t, err)
	require.Len(t, result.Timeline, 2)
	assert.Equal(t, int64(2), result.Timeline[0].RecordedObservations)
	assert.Equal(t, 2.0, result.Timeline[0].Longitude)
	assert.Equal(t, "2024-01-01T01:00:00Z", result.Timeline[0].LastObservationUTC)
	assert.Equal(t, int64(1), result.Timeline[1].RecordedObservations)
	assert.Equal(t, 2.002, result.Timeline[1].Longitude)
}

func TestGenerateValidatesRangeVersionAndCancellation(t *testing.T) {
	repository, err := observations.Open(filepath.Join(t.TempDir(), "observations.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, repository.Close()) })
	snapshot, err := repository.Snapshot()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, snapshot.Close()) })
	start := mustTimelineTime(t, "2024-01-01T00:00:00Z")

	_, err = timeline.Generate(context.Background(), snapshot, timelineResolver{}, timeline.DatasetMetadata{DatasetVersion: "wrong"}, start, start.Add(time.Hour), nil)
	assert.ErrorContains(t, err, "does not match")
	_, err = timeline.Generate(context.Background(), snapshot, timelineResolver{}, timeline.DatasetMetadata{DatasetVersion: "dataset-v1"}, start, start, nil)
	assert.ErrorContains(t, err, "must follow")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = timeline.Generate(ctx, snapshot, timelineResolver{}, timeline.DatasetMetadata{DatasetVersion: "dataset-v1"}, start, start.Add(time.Hour), nil)
	assert.ErrorIs(t, err, context.Canceled)
}

func writeTimelineGPX(t *testing.T, path, waypoints string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	content := `<?xml version="1.0"?><gpx version="1.1" xmlns="http://www.topografix.com/GPX/1/1">` + waypoints + `</gpx>`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func timelineWaypoint(name, latitude, longitude, timestamp string) string {
	timeElement := ""
	if timestamp != "" {
		timeElement = fmt.Sprintf("<time>%s</time>", timestamp)
	}
	return fmt.Sprintf(`<wpt lat="%s" lon="%s"><name>%s</name>%s</wpt>`, latitude, longitude, name, timeElement)
}

func mustTimelineTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	require.NoError(t, err)
	return parsed
}
