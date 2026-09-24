package producer

import (
	"testing"
	"time"

	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBridgeSerialCoalescingRetirementAndReset(t *testing.T) {
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
	assert.Same(t, second, b.Target(), "accepted targets are serialized")
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
