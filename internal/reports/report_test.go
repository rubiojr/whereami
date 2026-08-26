package reports_test

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
	"github.com/rubiojr/whereami/internal/reports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type reportResolver struct{}

func (reportResolver) Resolve(_ context.Context, coordinate admingeo.Coordinate) (admingeo.AdminPath, error) {
	switch coordinate.Longitude {
	case 2:
		return admingeo.AdminPath{Country: "Spain", CountryID: "ES", Region: "Catalonia", RegionID: "CAT", Locality: "Barcelona", LocalityID: "BCN"}, nil
	case 3:
		return admingeo.AdminPath{}, nil
	default:
		return admingeo.AdminPath{Country: "France", CountryID: "FR", Locality: "Paris", LocalityID: "PAR"}, nil
	}
}

func (reportResolver) Version() admingeo.DatasetVersion { return "dataset-v1" }
func (reportResolver) Close() error                     { return nil }

func TestGeneratePreservesRecordedObservationsAndOmitsCoordinates(t *testing.T) {
	root := t.TempDir()
	writeReportGPX(t, filepath.Join(root, "one.gpx"),
		reportWaypoint("first", "41", "2", "2024-01-01T00:00:00Z")+
			reportWaypoint("duplicate", "41", "2", "2024-01-01T00:00:00Z")+
			reportWaypoint("ocean", "0", "3", "2024-01-01T01:00:00Z")+
			reportWaypoint("bad-coordinate", "91", "181", "2024-01-01T02:00:00Z")+
			reportWaypoint("excluded-end", "41", "2", "2024-01-03T00:00:00Z")+
			reportWaypoint("missing-time", "41", "2", ""))
	writeReportGPX(t, filepath.Join(root, "two.gpx"),
		reportWaypoint("next-day", "41", "2", "2024-01-02T00:00:00Z"))

	repository, err := observations.Open(filepath.Join(t.TempDir(), "observations.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, repository.Close()) })
	require.NoError(t, repository.Rebuild(root, ""))
	snapshot, err := repository.Snapshot()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, snapshot.Close()) })

	var progress int64
	report, err := reports.Generate(context.Background(), snapshot, reportResolver{}, reports.DatasetMetadata{
		DatasetVersion: "dataset-v1",
		SourceVersion:  "source-v1",
		Attribution:    "Test contributors",
		License:        "Test license",
	}, mustReportTime(t, "2024-01-01T00:00:00Z"), mustReportTime(t, "2024-01-03T00:00:00Z"), func(processed int64) {
		progress = processed
	})
	require.NoError(t, err)
	assert.Equal(t, int64(5), report.Summary.RecordedObservations)
	assert.Equal(t, int64(3), report.Summary.ResolvedObservations)
	assert.Equal(t, int64(1), report.Summary.UnresolvedObservations)
	assert.Equal(t, int64(1), report.Summary.InvalidCoordinates)
	assert.Equal(t, int64(6), report.Summary.IndexedValidTimes)
	assert.Equal(t, int64(1), report.Summary.IndexedMissingTimes)
	assert.Equal(t, int64(5), progress)
	require.Len(t, report.Places, 1)
	assert.Equal(t, int64(3), report.Places[0].RecordedObservations)
	assert.Equal(t, 2, report.Places[0].RecordedDays)
	assert.Equal(t, 2, report.Places[0].SourceFiles)

	encoded, err := json.Marshal(report)
	require.NoError(t, err)
	response := strings.ToLower(string(encoded))
	assert.NotContains(t, response, "latitude")
	assert.NotContains(t, response, "longitude")
	assert.NotContains(t, response, "one.gpx")
	assert.NotContains(t, response, "two.gpx")
}

func TestGenerateValidatesRangeVersionAndCancellation(t *testing.T) {
	repository, err := observations.Open(filepath.Join(t.TempDir(), "observations.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, repository.Close()) })
	snapshot, err := repository.Snapshot()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, snapshot.Close()) })
	start := mustReportTime(t, "2024-01-01T00:00:00Z")

	_, err = reports.Generate(context.Background(), snapshot, reportResolver{}, reports.DatasetMetadata{DatasetVersion: "wrong"}, start, start.Add(time.Hour), nil)
	assert.ErrorContains(t, err, "does not match")
	_, err = reports.Generate(context.Background(), snapshot, reportResolver{}, reports.DatasetMetadata{DatasetVersion: "dataset-v1"}, start, start, nil)
	assert.ErrorContains(t, err, "must follow")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = reports.Generate(ctx, snapshot, reportResolver{}, reports.DatasetMetadata{DatasetVersion: "dataset-v1"}, start, start.Add(time.Hour), nil)
	assert.ErrorIs(t, err, context.Canceled)
}

func writeReportGPX(t *testing.T, path, waypoints string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	content := `<?xml version="1.0"?><gpx version="1.1" xmlns="http://www.topografix.com/GPX/1/1">` + waypoints + `</gpx>`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func reportWaypoint(name, latitude, longitude, timestamp string) string {
	timeElement := ""
	if timestamp != "" {
		timeElement = fmt.Sprintf("<time>%s</time>", timestamp)
	}
	return fmt.Sprintf(`<wpt lat="%s" lon="%s"><name>%s</name>%s</wpt>`, latitude, longitude, name, timeElement)
}

func mustReportTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	require.NoError(t, err)
	return parsed
}
