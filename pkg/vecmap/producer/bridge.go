package producer

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
)

// Bridge supersedes targets at acknowledged packet boundaries while coalescing
// newer producer targets. Four producer lease slots cover Current, target handoff,
// pending and output/in-transfer work, with backpressure during handoff. Camera
// requests continue independently. The renderer must call Restarted, Consumed and
// Completed in the Worker/native packet protocol; no producer lease is exposed to the renderer.
// Documents and packets must not be retained beyond their corresponding protocol
// ownership. Construct with NewBridge and close after native use has stopped.
type Bridge struct {
	producer                                  *Producer
	worker                                    *retained.WorkerWithData[*scene.Document]
	initial                                   *scene.Document
	latest                                    atomic.Pointer[scene.Document]
	mu                                        sync.Mutex
	held                                      map[*scene.Document]*Lease
	pending                                   *scene.Document
	busy                                      bool
	advance                                   bool
	receiving                                 bool
	generation, consumed, completed, revision uint64
	stop, done                                chan struct{}
	once                                      sync.Once
	stats                                     BridgeStats
	started                                   time.Time
	current                                   *scene.Document // aliases the held Current lease, not another owner
	currentSince                              time.Time
}

// BridgeStats counts bounded mailbox and acknowledged native progress. Overtaken
// uploads completed while a newer CPU document was pending or exposed; they aren't
// necessarily wasted (versions can be shared). Counts include all native generations.
type BridgeStats struct {
	Received, Coalesced, SameSnapshot, Targets, Superseded uint64
	UploadBatches, UploadBytes, OvertakenUploadBytes       uint64
	ReleaseBatches, ReleasedResources, Settlements         uint64
	// Since construction; zero until a drawable Current is consumed.
	FirstVisibleCurrent                   time.Duration
	CurrentChanges                        uint64 // drawable document changes, not camera-only reprojection
	FirstCurrentTiles, FirstCurrentLabels int
	CurrentTiles, CurrentFallbacks        int
	LongestCurrentHold                    time.Duration // drawable document unchanged; not content age or presentation time
}

func (b *Bridge) Stats() BridgeStats {
	b.mu.Lock()
	defer b.mu.Unlock()
	stats := b.stats
	if b.current != nil && len(b.current.Scene.Draws) > 0 {
		stats.LongestCurrentHold = max(stats.LongestCurrentHold, time.Since(b.currentSince))
	}
	return stats
}

func NewBridge(p *Producer, initial *scene.Document, limits retained.ResidencyLimits, budget retained.Budget) (*Bridge, error) {
	if p == nil || initial == nil || initial.Camera == nil || p.limits.Leases < 4 || len(initial.Scene.Meshes)+len(initial.Scene.Textures)+len(initial.Scene.Draws) != 0 || len(initial.TileSpaces) > 1 || len(initial.Transforms) > 1 {
		return nil, ErrInput
	}
	if err := initial.Validate(); err != nil {
		return nil, err
	}
	w, err := retained.NewSettlingWorkerWithData[*scene.Document](limits, budget)
	if err != nil {
		return nil, err
	}
	b := &Bridge{producer: p, worker: w, initial: initial, held: make(map[*scene.Document]*Lease), busy: true, started: time.Now(),
		stop: make(chan struct{}), done: make(chan struct{})}
	b.latest.Store(initial)
	go b.run()
	return b, nil
}

func (b *Bridge) Worker() *retained.WorkerWithData[*scene.Document] { return b.worker }
func (b *Bridge) Target() *scene.Document                           { return b.latest.Load() }

// Ready includes receipt of the producer's final request publication. It does not
// imply the producer has finished its loads; additionally inspect Status.Pending.
func (b *Bridge) Ready(revision uint64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.busy && !b.receiving && b.pending == nil && b.revision >= revision
}

func (b *Bridge) Restarted(generation uint64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.producer.ResetCurrent(generation) {
		return false
	}
	b.generation, b.consumed, b.completed, b.busy = generation, 0, 0, true
	b.advance = false
	b.observeCurrent(nil, nil)
	return true
}

// Consumed reports only Packet.Current, before executing that packet's batch.
// The producer copies coverage; it does not treat the batch as completed.
func (b *Bridge) Consumed(packet retained.PacketWithData[*scene.Document]) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if packet.Generation != b.generation || packet.Sequence <= b.consumed {
		return false
	}
	l := b.held[packet.CurrentData]
	if packet.CurrentData != nil && packet.CurrentData != b.initial && l == nil {
		return false
	}
	if !b.producer.Current(packet.Generation, packet.Sequence, l) {
		return false
	}
	b.consumed = packet.Sequence
	b.observeCurrent(packet.CurrentData, l)
	if b.stats.FirstVisibleCurrent == 0 && packet.CurrentData != nil && len(packet.CurrentData.Scene.Draws) > 0 {
		b.stats.FirstVisibleCurrent = time.Since(b.started)
		b.stats.FirstCurrentTiles = b.stats.CurrentTiles
		b.stats.FirstCurrentLabels = packet.CurrentData.Labels
	}
	return true
}

