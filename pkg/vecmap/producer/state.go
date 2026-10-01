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
	decoded       *tiles.Source // Limits.ReuseDecoded; charged as Prepared
	fragment      *tiles.Fragment
	style, assets uint64 // owner epochs, never native generations
	usage         CacheUsage
	styleSnapshot *Style
	preparedUse   uint64
}

type failure struct {
	attempts              int
	retryAt               time.Time
	askAgain              bool // a failed load that Retry may request again
	capacity              bool
	fixedAt, maximumFixed uint64
}

type state struct {
	p                                   *Producer
	set                                 *tiles.Set
	request                             *Request // latest camera and targets
	working                             *Request // adopted style/asset pair for loads and compilation
	revision, generation, style, assets uint64
	current                             current
	targets, order                      []view.TileID
	desired, pinned                     map[view.TileID]bool
	entries                             map[view.TileID]*entry
	running                             map[view.TileID]job
	compiling                           map[view.TileID]compileJob // unfinished until compiled consumes the result
	failures                            map[view.TileID]failure
	stats                               Status
	dirty                               bool
	notify                              bool // the consumer can see a change the next report must signal
	published                           bool // the working epoch reached a coherent publication attempt
	workingPrepares                     int  // Prepare attempts since adoption; bounds a held epoch
	heldStyle                           *Style
	heldAssets                          *Assets
	pausedSource                        string // the source that asked for no requests before pausedUntil
	pausedUntil                         time.Time
}

