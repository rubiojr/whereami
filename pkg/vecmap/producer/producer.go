// Package producer loads and compiles bounded live tile covers without Qt or cgo.
// One owner performs preparation, composition and cache mutation; fixed workers
// perform only transport. Native residency and completion remain adapter-owned.
package producer

import (
	"context"
	"errors"
	"io"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/rubiojr/whereami/pkg/vecmap/mvt"
	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
	"github.com/rubiojr/whereami/pkg/vecmap/tiles"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
)

var (
	ErrInput  = errors.New("invalid producer input")
	ErrLimit  = errors.New("producer storage limit exceeded")
	ErrClosed = errors.New("producer closed")
	// Missing and permanent transport failures are not retried. A successful
	// empty MVT response is a ready blank tile, unlike ErrMissing.
	ErrMissing   = errors.New("tile missing")
	ErrPermanent = errors.New("permanent tile load failure")
)

// Key identifies a transport request. Generation is a producer job generation,
// unrelated to the retained Worker's native residency generation. Source and Tile
// identify immutable raw data; change Source when that data changes. StyleEpoch
// describes the issuing request, not necessarily the style used when data arrives.
type Key struct {
	Source                 string
	Tile                   view.TileID
	StyleEpoch, Generation uint64
}

// Loader writes one response to a bounded writer. It must return after context
// cancellation, must not retain/use the writer after returning, and must bound its
// own transport buffers. Ignored cancellation consumes a worker until it returns;
// it never permits another worker to be spawned. No decoding belongs here.
type Loader func(context.Context, Key, io.Writer) error

// Style and Assets are immutable application-owned snapshots. Bytes declares an
// upper bound on reachable storage (including callback caches for Assets). This
// is a trusted input contract, not introspection of arbitrary maps/closures.
// Reserve owner, latest and one in-transfer snapshot pair. Use a new pointer and
// epoch on changes; style zoom/topology/limits are fixed by Options.
// Shared Layers storage must remain immutable across all such snapshots. Returning
// to identical Source/Options/Layer storage may reuse an installed fragment.
type Style struct {
	Source  string
	Epoch   uint64
	Layers  []style.CompiledLayer
	Options tiles.PrepareOptions
	Bytes   uint64
}

type Assets struct {
	Epoch uint64
	Value tiles.Assets
	Bytes uint64
}

type Request struct {
	Camera view.Camera
	Style  *Style
	Assets *Assets
	// Nil selects VisibleTileCover. A nonnil empty slice explicitly clears.
	Targets []view.TileID
}

// Limits are explicit positive budgets; DefaultLimits supplies an initial policy.
// CacheBytes covers raw/backing capacity, optional preparation, fragment payload
// plus Set's metadata copies, and unique borrowed style profiles. Fragments own
// texture pixels. SnapshotBytes covers each lease and one internal selection slot.
// RawBytes*Workers reserves running and pending-result buffers. ProfileBytes
// reserves each style+asset pair; three pairs can coexist during request handoff.
// Compiler scratch is separately bounded by MVT/PrepareOptions and one compiler.
// One additional <=RawBytes scratch copy compacts a completed response for caching.
// Sprite copying is bounded by CacheBytes for the one in-progress BuildOwned job.
type Limits struct {
	Workers, Tiles, Leases, RawBytes        int
	CacheBytes, SnapshotBytes, ProfileBytes uint64
	Store                                   retained.Limits
	RetryDelay                              time.Duration
}

func DefaultLimits() Limits {
	return Limits{Workers: 4, Tiles: 128, Leases: 4, RawBytes: mvt.MaxTileBytes,
		CacheBytes: 256 << 20, SnapshotBytes: 128 << 20, ProfileBytes: 64 << 20,
		RetryDelay: 250 * time.Millisecond}
}