func (b *Bridge) observeCurrent(document *scene.Document, lease *Lease) {
	if document == b.current {
		return
	}
	now := time.Now()
	if b.current != nil && len(b.current.Scene.Draws) > 0 {
		b.stats.LongestCurrentHold = max(b.stats.LongestCurrentHold, now.Sub(b.currentSince))
	}
	b.current, b.currentSince = document, now
	b.stats.CurrentTiles, b.stats.CurrentFallbacks = 0, 0
	if lease != nil {
		b.stats.CurrentTiles, b.stats.CurrentFallbacks = len(lease.Snapshot.Cover), lease.Snapshot.Fallbacks
	}
	if document != nil && len(document.Scene.Draws) > 0 {
		b.stats.CurrentChanges++
	}
}

// Completed follows successful enqueueing of the native acknowledgement, after
// the adapter has selected Current and dropped all older CPU scene/batch borrows.
// Even nil-batch packets require that selection boundary and, after reset, the
// previous-namespace drains. GPU residency still retires separately through batches.
// A matching TargetData proves the Worker has processed our sole submitted target;
// no older target remains queued. Only then may another pending target be exposed.
func (b *Bridge) Completed(packet retained.PacketWithData[*scene.Document], success bool) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if packet.Generation != b.generation || packet.Sequence != b.consumed || packet.Sequence <= b.completed {
		return false
	}
	b.completed = packet.Sequence
	if success && packet.Err == nil {
		b.recordCompletion(packet)
	}
	if !success || packet.Err != nil || packet.TargetData != b.latest.Load() {
		return true
	}
	for document, lease := range b.held {
		if document != packet.CurrentData && document != packet.TargetData && document != b.pending {
			lease.Release()
			delete(b.held, document)
		}
	}
	// Establish initial continuity before allowing supersession. Otherwise a
	// moving camera can repeatedly abandon the first cover and stay blank until
	// motion ends. Once Current exists, unfinished replacements may be skipped.
	b.advance = packet.Settled || (packet.CurrentData != nil && packet.CurrentData != b.initial)
	b.busy = !packet.Settled
	return true
}

func (b *Bridge) recordCompletion(packet retained.PacketWithData[*scene.Document]) {
	if packet.Settled {
		b.stats.Settlements++
	}
	if batch := packet.Batch; batch != nil {
		if len(batch.Uploads) > 0 {
			b.stats.UploadBatches++
			b.stats.UploadBytes += batch.Bytes
			if b.pending != nil || packet.TargetData != b.latest.Load() {
				b.stats.OvertakenUploadBytes += batch.Bytes
			}
		} else {
			b.stats.ReleaseBatches++
			b.stats.ReleasedResources += uint64(len(batch.Releases))
		}
	}
}

// Caller holds mu. Expose only one unconfirmed target at a time. The producer's
// fixed lease pool also covers the former target until its replacement is observed.
func (b *Bridge) promotePending() {
	if b.advance && b.pending != nil {
		b.stats.Targets++
		if b.busy {
			b.stats.Superseded++
		}
		b.latest.Store(b.pending)
		b.pending, b.busy, b.advance = nil, true, false
	}
}

func (b *Bridge) run() {
	defer close(b.done)
	ticker := time.NewTicker(8 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-b.stop:
			return
		case <-ticker.C:
			b.mu.Lock()
			b.receiving = true
			b.mu.Unlock()
			if lease, ok := b.producer.Next(); ok {
				b.receive(lease)
			}
			b.mu.Lock()
			b.receiving = false
			b.promotePending()
			b.mu.Unlock()
		}
	}
}

func (b *Bridge) receive(lease *Lease) {
	// Metadata and draw counting stay on this background owner, not GUI/render.
	document := &scene.Document{Scene: *lease.Snapshot.Scene, TileSpaces: lease.Snapshot.TileSpaces,
		Width: b.initial.Width, Height: b.initial.Height, Camera: b.initial.Camera}
	for _, draw := range document.Scene.Draws {
		if draw.Material.Kind == scene.SDFFill {
			document.Labels++
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stats.Received++
	b.revision = max(b.revision, lease.Revision)
	if b.latest.Load() == b.initial && len(lease.Snapshot.Cover) == 0 {
		lease.Release()
		return
	}
	if b.pending != nil {
		b.stats.Coalesced++
		b.held[b.pending].Release()
		delete(b.held, b.pending)
		b.pending = nil
	}
	if current := b.held[b.latest.Load()]; current != nil && current.Snapshot == lease.Snapshot {
		b.stats.SameSnapshot++
		lease.Release()
		return
	}
	b.held[document], b.pending = lease, document
}

// Close joins outside GUI/render callbacks, after native use of documents and
// packets has stopped. Closing a Worker alone is not proof of native destruction.
// The bridge owns both its Worker and Producer and drops leases only after joins.
func (b *Bridge) Close() {
	b.once.Do(func() {
		close(b.stop)
		<-b.done
		b.worker.Close()
		b.producer.Close()
		<-b.worker.Done()
		<-b.producer.Done()
		b.mu.Lock()
		defer b.mu.Unlock()
		b.observeCurrent(nil, nil)
		for document, lease := range b.held {
			lease.Release()
			delete(b.held, document)
		}
		b.pending = nil
		b.latest.Store(nil)
	})
}
