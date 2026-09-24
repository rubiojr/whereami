package producer

import (
	"context"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rubiojr/whereami/pkg/vecmap/tiles"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
)

type entry struct {
	source        string
	raw           []byte
	prepared      *tiles.Prepared
	fragment      *tiles.Fragment
	style, assets uint64 // owner epochs, never native generations
	usage         CacheUsage
	styleSnapshot *Style
	assetSnapshot *Assets
}

type failure struct {
	attempts int
	retryAt  time.Time
}

type state struct {
	p                                   *Producer
	set                                 *tiles.Set
	request                             *Request
	revision, generation, style, assets uint64
	current                             current
	targets, order                      []view.TileID
	desired, pinned                     map[view.TileID]bool
	entries                             map[view.TileID]*entry
	running                             map[view.TileID]job
	failures                            map[view.TileID]failure
	stats                               Status
	dirty                               bool
}

func (p *Producer) run(set *tiles.Set) {
	var wg sync.WaitGroup
	for range p.limits.Workers {
		wg.Add(1)
		go p.load(&wg)
	}
	defer func() {
		p.cancel()
		wg.Wait()
		p.loader = nil // release transport captures after the last worker exits
		// Results can retain raw buffers after canceled workers have exited.
		for len(p.results) > 0 {
			<-p.results
		}
		p.mu.Lock()
		if p.output != nil {
			p.output.released = true
			delete(p.leases, p.output)
			p.output = nil
		}
		p.latest = nil
		p.status.Jobs, p.status.Cached = 0, 0
		p.status.Pending, p.status.Failed = 0, 0
		p.status.CacheBytes, p.status.ReservedRawBytes = 0, 0
		p.status.Cache = CacheUsage{}
		p.status.Leases, p.status.LeaseBytes = len(p.leases), 0
		for l := range p.leases {
			p.status.LeaseBytes += l.bytes
		}
		p.mu.Unlock()
		close(p.done)
	}()
	timer := time.NewTicker(p.limits.RetryDelay)
	defer timer.Stop()
	s := state{p: p, set: set, entries: make(map[view.TileID]*entry), running: make(map[view.TileID]job), failures: make(map[view.TileID]failure)}
	for {
		if p.ctx.Err() != nil {
			return
		}
		s.inputs()
		s.refine()
		s.evict()
		s.publish()
		s.report()
		// Drain completed loads before compiling another tile. No worker can be
		// replaced while its result remains queued.
		select {
		case r := <-p.results:
			s.loaded(r)
			continue
		default:
		}
		if s.compile() {
			continue
		}
		next, available := s.nextJob(time.Now())
		var jobs chan job
		if available {
			jobs = p.jobs
		}
		select {
		case <-p.ctx.Done():
			if available {
				next.cancel()
			}
			return
		case <-p.wake:
			if available {
				next.cancel()
			}
		case <-timer.C:
			if available {
				next.cancel()
			}
		case r := <-p.results:
			if available {
				next.cancel()
			}
			s.loaded(r)
		case jobs <- next:
			s.running[next.key.Tile] = next
			f := s.failures[next.key.Tile]
			f.attempts++
			f.retryAt = time.Time{}
			s.failures[next.key.Tile] = f
			s.stats.Loads++
		}
	}
}

func (s *state) inputs() {
	p := s.p
	p.mu.Lock()
	r, revision := p.latest, p.revision
	if r != nil {
		p.latest = nil
	}
	ack := p.current
	pinned := make(map[view.TileID]bool)
	for l := range p.leases {
		for _, tile := range l.Snapshot.Cover {
			pinned[tile] = true
		}
	}
	p.mu.Unlock()
	if ack.generation != s.current.generation || ack.sequence != s.current.sequence {
		s.current = ack
		s.dirty = true
	}
	for _, tile := range s.current.cover {
		pinned[tile] = true
	}
	s.pinned = pinned
	if r == nil {
		return
	}
	targets := r.Targets
	if targets == nil {
		targets = view.VisibleTileCover(r.Camera)
	}
	styleChanged := s.request == nil || s.request.Style != r.Style
	assetsChanged := s.request == nil || s.request.Assets != r.Assets
	changed := styleChanged || assetsChanged || !slices.Equal(targets, s.targets)
	if changed {
		if s.generation == math.MaxUint64 {
			s.error(ErrLimit)
			p.Close()
			return
		}
		s.generation++
		if styleChanged {
			s.style++
		}
		if assetsChanged {
			s.assets++
		}
		clear(s.failures)
	}
	s.request, s.revision, s.targets = r, revision, targets
	s.order = view.LoadOrder(targets)
	s.desired = make(map[view.TileID]bool, len(s.order))
	for _, tile := range s.order {
		s.desired[tile] = true
	}
	for tile, j := range s.running {
		if styleChanged || !s.desired[tile] {
			j.cancel()
		}
	}
	s.dirty = true
}