// Status is a bounded observation, not an event log. Revision is the latest
// request consumed by the owner. ReservedRawBytes includes canceled jobs and
// queued results until the owner consumes them. Peak fields are lifetime peaks.
type Status struct {
	CapacityRetries                             uint64
	UncachedPreparations                        uint64
	PreparationEvictions, PreparationBytesFreed uint64
	CurrentGeneration, CurrentSequence          uint64 // native Current mailbox consumed by the owner
	Revision, Generation                        uint64
	Jobs, Cached, Leases                        int
	Pending                                     int // desired unfinished tiles, plus unpublished/request-mailbox work
	Failed                                      int // terminal failures still relevant to desired coverage
	CacheBytes, LeaseBytes, ReservedRawBytes    uint64
	PeakJobs, PeakCached, PeakLeases            int
	PeakCacheBytes, PeakLeaseBytes              uint64
	Loads, Prepares, Builds, Rejected           uint64
	LoadStyleReuses, SkippedBuilds              uint64 // retained in-flight jobs across style changes; obsolete pre-pack work
	StyleReuses                                 uint64 // installed fragments with identical immutable preparation inputs
	DeferredSelections, UnchangedCurrent        uint64 // mixed epochs rejected before placement; no continuity change on native ack
	LastError                                   string
	LastErrorStage                              string
	Cache, PeakCache                            CacheUsage
	Loading, Preparing, Building, Selecting     PhaseTime
	ResponseBytes, RawCapacityBytes             uint64 // completed responses, before cache admission
	Requested, SelectedTiles, Fallbacks         int    // latest CPU target, not native Current
}

// Lease pins an immutable snapshot until Release. Keep it alive through every
// Worker target, Current, packet and native borrow. Release is idempotent. Copying
// the pointer does not acquire another lease. Do not use Snapshot after Release.
type Lease struct {
	Snapshot             *tiles.Snapshot
	Revision, Generation uint64
	p                    *Producer
	released             bool // protected by p.mu
	bytes                uint64
}

func (l *Lease) Release() {
	if l == nil {
		return
	}
	l.p.mu.Lock()
	l.released = true
	delete(l.p.leases, l)
	if l.p.output == l {
		l.p.output = nil
	}
	l.p.mu.Unlock()
	l.p.signal()
}

type current struct {
	generation, sequence uint64
	cover                []view.TileID
}

type Producer struct {
	mu       sync.Mutex
	latest   *Request
	revision uint64
	closed   bool
	output   *Lease
	leases   map[*Lease]struct{}
	current  current
	status   Status
	wake     chan struct{}
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	jobs     chan job
	results  chan result
	limits   Limits
	loader   Loader
}

func New(loader Loader, limits Limits) (*Producer, error) {
	if loader == nil || limits.Workers < 1 || limits.Workers > 4 || limits.Tiles < 1 || limits.Tiles > tiles.MaxTiles ||
		limits.Leases < 1 || limits.Leases > 8 || limits.RawBytes < 1 || limits.RawBytes > mvt.MaxTileBytes ||
		limits.CacheBytes == 0 || limits.CacheBytes > 1<<30 || limits.SnapshotBytes == 0 || limits.SnapshotBytes > 1<<30 ||
		limits.ProfileBytes == 0 || limits.ProfileBytes > 1<<30 || limits.RetryDelay <= 0 {
		return nil, ErrInput
	}
	set, err := tiles.New(limits.Store)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &Producer{ctx: ctx, cancel: cancel, done: make(chan struct{}), wake: make(chan struct{}, 1),
		jobs: make(chan job), results: make(chan result, limits.Workers), limits: limits, loader: loader,
		leases: make(map[*Lease]struct{})}
	go p.run(set)
	return p, nil
}

// Submit coalesces requests without doing I/O, decoding, shaping or composition.
// Target validation and copying are bounded by 64 entries. Returned revision is
// a producer request identity; zero on error. Inputs stay immutable while borrowed.
func (p *Producer) Submit(r Request) (uint64, error) {
	if err := p.validate(r); err != nil {
		return 0, err
	}
	if r.Targets != nil {
		r.Targets = append([]view.TileID{}, r.Targets...)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return 0, ErrClosed
	}
	if p.revision == math.MaxUint64 {
		return 0, ErrLimit
	}
	p.revision++
	p.latest = &r
	p.signal()
	return p.revision, nil
}

