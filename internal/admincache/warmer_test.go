package admincache

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rubiojr/whereami/internal/admingeo"
	"github.com/rubiojr/whereami/internal/observations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type warmerResolver struct {
	version  admingeo.DatasetVersion
	resolve  func(context.Context, admingeo.Coordinate) (admingeo.AdminPath, error)
	closes   *atomic.Int32
	closeErr error
}

func (r *warmerResolver) Resolve(ctx context.Context, coordinate admingeo.Coordinate) (admingeo.AdminPath, error) {
	return r.resolve(ctx, coordinate)
}

func (r *warmerResolver) Version() admingeo.DatasetVersion { return r.version }

func (r *warmerResolver) Close() error {
	if r.closes != nil {
		r.closes.Add(1)
	}
	return r.closeErr
}

func TestWarmerCachesDistinctCoordinatesAndMarksCompletion(t *testing.T) {
	repository, revision := openWarmerRepository(t,
		warmerWaypoint("one", "41", "2", "2024-01-01T00:00:00Z")+
			warmerWaypoint("duplicate", "41.0", "2.0", "2024-01-01T00:00:01Z")+
			warmerWaypoint("negative", "20", "-30", "2024-01-01T00:00:02Z"))
	store := openTestStore(t)
	previousCoordinate := admingeo.Coordinate{Longitude: 10, Latitude: 10}
	require.NoError(t, store.Put(context.Background(), "dataset-previous", []Entry{{
		Coordinate: previousCoordinate,
		Path:       admingeo.AdminPath{Country: "Previous"},
	}}))
	var resolveCalls, closeCalls atomic.Int32
	acquire := func() (admingeo.Resolver, error) {
		return &warmerResolver{
			version: "dataset-v1",
			closes:  &closeCalls,
			resolve: func(_ context.Context, coordinate admingeo.Coordinate) (admingeo.AdminPath, error) {
				resolveCalls.Add(1)
				if coordinate.Longitude < 0 {
					return admingeo.AdminPath{}, nil
				}
				return admingeo.AdminPath{Country: "Spain"}, nil
			},
		}, nil
	}
	warmer, err := NewWarmer(repository, store, acquire, func() []admingeo.DatasetVersion {
		return []admingeo.DatasetVersion{"dataset-v1", "dataset-previous"}
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, warmer.Close()) })

	warmer.Trigger()
	require.Eventually(t, func() bool {
		complete, err := store.WarmComplete(context.Background(), "dataset-v1", revision)
		return err == nil && complete
	}, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, int32(2), resolveCalls.Load())
	for coordinate, want := range map[admingeo.Coordinate]admingeo.AdminPath{
		{Longitude: 2, Latitude: 41}:   {Country: "Spain"},
		{Longitude: -30, Latitude: 20}: {},
	} {
		path, found, err := store.Lookup(context.Background(), "dataset-v1", coordinate)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, want, path)
	}

	warmer.Trigger()
	require.Eventually(t, func() bool { return closeCalls.Load() == 2 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, int32(2), resolveCalls.Load(), "completed revisions should not resolve again")
	path, found, err := store.Lookup(context.Background(), "dataset-previous", previousCoordinate)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, admingeo.AdminPath{Country: "Previous"}, path)
}

func TestWarmerPausesForForegroundPriority(t *testing.T) {
	repository, revision := openWarmerRepository(t, warmerWaypoint("one", "41", "2", "2024-01-01T00:00:00Z"))
	store := openTestStore(t)
	var acquireCalls atomic.Int32
	acquire := func() (admingeo.Resolver, error) {
		acquireCalls.Add(1)
		return &warmerResolver{
			version: "dataset-v1",
			resolve: func(context.Context, admingeo.Coordinate) (admingeo.AdminPath, error) {
				return admingeo.AdminPath{Country: "Spain"}, nil
			},
		}, nil
	}
	warmer, err := NewWarmer(repository, store, acquire, nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, warmer.Close()) })

	release := warmer.BeginForeground()
	warmer.Trigger()
	assert.Never(t, func() bool { return acquireCalls.Load() != 0 }, 150*time.Millisecond, 10*time.Millisecond)
	release()
	release()
	require.Eventually(t, func() bool {
		complete, err := store.WarmComplete(context.Background(), "dataset-v1", revision)
		return err == nil && complete
	}, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, int32(1), acquireCalls.Load())
}

