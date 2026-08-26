package reports

import (
	"context"
	"testing"
	"time"

	"github.com/rubiojr/whereami/internal/admingeo"
	"github.com/rubiojr/whereami/internal/observations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJourneyLimitTruncatesWithoutLosingLatestPosition(t *testing.T) {
	report := Report{}
	accumulator := accumulator{
		ctx:           context.Background(),
		report:        &report,
		timeline:      make([]*timelineAggregate, 0, 2),
		timelineLimit: 2,
	}
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for index, longitude := range []float64{0, 1, 2} {
		err := accumulator.recordTimeline(reportResolverPath(), observations.Observation{
			Latitude:  41,
			Longitude: longitude,
			Time:      start.Add(time.Duration(index) * time.Hour),
		})
		require.NoError(t, err)
	}

	stops := accumulator.timelineStops()
	require.Len(t, stops, 2)
	assert.True(t, report.JourneyTruncated)
	assert.Equal(t, 0.0, stops[0].Longitude)
	assert.Equal(t, 2.0, stops[1].Longitude)
	assert.Equal(t, int64(2), stops[1].RecordedObservations)
	assert.Equal(t, "2024-01-01T02:00:00Z", stops[1].LastObservationUTC)
}

func TestJourneyZeroLimitDoesNotPanic(t *testing.T) {
	report := Report{}
	accumulator := accumulator{ctx: context.Background(), report: &report}
	err := accumulator.recordTimeline(reportResolverPath(), observations.Observation{
		Latitude: 41, Longitude: 2, Time: time.Now(),
	})
	require.NoError(t, err)
	assert.True(t, report.JourneyTruncated)
	assert.Empty(t, accumulator.timeline)
}

func reportResolverPath() admingeo.AdminPath {
	return admingeo.AdminPath{Country: "Test"}
}
