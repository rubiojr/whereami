package producer

import (
	"testing"
	"time"

	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
	"github.com/rubiojr/whereami/pkg/vecmap/tiles"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBridgeCoalescingRetirementAndReset(t *testing.T) {
	limits := DefaultLimits()
	limits.Workers = 1
	limits.RawBytes = 128
	p, c := newControlled(t, limits)
	r := testRequest(t, view.TileID{})
	initial := &scene.Document{Width: 256, Height: 256, Camera: &r.Camera, TileSpaces: []scene.TileSpace{{Tile: view.TileID{}}}}
	b, err := NewBridge(p, initial, retained.ResidencyLimits{}, retained.Budget{Bytes: 1 << 20, Resources: 1})
	require.NoError(t, err)
	t.Cleanup(func() { c.close(); b.Close() })
	w := b.Worker()
	gen, err := w.RestartWithData(&initial.Scene, initial)
	require.NoError(t, err)
	require.True(t, b.Restarted(gen))
	next := func() retained.PacketWithData[*scene.Document] {
		var packet retained.PacketWithData[*scene.Document]
		require.Eventually(t, func() bool { var ok bool; packet, ok = w.Next(); return ok }, 5*time.Second, time.Millisecond)
		require.True(t, b.Consumed(packet))
		require.NoError(t, packet.Err)
		return packet
	}
	ack := func(packet retained.PacketWithData[*scene.Document]) {
		require.True(t, w.Acknowledge(packet.Generation, packet.Sequence, true))
		require.True(t, b.Completed(packet, true))
	}
	ack(next())
	rev, err := p.Submit(r)
	require.NoError(t, err)
	nextCall(t, c).reply <- answer{data: tilePBF()}
	require.Eventually(t, func() bool { return b.Target() != initial }, 5*time.Second, time.Millisecond)
	first := b.Target()
	require.True(t, w.SetTargetWithData(gen, &first.Scene, first))
	for {
		packet := next()
		ack(packet)
		if packet.Settled {
			break
		}
	}
	// The first document becoming Current changes the producer's continuity
	// cover, so it may select once more after Settled; the bridge drops that
	// identical snapshot.
	require.Eventually(t, func() bool {
		s := p.Status()
		return s.Revision == rev && s.Pending == 0 && s.Failed == 0 && b.Ready(rev)
	}, 5*time.Second, time.Millisecond)
	for range 1000 {
		b.progressed()
		require.True(t, b.Ready(rev), "a wakeup that finds no document leaves Ready alone")
	}
	progress := b.Stats()
	assert.Equal(t, uint64(1), progress.CurrentChanges)
	assert.Equal(t, 1, progress.FirstCurrentTiles)
	assert.Equal(t, 1, progress.CurrentTiles)
	assert.Zero(t, progress.CurrentFallbacks)
	assert.Positive(t, progress.FirstVisibleCurrent)
	assert.Positive(t, progress.LongestCurrentHold)
	b.mu.Lock()
	firstLease := b.held[first]
	b.mu.Unlock()
	// Start a style replacement whose geometry differs (a second background quad;
	// a zoom-only change would keep every resident version), then coalesce repeated
	// newer requests while its upload is unacknowledged. Current and accepted-target
	// leases remain held.
	layers, err := style.Parse([]byte(`{"version":8,"layers":[{"id":"background","type":"background","paint":{"background-color":"#ff0000"}},{"id":"tint","type":"background","paint":{"background-color":"#00ff00"}}]}`))
	require.NoError(t, err)
	updated := *r.Style
	updated.Epoch++
	updated.Layers = layers
	r.Style = &updated
	_, err = p.Submit(r)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return b.Target() != first }, 5*time.Second, time.Millisecond)
	second := b.Target()
	require.True(t, w.SetTargetWithData(gen, &second.Scene, second))
	upload := next()
	require.NotNil(t, upload.Batch)
	for range 10 {
		r.Camera.Bearing++
		rev, err = p.Submit(r)
		require.NoError(t, err)
	}
	waitStatus(t, p, func(s Status) bool { return s.Revision == rev })
	assert.Same(t, second, b.Target(), "supersession waits for the first checked packet boundary")
	p.mu.Lock()
	assert.False(t, firstLease.released)
	p.mu.Unlock()
	ack(upload)
	retirement := next()
	require.NotNil(t, retirement.Batch)
	require.NotEmpty(t, retirement.Batch.Releases)
	assert.False(t, retirement.Settled)
	p.mu.Lock()
	assert.False(t, firstLease.released)
	p.mu.Unlock()
	ack(retirement)
	settled := next()
	require.True(t, settled.Settled)
	ack(settled)
	p.mu.Lock()
	assert.True(t, firstLease.released)
	p.mu.Unlock()
	// Reset is not a lease release. A fresh namespace must settle first.
	b.mu.Lock()
	currentLease := b.held[second]
	b.mu.Unlock()
	gen, err = w.RestartWithData(&second.Scene, second)
	require.NoError(t, err)
	require.True(t, b.Restarted(gen))
	assert.False(t, b.Completed(settled, true))
	p.mu.Lock()
	assert.False(t, currentLease.released)
	p.mu.Unlock()
	for {
		packet := next()
		ack(packet)
		if packet.Settled {
			break
		}
	}
	assert.LessOrEqual(t, p.Status().PeakLeases, limits.Leases)
	// No native borrow remains in this fake backend. Close joins even if a
	// future Worker packet is waiting, then releases the final producer lease.
	c.close()
	b.Close()
	b.Close()
	assert.Zero(t, p.Status().Leases)
	assert.Zero(t, p.Status().LeaseBytes)
	p.mu.Lock()
	assert.True(t, currentLease.released)
	p.mu.Unlock()
}

