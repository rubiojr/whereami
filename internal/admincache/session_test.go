package admincache

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/rubiojr/whereami/internal/admingeo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubResolver struct {
	mu        sync.Mutex
	version   admingeo.DatasetVersion
	path      admingeo.AdminPath
	resolve   func(context.Context, admingeo.Coordinate) (admingeo.AdminPath, error)
	calls     int
	closeCall int
}

func (r *stubResolver) Resolve(ctx context.Context, coordinate admingeo.Coordinate) (admingeo.AdminPath, error) {
	r.mu.Lock()
	r.calls++
	resolve := r.resolve
	path := r.path
	r.mu.Unlock()
	if resolve != nil {
		return resolve(ctx, coordinate)
	}
	return path, nil
}

func (r *stubResolver) Version() admingeo.DatasetVersion { return r.version }

func (r *stubResolver) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeCall++
	return nil
}

func (r *stubResolver) counts() (int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls, r.closeCall
}

func TestSessionUsesPersistentCacheAndMemoizesDuplicates(t *testing.T) {
	store := openTestStore(t)
	coordinate := admingeo.Coordinate{Longitude: 2, Latitude: 41}
	want := admingeo.AdminPath{Country: "Spain"}
	require.NoError(t, store.Put(context.Background(), "dataset-v1", []Entry{{Coordinate: coordinate, Path: want}}))
	upstream := &stubResolver{version: "dataset-v1", path: admingeo.AdminPath{Country: "wrong"}}
	session, err := NewSession(store, upstream)
	require.NoError(t, err)

	for range 2 {
		got, err := session.Resolve(context.Background(), coordinate)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
	require.NoError(t, session.Close())
	calls, closeCalls := upstream.counts()
	assert.Zero(t, calls)
	assert.Zero(t, closeCalls)
}

func TestSessionWritesMissesOnCloseIncludingNegativeResults(t *testing.T) {
	store := openTestStore(t)
	coordinate := admingeo.Coordinate{Longitude: -30, Latitude: 20}
	upstream := &stubResolver{version: "dataset-v1"}
	session, err := NewSession(store, upstream)
	require.NoError(t, err)

	for range 2 {
		path, err := session.Resolve(context.Background(), coordinate)
		require.NoError(t, err)
		assert.Equal(t, admingeo.AdminPath{}, path)
	}
	calls, _ := upstream.counts()
	assert.Equal(t, 1, calls)
	_, found, err := store.Lookup(context.Background(), "dataset-v1", coordinate)
	require.NoError(t, err)
	assert.False(t, found)
	require.NoError(t, session.Close())

	path, found, err := store.Lookup(context.Background(), "dataset-v1", coordinate)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, admingeo.AdminPath{}, path)
}

func TestSessionWritesFullBatchesBeforeClose(t *testing.T) {
	store := openTestStore(t)
	upstream := &stubResolver{version: "dataset-v1", path: admingeo.AdminPath{Country: "resolved"}}
	session, err := NewSession(store, upstream)
	require.NoError(t, err)
	for index := range sessionBatchSize {
		_, err := session.Resolve(context.Background(), admingeo.Coordinate{Longitude: float64(index), Latitude: 1})
		require.NoError(t, err)
	}

	path, found, err := store.Lookup(context.Background(), "dataset-v1", admingeo.Coordinate{Longitude: 0, Latitude: 1})
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, admingeo.AdminPath{Country: "resolved"}, path)
	require.NoError(t, session.Close())
}

func TestSessionDoesNotCacheResolverErrorsOrCancellation(t *testing.T) {
	store := openTestStore(t)
	coordinateWithError := admingeo.Coordinate{Longitude: 1, Latitude: 1}
	canceledCoordinate := admingeo.Coordinate{Longitude: 2, Latitude: 2}
	resolveErr := errors.New("resolution failed")
	ctx, cancel := context.WithCancel(context.Background())
	upstream := &stubResolver{
		version: "dataset-v1",
		resolve: func(_ context.Context, coordinate admingeo.Coordinate) (admingeo.AdminPath, error) {
			if coordinate == coordinateWithError {
				return admingeo.AdminPath{}, resolveErr
			}
			cancel()
			return admingeo.AdminPath{Country: "must not be cached"}, nil
		},
	}
	session, err := NewSession(store, upstream)
	require.NoError(t, err)

	_, err = session.Resolve(context.Background(), coordinateWithError)
	require.ErrorIs(t, err, resolveErr)
	_, err = session.Resolve(ctx, canceledCoordinate)
	require.ErrorIs(t, err, context.Canceled)
	require.NoError(t, session.Close())
	for _, coordinate := range []admingeo.Coordinate{coordinateWithError, canceledCoordinate} {
		_, found, err := store.Lookup(context.Background(), "dataset-v1", coordinate)
		require.NoError(t, err)
		assert.False(t, found)
	}
}

func TestSessionTreatsCacheFailuresAsNonfatalAndReportsFlushError(t *testing.T) {
	store := openTestStore(t)
	upstream := &stubResolver{version: "dataset-v1", path: admingeo.AdminPath{Country: "Spain"}}
	session, err := NewSession(store, upstream)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	path, err := session.Resolve(context.Background(), admingeo.Coordinate{Longitude: 2, Latitude: 41})
	require.NoError(t, err)
	assert.Equal(t, admingeo.AdminPath{Country: "Spain"}, path)
	require.ErrorContains(t, session.Close(), "flush session cache")
	_, closeCalls := upstream.counts()
	assert.Zero(t, closeCalls)
}

func TestSessionMemoIsBounded(t *testing.T) {
	store := openTestStore(t)
	upstream := &stubResolver{version: "dataset-v1", path: admingeo.AdminPath{Country: "resolved"}}
	session, err := NewSession(store, upstream)
	require.NoError(t, err)
	for index := range sessionMemoCapacity + 10 {
		_, err := session.Resolve(context.Background(), admingeo.Coordinate{Longitude: float64(index), Latitude: 1})
		require.NoError(t, err)
	}
	session.mu.Lock()
	assert.Len(t, session.memo, sessionMemoCapacity)
	session.mu.Unlock()
	require.NoError(t, session.Close())
}

func TestSessionPersistsCompletedBatchAfterReportCancellation(t *testing.T) {
	store := openTestStore(t)
	upstream := &stubResolver{version: "dataset-v1"}
	session, err := NewSession(store, upstream)
	require.NoError(t, err)
	coordinate := admingeo.Coordinate{Longitude: 2, Latitude: 41}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	session.write(ctx, []Entry{{Coordinate: coordinate, Path: admingeo.AdminPath{Country: "Spain"}}})
	require.NoError(t, session.Close())
	path, found, err := store.Lookup(context.Background(), "dataset-v1", coordinate)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, admingeo.AdminPath{Country: "Spain"}, path)
}
