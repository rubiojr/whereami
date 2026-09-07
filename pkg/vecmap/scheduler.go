package vecmap

import (
	"context"
	"errors"
	"sync"
	"time"
)

const (
	maximumConcurrentTileLoads = 4
	maximumTileLoadAttempts    = 3
	tileLoadRetryDelay         = 250 * time.Millisecond
)

type tileLoadResult struct {
	id     vectorTileID
	serial uint64
	roads  *tileBucket
	err    error
}

type runningTileLoad struct {
	serial uint64
	cancel context.CancelFunc
}

type tileLoadFailure struct {
	message string
	retryAt time.Time
}

type tileScheduler struct {
	ctx       context.Context
	cancel    context.CancelFunc
	loader    roadTileLoader
	publish   func(*roadTileSnapshot)
	requests  chan struct{}
	retries   chan struct{}
	results   chan tileLoadResult
	requestMu sync.Mutex
	latest    []vectorTileID
	wg        sync.WaitGroup
}

type tileSchedulerState struct {
	order         []vectorTileID
	resourceOrder []vectorTileID
	targets       map[vectorTileID]struct{}
	desired       map[vectorTileID]struct{}
	continuity    map[vectorTileID]struct{}
	rendered      []vectorTileID
	loaded        map[vectorTileID]*tileBucket
	failed        map[vectorTileID]tileLoadFailure
	attempts      map[vectorTileID]int
	inFlight      map[vectorTileID]runningTileLoad
	pending       []vectorTileID
	nextSerial    uint64
	revision      uint64
}

func newTileScheduler(loader roadTileLoader, publish func(*roadTileSnapshot)) *tileScheduler {
	ctx, cancel := context.WithCancel(context.Background())
	scheduler := &tileScheduler{
		ctx:      ctx,
		cancel:   cancel,
		loader:   loader,
		publish:  publish,
		requests: make(chan struct{}, 1),
		retries:  make(chan struct{}, 1),
		results:  make(chan tileLoadResult, maximumConcurrentTileLoads),
	}
	scheduler.wg.Add(1)
	go scheduler.run()
	return scheduler
}

func (s *tileScheduler) request(camera Camera) {
	s.requestTiles(visibleTileCover(camera))
}

func (s *tileScheduler) requestTiles(tiles []vectorTileID) {
	if s == nil || s.loader == nil {
		return
	}
	s.requestMu.Lock()
	s.latest = append(s.latest[:0], tiles...)
	s.requestMu.Unlock()
	select {
	case s.requests <- struct{}{}:
	default:
	}
}

func (s *tileScheduler) stop() {
	if s == nil {
		return
	}
	s.cancel()
	s.wg.Wait()
}

func (s *tileScheduler) run() {
	defer s.wg.Done()
	state := tileSchedulerState{
		targets:    make(map[vectorTileID]struct{}),
		desired:    make(map[vectorTileID]struct{}),
		continuity: make(map[vectorTileID]struct{}),
		loaded:     make(map[vectorTileID]*tileBucket),
		failed:     make(map[vectorTileID]tileLoadFailure),
		attempts:   make(map[vectorTileID]int),
		inFlight:   make(map[vectorTileID]runningTileLoad),
	}
	for {
		select {
		case <-s.ctx.Done():
			for _, load := range state.inFlight {
				load.cancel()
			}
			return
		case <-s.requests:
			s.requestMu.Lock()
			tiles := append([]vectorTileID(nil), s.latest...)
			s.requestMu.Unlock()
			if state.sameCover(tiles) {
				continue
			}
			state.setCover(tiles)
			state.startLoads(s)
			state.publish(s.publish)
		case <-s.retries:
			if state.queueRetries(time.Now()) {
				state.startLoads(s)
				state.publish(s.publish)
			}
		case result := <-s.results:
			load, exists := state.inFlight[result.id]
			if !exists || load.serial != result.serial {
				continue
			}
			load.cancel()
			delete(state.inFlight, result.id)
			if _, wanted := state.desired[result.id]; wanted {
				if result.err != nil {
					retry := !errors.Is(result.err, errPolygonResourceLimit) &&
						!errors.Is(result.err, errRoadResourceLimit) &&
						!errors.Is(result.err, errFeatureResourceLimit) &&
						!errors.Is(result.err, errRasterData)
					state.failTile(s, result.id, result.err.Error(), retry)
				} else if result.roads == nil {
					state.failTile(s, result.id, "tile loader returned no tile bucket", false)
				} else {
					if err := state.admitTile(result.id, result.roads); err != nil {
						state.failTile(s, result.id, err.Error(), false)
					} else {
						delete(state.failed, result.id)
						delete(state.attempts, result.id)
					}
				}
			}
			state.dropSatisfiedParents()
			state.startLoads(s)
			state.publish(s.publish)
		}
	}
}

func (s *tileSchedulerState) failTile(
	scheduler *tileScheduler,
	tile vectorTileID,
	message string,
	retry bool,
) {
	failure := tileLoadFailure{message: message}
	willRetry := retry && s.attempts[tile] < maximumTileLoadAttempts
	if willRetry {
		failure.retryAt = time.Now().Add(tileLoadRetryDelay)
		scheduler.scheduleRetry(tileLoadRetryDelay)
	}
	s.failed[tile] = failure
	reportVectorError(
		"vecmap tile z=%d x=%d y=%d failed on attempt %d/%d (retry=%t): %s",
		tile.Z,
		tile.X,
		tile.Y,
		s.attempts[tile],
		maximumTileLoadAttempts,
		willRetry,
		message,
	)
}

func (s *tileSchedulerState) queueRetries(now time.Time) bool {
	queued := false
	for _, tile := range s.resourceOrder {
		failure, exists := s.failed[tile]
		if !exists || failure.retryAt.IsZero() || now.Before(failure.retryAt) {
			continue
		}
		delete(s.failed, tile)
		s.pending = append(s.pending, tile)
		queued = true
	}
	return queued
}

func (s *tileScheduler) scheduleRetry(delay time.Duration) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
			select {
			case s.retries <- struct{}{}:
			default:
			}
		case <-s.ctx.Done():
		}
	}()
}

func (s *tileSchedulerState) startLoads(scheduler *tileScheduler) {
	for len(s.inFlight) < maximumConcurrentTileLoads && len(s.pending) > 0 {
		tile := s.pending[0]
		s.pending = s.pending[1:]
		if _, wanted := s.desired[tile]; !wanted {
			continue
		}
		if s.loaded[tile] != nil {
			continue
		}
		if _, loading := s.inFlight[tile]; loading {
			continue
		}
		s.nextSerial++
		s.attempts[tile]++
		serial := s.nextSerial
		ctx, cancel := context.WithCancel(scheduler.ctx)
		s.inFlight[tile] = runningTileLoad{serial: serial, cancel: cancel}
		scheduler.wg.Add(1)
		go func(ctx context.Context, tile vectorTileID, serial uint64, cancel context.CancelFunc) {
			defer scheduler.wg.Done()
			defer cancel()
			roads, err := scheduler.loader(ctx, tile)
			result := tileLoadResult{id: tile, serial: serial, roads: roads, err: err}
			select {
			case scheduler.results <- result:
			case <-scheduler.ctx.Done():
			}
		}(ctx, tile, serial, cancel)
	}
}

func (s *tileSchedulerState) retryingCount() int {
	count := 0
	for _, failure := range s.failed {
		if !failure.retryAt.IsZero() {
			count++
		}
	}
	return count
}
