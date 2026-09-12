package vecmap

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTileSchedulerBoundsConcurrentLoads(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	release := make(chan struct{})
	snapshots := make(chan *roadTileSnapshot, 32)
	loader := func(ctx context.Context, tile vectorTileID) (*tileBucket, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		select {
		case <-release:
			return testRoadBucket(tile), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	scheduler := newTileScheduler(loader, func(snapshot *roadTileSnapshot) { snapshots <- snapshot })
	t.Cleanup(scheduler.stop)
	tiles := make([]vectorTileID, 10)
	for index := range tiles {
		tiles[index] = vectorTileID{X: uint32(index), Y: 0, Z: 4}
	}

	scheduler.requestTiles(tiles)
	require.Eventually(t, func() bool { return active.Load() == maximumConcurrentTileLoads }, time.Second, time.Millisecond)
	assert.Equal(t, int32(maximumConcurrentTileLoads), maximum.Load())
	close(release)
	require.Eventually(t, func() bool {
		return latestSnapshot(snapshots).loadedCount() == len(tiles)
	}, time.Second, time.Millisecond)
}

func TestTileSchedulerCancelsStaleLoads(t *testing.T) {
	oldTile := vectorTileID{X: 1, Y: 1, Z: 2}
	newTile := vectorTileID{X: 2, Y: 1, Z: 2}
	oldStarted := make(chan struct{})
	oldCanceled := make(chan struct{})
	snapshots := make(chan *roadTileSnapshot, 16)
	loader := func(ctx context.Context, tile vectorTileID) (*tileBucket, error) {
		if tile == oldTile {
			close(oldStarted)
			<-ctx.Done()
			close(oldCanceled)
			return nil, ctx.Err()
		}
		return testRoadBucket(tile), nil
	}
	scheduler := newTileScheduler(loader, func(snapshot *roadTileSnapshot) { snapshots <- snapshot })
	t.Cleanup(scheduler.stop)

	scheduler.requestTiles([]vectorTileID{oldTile})
	requireReceive(t, oldStarted)
	scheduler.requestTiles([]vectorTileID{newTile})
	requireReceive(t, oldCanceled)
	require.Eventually(t, func() bool {
		snapshot := latestSnapshot(snapshots)
		return snapshot.loadedCount() == 1 && snapshot.tiles[0].id == newTile
	}, time.Second, time.Millisecond)
}

func TestTileSchedulerBoundsRetries(t *testing.T) {
	var calls atomic.Int32
	var latest atomic.Pointer[roadTileSnapshot]
	tile := vectorTileID{X: 1, Y: 1, Z: 2}
	loader := func(_ context.Context, requested vectorTileID) (*tileBucket, error) {
		if requested != tile {
			return testRoadBucket(requested), nil
		}
		calls.Add(1)
		return nil, errors.New("unavailable")
	}
	scheduler := newTileScheduler(loader, latest.Store)
	t.Cleanup(scheduler.stop)

	scheduler.requestTiles([]vectorTileID{tile})
	require.Eventually(t, func() bool {
		snapshot := latest.Load()
		if snapshot == nil {
			return false
		}
		return snapshot.errors == 1 && snapshot.loading == 1
	}, time.Second, time.Millisecond)
	require.Eventually(t, func() bool {
		return calls.Load() == maximumTileLoadAttempts
	}, 2*time.Second, time.Millisecond)
	time.Sleep(2 * tileLoadRetryDelay)
	assert.Equal(t, int32(maximumTileLoadAttempts), calls.Load())
}

func TestTileSchedulerDoesNotRetryResourceLimit(t *testing.T) {
	var calls atomic.Int32
	var latest atomic.Pointer[roadTileSnapshot]
	tile := vectorTileID{X: 1, Y: 1, Z: 2}
	loader := func(_ context.Context, requested vectorTileID) (*tileBucket, error) {
		if requested != tile {
			return testRoadBucket(requested), nil
		}
		calls.Add(1)
		return nil, errPolygonResourceLimit
	}
	scheduler := newTileScheduler(loader, latest.Store)
	t.Cleanup(scheduler.stop)

	scheduler.requestTiles([]vectorTileID{tile})
	require.Eventually(t, func() bool {
		snapshot := latest.Load()
		return snapshot != nil && snapshot.errors == 1 && snapshot.loading == 0
	}, time.Second, time.Millisecond)
	time.Sleep(2 * tileLoadRetryDelay)
	assert.Equal(t, int32(1), calls.Load())
}

func TestTileSchedulerAcceptsTileWithResourceLimitedFeature(t *testing.T) {
	tile := vectorTileID{}
	oversized := make([][2]int32, maxFillRingPoints+1)
	for index := range oversized {
		oversized[index] = [2]int32{int32(index % 1024), int32(index / 1024)}
	}
	valid := [][2]int32{{0, 0}, {100, 0}, {100, 100}, {0, 100}}
	data := mvtTile(mvtLayerMessage(
		"landcover",
		mvtTypedFeatureMessage(0, mvtPolygonType, mvtPolygonGeometry(oversized)),
		mvtTypedFeatureMessage(0, mvtPolygonType, mvtPolygonGeometry(valid)),
	))
	messages := make(chan string, 2)
	previousReporter := reportVectorWarning
	reportVectorWarning = func(format string, args ...any) {
		messages <- fmt.Sprintf(format, args...)
	}
	t.Cleanup(func() { reportVectorWarning = previousReporter })
	var latest atomic.Pointer[roadTileSnapshot]
	scheduler := newTileScheduler(func(_ context.Context, requested vectorTileID) (*tileBucket, error) {
		assert.Equal(t, tile, requested)
		return decodeRoadBucket(data, requested)
	}, latest.Store)
	t.Cleanup(scheduler.stop)

	scheduler.requestTiles([]vectorTileID{tile})

	require.Eventually(t, func() bool {
		snapshot := latest.Load()
		return snapshot != nil && snapshot.loading == 0 && snapshot.loadedCount() == 1
	}, time.Second, time.Millisecond)
	assert.Zero(t, latest.Load().errors)
	assert.Len(t, messages, 2)
}

func TestTileSchedulerRetriesTransientFailure(t *testing.T) {
	var calls atomic.Int32
	var latest atomic.Pointer[roadTileSnapshot]
	tile := vectorTileID{X: 1, Y: 1, Z: 2}
	loader := func(_ context.Context, requested vectorTileID) (*tileBucket, error) {
		if requested != tile {
			return testRoadBucket(requested), nil
		}
		if calls.Add(1) == 1 {
			return nil, errors.New("temporary")
		}
		return testRoadBucket(tile), nil
	}
	scheduler := newTileScheduler(loader, latest.Store)
	t.Cleanup(scheduler.stop)

	scheduler.requestTiles([]vectorTileID{tile})
	require.Eventually(t, func() bool {
		snapshot := latest.Load()
		if snapshot == nil {
			return false
		}
		return snapshot.loadedCount() == 1 && snapshot.tiles[0].id == tile && snapshot.fallbacks == 0
	}, 2*time.Second, time.Millisecond)
	assert.Equal(t, int32(2), calls.Load())
	time.Sleep(2 * tileLoadRetryDelay)
	assert.Equal(t, int32(2), calls.Load(), "successful tile was retried again")
	snapshot := latest.Load()
	assert.Zero(t, snapshot.errors)
	assert.Zero(t, snapshot.loading)
}

func TestTileSchedulerReplacesParentFallbackAtomically(t *testing.T) {
	parent := vectorTileID{X: 1, Y: 1, Z: 2}
	children := []vectorTileID{
		{X: 2, Y: 2, Z: 3},
		{X: 3, Y: 2, Z: 3},
		{X: 2, Y: 3, Z: 3},
		{X: 3, Y: 3, Z: 3},
	}
	gates := make(map[vectorTileID]chan struct{}, len(children))
	for _, child := range children {
		gates[child] = make(chan struct{})
	}
	var latest atomic.Pointer[roadTileSnapshot]
	var partialChildren atomic.Bool
	loader := func(ctx context.Context, tile vectorTileID) (*tileBucket, error) {
		if gate := gates[tile]; gate != nil {
			select {
			case <-gate:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return testRoadBucket(tile), nil
	}
	scheduler := newTileScheduler(loader, func(snapshot *roadTileSnapshot) {
		if snapshot.requested == len(children) && snapshot.fallbacks == 0 &&
			len(snapshot.tiles) > 0 && len(snapshot.tiles) < len(children) {
			partialChildren.Store(true)
		}
		latest.Store(snapshot)
	})
	t.Cleanup(scheduler.stop)

	scheduler.requestTiles([]vectorTileID{parent})
	require.Eventually(t, func() bool {
		snapshot := latest.Load()
		return snapshot != nil && snapshot.loading == 0 && snapshot.loadedCount() == 1 &&
			snapshot.tiles[0].id == parent && snapshot.fallbacks == 0
	}, time.Second, time.Millisecond)

	scheduler.requestTiles(children)
	require.Eventually(t, func() bool {
		snapshot := latest.Load()
		return snapshot != nil && snapshot.loading == len(children) && snapshot.loadedCount() == 1 &&
			snapshot.tiles[0].id == parent && snapshot.fallbacks == 1
	}, time.Second, time.Millisecond)

	firstRevision := latest.Load().revision
	close(gates[children[0]])
	require.Eventually(t, func() bool {
		snapshot := latest.Load()
		return snapshot.revision > firstRevision && snapshot.loadedCount() == 1 &&
			snapshot.tiles[0].id == parent && snapshot.fallbacks == 1
	}, time.Second, time.Millisecond)

	for _, child := range children[1:] {
		close(gates[child])
	}
	require.Eventually(t, func() bool {
		snapshot := latest.Load()
		return snapshot.loading == 0 && snapshot.loadedCount() == len(children) && snapshot.fallbacks == 0
	}, time.Second, time.Millisecond)
	assert.False(t, partialChildren.Load(), "children were exposed before their parent group completed")
	for _, child := range children {
		assert.Contains(t, snapshotTileIDs(latest.Load()), child)
	}
}

func TestTileSchedulerKeepsChildrenDuringZoomOut(t *testing.T) {
	target := vectorTileID{X: 1, Y: 1, Z: 2}
	children := []vectorTileID{
		{X: 2, Y: 2, Z: 3},
		{X: 3, Y: 2, Z: 3},
		{X: 2, Y: 3, Z: 3},
		{X: 3, Y: 3, Z: 3},
	}
	releaseTarget := make(chan struct{})
	targetStarted := make(chan struct{})
	var targetLoads atomic.Int32
	var latest atomic.Pointer[roadTileSnapshot]
	loader := func(ctx context.Context, tile vectorTileID) (*tileBucket, error) {
		if tile == target && targetLoads.Add(1) > 1 {
			close(targetStarted)
			select {
			case <-releaseTarget:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return testRoadBucket(tile), nil
	}
	scheduler := newTileScheduler(loader, latest.Store)
	t.Cleanup(scheduler.stop)

	scheduler.requestTiles(children)
	require.Eventually(t, func() bool {
		snapshot := latest.Load()
		return snapshot != nil && snapshot.loading == 0 && snapshot.loadedCount() == len(children) &&
			snapshot.fallbacks == 0
	}, time.Second, time.Millisecond)

	scheduler.requestTiles([]vectorTileID{target})
	requireReceive(t, targetStarted)
	require.Eventually(t, func() bool {
		snapshot := latest.Load()
		return snapshot.requested == 1 && snapshot.loading == 1 &&
			snapshot.loadedCount() == len(children) && snapshot.fallbacks == len(children)
	}, time.Second, time.Millisecond)
	for _, child := range children {
		assert.Contains(t, snapshotTileIDs(latest.Load()), child)
	}

	close(releaseTarget)
	require.Eventually(t, func() bool {
		snapshot := latest.Load()
		return snapshot.loading == 0 && snapshot.loadedCount() == 1 &&
			snapshot.tiles[0].id == target && snapshot.fallbacks == 0
	}, time.Second, time.Millisecond)
}

func TestTileSchedulerKeepsDeepDescendantsDuringZoomOut(t *testing.T) {
	target := vectorTileID{X: 1, Y: 1, Z: 2}
	descendants := tileDescendantsAtZoom(target, 4)
	releaseTarget := make(chan struct{})
	targetStarted := make(chan struct{})
	var targetLoads atomic.Int32
	var latest atomic.Pointer[roadTileSnapshot]
	loader := func(ctx context.Context, tile vectorTileID) (*tileBucket, error) {
		if tile == target {
			targetLoads.Add(1)
			close(targetStarted)
			select {
			case <-releaseTarget:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return testRoadBucket(tile), nil
	}
	scheduler := newTileScheduler(loader, latest.Store)
	t.Cleanup(scheduler.stop)

	scheduler.requestTiles(descendants)
	require.Eventually(t, func() bool {
		snapshot := latest.Load()
		return snapshot != nil && snapshot.loading == 0 && snapshot.loadedCount() == len(descendants)
	}, time.Second, time.Millisecond)

	scheduler.requestTiles([]vectorTileID{target})
	requireReceive(t, targetStarted)
	require.Eventually(t, func() bool {
		snapshot := latest.Load()
		return snapshot.requested == 1 && snapshot.loading == 1 &&
			snapshot.loadedCount() == len(descendants) && snapshot.fallbacks == len(descendants)
	}, time.Second, time.Millisecond)

	close(releaseTarget)
	require.Eventually(t, func() bool {
		snapshot := latest.Load()
		return snapshot.loading == 0 && snapshot.loadedCount() == 1 &&
			snapshot.tiles[0].id == target && snapshot.fallbacks == 0
	}, time.Second, time.Millisecond)
}

func TestTileSchedulerKeepsParentWhenChildFails(t *testing.T) {
	target := vectorTileID{X: 3, Y: 2, Z: 3}
	parent, exists := target.Parent()
	require.True(t, exists)
	var targetCalls atomic.Int32
	var latest atomic.Pointer[roadTileSnapshot]
	loader := func(_ context.Context, tile vectorTileID) (*tileBucket, error) {
		if tile == target {
			targetCalls.Add(1)
			return nil, errors.New("child unavailable")
		}
		return testRoadBucket(tile), nil
	}
	scheduler := newTileScheduler(loader, latest.Store)
	t.Cleanup(scheduler.stop)

	scheduler.requestTiles([]vectorTileID{target})
	require.Eventually(t, func() bool {
		snapshot := latest.Load()
		return snapshot != nil && targetCalls.Load() == maximumTileLoadAttempts &&
			snapshot.loading == 0 && snapshot.errors == 1
	}, 2*time.Second, time.Millisecond)
	snapshot := latest.Load()
	require.Equal(t, 1, snapshot.loadedCount())
	assert.Equal(t, parent, snapshot.tiles[0].id)
	assert.Equal(t, 1, snapshot.fallbacks)
}

func TestRenderSelectionDoesNotCountCurrentTargetAsFallback(t *testing.T) {
	loaded := vectorTileID{X: 2, Y: 2, Z: 3}
	missing := vectorTileID{X: 3, Y: 2, Z: 3}
	state := tileSchedulerState{
		order:      []vectorTileID{loaded, missing},
		continuity: map[vectorTileID]struct{}{loaded: {}},
		rendered:   []vectorTileID{loaded},
		loaded:     map[vectorTileID]*tileBucket{loaded: testRoadBucket(loaded)},
	}

	tiles, fallbacks := state.renderSelection()

	require.Len(t, tiles, 1)
	assert.Equal(t, loaded, tiles[0].id)
	assert.Zero(t, fallbacks)
}

func TestRenderSelectionUsesCoarseContinuityAncestor(t *testing.T) {
	missing := vectorTileID{X: 0, Y: 0, Z: 3}
	loaded := vectorTileID{X: 4, Y: 0, Z: 3}
	coarse := vectorTileID{X: 0, Y: 0, Z: 1}
	state := tileSchedulerState{
		order:      []vectorTileID{missing, loaded},
		continuity: map[vectorTileID]struct{}{coarse: {}},
		rendered:   []vectorTileID{coarse},
		loaded: map[vectorTileID]*tileBucket{
			loaded: testRoadBucket(loaded),
			coarse: testRoadBucket(coarse),
		},
	}

	tiles, fallbacks := state.renderSelection()

	require.Len(t, tiles, 2)
	assert.Contains(t, snapshotTileIDs(&roadTileSnapshot{tiles: tiles}), coarse)
	assert.Contains(t, snapshotTileIDs(&roadTileSnapshot{tiles: tiles}), loaded)
	assert.Equal(t, 1, fallbacks)
}

func TestRenderSelectionUsesSharedParentForMixedContinuity(t *testing.T) {
	first := vectorTileID{X: 2, Y: 2, Z: 3}
	second := vectorTileID{X: 3, Y: 2, Z: 3}
	parent, exists := first.Parent()
	require.True(t, exists)
	firstChildren := []vectorTileID{
		{X: 4, Y: 4, Z: 4},
		{X: 5, Y: 4, Z: 4},
		{X: 4, Y: 5, Z: 4},
		{X: 5, Y: 5, Z: 4},
	}
	state := tileSchedulerState{
		order:      []vectorTileID{first, second},
		continuity: map[vectorTileID]struct{}{parent: {}},
		rendered:   append(append([]vectorTileID(nil), firstChildren...), parent),
		loaded:     map[vectorTileID]*tileBucket{parent: testRoadBucket(parent)},
	}
	for _, child := range firstChildren {
		state.continuity[child] = struct{}{}
		state.loaded[child] = testRoadBucket(child)
	}

	tiles, fallbacks := state.renderSelection()

	require.Len(t, tiles, 1)
	assert.Equal(t, parent, tiles[0].id)
	assert.Equal(t, 1, fallbacks)
}

func TestRenderSelectionUsesMixedDepthDescendantCover(t *testing.T) {
	target := vectorTileID{X: 1, Y: 1, Z: 2}
	directChildren := tileDescendantsAtZoom(target, 3)
	retained := []vectorTileID{directChildren[0]}
	for _, child := range directChildren[1:] {
		retained = append(retained, tileDescendantsAtZoom(child, 4)...)
	}
	state := tileSchedulerState{
		order:      []vectorTileID{target},
		continuity: make(map[vectorTileID]struct{}, len(retained)),
		rendered:   retained,
		loaded:     make(map[vectorTileID]*tileBucket, len(retained)),
	}
	for _, tile := range retained {
		state.continuity[tile] = struct{}{}
		state.loaded[tile] = testRoadBucket(tile)
	}

	tiles, fallbacks := state.renderSelection()

	assert.Len(t, tiles, len(retained))
	assert.Equal(t, len(retained), fallbacks)
}

func TestTileSchedulerCoalescesConcurrentRequests(t *testing.T) {
	snapshots := make(chan *roadTileSnapshot, 64)
	scheduler := newTileScheduler(
		func(_ context.Context, tile vectorTileID) (*tileBucket, error) {
			return testRoadBucket(tile), nil
		},
		func(snapshot *roadTileSnapshot) { snapshots <- snapshot },
	)
	t.Cleanup(scheduler.stop)

	var requests sync.WaitGroup
	for index := range 16 {
		requests.Add(1)
		go func() {
			defer requests.Done()
			scheduler.requestTiles([]vectorTileID{{X: uint32(index), Y: 0, Z: 5}})
		}()
	}
	requests.Wait()
	finalTile := vectorTileID{X: 17, Y: 0, Z: 5}
	scheduler.requestTiles([]vectorTileID{finalTile})

	require.Eventually(t, func() bool {
		snapshot := latestSnapshot(snapshots)
		return snapshot.loadedCount() == 1 && snapshot.tiles[0].id == finalTile
	}, time.Second, time.Millisecond)
}

func TestTileSchedulerStopCancelsInFlightLoad(t *testing.T) {
	started := make(chan struct{}, 2)
	canceled := make(chan struct{}, 2)
	scheduler := newTileScheduler(
		func(ctx context.Context, _ vectorTileID) (*tileBucket, error) {
			started <- struct{}{}
			<-ctx.Done()
			canceled <- struct{}{}
			return nil, ctx.Err()
		},
		nil,
	)
	scheduler.requestTiles([]vectorTileID{{X: 1, Y: 1, Z: 2}})
	requireReceive(t, started)
	requireReceive(t, started)

	scheduler.stop()
	requireReceive(t, canceled)
	requireReceive(t, canceled)
	scheduler.stop()
}

func TestTileSchedulerCancelsCompletedLoadContext(t *testing.T) {
	contexts := make(chan context.Context, 1)
	var latest atomic.Pointer[roadTileSnapshot]
	scheduler := newTileScheduler(
		func(ctx context.Context, tile vectorTileID) (*tileBucket, error) {
			contexts <- ctx
			return testRoadBucket(tile), nil
		},
		latest.Store,
	)
	t.Cleanup(scheduler.stop)
	scheduler.requestTiles([]vectorTileID{{X: 0, Y: 0, Z: 0}})
	require.Eventually(t, func() bool {
		snapshot := latest.Load()
		return snapshot != nil && snapshot.loadedCount() == 1 && snapshot.loading == 0
	}, time.Second, time.Millisecond)

	loadedContext := <-contexts
	select {
	case <-loadedContext.Done():
	case <-time.After(time.Second):
		require.FailNow(t, "completed tile context was not canceled")
	}
}

func TestSelectedCoverResourcesExcludeAtomicReplacements(t *testing.T) {
	parent := vectorTileID{X: 1, Y: 1, Z: 2}
	children := []vectorTileID{
		{X: 2, Y: 2, Z: 3},
		{X: 3, Y: 2, Z: 3},
		{X: 2, Y: 3, Z: 3},
		{X: 3, Y: 3, Z: 3},
	}
	parentBucket := testRoadBucket(parent)
	parentBucket.symbols = make([]libertySymbolCandidate, 5)
	state := tileSchedulerState{
		order:  children,
		loaded: map[vectorTileID]*tileBucket{parent: parentBucket},
	}
	for _, child := range children[:3] {
		bucket := testRoadBucket(child)
		bucket.symbols = make([]libertySymbolCandidate, 2)
		state.loaded[child] = bucket
	}

	resources := state.selectedCoverResources()
	assert.Equal(t, 1, resources.roadSegments)
	assert.Equal(t, 5, resources.symbols)

	lastBucket := testRoadBucket(children[3])
	lastBucket.symbols = make([]libertySymbolCandidate, 2)
	state.loaded[children[3]] = lastBucket

	resources = state.selectedCoverResources()
	assert.Equal(t, len(children), resources.roadSegments)
	assert.Equal(t, 8, resources.symbols)
}

func TestAdmitTileAllowsAtomicReplacementWithinRenderLimit(t *testing.T) {
	parent := vectorTileID{X: 1, Y: 1, Z: 2}
	children := tileDescendantsAtZoom(parent, 3)
	parentBucket := testRoadBucket(parent)
	parentBucket.symbols = make([]libertySymbolCandidate, 5)
	state := tileSchedulerState{
		order:  children,
		loaded: map[vectorTileID]*tileBucket{parent: parentBucket},
	}
	limits := tileCoverResources{roadSegments: 10, fillTriangles: 10, symbols: 5}

	for _, child := range children {
		bucket := testRoadBucket(child)
		bucket.symbols = make([]libertySymbolCandidate, 1)
		require.NoError(t, state.admitTileWithLimits(child, bucket, limits))
	}

	resources := state.selectedCoverResources()
	assert.Equal(t, len(children), resources.roadSegments)
	assert.Equal(t, len(children), resources.symbols)
	assert.NotNil(t, state.loaded[parent], "parent remains loaded until the scheduler drops the completed fallback")
}

func TestAdmitTileRejectsReplacementOverRenderLimit(t *testing.T) {
	parent := vectorTileID{X: 1, Y: 1, Z: 2}
	children := tileDescendantsAtZoom(parent, 3)
	parentBucket := testRoadBucket(parent)
	parentBucket.symbols = make([]libertySymbolCandidate, 1)
	state := tileSchedulerState{
		order:  children,
		loaded: map[vectorTileID]*tileBucket{parent: parentBucket},
	}
	limits := tileCoverResources{roadSegments: 10, fillTriangles: 10, symbols: 5}
	for _, child := range children[:3] {
		bucket := testRoadBucket(child)
		bucket.symbols = make([]libertySymbolCandidate, 2)
		require.NoError(t, state.admitTileWithLimits(child, bucket, limits))
	}
	lastBucket := testRoadBucket(children[3])
	lastBucket.symbols = make([]libertySymbolCandidate, 2)

	err := state.admitTileWithLimits(children[3], lastBucket, limits)

	assert.ErrorContains(t, err, "symbol cover exceeds 5-candidate limit")
	assert.ErrorContains(t, err, "selected=8")
	assert.Nil(t, state.loaded[children[3]])
	assert.Equal(t, 1, state.selectedCoverResources().symbols)
}

func TestSetCoverPrioritizesFallbackParents(t *testing.T) {
	targets := []vectorTileID{
		{X: 2, Y: 2, Z: 3},
		{X: 4, Y: 2, Z: 3},
	}
	state := tileSchedulerState{
		loaded:   make(map[vectorTileID]*tileBucket),
		failed:   make(map[vectorTileID]tileLoadFailure),
		attempts: make(map[vectorTileID]int),
		inFlight: make(map[vectorTileID]runningTileLoad),
	}

	state.setCover(targets)

	require.Len(t, state.resourceOrder, 4)
	for index, target := range targets {
		parent, exists := target.Parent()
		require.True(t, exists)
		assert.Equal(t, parent, state.resourceOrder[index])
		assert.Equal(t, target, state.resourceOrder[index+len(targets)])
	}
}

func TestSchedulerPublishPreservesRevisionForStatusOnlyChanges(t *testing.T) {
	tile := vectorTileID{X: 3, Y: 2, Z: 4}
	bucket := testRoadBucket(tile)
	state := tileSchedulerState{
		order:    []vectorTileID{tile},
		loaded:   map[vectorTileID]*tileBucket{tile: bucket},
		failed:   make(map[vectorTileID]tileLoadFailure),
		inFlight: make(map[vectorTileID]runningTileLoad),
	}
	var snapshots []*roadTileSnapshot
	state.publish(func(snapshot *roadTileSnapshot) { snapshots = append(snapshots, snapshot) })
	state.inFlight[vectorTileID{X: 4, Y: 2, Z: 4}] = runningTileLoad{}
	state.publish(func(snapshot *roadTileSnapshot) { snapshots = append(snapshots, snapshot) })

	require.Len(t, snapshots, 2)
	assert.Equal(t, snapshots[0].contentRevision, snapshots[1].contentRevision)
	assert.Greater(t, snapshots[1].revision, snapshots[0].revision)
	assert.NotEqual(t, snapshots[0].loading, snapshots[1].loading)

	state.loaded[tile] = testRoadBucket(tile)
	state.publish(func(snapshot *roadTileSnapshot) { snapshots = append(snapshots, snapshot) })
	assert.Greater(t, snapshots[2].contentRevision, snapshots[1].contentRevision)
}

func TestTileSchedulerReportsFailuresWithTileContext(t *testing.T) {
	tile := vectorTileID{X: 3, Y: 2, Z: 4}
	messages := make(chan string, maximumTileLoadAttempts)
	previousReporter := reportVectorError
	reportVectorError = func(format string, args ...any) {
		messages <- fmt.Sprintf(format, args...)
	}
	t.Cleanup(func() { reportVectorError = previousReporter })

	var latest atomic.Pointer[roadTileSnapshot]
	scheduler := newTileScheduler(func(_ context.Context, requested vectorTileID) (*tileBucket, error) {
		if requested == tile {
			return nil, errors.New("copyable failure")
		}
		return testRoadBucket(requested), nil
	}, latest.Store)
	t.Cleanup(scheduler.stop)
	scheduler.requestTiles([]vectorTileID{tile})

	var message string
	require.Eventually(t, func() bool {
		select {
		case message = <-messages:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
	assert.Contains(t, message, "z=4 x=3 y=2")
	assert.Contains(t, message, "attempt 1/3")
	assert.Contains(t, message, "copyable failure")
}

func testRoadBucket(tile vectorTileID) *tileBucket {
	return &tileBucket{
		tile:         tile,
		featureCount: 1,
		segments: []roadSegment{{
			Start: roadPoint{X: 0, Y: 0},
			End:   roadPoint{X: tileSize, Y: tileSize},
		}},
	}
}

func tileDescendantsAtZoom(tile vectorTileID, zoom uint32) []vectorTileID {
	if zoom < tile.Z {
		return nil
	}
	scale := uint32(1) << (zoom - tile.Z)
	tiles := make([]vectorTileID, 0, scale*scale)
	for y := tile.Y * scale; y < (tile.Y+1)*scale; y++ {
		for x := tile.X * scale; x < (tile.X+1)*scale; x++ {
			tiles = append(tiles, vectorTileID{X: x, Y: y, Z: zoom})
		}
	}
	return tiles
}

func latestSnapshot(snapshots <-chan *roadTileSnapshot) *roadTileSnapshot {
	var latest *roadTileSnapshot
	for {
		select {
		case latest = <-snapshots:
		default:
			if latest == nil {
				return &roadTileSnapshot{}
			}
			return latest
		}
	}
}

func snapshotTileIDs(snapshot *roadTileSnapshot) []vectorTileID {
	ids := make([]vectorTileID, 0, len(snapshot.tiles))
	for _, tile := range snapshot.tiles {
		ids = append(ids, tile.id)
	}
	return ids
}

func (s *roadTileSnapshot) loadedCount() int {
	if s == nil {
		return 0
	}
	return len(s.tiles)
}

func requireReceive(t *testing.T, channel <-chan struct{}) {
	t.Helper()
	select {
	case <-channel:
	case <-time.After(time.Second):
		require.FailNow(t, "channel receive timed out")
	}
}
