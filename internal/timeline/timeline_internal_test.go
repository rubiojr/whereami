package timeline

import (
	"context"
	"testing"
	"time"

	"github.com/rubiojr/whereami/internal/admingeo"
	"github.com/rubiojr/whereami/internal/observations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTimelineLimitTruncatesWithoutLosingLatestPosition(t *testing.T) {
	result := Result{}
	accumulator := accumulator{
		ctx:           context.Background(),
		result:        &result,
		timeline:      make([]*timelineAggregate, 0, 2),
		timelineLimit: 2,
	}
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for index, longitude := range []float64{0, 1, 2} {
		err := accumulator.recordTimeline(timelineResolverPath(), observations.Observation{
			Latitude:  41,
			Longitude: longitude,
			Time:      start.Add(time.Duration(index) * time.Hour),
		})
		require.NoError(t, err)
	}

	stops := accumulator.timelineStops()
	require.Len(t, stops, 2)
	assert.True(t, result.TimelineTruncated)
	assert.Equal(t, 0.0, stops[0].Longitude)
	assert.Equal(t, 2.0, stops[1].Longitude)
	assert.Equal(t, int64(2), stops[1].RecordedObservations)
	assert.Equal(t, "2024-01-01T02:00:00Z", stops[1].LastObservationUTC)
}

func TestTimelineZeroLimitDoesNotPanic(t *testing.T) {
	result := Result{}
	accumulator := accumulator{ctx: context.Background(), result: &result}
	err := accumulator.recordTimeline(timelineResolverPath(), observations.Observation{
		Latitude: 41, Longitude: 2, Time: time.Now(),
	})
	require.NoError(t, err)
	assert.True(t, result.TimelineTruncated)
	assert.Empty(t, accumulator.timeline)
}

func TestTruncationInvalidatesUnreliablePlaceTimelineIndexes(t *testing.T) {
	result := Result{}
	accumulator := accumulator{
		ctx:           context.Background(),
		resolver:      indexedTimelineResolver{},
		result:        &result,
		aggregates:    make(map[admingeo.AdminPath]*aggregate),
		timeline:      make([]*timelineAggregate, 0, 2),
		timelineLimit: 2,
	}
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for index, longitude := range []float64{0, 1, 2} {
		require.NoError(t, accumulator.record(observations.Observation{
			CoordinatesValid: true,
			Latitude:         41,
			Longitude:        longitude,
			Time:             start.Add(time.Duration(index) * time.Hour),
			Source:           "test.gpx",
		}))
	}

	places := accumulator.places()
	require.Len(t, places, 3)
	indexes := make(map[string]int, len(places))
	for _, place := range places {
		indexes[place.Locality] = place.TimelineIndex
	}
	assert.Equal(t, 0, indexes["First"])
	assert.Equal(t, -1, indexes["Second"])
	assert.Equal(t, -1, indexes["Third"])
}

type indexedTimelineResolver struct{}

func (indexedTimelineResolver) Resolve(_ context.Context, coordinate admingeo.Coordinate) (admingeo.AdminPath, error) {
	locality := "Third"
	if coordinate.Longitude == 0 {
		locality = "First"
	} else if coordinate.Longitude == 1 {
		locality = "Second"
	}
	return admingeo.AdminPath{Country: "Test", Locality: locality}, nil
}

func (indexedTimelineResolver) Version() admingeo.DatasetVersion { return "test" }
func (indexedTimelineResolver) Close() error                     { return nil }

func timelineResolverPath() admingeo.AdminPath {
	return admingeo.AdminPath{Country: "Test"}
}