func (s *state) evict() {
	for tile, e := range s.entries {
		if s.desired[tile] || s.pinned[tile] {
			continue
		}
		if e.fragment != nil {
			if err := s.set.Apply([]tiles.Change{{Tile: tile}}); err != nil {
				s.error(err)
				continue
			}
		}
		delete(s.entries, tile)
		s.recordCache(tile, nil)
		s.dirty = true
	}
}

// Stop requesting a fallback once all requested siblings are CPU-ready. Keep
// its fragment while Current or any leased target can still require it.
func (s *state) refine() {
	for _, group := range view.GroupTiles(s.targets) {
		if !group.HasParent {
			continue
		}
		ready := true
		for _, tile := range group.Targets {
			e := s.entries[tile]
			if e == nil || e.fragment == nil || e.style != s.style || e.assets != s.assets {
				ready = false
				break
			}
		}
		if ready {
			delete(s.desired, group.Parent)
			if j, ok := s.running[group.Parent]; ok {
				j.cancel()
			}
		}
	}
}

func (s *state) nextJob(now time.Time) (job, bool) {
	if s.request == nil || len(s.running) >= s.p.limits.Workers {
		return job{}, false
	}
	for _, tile := range s.order {
		if !s.desired[tile] {
			continue
		}
		if _, running := s.running[tile]; running {
			continue
		}
		e := s.entries[tile]
		if e != nil && e.source == s.request.Style.Source {
			continue
		}
		f := s.failures[tile]
		if f.attempts >= 3 || (!f.retryAt.IsZero() && now.Before(f.retryAt)) {
			continue
		}
		if !s.roomForTile(tile) {
			continue
		}
		ctx, cancel := context.WithCancel(s.p.ctx)
		return job{ctx: ctx, cancel: cancel, key: Key{Source: strings.Clone(s.request.Style.Source), Tile: tile,
			StyleEpoch: s.request.Style.Epoch, Generation: s.generation}}, true
	}
	return job{}, false
}

func (s *state) roomForTile(tile view.TileID) bool {
	if s.entries[tile] == nil && len(s.entries) >= s.p.limits.Tiles {
		s.errorAt("cache/tiles", ErrLimit)
		return false
	}
	return true
}

func (s *state) loaded(r result) {
	s.stats.Loading.observe(r.duration)
	s.stats.ResponseBytes += uint64(len(r.data))
	s.stats.RawCapacityBytes += uint64(cap(r.data))
	defer r.job.cancel()
	tile := r.job.key.Tile
	// A request can arrive while select is receiving this result. Consume the
	// latest input first, so neither ignored cancel nor queued success revives it.
	s.inputs()
	active, exists := s.running[tile]
	if !exists || active.key.Generation != r.job.key.Generation {
		s.stats.Rejected++
		return
	}
	delete(s.running, tile)
	if s.request == nil || r.job.ctx.Err() != nil || !s.desired[tile] {
		s.stats.Rejected++
		return
	}
	if r.err != nil {
		s.errorAt("load", r.err)
		f := s.failures[tile]
		if r.retry && f.attempts < 3 {
			f.retryAt = time.Now().Add(s.p.limits.RetryDelay)
		} else {
			f.attempts = 3
		}
		s.failures[tile] = f
		return
	}
	if !s.roomForTile(tile) {
		s.failures[tile] = failure{attempts: 3}
		return
	}
	// Admission uses the compact length, not the transport reservation. Preserve
	// the old entry atomically on failure, including source replacement. The job's
	// fixed raw slot and one <=RawBytes compaction scratch buffer cover this copy.
	var next entry
	if old := s.entries[tile]; old != nil {
		next = *old
	}
	next.raw, next.prepared, next.style = nil, nil, 0
	next.usage = entryUsage(&next)
	next.usage.Raw = 2 * uint64(len(r.data))
	if s.cacheCharge(tile, &next) > s.p.limits.CacheBytes {
		s.errorAt("cache/raw", ErrLimit)
		s.failures[tile] = failure{attempts: 3}
		return
	}
	next.raw = compactResponse(r.data)
	next.source = strings.Clone(r.job.key.Source)
	s.entries[tile] = &next
	s.recordCache(tile, &next)
	s.stats.PeakCached = max(s.stats.PeakCached, len(s.entries))
	delete(s.failures, tile)
}