// syntheticBridge drives the mailbox tick explicitly. Real Worker queues and
// acknowledgements run concurrently; synthetic leases isolate ordering from tile
// preparation. The captured corpus covers the real producer.
type syntheticBridge struct {
	t   *testing.T
	p   *Producer
	w   *retained.WorkerWithData[*scene.Document]
	b   *Bridge
	gen uint64
}

func newSyntheticBridge(t *testing.T, budget retained.Budget) *syntheticBridge {
	t.Helper()
	p := &Producer{leases: make(map[*Lease]struct{}), wake: make(chan struct{}, 1)}
	initial := &scene.Document{Width: 256, Height: 256}
	w, err := retained.NewSettlingWorkerWithData[*scene.Document](retained.ResidencyLimits{}, budget)
	require.NoError(t, err)
	t.Cleanup(func() { w.Close(); <-w.Done() })
	b := &Bridge{producer: p, worker: w, initial: initial, held: make(map[*scene.Document]*Lease), received: make(map[*scene.Document]time.Time), resident: make(map[retained.Version]bool), busy: true}
	b.latest.Store(initial)
	gen, err := w.RestartWithData(&initial.Scene, initial)
	require.NoError(t, err)
	require.True(t, b.Restarted(gen))
	return &syntheticBridge{t: t, p: p, w: w, b: b, gen: gen}
}

func (s *syntheticBridge) next() retained.PacketWithData[*scene.Document] {
	s.t.Helper()
	var packet retained.PacketWithData[*scene.Document]
	require.Eventually(s.t, func() bool { var ok bool; packet, ok = s.w.Next(); return ok }, 5*time.Second, time.Millisecond)
	require.True(s.t, s.b.Consumed(packet))
	return packet
}

func (s *syntheticBridge) ack(packet retained.PacketWithData[*scene.Document], success bool) {
	s.t.Helper()
	require.True(s.t, s.w.Acknowledge(s.gen, packet.Sequence, success))
	require.True(s.t, s.b.Completed(packet, success))
}

// receive hands the bridge a lease whose scene has count 1x1 textures at revision.
func (s *syntheticBridge) receive(revision uint64, count int) *Lease {
	s.t.Helper()
	sc := &scene.Scene{}
	for id := range count {
		sc.Textures = append(sc.Textures, scene.Texture{ID: uint64(id + 1), Revision: revision, Width: 1, Height: 1, RGBA: []byte{1, 2, 3, 255}})
	}
	require.NoError(s.t, sc.Validate())
	l := &Lease{p: s.p, Revision: revision, Snapshot: &tiles.Snapshot{Scene: sc, Cover: []view.TileID{{}}}}
	s.p.leases[l] = struct{}{}
	require.LessOrEqual(s.t, len(s.p.leases), 4)
	s.b.receive(l)
	return l
}

func (s *syntheticBridge) promote() *scene.Document {
	s.t.Helper()
	s.b.promotePending()
	d := s.b.Target()
	require.True(s.t, s.w.SetTargetWithData(s.gen, &d.Scene, d))
	return d
}

func (s *syntheticBridge) settle() {
	s.t.Helper()
	for {
		packet := s.next()
		require.NoError(s.t, packet.Err)
		s.ack(packet, true)
		if packet.Settled {
			return
		}
	}
}

