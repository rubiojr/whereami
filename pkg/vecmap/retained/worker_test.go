package retained

import (
	"sync"
	"testing"
	"time"

	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func uploadWorker(t *testing.T, budget Budget) *Worker {
	t.Helper()
	w, err := NewWorker(ResidencyLimits{}, budget)
	require.NoError(t, err)
	t.Cleanup(func() {
		w.Close()
		select {
		case <-w.Done():
		case <-time.After(5 * time.Second):
			t.Error("worker did not stop")
		}
	})
	return w
}

func workerPacket(t *testing.T, w *Worker, generation uint64) Packet {
	t.Helper()
	var result Packet
	require.Eventually(t, func() bool {
		packet, ok := w.Next()
		if ok && packet.Generation == generation {
			result = packet
			return true
		}
		return false
	}, 5*time.Second, time.Millisecond)
	require.NotZero(t, result.Sequence)
	return result
}

func workerAckPacket(t *testing.T, w *Worker, p Packet, success bool) {
	t.Helper()
	require.True(t, w.Acknowledge(p.Generation, p.Sequence, success))
}

func TestWorkerTransitions(t *testing.T) {
	store := newStore(t, Limits{})
	require.NoError(t, store.Apply([]Change{{"a", triangle()}}))
	first := snapshot(t, store, "a")
	w := uploadWorker(t, Budget{Bytes: 84, Resources: 1})
	gen, err := w.Restart(first)
	require.NoError(t, err)
	p := workerPacket(t, w, gen)
	require.Len(t, p.Batch.Uploads, 1)
	assert.Nil(t, p.Current)
	workerAckPacket(t, w, p, false)
	retry := workerPacket(t, w, gen)
	assert.Equal(t, p.Batch.Uploads, retry.Batch.Uploads)
	assert.NotEqual(t, p.Sequence, retry.Sequence)
	workerAckPacket(t, w, retry, true)
	p = workerPacket(t, w, gen)
	assert.Equal(t, TextureResource, p.Batch.Uploads[0].Version.Kind)
	assert.Nil(t, p.Current)
	workerAckPacket(t, w, p, true)
	p = workerPacket(t, w, gen)
	assert.Nil(t, p.Batch)
	assert.Same(t, first, p.Current)
	workerAckPacket(t, w, p, true)
	// Only the newest target is retained while a resource batch is outstanding.
	require.NoError(t, store.Apply([]Change{{"a", variant(2)}}))
	second := snapshot(t, store, "a")
	require.True(t, w.SetTarget(gen, second))
	p = workerPacket(t, w, gen)
	assert.Same(t, first, p.Current)
	require.NoError(t, store.Apply([]Change{{"a", variant(3)}}))
	third := snapshot(t, store, "a")
	for range 100 {
		require.True(t, w.SetTarget(gen, second))
	}
	require.True(t, w.SetTarget(gen, third))
	workerAckPacket(t, w, p, true)
	for range 2 {
		p = workerPacket(t, w, gen)
		assert.Same(t, first, p.Current)
		assert.Empty(t, p.Batch.Releases, "uploads for the newest target precede retirement while residency fits")
		assert.LessOrEqual(t, p.Batch.Bytes, uint64(84))
		require.Len(t, p.Batch.Uploads, 1)
		workerAckPacket(t, w, p, true)
	}
	for i := range 3 {
		p = workerPacket(t, w, gen)
		assert.Same(t, third, p.Current, "publication must accompany retirement")
		require.Len(t, p.Batch.Releases, 1)
		if i == 2 {
			assert.Equal(t, second.Meshes[0].Revision, p.Batch.Releases[0].Revision, "the abandoned partial revision retires in admission order")
		}
		workerAckPacket(t, w, p, true)
	}
	require.True(t, w.SetTarget(gen, nil))
	for range 2 {
		p = workerPacket(t, w, gen)
		assert.Nil(t, p.Current)
		require.Len(t, p.Batch.Releases, 1)
		workerAckPacket(t, w, p, true)
	}
}

func TestWorkerRestartAndErrors(t *testing.T) {
	store := newStore(t, Limits{})
	require.NoError(t, store.Apply([]Change{{"a", triangle()}}))
	source := snapshot(t, store, "a")
	w := uploadWorker(t, Budget{Bytes: 84, Resources: 1})
	gen, err := w.Restart(source)
	require.NoError(t, err)
	old := workerPacket(t, w, gen)
	gen, err = w.Restart(source) // no acknowledgement from the lost native namespace
	require.NoError(t, err)
	workerAckPacket(t, w, old, true)
	p := workerPacket(t, w, gen)
	assert.Nil(t, p.Current)
	assert.Equal(t, old.Batch.Uploads, p.Batch.Uploads, "fresh namespace reuploads ready bits")
	assert.False(t, w.SetTarget(old.Generation, source))
	workerAckPacket(t, w, p, true)
	// Restart can interrupt a pending publication too. Invalid target errors are
	// asynchronous and do not spin the worker until a fresh target is submitted.
	invalid := &scene.Scene{Meshes: []scene.Mesh{{ID: 1}}}
	gen, err = w.Restart(invalid)
	require.NoError(t, err)
	p = workerPacket(t, w, gen)
	require.Error(t, p.Err)
	assert.Nil(t, p.Batch)
	workerAckPacket(t, w, p, false)
	require.True(t, w.SetTarget(gen, &scene.Scene{}))
	p = workerPacket(t, w, gen)
	require.NoError(t, p.Err)
	assert.NotNil(t, p.Current)
	assert.Nil(t, p.Batch)
	workerAckPacket(t, w, p, true)
	tooSmall := uploadWorker(t, Budget{Bytes: 1, Resources: 1})
	g, err := tooSmall.Restart(source)
	require.NoError(t, err)
	p = workerPacket(t, tooSmall, g)
	assert.ErrorIs(t, p.Err, ErrBudget)
}

func TestWorkerShutdownAndBounds(t *testing.T) {
	_, err := NewWorker(ResidencyLimits{Resources: -1}, Budget{Bytes: 1, Resources: 1})
	assert.ErrorIs(t, err, ErrLimit)
	_, err = NewWorker(ResidencyLimits{}, Budget{})
	assert.ErrorIs(t, err, ErrBudget)
	w := uploadWorker(t, Budget{Bytes: 100, Resources: 1})
	gen, err := w.Restart(&scene.Scene{})
	require.NoError(t, err)
	var producers sync.WaitGroup
	for range 4 {
		producers.Go(func() {
			for range 100 {
				w.SetTarget(gen, &scene.Scene{})
			}
		})
	}
	producers.Wait()
	assert.Equal(t, 1, cap(w.packets))
	assert.Equal(t, 1, cap(w.acks))
	workerPacket(t, w, gen) // cancellation must not wait for native acknowledgement
	w.Close()
	<-w.Done()
	w.Close()
	_, ok := w.Next()
	assert.False(t, ok)
	assert.False(t, w.Acknowledge(gen, 1, true))
	assert.False(t, w.SetTarget(gen, nil))
	_, err = w.Restart(nil)
	assert.ErrorIs(t, err, ErrClosed)
	exhausted := uploadWorker(t, Budget{Bytes: 100, Resources: 1})
	exhausted.mu.Lock()
	exhausted.generation = ^uint64(0)
	exhausted.mu.Unlock()
	_, err = exhausted.Restart(nil)
	assert.ErrorIs(t, err, ErrLimit)
}

func TestWorkerPrivateProtocol(t *testing.T) {
	// Deterministic stale/full-mailbox checks without racing the worker's drain.
	w := &Worker{limits: ResidencyLimits{}, packets: make(chan Packet, 1), acks: make(chan workerAck, 1), stop: make(chan struct{})}
	require.True(t, w.Acknowledge(1, 1, true))
	assert.False(t, w.Acknowledge(1, 2, true))
	s := workerState[struct{}]{}
	assert.True(t, s.advance(Budget{}))
	s.acknowledge(workerAck{})
	w.packets <- Packet{Generation: 1}
	s.target(w, &workerTarget[struct{}]{generation: 2, scene: &scene.Scene{}})
	assert.Empty(t, w.packets)
	assert.True(t, s.advance(Budget{Bytes: 100, Resources: 1}))
	s.waiting, s.out = s.out, nil
	s.acknowledge(workerAck{generation: 1, sequence: s.waiting.Sequence, success: true})
	require.NotNil(t, s.waiting)
	s.acknowledge(workerAck{generation: 2, sequence: s.waiting.Sequence + 1, success: true})
	require.NotNil(t, s.waiting)
	s.acknowledge(workerAck{generation: 2, sequence: s.waiting.Sequence, success: true})
	assert.Nil(t, s.waiting)
	assert.True(t, s.advance(Budget{Bytes: 100, Resources: 1}))
	assert.Nil(t, s.out, "idle publication must not spin")
	s.blocked = true
	assert.True(t, s.advance(Budget{}))
	s.blocked, s.publish, s.sequence = false, true, ^uint64(0)
	assert.False(t, s.advance(Budget{Bytes: 100, Resources: 1}), "sequence exhaustion stops rather than reusing a token")
}