func compactResponse(data []byte) []byte {
	if len(data) == cap(data) {
		return data
	}
	compact := make([]byte, len(data))
	copy(compact, data)
	return compact
}

func entryUsage(e *entry) CacheUsage {
	// In addition to the response, reserve its entire size for decoded string
	// backing that an evaluated candidate can borrow through a short substring.
	return CacheUsage{Raw: 2 * uint64(cap(e.raw)), Prepared: e.prepared.RetainedBytes(), Fragments: 2 * e.fragment.RetainedBytes()}
}

// Charge complete immutable profiles once per cache, including obsolete epochs
// still backing continuity fragments. This bounds hidden substring/callback-image
// backing without copying every borrowed style string or sprite image.
func (s *state) cacheCharge(tile view.TileID, replacement *entry) uint64 {
	return s.cacheUsage(tile, replacement).Total()
}

func (s *state) recordCache(tile view.TileID, replacement *entry) {
	s.stats.Cache = s.cacheUsage(tile, replacement)
	s.stats.CacheBytes = s.stats.Cache.Total()
	if s.stats.CacheBytes > s.stats.PeakCacheBytes {
		s.stats.PeakCacheBytes = s.stats.CacheBytes
		s.stats.PeakCache = s.stats.Cache
	}
}

func (s *state) cacheUsage(tile view.TileID, replacement *entry) CacheUsage {
	styles := make(map[*Style]bool)
	assets := make(map[*Assets]bool)
	var total CacheUsage
	add := func(e *entry) {
		if e == nil {
			return
		}
		total.Raw += e.usage.Raw
		total.Prepared += e.usage.Prepared
		total.Fragments += e.usage.Fragments
		if profile := e.styleSnapshot; profile != nil && !styles[profile] {
			styles[profile] = true
			total.Profiles += profile.Bytes
		}
		if profile := e.assetSnapshot; profile != nil && !assets[profile] {
			assets[profile] = true
			total.Profiles += profile.Bytes
		}
	}
	for key, e := range s.entries {
		if key != tile {
			add(e)
		}
	}
	add(replacement)
	return total
}

func (s *state) snapshotCharge(snapshot *tiles.Snapshot) uint64 {
	total := snapshot.RetainedBytes()
	assets := make(map[*Assets]bool)
	for _, tile := range snapshot.Cover {
		if e := s.entries[tile]; e != nil && e.assetSnapshot != nil && !assets[e.assetSnapshot] {
			assets[e.assetSnapshot] = true
			total += e.assetSnapshot.Bytes
		}
	}
	return total
}

func (s *state) compile() bool {
	if s.request == nil {
		return false
	}
	for _, tile := range s.order {
		if !s.desired[tile] {
			continue
		}
		e := s.entries[tile]
		if e == nil || e.source != s.request.Style.Source || (e.style == s.style && e.assets == s.assets) || s.failures[tile].attempts >= 3 {
			continue
		}
		s.build(tile, e)
		return true
	}
	return false
}

func (s *state) build(tile view.TileID, old *entry) {
	styleEpoch, assetEpoch := s.style, s.assets
	next := *old
	var err error
	stage := "prepare"
	if next.prepared == nil || next.style != s.style {
		options := s.request.Style.Options
		options.Tile = tile
		started := time.Now()
		next.prepared, err = tiles.Prepare(next.raw, s.request.Style.Layers, options)
		s.stats.Preparing.observe(time.Since(started))
		s.stats.Prepares++
	}
	if err == nil {
		stage = "build"
		var built *tiles.BuildResult
		started := time.Now()
		built, err = next.prepared.Build(s.request.Assets.Value)
		s.stats.Building.observe(time.Since(started))
		s.stats.Builds++
		if err == nil {
			next.fragment = built.Fragment
		}
	}
	// Adopt camera/cover updates without starving useful compilation. Source/style
	// or asset changes and obsolete tiles discard the finished bounded operation.
	s.inputs()
	if s.p.ctx.Err() != nil || s.style != styleEpoch || s.assets != assetEpoch || !s.desired[tile] {
		s.stats.Rejected++
		return
	}
	if err == nil {
		stage = "cache/compiled"
		next.usage = entryUsage(&next)
		next.styleSnapshot, next.assetSnapshot = s.request.Style, s.request.Assets
		if s.cacheCharge(tile, &next) > s.p.limits.CacheBytes {
			err = ErrLimit
		}
	}
	if err == nil {
		stage = "store"
		err = s.set.Apply([]tiles.Change{{Tile: tile, Fragment: next.fragment}})
	}
	if err != nil {
		s.errorAt(stage, err)
		s.failures[tile] = failure{attempts: 3}
		return
	}
	next.style, next.assets = s.style, s.assets
	s.entries[tile] = &next
	s.recordCache(tile, &next)
	s.dirty = true
}