func TestBridgeCommitsTargetOnceUploadsLand(t *testing.T) {
	const count = 2 // after B's first upload lands, a two-version C would need more than B's one
	s := newSyntheticBridge(t, retained.Budget{Bytes: 16, Resources: 1})
	b := s.b
	s.ack(s.next(), true)
	a := s.receive(1, count)
	docA := s.promote()
	for packet := s.next(); ; packet = s.next() {
		s.ack(packet, true)
		if packet.CurrentData == b.initial {
			assert.False(t, b.advance, "camera churn cannot starve the initial cover")
		}
		if packet.Settled {
			break
		}
	}
	assert.Equal(t, uint64(1), b.Stats().CurrentTargets)
	old := s.receive(2, count)
	docB := s.promote()
	first := s.next()
	require.Same(t, docA, first.CurrentData)
	require.NotEmpty(t, first.Batch.Uploads)
	s.receive(3, count)
	latest := s.receive(4, count) // coalesce a never-submitted target
	require.Len(t, s.p.leases, 3)
	b.promotePending()
	assert.Same(t, docB, b.Target(), "supersession waits for the first checked packet boundary")
	s.ack(first, false)
	b.promotePending()
	assert.Same(t, docB, b.Target(), "failed upload grants no supersession boundary")
	retry := s.next()
	require.NotEmpty(t, retry.Batch.Uploads)
	s.ack(retry, true)
	b.promotePending()
	assert.Same(t, docB, b.Target(), "a document needing more uploads than the target has left must wait")
	assert.False(t, old.released)
	assert.False(t, latest.released)
	// Finish B. The packet that reports B as Current already begins retiring A.
	var packet retained.PacketWithData[*scene.Document]
	for packet = s.next(); packet.CurrentData != docB; packet = s.next() {
		require.Same(t, docB, packet.TargetData)
		b.promotePending()
		assert.Same(t, docB, b.Target())
		s.ack(packet, true)
	}
	require.NotEmpty(t, packet.Batch.Releases, "A's obsolete versions retire before further uploads")
	// B is Current, so the next pending document may be exposed even before
	// B's packet is acknowledged. Submitting it now keeps the Worker's packet
	// order deterministic for the rest of this test.
	b.promotePending()
	docC := b.Target()
	require.NotSame(t, docB, docC, "a Current target releases the next pending document")
	require.True(t, s.w.SetTargetWithData(s.gen, &docC.Scene, docC))
	s.ack(packet, true)
	assert.False(t, a.released, "B's packet cannot prove C's handoff")
	assert.False(t, old.released)
	progress := b.Stats()
	assert.Zero(t, progress.Superseded)
	assert.Equal(t, uint64(3), progress.Targets)
	assert.Equal(t, uint64(2), progress.CurrentTargets)
	assert.Positive(t, progress.TotalCurrentAge)
	assert.GreaterOrEqual(t, progress.LongestCurrentAge, progress.TotalCurrentAge/2)
	// With residency headroom, C's first packet uploads instead of retiring A's
	// remaining version: the Planner no longer delays the newest target behind
	// retirement. A is still released only once C's handoff is proven.
	packet = s.next()
	require.Same(t, docC, packet.TargetData)
	require.NotEmpty(t, packet.Batch.Uploads)
	require.Empty(t, packet.Batch.Releases)
	newest := s.receive(5, count)
	s.ack(packet, true)
	assert.True(t, a.released, "A is neither Current, target nor pending once C's handoff is proven")
	assert.False(t, latest.released, "C stays held until its replacement's handoff is proven")
	b.promotePending()
	assert.Same(t, docC, b.Target(), "a landed upload commits C: the newer document needs more uploads than C has left")
	assert.Zero(t, b.Stats().Superseded)
	for packet = s.next(); packet.CurrentData != docC; packet = s.next() {
		require.Same(t, docC, packet.TargetData)
		s.ack(packet, true)
	}
	require.NotEmpty(t, packet.Batch.Releases, "A's remaining and B's versions retire once C is Current")
	b.promotePending()
	docD := b.Target()
	require.NotSame(t, docC, docD, "a Current target releases the next pending document")
	require.True(t, s.w.SetTargetWithData(s.gen, &docD.Scene, docD))
	s.ack(packet, true)
	s.settle()
	assert.True(t, b.Ready(5))
	assert.True(t, latest.released)
	assert.True(t, old.released)
	assert.False(t, newest.released)
	final := b.Stats()
	assert.Zero(t, final.Superseded)
	assert.Equal(t, uint64(1), final.Coalesced)
	assert.Equal(t, uint64(4), final.CurrentTargets)
	assert.Equal(t, uint64(4), final.Targets)
	assert.Zero(t, final.TargetErrors)
	s.w.Close()
	<-s.w.Done()
	newest.Release()
}

