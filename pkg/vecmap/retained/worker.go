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
type Packet struct {
	Generation, Sequence uint64
	Current              *scene.Scene
	Batch                *Batch
	Err                  error
}

type workerTarget struct {
	generation uint64
	scene      *scene.Scene
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
type Worker struct {
	mu         sync.Mutex
	generation uint64
	latest     *workerTarget
	closed     bool
	wake       chan struct{}
	stop, done chan struct{}
	packets    chan Packet
	acks       chan workerAck
	limits     ResidencyLimits
	budget     Budget
}

func NewWorker(limits ResidencyLimits, budget Budget) (*Worker, error) {
	p, err := NewPlanner(limits)
	if err != nil {
		return nil, err
	}
	if _, err := p.Next(budget); err != nil {
		return nil, err
	}
	w := &Worker{wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{}), packets: make(chan Packet, 1), acks: make(chan workerAck, 1), limits: p.limits, budget: budget}
	go w.run()
	return w, nil
}

// Restart begins a fresh native residency namespace. The adapter must dispose of
// the old namespace first; this call cannot cancel GPU commands. It coalesces reset
// requests, returns a fresh generation, and invalidates old packets/acks. Native
// consumers must discard packets whose generation differs from the returned value.
func (w *Worker) Restart(target *scene.Scene) (uint64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, ErrClosed
	}
	if w.generation == ^uint64(0) {
		return 0, ErrLimit
	}
	w.generation++
	w.latest = &workerTarget{w.generation, target}
	w.signal()
	return w.generation, nil
}

// SetTarget keeps only the newest immutable target in this generation. Validation
// errors arrive asynchronously in Packet.Err. Same-generation supersession waits
// for the outstanding packet's acknowledgement. Nil clears the active scene.
func (w *Worker) SetTarget(generation uint64, target *scene.Scene) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || generation == 0 || generation != w.generation {
		return false
	}
	w.latest = &workerTarget{generation, target}
	w.signal()
	return true
}

func (w *Worker) signal() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *Worker) takeTarget(generation uint64, busy bool) *workerTarget {
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
func (w *Worker) Next() (Packet, bool) {
	select {
	case p := <-w.packets:
		return p, true
	default:
		return Packet{}, false
	}
}

// Acknowledge never waits. False means no message was accepted (closed/full).
// There should be exactly one result per consumed packet. Do not drop a required
// acknowledgement on false: retry later or reset the native namespace and Restart.
// Old-generation and stale/duplicate sequence messages cannot commit new work.
func (w *Worker) Acknowledge(generation, sequence uint64, success bool) bool {
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
func (w *Worker) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.closed {
		w.closed = true
		w.latest = nil
		close(w.stop)
	}
}

func (w *Worker) Done() <-chan struct{} { return w.done }

func (w *Worker) run() {
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
	s := workerState{}
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
		var output chan Packet
		var packet Packet
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

type workerState struct {
	planner              *Planner
	generation, sequence uint64
	ticket               uint64
	desired              *workerTarget
	out, waiting         *Packet
	lastCurrent          *scene.Scene
	publish, blocked     bool
}

func (s *workerState) target(w *Worker, target *workerTarget) {
	if target.generation != s.generation {
		p, _ := NewPlanner(w.limits) // checked once by NewWorker
		*s = workerState{planner: p, generation: target.generation}
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

func (s *workerState) advance(budget Budget) bool {
	if s.planner == nil || s.out != nil || s.waiting != nil {
		return true
	}
	var err error
	if s.desired != nil {
		err = s.planner.SetTarget(s.desired.scene)
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
	if batch == nil && err == nil && !s.publish && current == s.lastCurrent {
		return true
	}
	s.sequence++
	if s.sequence == 0 {
		return false
	}
	s.out = &Packet{Generation: s.generation, Sequence: s.sequence, Current: current, Batch: batch, Err: err}
	if batch != nil {
		s.ticket = batch.Ticket
	}
	s.lastCurrent, s.publish, s.blocked = current, false, err != nil
	return true
}

func (s *workerState) acknowledge(ack workerAck) {
	if s.waiting == nil || ack.generation != s.generation || ack.sequence != s.waiting.Sequence {
		return
	}
	if s.ticket != 0 {
		// The private outstanding ticket came directly from this Planner.
		_ = s.planner.Acknowledge(s.ticket, ack.success)
	}
	s.waiting, s.ticket = nil, 0
}