func TestWarmerPreemptsActivePassAndRestartsAfterForeground(t *testing.T) {
	repository, revision := openWarmerRepository(t, warmerWaypoint("one", "41", "2", "2024-01-01T00:00:00Z"))
	store := openTestStore(t)
	started := make(chan struct{})
	errorsReported := make(chan error, 1)
	closeErr := errors.New("close resolver")
	var acquireCalls, closeCalls atomic.Int32
	acquire := func() (admingeo.Resolver, error) {
		call := acquireCalls.Add(1)
		resolver := &warmerResolver{
			version: "dataset-v1",
			closes:  &closeCalls,
			resolve: func(ctx context.Context, _ admingeo.Coordinate) (admingeo.AdminPath, error) {
				if call == 1 {
					close(started)
					<-ctx.Done()
					return admingeo.AdminPath{}, ctx.Err()
				}
				return admingeo.AdminPath{Country: "Spain"}, nil
			},
		}
		if call == 1 {
			resolver.closeErr = closeErr
		}
		return resolver, nil
	}
	warmer, err := NewWarmer(repository, store, acquire, nil, func(err error) { errorsReported <- err })
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, warmer.Close()) })

	warmer.Trigger()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("warmer did not begin resolving")
	}
	release := warmer.BeginForeground()
	require.Eventually(t, func() bool { return closeCalls.Load() == 1 }, 2*time.Second, 10*time.Millisecond)
	select {
	case err := <-errorsReported:
		require.ErrorIs(t, err, closeErr)
	case <-time.After(2 * time.Second):
		t.Fatal("resolver close error was not reported")
	}
	assert.Equal(t, int32(1), acquireCalls.Load(), "warming must remain idle while foreground work is active")
	complete, err := store.WarmComplete(context.Background(), "dataset-v1", revision)
	require.NoError(t, err)
	assert.False(t, complete)

	release()
	require.Eventually(t, func() bool {
		complete, err := store.WarmComplete(context.Background(), "dataset-v1", revision)
		return err == nil && complete
	}, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, int32(2), acquireCalls.Load())
	require.Eventually(t, func() bool { return closeCalls.Load() == 2 }, 2*time.Second, 10*time.Millisecond)
}

func TestWarmerCoalescesTriggersAndAllowsTriggersAfterClose(t *testing.T) {
	repository, _ := openWarmerRepository(t, warmerWaypoint("one", "41", "2", "2024-01-01T00:00:00Z"))
	store := openTestStore(t)
	started := make(chan struct{})
	unblock := make(chan struct{})
	var acquireCalls atomic.Int32
	acquire := func() (admingeo.Resolver, error) {
		if acquireCalls.Add(1) == 1 {
			close(started)
			<-unblock
		}
		return nil, nil
	}
	warmer, err := NewWarmer(repository, store, acquire, nil, nil)
	require.NoError(t, err)

	warmer.Trigger()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("warmer did not start")
	}
	for range 100 {
		warmer.Trigger()
	}
	close(unblock)
	require.Eventually(t, func() bool { return acquireCalls.Load() == 2 }, 2*time.Second, 10*time.Millisecond)
	assert.Never(t, func() bool { return acquireCalls.Load() > 2 }, 100*time.Millisecond, 10*time.Millisecond)
	require.NoError(t, warmer.Close())
	warmer.Trigger()
}

func TestWarmerDoesNotCompleteOrCacheFailedResolution(t *testing.T) {
	repository, revision := openWarmerRepository(t, warmerWaypoint("one", "41", "2", "2024-01-01T00:00:00Z"))
	store := openTestStore(t)
	resolveErr := errors.New("resolver failed")
	errorsReported := make(chan error, 1)
	warmer, err := NewWarmer(repository, store, func() (admingeo.Resolver, error) {
		return &warmerResolver{
			version: "dataset-v1",
			resolve: func(context.Context, admingeo.Coordinate) (admingeo.AdminPath, error) {
				return admingeo.AdminPath{}, resolveErr
			},
		}, nil
	}, nil, func(err error) { errorsReported <- err })
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, warmer.Close()) })

	warmer.Trigger()
	select {
	case err := <-errorsReported:
		require.ErrorIs(t, err, resolveErr)
	case <-time.After(2 * time.Second):
		t.Fatal("warmer error was not reported")
	}
	complete, err := store.WarmComplete(context.Background(), "dataset-v1", revision)
	require.NoError(t, err)
	assert.False(t, complete)
	_, found, err := store.Lookup(context.Background(), "dataset-v1", admingeo.Coordinate{Longitude: 2, Latitude: 41})
	require.NoError(t, err)
	assert.False(t, found)
}