func (s *state) error(err error) {
	s.errorAt("owner", err)
}

func (s *state) errorAt(stage string, err error) {
	s.stats.LastError = errorMessage(err)
	s.stats.LastErrorStage = stage
}

func errorMessage(err error) string {
	message := err.Error()
	if len(message) > 512 {
		message = message[:512]
	}
	return strings.Clone(message)
}

func (s *state) publish() {
	if !s.dirty || s.request == nil {
		return
	}
	p := s.p
	p.mu.Lock()
	// Reclaim only the output slot that no caller has acquired.
	if p.output != nil {
		p.output.released = true
		delete(p.leases, p.output)
		p.output = nil
	}
	full := len(p.leases) >= p.limits.Leases
	p.mu.Unlock()
	if full {
		return
	}
	started := time.Now()
	snapshot, err := s.set.SelectBounded(s.targets, s.current.cover, s.request.Camera, 0, p.limits.SnapshotBytes)
	s.stats.Selecting.observe(time.Since(started))
	stage := "select"
	var charge uint64
	if err == nil {
		// Old fragments remain available for acknowledged continuity, but a new
		// target must not mix source/style/asset epochs. Native Current continues
		// displaying its own immutable snapshot while this replacement is built.
		for _, tile := range snapshot.Cover {
			e := s.entries[tile]
			if e.style != s.style || e.assets != s.assets || e.source != s.request.Style.Source {
				s.dirty = false
				return
			}
		}
		charge = s.snapshotCharge(snapshot)
		if charge > p.limits.SnapshotBytes {
			err = ErrLimit
			stage = "snapshot/backing"
		}
	}
	if err != nil {
		s.errorAt(stage, err)
		s.dirty = false
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.revision != s.revision {
		return
	}
	l := &Lease{Snapshot: snapshot, Revision: s.revision, Generation: s.generation, p: p, bytes: charge}
	p.leases[l] = struct{}{}
	s.stats.PeakLeases = max(s.stats.PeakLeases, len(p.leases))
	var leaseBytes uint64
	for held := range p.leases {
		leaseBytes += held.bytes
	}
	s.stats.PeakLeaseBytes = max(s.stats.PeakLeaseBytes, leaseBytes)
	p.output = l
	s.stats.SelectedTiles, s.stats.Fallbacks = len(snapshot.Cover), snapshot.Fallbacks
	s.dirty = false
}

func (s *state) report() {
	p := s.p
	p.mu.Lock()
	defer p.mu.Unlock()
	s.stats.Revision, s.stats.Generation = s.revision, s.generation
	s.stats.Requested = len(s.targets)
	s.stats.CurrentGeneration, s.stats.CurrentSequence = s.current.generation, s.current.sequence
	s.stats.Pending = len(s.running)
	s.stats.Failed = 0
	for _, tile := range s.order {
		e := s.entries[tile]
		if s.desired[tile] && (e == nil || e.style != s.style || e.assets != s.assets) {
			if s.failures[tile].attempts >= 3 {
				s.stats.Failed++
			} else {
				s.stats.Pending++
			}
		}
	}
	if s.dirty {
		s.stats.Pending++
	}
	s.stats.Jobs, s.stats.Cached, s.stats.Leases = len(s.running), len(s.entries), len(p.leases)
	s.stats.ReservedRawBytes = uint64(len(s.running)) * uint64(p.limits.RawBytes)
	s.stats.LeaseBytes = 0
	for l := range p.leases {
		s.stats.LeaseBytes += l.bytes
	}
	s.stats.PeakJobs = max(s.stats.PeakJobs, s.stats.Jobs)
	s.stats.PeakCached = max(s.stats.PeakCached, s.stats.Cached)
	s.stats.PeakLeases = max(s.stats.PeakLeases, s.stats.Leases)
	s.stats.PeakCacheBytes = max(s.stats.PeakCacheBytes, s.stats.CacheBytes)
	s.stats.PeakLeaseBytes = max(s.stats.PeakLeaseBytes, s.stats.LeaseBytes)
	p.status = s.stats
}
