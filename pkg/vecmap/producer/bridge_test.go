package producer

import (
	"testing"
	"time"

	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
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
	require.True(t, b.Ready(rev))
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
	// Start a style replacement, then coalesce repeated newer requests while its
	// upload is unacknowledged. Current and accepted-target leases remain held.
	updated := *r.Style
	updated.Epoch++
	updated.Options.Zoom++
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

func TestBridgeBoundedSupersession(t *testing.T) {
	for _, count := range []int{2, 4} {
		name := "testPartialUpload"
		if count == 2 {
			name = "testFinalUploadRace"
		}
		t.Run(name, func(t *testing.T) {
			// Drive the bridge's mailbox tick explicitly. Real Worker queues and
			// acknowledgements run concurrently; synthetic leases isolate ordering
			// from tile preparation. The captured corpus covers the real producer.
			p := &Producer{leases: make(map[*Lease]struct{}), wake: make(chan struct{}, 1)}
			initial := &scene.Document{Width: 256, Height: 256}
			w, err := retained.NewSettlingWorkerWithData[*scene.Document](retained.ResidencyLimits{}, retained.Budget{Bytes: 16, Resources: 1})
			require.NoError(t, err)
			t.Cleanup(func() { w.Close(); <-w.Done() })
			b := &Bridge{producer: p, worker: w, initial: initial, held: make(map[*scene.Document]*Lease), busy: true}
			b.latest.Store(initial)
			gen, err := w.RestartWithData(&initial.Scene, initial)
			require.NoError(t, err)
			require.True(t, b.Restarted(gen))
			next := func() retained.PacketWithData[*scene.Document] {
				t.Helper()
				var packet retained.PacketWithData[*scene.Document]
				require.Eventually(t, func() bool { var ok bool; packet, ok = w.Next(); return ok }, 5*time.Second, time.Millisecond)
				require.True(t, b.Consumed(packet))
				require.NoError(t, packet.Err)
				return packet
			}
			ack := func(packet retained.PacketWithData[*scene.Document], success bool) {
				t.Helper()
				require.True(t, w.Acknowledge(gen, packet.Sequence, success))
				require.True(t, b.Completed(packet, success))
			}
			receive := func(revision uint64) *Lease {
				t.Helper()
				s := &scene.Scene{}
				for id := range count {
					s.Textures = append(s.Textures, scene.Texture{ID: uint64(id + 1), Revision: revision, Width: 1, Height: 1, RGBA: []byte{1, 2, 3, 255}})
				}
				require.NoError(t, s.Validate())
				l := &Lease{p: p, Revision: revision, Snapshot: &tiles.Snapshot{Scene: s, Cover: []view.TileID{{}}}}
				p.leases[l] = struct{}{}
				require.LessOrEqual(t, len(p.leases), 4)
				b.receive(l)
				return l
			}
			promote := func() *scene.Document {
				b.promotePending()
				d := b.Target()
				require.True(t, w.SetTargetWithData(gen, &d.Scene, d))
				return d
			}
			ack(next(), true)
			a := receive(1)
			docA := promote()
			for packet := next(); ; packet = next() {
				ack(packet, true)
				if packet.CurrentData == initial {
					assert.False(t, b.advance, "camera churn cannot starve the initial cover")
				}
				if packet.Settled {
					break
				}
			}
			old := receive(2)
			docB := promote()
			first := next()
			require.Same(t, docA, first.CurrentData)
			receive(3)
			latest := receive(4) // coalesce a never-submitted target
			require.Len(t, p.leases, 3)
			b.promotePending()
			assert.Same(t, docB, b.Target())
			ack(first, false)
			b.promotePending()
			assert.Same(t, docB, b.Target(), "failed upload grants no supersession boundary")
			ack(next(), true)
			// Hold B's second packet before queueing C, making both partial and
			// final-ack promotion races deterministic.
			second := next()
			docC := promote()
			assert.False(t, old.released, "submission alone does not drop B")
			ack(second, true)
			packet := next()
			require.Same(t, docC, packet.TargetData)
			require.NotEmpty(t, packet.Batch.Releases)
			if count == 2 {
				assert.Same(t, docB, packet.CurrentData, "B became Current during final ack")
			} else {
				assert.Same(t, docA, packet.CurrentData, "unfinished B never became Current")
			}
			assert.False(t, old.released)
			ack(packet, true)
			assert.Equal(t, count != 2, old.released)
			assert.Equal(t, count == 2, a.released)
			assert.False(t, latest.released)
			// There is still native retirement left: CPU lease release is not
			// optimistic GPU capacity credit.
			packet = next()
			require.NotEmpty(t, packet.Batch.Releases)
			for {
				ack(packet, true)
				if packet.Settled {
					break
				}
				packet = next()
			}
			assert.True(t, b.Ready(4))
			assert.True(t, old.released)
			assert.True(t, a.released)
			assert.False(t, latest.released)
			assert.Equal(t, uint64(1), b.Stats().Superseded)
			assert.Equal(t, uint64(1), b.Stats().Coalesced)
			w.Close()
			<-w.Done()
			latest.Release()
		})
	}
}