func (p *Producer) validate(r Request) error {
	if r.Style == nil || r.Assets == nil || r.Style.Epoch == 0 || r.Assets.Epoch == 0 ||
		len(r.Style.Source) == 0 || len(r.Style.Source) > 256 || len(r.Style.Layers) > tiles.MaxStyleLayers ||
		len(r.Targets) > 64 || r.Style.Bytes > p.limits.ProfileBytes || r.Assets.Bytes > p.limits.ProfileBytes-r.Style.Bytes {
		return ErrInput
	}
	if r.Style.Bytes == 0 || r.Assets.Bytes == 0 || math.IsNaN(r.Style.Options.Zoom) || r.Style.Options.Zoom < 0 || r.Style.Options.Zoom > 20 {
		return ErrInput
	}
	if !finiteViewport(r.Camera.Width) || !finiteViewport(r.Camera.Height) {
		return ErrInput
	}
	seen := make(map[view.TileID]bool, len(r.Targets))
	for _, tile := range r.Targets {
		if tile.Z > 14 || tile.X >= 1<<tile.Z || tile.Y >= 1<<tile.Z || seen[tile] || tile.Z != r.Targets[0].Z {
			return ErrInput
		}
		seen[tile] = true
	}
	return nil
}

func finiteViewport(v float64) bool { return v > 0 && v <= 1<<20 && !math.IsNaN(v) }

// Next transfers the latest output lease to the caller. Unconsumed outputs may
// be coalesced; consumed leases are never reclaimed until explicitly released.
func (p *Producer) Next() (*Lease, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	l := p.output
	p.output = nil
	if l != nil {
		p.signal()
	}
	return l, l != nil
}

// Current returns acknowledged native coverage through a coalescing mailbox.
// Call with Packet.CurrentData's lease, never the latest target's lease. Native
// generation/sequence pairs must increase; use ResetCurrent on native reset.
// This neither acknowledges a Worker packet nor releases any lease.
func (p *Producer) Current(generation, sequence uint64, l *Lease) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || generation == 0 || sequence == 0 || generation < p.current.generation ||
		(generation == p.current.generation && sequence <= p.current.sequence) {
		return false
	}
	var cover []view.TileID
	if l != nil {
		if l.p != p || l.released {
			return false
		}
		if _, ok := p.leases[l]; !ok {
			return false
		}
		cover = slices.Clone(l.Snapshot.Cover)
	}
	p.current = current{generation: generation, sequence: sequence, cover: cover}
	p.signal()
	return true
}

// ResetCurrent clears continuity for a fresh native namespace without consuming
// that namespace's first packet sequence. It does not cancel useful producer jobs
// or release leases still borrowed by the old native namespace or Worker.
func (p *Producer) ResetCurrent(generation uint64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || generation == 0 || generation <= p.current.generation {
		return false
	}
	p.current = current{generation: generation}
	p.signal()
	return true
}

func (p *Producer) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	status := p.status
	status.Leases, status.LeaseBytes = len(p.leases), 0
	for lease := range p.leases {
		status.LeaseBytes += lease.bytes
	}
	if !p.closed && (p.latest != nil || p.output != nil || p.current.generation != status.CurrentGeneration || p.current.sequence != status.CurrentSequence) {
		status.Pending++
	}
	return status
}

// Close cancels transport and stops production without waiting for native work.
// Join Done outside GUI/render callbacks. A loader ignoring cancellation delays
// Done until it returns; no goroutine is abandoned. Caller-held leases stay valid.
func (p *Producer) Close() {
	p.mu.Lock()
	p.closed = true
	p.latest = nil
	p.mu.Unlock()
	p.cancel()
}

func (p *Producer) Done() <-chan struct{} { return p.done }

func (p *Producer) signal() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}
