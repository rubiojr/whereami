package retained

import (
	"errors"
	"sync"

	"github.com/rubiojr/whereami/pkg/vecmap/scene"
)

var ErrClosed = errors.New("upload worker closed")

// Packet couples native work with the scene that must be selected before that
// work executes. A packet with no Batch still requires acknowledgement after its
// Current has been consumed. Err reports a target/budget error and pauses planning
// until another target or generation is supplied. All scene payloads are borrowed.
type Packet = PacketWithData[struct{}]

// PacketWithData carries CurrentData belonging to Current, never to an unfinished
// target. Data is an immutable borrow owned and validated by the producer.
type PacketWithData[T any] struct {
	Generation, Sequence uint64
	Current              *scene.Scene
	CurrentData          T
	// TargetData identifies the accepted target used to plan this packet, not
	// the concurrently writable latest-target slot. Together with CurrentData
	// it covers this packet's and Planner's CPU payload borrows. Native/caller
	// borrows and queued target inputs still require their own lifetime proof.
	TargetData T
	Batch      *Batch
	Err        error
	// Settled means this packet has no batch/error and the accepted target is
	// Current, with all obsolete residency already acknowledged released. It is
	// not a GPU fence. The packet itself still requires consumption and ack.
	Settled bool
}

type workerTarget[T any] struct {
	generation uint64
	scene      *scene.Scene
	data       T
}
type workerAck struct {
	generation, sequence uint64
	success              bool
}

// Worker is a bounded asynchronous owner of a Planner. It has one goroutine, one
// latest-target slot, one output slot and one acknowledgement slot. Restart does
// not spawn a goroutine. Methods are concurrent-safe; no caller performs payload
// validation. The short input mutex never protects validation or native work.
// Workers must not be copied. Close and wait for Done when no longer needed.
type Worker = WorkerWithData[struct{}]

// WorkerWithData pairs scene publication with immutable producer data, such as
// transform-slot mappings. It has the same bounded queues and lifetime as Worker;
// it neither interprets nor validates data. Data must stay immutable through all
// queued packets and native borrows. Use RestartWithData and SetTargetWithData.
type WorkerWithData[T any] struct {
	mu          sync.Mutex
	generation  uint64
	latest      *workerTarget[T]
	closed      bool
	wake        chan struct{}
	stop, done  chan struct{}
	packets     chan PacketWithData[T]
	acks        chan workerAck
	limits      ResidencyLimits
	budget      Budget
	emitSettled bool
}

func NewWorker(limits ResidencyLimits, budget Budget) (*Worker, error) {
	return NewWorkerWithData[struct{}](limits, budget)
}

func NewWorkerWithData[T any](limits ResidencyLimits, budget Budget) (*WorkerWithData[T], error) {
	return newWorkerWithData[T](limits, budget, false)
}

// NewSettlingWorkerWithData additionally publishes an acknowledged nil-batch
// packet when retirement settles. A serial-target owner can use this boundary
// to release old CPU leases after native retirement. Ordinary workers retain
// their existing packet cadence. New targets still coalesce under the same rules.
func NewSettlingWorkerWithData[T any](limits ResidencyLimits, budget Budget) (*WorkerWithData[T], error) {
	return newWorkerWithData[T](limits, budget, true)
}

func newWorkerWithData[T any](limits ResidencyLimits, budget Budget, settling bool) (*WorkerWithData[T], error) {
	p, err := NewPlanner(limits)
	if err != nil {
		return nil, err
	}
	if _, err := p.Next(budget); err != nil {
		return nil, err
	}
	w := &WorkerWithData[T]{wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{}), packets: make(chan PacketWithData[T], 1), acks: make(chan workerAck, 1), limits: p.limits, budget: budget}
	w.emitSettled = settling
	go w.run()
	return w, nil
}

// Restart begins a fresh native residency namespace. The adapter must dispose of
// the old namespace first; this call cannot cancel GPU commands. It coalesces reset
// requests, returns a fresh generation, and invalidates old packets/acks. Native
// consumers must discard packets whose generation differs from the returned value.
func (w *WorkerWithData[T]) Restart(target *scene.Scene) (uint64, error) {
	var zero T
	return w.RestartWithData(target, zero)
}

// RestartWithData is Restart with producer data paired to the target. Nil target
// clears both scene and data; a reset never retains an old generation's data.
func (w *WorkerWithData[T]) RestartWithData(target *scene.Scene, data T) (uint64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, ErrClosed
	}
	if w.generation == ^uint64(0) {
		return 0, ErrLimit
	}
	w.generation++
	w.latest = newWorkerTarget(w.generation, target, data)
	w.signal()
	return w.generation, nil
}

// SetTarget keeps only the newest immutable target in this generation. Validation
// errors arrive asynchronously in Packet.Err. Same-generation supersession waits
// for the outstanding packet's acknowledgement. Nil clears the active scene.
func (w *WorkerWithData[T]) SetTarget(generation uint64, target *scene.Scene) bool {
	var zero T
	return w.SetTargetWithData(generation, target, zero)
}

// SetTargetWithData coalesces scene and data atomically. Replacing only data for
// the same scene still publishes an acknowledged packet, without resource uploads.
func (w *WorkerWithData[T]) SetTargetWithData(generation uint64, target *scene.Scene, data T) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || generation == 0 || generation != w.generation {
		return false
	}
	w.latest = newWorkerTarget(generation, target, data)
	w.signal()
	return true
}