func TestWarmerCloseCancelsAndJoinsActiveResolution(t *testing.T) {
	repository, _ := openWarmerRepository(t, warmerWaypoint("one", "41", "2", "2024-01-01T00:00:00Z"))
	store := openTestStore(t)
	started := make(chan struct{})
	var closeCalls atomic.Int32
	warmer, err := NewWarmer(repository, store, func() (admingeo.Resolver, error) {
		return &warmerResolver{
			version: "dataset-v1",
			closes:  &closeCalls,
			resolve: func(ctx context.Context, _ admingeo.Coordinate) (admingeo.AdminPath, error) {
				close(started)
				<-ctx.Done()
				return admingeo.AdminPath{}, ctx.Err()
			},
		}, nil
	}, nil, nil)
	require.NoError(t, err)
	warmer.Trigger()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("warmer did not begin resolving")
	}
	require.NoError(t, warmer.Close())
	assert.Equal(t, int32(1), closeCalls.Load())
	warmer.Trigger()
}

func TestWarmerReleasesRetiredGenerationAndRestartsQueuedPass(t *testing.T) {
	var waypoints strings.Builder
	for index := range warmerLeaseScans + 1 {
		waypoints.WriteString(warmerWaypoint(
			fmt.Sprintf("point-%d", index),
			"41",
			fmt.Sprintf("%.6f", 2+float64(index)/10000),
			fmt.Sprintf("2024-01-01T00:%02d:%02dZ", (index/60)%60, index%60),
		))
	}
	repository, revision := openWarmerRepository(t, waypoints.String())
	store := openTestStore(t)
	firstResolve := make(chan struct{})
	releaseFirst := make(chan struct{})
	var acquireCalls atomic.Int32
	var blockFirst atomic.Bool
	blockFirst.Store(true)
	acquire := func() (admingeo.Resolver, error) {
		call := acquireCalls.Add(1)
		version := admingeo.DatasetVersion("dataset-v2")
		if call == 1 {
			version = "dataset-v1"
		}
		return &warmerResolver{
			version: version,
			resolve: func(context.Context, admingeo.Coordinate) (admingeo.AdminPath, error) {
				if version == "dataset-v1" && blockFirst.CompareAndSwap(true, false) {
					close(firstResolve)
					<-releaseFirst
				}
				return admingeo.AdminPath{Country: string(version)}, nil
			},
		}, nil
	}
	warmer, err := NewWarmer(repository, store, acquire, nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, warmer.Close()) })

	warmer.Trigger()
	select {
	case <-firstResolve:
	case <-time.After(2 * time.Second):
		t.Fatal("first generation did not begin warming")
	}
	warmer.Trigger()
	close(releaseFirst)

	require.Eventually(t, func() bool {
		complete, err := store.WarmComplete(context.Background(), "dataset-v2", revision)
		return err == nil && complete
	}, 5*time.Second, 10*time.Millisecond)
	complete, err := store.WarmComplete(context.Background(), "dataset-v1", revision)
	require.NoError(t, err)
	assert.False(t, complete)
	assert.GreaterOrEqual(t, acquireCalls.Load(), int32(4))
}

func openWarmerRepository(t *testing.T, waypoints string) (*observations.Repository, string) {
	t.Helper()
	root := t.TempDir()
	content := `<?xml version="1.0" encoding="UTF-8"?>
<gpx version="1.1" xmlns="http://www.topografix.com/GPX/1/1">` + waypoints + `</gpx>`
	require.NoError(t, os.WriteFile(filepath.Join(root, "observations.gpx"), []byte(content), 0o600))
	repository, err := observations.Open(filepath.Join(t.TempDir(), "observations", "index.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, repository.Close()) })
	require.NoError(t, repository.Rebuild(root, ""))
	snapshot, err := repository.Snapshot()
	require.NoError(t, err)
	revision := snapshot.Revision()
	require.NoError(t, snapshot.Close())
	return repository, revision
}

func warmerWaypoint(name, latitude, longitude, timestamp string) string {
	return fmt.Sprintf(`<wpt lat="%s" lon="%s"><name>%s</name><time>%s</time></wpt>`, latitude, longitude, name, timestamp)
}