func (p *Producer) run(set *tiles.Set) {
	var wg sync.WaitGroup
	for range p.limits.Workers {
		wg.Add(1)
		go p.load(&wg)
	}
	for range p.limits.Compilers {
		wg.Add(1)
		go p.compiler(&wg)
	}
	defer func() {
		p.cancel()
		wg.Wait()
		p.loader = nil // release transport captures after the last worker exits
		// Results can retain raw buffers after canceled workers have exited.
		for len(p.results) > 0 {
			<-p.results
		}
		for len(p.compileJobs) > 0 {
			(<-p.compileJobs).cancel()
		}
		for len(p.compiled) > 0 {
			(<-p.compiled).job.cancel()
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
	s := state{p: p, set: set, entries: make(map[view.TileID]*entry), running: make(map[view.TileID]job),
		compiling: make(map[view.TileID]compileJob), failures: make(map[view.TileID]failure)}
	for {
		if p.ctx.Err() != nil {
			return
		}
		s.inputs()
		s.refine()
		s.evict()
		s.retryCapacity()
		s.publish()
		s.report()
		// Drain completed loads and builds before compiling another tile. No
		// worker can be replaced while its result remains queued.
		select {
		case r := <-p.results:
			s.loaded(r)
			continue
		case r := <-p.compiled:
			s.compiled(r)
			continue
		default:
		}
		s.compile()
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
		case r := <-p.compiled:
			if available {
				next.cancel()
			}
			s.compiled(r)
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
	retry := p.retry
	p.retry = false
	s.notify = s.notify || r != nil || retry
	ack := p.current
	pinned := make(map[view.TileID]bool)
	for l := range p.leases {
		for _, tile := range l.Snapshot.Cover {
			pinned[tile] = true
		}
	}
	p.mu.Unlock()
	if ack.generation != s.current.generation || ack.sequence != s.current.sequence {
		s.notify = true
		// Native packets advance sequence for every batch, but only changed
		// coverage affects continuity selection. Don't feed uploads back into
		// identical CPU publications and placement work.
		if !slices.Equal(ack.cover, s.current.cover) {
			s.dirty = true
		} else {
			s.stats.UnchangedCurrent++
		}
		s.current = ack
	}
	for _, tile := range s.current.cover {
		pinned[tile] = true
	}
	s.pinned = pinned
	if r != nil {
		targets := r.Targets
		if targets == nil {
			targets = view.VisibleTileCoverRing(r.Camera, r.Style.Options.Coarser, p.limits.PrefetchRing)
		}
		if !slices.Equal(targets, s.targets) {
			if !s.advanceGeneration() {
				return
			}
			clear(s.failures)
		}
		s.request, s.revision, s.targets = r, revision, targets
		s.order = targets
		if p.limits.Parents {
			s.order = view.LoadOrder(targets)
		}
		s.desired = make(map[view.TileID]bool, len(s.order))
		for _, tile := range s.order {
			s.desired[tile] = true
		}
		for tile, j := range s.running {
			if !s.desired[tile] {
				j.cancel()
			}
		}
		for tile, j := range s.compiling {
			if !s.desired[tile] {
				j.cancel()
			}
		}
		s.dirty = true
	}
	s.adopt()
	if retry {
		s.retryLoads()
	}
}

// retryLoads forgets the terminal load failures of desired tiles, so nextJob
// requests them again.
func (s *state) retryLoads() {
	for tile, f := range s.failures {
		if f.askAgain && f.attempts >= 3 && s.desired[tile] {
			delete(s.failures, tile)
			s.stats.RetriedLoads++
		}
	}
}

func (s *state) advanceGeneration() bool {
	if s.generation == math.MaxUint64 {
		s.error(ErrLimit)
		s.p.Close()
		return false
	}
	s.generation++
	return true
}

// adopt switches loads and compilation to the newest style/asset pair at a bounded
// boundary. Camera and targets always follow the latest request; only paint inputs
// may lag. A working epoch with installed progress is held until one coherent
// publication has been attempted, its remaining desired work can no longer arrive,
// or it has spent two covers' worth of preparation without becoming coherent.
// Continuous sixteenth-zoom changes therefore yield coherent intermediate covers,
// each evaluated at an exact style zoom, instead of publishing nothing until motion
// ends. Mixed epochs are still never published.
func (s *state) adopt() {
	r := s.request
	if r == nil || (s.working != nil && s.working.Style == r.Style && s.working.Assets == r.Assets) {
		return
	}
	if s.working != nil && s.holdWorking() {
		if s.heldStyle != r.Style || s.heldAssets != r.Assets {
			s.heldStyle, s.heldAssets = r.Style, r.Assets
			s.stats.HeldStyles++
		}
		return
	}
	styleChanged := s.working == nil || s.working.Style != r.Style
	assetsChanged := s.working == nil || s.working.Assets != r.Assets
	if !s.advanceGeneration() {
		return
	}
	if styleChanged {
		s.style++
		s.reuseStyle(r.Style)
		for _, j := range s.compiling {
			j.cancel() // prepared for the old style
		}
	}
	if assetsChanged {
		s.assets++
		s.p.buildAssets.Store(&buildAssets{value: r.Assets.Value, epoch: s.assets})
	}
	clear(s.failures)
	if s.working != nil {
		s.stats.StyleAdoptions++
	}
	s.working = r
	s.published, s.workingPrepares = false, 0
	s.heldStyle, s.heldAssets = nil, nil
	for _, j := range s.running {
		if j.key.Source != r.Style.Source {
			j.cancel()
		} else if styleChanged && j.ctx.Err() == nil {
			s.stats.LoadStyleReuses++
		}
	}
	s.dirty = true
}

// holdWorking reports whether adopting newer paint inputs now would discard
// installed working-epoch fragments before they could be published coherently.
func (s *state) holdWorking() bool {
	if s.published || s.workingPrepares >= 2*len(s.order) {
		return false
	}
	for _, tile := range s.order {
		if e := s.entries[tile]; s.desired[tile] && s.atWorkingEpoch(e) {
			return true
		}
	}
	return false
}

func (s *state) atWorkingEpoch(e *entry) bool {
	return e != nil && e.fragment != nil && e.source == s.working.Style.Source && e.style == s.style && e.assets == s.assets
}

// pendingWork reports whether any desired tile can still reach the working epoch.
func (s *state) pendingWork() bool {
	for _, tile := range s.order {
		if e := s.entries[tile]; s.desired[tile] && !s.atWorkingEpoch(e) && s.failures[tile].attempts < 3 {
			return true
		}
	}
	return false
}

// Epochs order requests; they aren't an input to compilation. A tile not rebuilt
// during an intervening zoom can reuse its existing immutable fragment when the
// exact preparation inputs return. Keep its original charged style backing and
// resource identities. Different layer storage is conservatively treated as new.
func (s *state) reuseStyle(style *Style) {
	for _, e := range s.entries {
		if e.fragment != nil && e.source == style.Source && samePreparation(e.styleSnapshot, style) {
			e.style = s.style
			s.stats.StyleReuses++
		}
	}
}

func samePreparation(a, b *Style) bool {
	if a == nil || b == nil || a.Source != b.Source || a.Options != b.Options || len(a.Layers) != len(b.Layers) {
		return false
	}
	return len(a.Layers) == 0 || &a.Layers[0] == &b.Layers[0]
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

// Stop requesting a fallback once all requested siblings have installed fragments.
// Set selects those siblings even during an epoch refresh, so rebuilding their
// unselectable parent first only delays a coherent cover. Keep its old fragment
// while Current or any leased target can still require it.
func (s *state) refine() {
	for _, group := range view.GroupTiles(s.targets) {
		if !group.HasParent {
			continue
		}
		ready := true
		for _, tile := range group.Targets {
			e := s.entries[tile]
			if e == nil || e.fragment == nil {
				ready = false
				break
			}
		}
		if ready {
			delete(s.desired, group.Parent)
			if j, ok := s.running[group.Parent]; ok {
				j.cancel()
			}
			if j, ok := s.compiling[group.Parent]; ok {
				j.cancel()
			}
		}
	}
}

// paused reports whether the working source asked for no requests yet.
func (s *state) paused(now time.Time) bool {
	return s.working != nil && s.pausedSource == s.working.Style.Source && now.Before(s.pausedUntil)
}

func (s *state) nextJob(now time.Time) (job, bool) {
	if s.request == nil || len(s.running) >= s.p.limits.Workers || s.paused(now) {
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
		if e != nil && e.source == s.working.Style.Source {
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
		return job{ctx: ctx, cancel: cancel, key: Key{Source: strings.Clone(s.working.Style.Source), Tile: tile,
			StyleEpoch: s.working.Style.Epoch, Generation: s.generation}}, true
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
		if r.busy.After(time.Now()) {
			// The source refused, not the tile: ask again once it takes
			// requests, without spending one of the tile's attempts.
			if s.pausedSource != r.job.key.Source || r.busy.After(s.pausedUntil) {
				s.pausedSource, s.pausedUntil = r.job.key.Source, r.busy
			}
			f.attempts = max(f.attempts-1, 0)
			s.failures[tile] = f
			return
		}
		f.askAgain = !r.oversize
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
	next.raw, next.prepared, next.decoded, next.style = nil, nil, nil, 0
	next.usage = entryUsage(tile, &next)
	next.usage.Raw = 2 * uint64(len(r.data))
	if !s.admitPrepared(tile, &next) {
		s.errorAt("cache/raw", ErrLimit)
		s.failCapacity(tile, &next)
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

func entryUsage(tile view.TileID, e *entry) CacheUsage {
	// In addition to the response, reserve its entire size for decoded string
	// backing that an evaluated candidate can borrow through a short substring.
	return CacheUsage{Raw: 2 * uint64(cap(e.raw)), Prepared: e.prepared.RetainedBytes() + e.decoded.RetainedBytes(), Fragments: e.fragment.RetainedBytes() + e.fragment.SetCopyBytes(tile)}
}

// Charge complete immutable style profiles once per cache, including obsolete
// epochs still backing candidate strings. BuildOwned eliminates asset backing.
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
	// BuildOwned makes all texture backing self-contained. Input assets live in
	// the separately reserved owner/latest/transfer profiles, never in a lease.
	return snapshot.RetainedBytes()
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
		s.notify = true
	}
	full := len(p.leases) >= p.limits.Leases
	p.mu.Unlock()
	if full {
		return
	}
	if !s.coherentCover() {
		s.stats.DeferredSelections++
		s.dirty = false
		if !s.pendingWork() {
			s.published = true // terminal failures block this epoch; don't hold newer paint for it
		}
		return
	}
	s.published = true
	started := time.Now()
	snapshot, err := s.set.SelectBounded(s.drawn(), s.current.cover, s.request.Camera, 0, p.limits.SnapshotBytes)
	s.stats.Selecting.observe(time.Since(started))
	stage := "select"
	var charge uint64
	if err == nil {
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
	s.notify = true
	s.stats.SelectedTiles, s.stats.Fallbacks = len(snapshot.Cover), snapshot.Fallbacks
	s.dirty = false
}

// Use the same coverage policy and readiness as Set.SelectBounded, but reject
// mixed epochs before projection, collision and composition. Entries with a
// fragment are exactly the tiles installed in Set; obsolete fragments remain
// ready for acknowledged continuity. This must not filter them out to manufacture
// a smaller publishable cover. The single owner cannot mutate Set between checks.
func (s *state) coherentCover() bool {
	for _, tile := range s.selectedCover() {
		e := s.entries[tile]
		if e.style != s.style || e.assets != s.assets || e.source != s.working.Style.Source {
			return false
		}
	}
	return true
}

// drawn lists, in target order, the targets a scene is composed from: all of
// them, or with Limits.DrawMargin those near the viewport.
func (s *state) drawn() []view.TileID {
	margin := s.p.limits.DrawMargin
	if margin == 0 || s.request == nil {
		return s.targets
	}
	near := view.TilesNear(s.request.Camera, s.targets, margin)
	if len(near) == len(s.targets) {
		return s.targets
	}
	kept := view.TilesNear(s.request.Camera, s.targets, 2*margin)
	drawn := make([]view.TileID, 0, len(kept))
	for _, tile := range kept {
		if slices.Contains(near, tile) || slices.Contains(s.current.cover, tile) {
			drawn = append(drawn, tile)
		}
	}
	return drawn
}

func (s *state) selectedCover() []view.TileID {
	cover, _ := view.SelectCover(s.drawn(), s.current.cover, func(tile view.TileID) bool {
		e := s.entries[tile]
		return e != nil && e.fragment != nil
	})
	return cover
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
	s.stats.StyleHeld = s.working != nil && (s.working.Style != s.request.Style || s.working.Assets != s.request.Assets)
	if s.stats.StyleHeld {
		s.stats.Pending++ // the newest paint inputs are not installed yet
	}
	s.stats.PausedUntil = time.Time{}
	if s.paused(time.Now()) {
		s.stats.PausedUntil = s.pausedUntil
	}
	s.stats.ReusedVersions = s.set.ReusedVersions()
	s.stats.Jobs, s.stats.Cached, s.stats.Leases = len(s.running), len(s.entries), len(p.leases)
	s.stats.Compiling = len(s.compiling)
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
	old := p.status
	if s.notify || old.Pending != s.stats.Pending || old.Failed != s.stats.Failed || old.StyleHeld != s.stats.StyleHeld ||
		old.LastError != s.stats.LastError || old.LastErrorStage != s.stats.LastErrorStage || !old.PausedUntil.Equal(s.stats.PausedUntil) {
		select {
		case p.changed <- struct{}{}:
		default:
		}
		s.notify = false
	}
	p.status = s.stats
}
