package timeline_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rubiojr/whereami/internal/observations"
	"github.com/rubiojr/whereami/internal/timeline"
)

func BenchmarkGenerateMillionObservations(b *testing.B) {
	const files = 1000
	const observationsPerFile = 1000
	root := b.TempDir()
	waypoints := strings.Builder{}
	waypoints.Grow(observationsPerFile * 90)
	for index := range observationsPerFile {
		waypoints.WriteString(fmt.Sprintf(`<wpt lat="41" lon="2"><name>point-%d</name><time>2024-01-01T00:00:00Z</time></wpt>`, index))
	}
	document := `<?xml version="1.0"?><gpx version="1.1" xmlns="http://www.topografix.com/GPX/1/1">` + waypoints.String() + `</gpx>`
	for index := range files {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("%04d.gpx", index)), []byte(document), 0o600); err != nil {
			b.Fatal(err)
		}
	}
	databasePath := filepath.Join(b.TempDir(), "observations.sqlite")
	repository, err := observations.Open(databasePath)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = repository.Close() })
	if err := repository.Rebuild(root, ""); err != nil {
		b.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		b.Fatal(err)
	}
	repository, err = observations.Open(databasePath)
	if err != nil {
		b.Fatal(err)
	}
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	metadata := timeline.DatasetMetadata{DatasetVersion: "dataset-v1"}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		snapshot, err := repository.Snapshot()
		if err != nil {
			b.Fatal(err)
		}
		result, err := timeline.Generate(context.Background(), snapshot, timelineResolver{}, metadata, start, end, nil)
		closeErr := snapshot.Close()
		if err != nil {
			b.Fatal(err)
		}
		if closeErr != nil {
			b.Fatal(closeErr)
		}
		if result.Summary.RecordedObservations != files*observationsPerFile {
			b.Fatalf("got %d observations", result.Summary.RecordedObservations)
		}
	}
	b.ReportMetric(files*observationsPerFile, "observations/op")
}
