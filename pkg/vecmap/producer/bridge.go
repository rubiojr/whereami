package producer

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
)

// Bridge serializes accepted targets through native retirement while coalescing
// newer producer targets. It retains at most Current, an uploading target and one
// pending document, plus the producer's output slot. Camera requests continue
// independently. The renderer must call Restarted, Consumed and Completed in the
// Worker/native packet protocol; no producer lease is exposed to the renderer.
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
	receiving                                 bool
	generation, consumed, completed, revision uint64
	stop, done                                chan struct{}
	once                                      sync.Once
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
	b := &Bridge{producer: p, worker: w, initial: initial, held: make(map[*scene.Document]*Lease), busy: true,
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
	return true
}

// Completed follows successful enqueueing of the native acknowledgement. Only
// an explicitly settled packet for the serial target permits old lease release.
// A reset retains every held lease until the new namespace settles (including
// its adapter's previous-namespace drains). No timer or readiness guess retires it.
func (b *Bridge) Completed(packet retained.PacketWithData[*scene.Document], success bool) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if packet.Generation != b.generation || packet.Sequence != b.consumed || packet.Sequence <= b.completed {
		return false
	}
	b.completed = packet.Sequence
	if !success || packet.Err != nil || !packet.Settled || packet.Batch != nil || packet.CurrentData != b.latest.Load() {
		return true
	}
	for document, lease := range b.held {
		if document != packet.CurrentData && document != b.pending {
			lease.Release()
			delete(b.held, document)
		}
	}
	b.busy = false
	return true
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
			if !b.busy && b.pending != nil {
				b.latest.Store(b.pending)
				b.pending, b.busy = nil, true
			}
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
	b.revision = max(b.revision, lease.Revision)
	if b.latest.Load() == b.initial && len(lease.Snapshot.Cover) == 0 {
		lease.Release()
		return
	}
	if b.pending != nil {
		b.held[b.pending].Release()
		delete(b.held, b.pending)
		b.pending = nil
	}
	if current := b.held[b.latest.Load()]; current != nil && current.Snapshot == lease.Snapshot {
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
		for document, lease := range b.held {
			lease.Release()
			delete(b.held, document)
		}
		b.pending = nil
		b.latest.Store(nil)
	})
}