func TestBridgeTargetErrorAllowsReplacement(t *testing.T) {
	s := newSyntheticBridge(t, retained.Budget{Bytes: 16, Resources: 1})
	b := s.b
	s.ack(s.next(), true)
	oversized := &scene.Scene{Textures: []scene.Texture{{ID: 1, Revision: 1, Width: 3, Height: 3, RGBA: make([]byte, 36)}}}
	require.NoError(t, oversized.Validate())
	bad := &Lease{p: s.p, Revision: 1, Snapshot: &tiles.Snapshot{Scene: oversized, Cover: []view.TileID{{}}}}
	s.p.leases[bad] = struct{}{}
	b.receive(bad)
	docBad := s.promote()
	failed := s.next()
	require.ErrorIs(t, failed.Err, retained.ErrBudget)
	require.Same(t, docBad, failed.TargetData)
	good := s.receive(2, 1)
	b.promotePending()
	assert.Same(t, docBad, b.Target(), "an unacknowledged error packet is not a boundary")
	require.True(t, s.w.Acknowledge(s.gen, failed.Sequence, false))
	require.True(t, b.Completed(failed, false))
	b.promotePending()
	docGood := b.Target()
	require.NotSame(t, docBad, docGood, "an unplannable target must not pin the bridge")
	assert.Equal(t, uint64(1), b.Stats().TargetErrors)
	require.True(t, s.w.SetTargetWithData(s.gen, &docGood.Scene, docGood))
	s.settle()
	assert.True(t, bad.released)
	assert.False(t, good.released)
	assert.Equal(t, uint64(1), b.Stats().CurrentTargets)
	s.w.Close()
	<-s.w.Done()
	good.Release()
}

func TestBridgeSupersessionNeverIncreasesRemainingUploads(t *testing.T) {
	const count = 4
	s := newSyntheticBridge(t, retained.Budget{Bytes: 16, Resources: 1})
	b := s.b
	s.ack(s.next(), true)
	s.receive(1, count)
	s.promote()
	s.settle()
	old := s.receive(2, count)
	docB := s.promote()
	first := s.next()
	require.NotEmpty(t, first.Batch.Uploads)
	s.ack(first, true)
	assert.Equal(t, 3, b.remaining(&docB.Scene))
	newEpoch := s.receive(3, count)
	b.promotePending()
	assert.Same(t, docB, b.Target(), "four fresh versions exceed the three B still needs")
	// A camera-only document shares B's versions: replacing B costs nothing and
	// the shared uploads keep counting toward it.
	variant := s.receive(2, count)
	assert.True(t, newEpoch.released, "the pending slot coalesces to the newest document")
	b.promotePending()
	docV := b.Target()
	require.NotSame(t, docB, docV, "an equally close document may replace the target")
	assert.Equal(t, uint64(1), b.Stats().Superseded)
	require.True(t, s.w.SetTargetWithData(s.gen, &docV.Scene, docV))
	var packet retained.PacketWithData[*scene.Document]
	for packet = s.next(); packet.CurrentData != docV; packet = s.next() {
		if packet.TargetData == docV {
			assert.Empty(t, packet.Batch.Releases, "nothing staged for B is abandoned")
			require.NotEmpty(t, packet.Batch.Uploads)
		}
		s.ack(packet, true)
	}
	require.NotEmpty(t, packet.Batch.Releases, "A retires once the variant is Current")
	assert.Zero(t, b.remaining(&docV.Scene))
	later := s.receive(3, count)
	b.promotePending()
	docC := b.Target()
	require.NotSame(t, docV, docC, "a Current target releases the next pending document")
	require.True(t, s.w.SetTargetWithData(s.gen, &docC.Scene, docC))
	s.ack(packet, true)
	s.settle()
	assert.True(t, old.released)
	assert.True(t, variant.released)
	assert.False(t, later.released)
	final := b.Stats()
	assert.Equal(t, uint64(1), final.Superseded)
	assert.Equal(t, uint64(1), final.Coalesced)
	assert.Equal(t, uint64(3), final.CurrentTargets)
	assert.Equal(t, count, len(b.resident), "mirrored residency matches the settled scene")
	s.w.Close()
	<-s.w.Done()
	later.Release()
}