func newWorkerTarget[T any](generation uint64, target *scene.Scene, data T) *workerTarget[T] {
	if target == nil {
		var zero T
		data = zero
	}
	return &workerTarget[T]{generation: generation, scene: target, data: data}
}

func (w *WorkerWithData[T]) signal() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *WorkerWithData[T]) takeTarget(generation uint64, busy bool) *workerTarget[T] {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.latest != nil && busy && w.latest.generation == generation {
		return nil // retain exactly one coalescing slot until acknowledgement
	}
	target := w.latest
	w.latest = nil
	return target
}

// Next polls without waiting. Returned descriptors are owned by the consumer;
// callers must obey Planner's immutable payload and all-or-nothing batch contract.
func (w *WorkerWithData[T]) Next() (PacketWithData[T], bool) {
	select {
	case p := <-w.packets:
		return p, true
	default:
		return PacketWithData[T]{}, false
	}
}

// Acknowledge never waits. False means no message was accepted (closed/full).
// There should be exactly one result per consumed packet. Do not drop a required
// acknowledgement on false: retry later or reset the native namespace and Restart.
// Old-generation and stale/duplicate sequence messages cannot commit new work.
func (w *WorkerWithData[T]) Acknowledge(generation, sequence uint64, success bool) bool {
	select {
	case <-w.stop:
		return false
	default:
	}
	select {
	case w.acks <- workerAck{generation, sequence, success}:
		return true
	default:
		return false
	}
}

// Close cancels queued work without waiting for the bounded validation currently
// in progress. Done closes when the goroutine exits and queued borrows are dropped.
// Stop native use separately before releasing CPU payloads borrowed by the adapter.
func (w *WorkerWithData[T]) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.closed {
		w.closed = true
		w.latest = nil
		close(w.stop)
	}
}

func (w *WorkerWithData[T]) Done() <-chan struct{} { return w.done }

func (w *WorkerWithData[T]) run() {
	defer close(w.done)
	defer func() {
		w.Close()
		for {
			select {
			case <-w.packets:
			default:
				return
			}
		}
	}()
	s := workerState[T]{}
	for {
		select {
		case <-w.stop:
			return
		default:
		}
		// Give an already queued reset/target precedence over further planning.
		if target := w.takeTarget(s.generation, s.out != nil || s.waiting != nil); target != nil {
			s.target(w, target)
		}
		if !s.advance(w.budget) {
			return
		} // sequence exhaustion: reset required
		var output chan PacketWithData[T]
		var packet PacketWithData[T]
		if s.out != nil {
			output, packet = w.packets, *s.out
		}
		select {
		case <-w.stop:
			return
		case <-w.wake:
		case ack := <-w.acks:
			s.acknowledge(ack)
		case output <- packet:
			s.waiting, s.out = s.out, nil
		}
	}
}

type workerState[T any] struct {
	planner                  *Planner
	generation, sequence     uint64
	ticket                   uint64
	desired, accepted        *workerTarget[T]
	currentData              T
	out, waiting             *PacketWithData[T]
	lastCurrent              *scene.Scene
	publish, blocked         bool
	emitSettled, lastSettled bool
}

func (s *workerState[T]) target(w *WorkerWithData[T], target *workerTarget[T]) {
	if target.generation != s.generation {
		p, _ := NewPlanner(w.limits) // checked once by NewWorker
		*s = workerState[T]{planner: p, generation: target.generation, emitSettled: w.emitSettled}
		// Drop a queued old packet. A concurrently consumed one is rejected by
		// the native owner's generation check instead.
		select {
		case <-w.packets:
		default:
		}
		select {
		case <-w.acks:
		default:
		}
	}
	s.desired = target
}

func (s *workerState[T]) advance(budget Budget) bool {
	if s.planner == nil || s.out != nil || s.waiting != nil {
		return true
	}
	var err error
	if s.desired != nil {
		err = s.planner.SetTarget(s.desired.scene)
		if err == nil {
			s.accepted = s.desired
			s.selectData()
		}
		s.desired, s.blocked, s.publish = nil, false, true
	}
	if s.blocked {
		return true
	}
	var batch *Batch
	if err == nil {
		batch, err = s.planner.Next(budget)
	}
	current := s.planner.Current()
	settled := batch == nil && err == nil
	if settled && !s.publish && current == s.lastCurrent && (!s.emitSettled || s.lastSettled) {
		return true
	}
	s.sequence++
	if s.sequence == 0 {
		return false
	}
	s.out = &PacketWithData[T]{Generation: s.generation, Sequence: s.sequence, Current: current, CurrentData: s.currentData, Batch: batch, Err: err, Settled: settled}
	if s.accepted != nil {
		s.out.TargetData = s.accepted.data
	}
	s.lastSettled = settled
	if batch != nil {
		s.ticket = batch.Ticket
	}
	s.lastCurrent, s.publish, s.blocked = current, false, err != nil
	return true
}

func (s *workerState[T]) acknowledge(ack workerAck) {
	if s.waiting == nil || ack.generation != s.generation || ack.sequence != s.waiting.Sequence {
		return
	}
	if s.ticket != 0 {
		// The private outstanding ticket came directly from this Planner.
		_ = s.planner.Acknowledge(s.ticket, ack.success)
		s.selectData()
	}
	s.waiting, s.ticket = nil, 0
}

func (s *workerState[T]) selectData() {
	if s.accepted != nil && s.planner.Current() == s.accepted.scene {
		s.currentData = s.accepted.data
	}
}
