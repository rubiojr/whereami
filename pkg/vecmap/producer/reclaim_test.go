package producer

import (
	"runtime"
	"testing"
	"time"
	"weak"

	"github.com/rubiojr/whereami/pkg/vecmap/sprite"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
	"github.com/rubiojr/whereami/pkg/vecmap/tiles"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreparedReclaimPreservesCurrentAndRebuildsAssets(t *testing.T) {
	r := testRequest(t, view.TileID{})
	prepared, err := tiles.Prepare(tilePBF(), r.Style.Layers, r.Style.Options)
	require.NoError(t, err)
	built, err := prepared.BuildOwned(r.Assets.Value, 1<<20)
	require.NoError(t, err)
	limits := DefaultLimits()
	limits.Workers = 1
	limits.RawBytes = 128
	base := 2*uint64(len(tilePBF())) + built.Fragment.RetainedBytes() + built.Fragment.SetCopyBytes(view.TileID{}) + r.Style.Bytes
	limits.CacheBytes = base + prepared.RetainedBytes() - 1
	p, c := newControlled(t, limits)
	_, err = p.Submit(r)
	require.NoError(t, err)
	nextCall(t, c).reply <- answer{data: tilePBF()}
	first := nextLease(t, p, func(l *Lease) bool { return len(l.Snapshot.Cover) == 1 })
	require.True(t, p.Current(1, 1, first))
	status := waitStatus(t, p, func(s Status) bool { return s.Builds == 1 })
	assert.Zero(t, status.Cache.Prepared)
	assert.Equal(t, uint64(1), status.UncachedPreparations)
	r.Camera.Bearing = 2
	rev, err := p.Submit(r)
	require.NoError(t, err)
	moved := nextLease(t, p, func(l *Lease) bool { return l.Revision == rev })
	assert.Same(t, first.Snapshot, moved.Snapshot)
	moved.Release()
	assert.Equal(t, uint64(1), p.Status().Prepares)
	r.Assets = &Assets{Epoch: 2, Bytes: 1}
	rev, err = p.Submit(r)
	require.NoError(t, err)
	second := nextLease(t, p, func(l *Lease) bool { return l.Revision == rev })
	assert.Equal(t, byteColor(first), byteColor(second))
	assert.NotSame(t, first.Snapshot, second.Snapshot)
	status = waitStatus(t, p, func(s Status) bool { return s.Builds == 2 })
	assert.Equal(t, uint64(2), status.Prepares)
	assert.Equal(t, uint64(1), status.Loads)
	assert.LessOrEqual(t, status.PeakCacheBytes, limits.CacheBytes)
}

func TestCachedPreparationEvictionDoesNotDropPinnedFragment(t *testing.T) {
	left, right := view.TileID{Z: 1}, view.TileID{Z: 1, X: 1}
	r := testRequest(t, left, right)
	prepared, err := tiles.Prepare(tilePBF(), r.Style.Layers, r.Style.Options)
	require.NoError(t, err)
	built, err := prepared.BuildOwned(r.Assets.Value, 1<<20)
	require.NoError(t, err)
	entry := 2*uint64(len(tilePBF())) + built.Fragment.RetainedBytes() + built.Fragment.SetCopyBytes(left)
	limits := DefaultLimits()
	limits.Workers = 1
	limits.RawBytes = 128
	limits.CacheBytes = 3*entry + r.Style.Bytes + prepared.RetainedBytes()
	p, c := newControlled(t, limits)
	_, err = p.Submit(r)
	require.NoError(t, err)
	nextCall(t, c).reply <- answer{data: tilePBF()}
	parent := nextLease(t, p, func(l *Lease) bool { return len(l.Snapshot.Cover) == 1 })
	require.True(t, p.Current(1, 1, parent))
	for range 2 {
		nextCall(t, c).reply <- answer{data: tilePBF()}
	}
	fine := nextLease(t, p, func(l *Lease) bool { return len(l.Snapshot.Cover) == 2 })
	status := waitStatus(t, p, func(s Status) bool { return s.Builds == 3 })
	assert.Greater(t, status.PreparationEvictions, uint64(0))
	assert.Equal(t, 3, status.Cached, "pinned parent remains available")
	assert.Equal(t, []byte{255, 0, 0, 255}, byteColor(parent))
	assert.Equal(t, []byte{255, 0, 0, 255}, byteColor(fine))
	assert.LessOrEqual(t, status.PeakCacheBytes, limits.CacheBytes)
}

type pixelBacking struct{ bytes [1 << 20]byte }

func borrowedAssets() (*Assets, weak.Pointer[pixelBacking]) {
	backing := &pixelBacking{}
	copy(backing.bytes[:4], []byte{255, 0, 0, 255})
	return &Assets{Epoch: 1, Bytes: 2 << 20, Value: tiles.Assets{
		Sprite: func(string, style.Color, float64) (sprite.Image, bool) {
			return sprite.Image{Width: 1, Height: 1, PixelRatio: 1, Pixels: backing.bytes[:4:4]}, true
		},
		SpriteEntry: func(string) (sprite.Entry, bool) { return sprite.Entry{Width: 1, Height: 1, PixelRatio: 1}, true },
	}}, weak.Make(backing)
}

func TestOwnedTexturesReleaseHiddenAssetBackingWhileLeaseLives(t *testing.T) {
	limits := DefaultLimits()
	limits.Workers = 1
	limits.RawBytes = 128
	p, c := newControlled(t, limits)
	r := testRequest(t, view.TileID{})
	layers, err := style.Parse([]byte(`{"version":8,"layers":[{"id":"icon","type":"symbol","source-layer":"labels","layout":{"icon-image":"dot"}}]}`))
	require.NoError(t, err)
	r.Style.Layers = layers
	assets, probe := borrowedAssets()
	r.Assets = assets
	assets = nil
	_, err = p.Submit(r)
	require.NoError(t, err)
	nextCall(t, c).reply <- answer{data: tilePBF()}
	old := nextLease(t, p, func(l *Lease) bool { return len(l.Snapshot.Scene.Textures) > 0 })
	assert.Equal(t, 4, cap(old.Snapshot.Scene.Textures[0].RGBA))
	r.Assets = &Assets{Epoch: 2, Bytes: 1}
	rev, err := p.Submit(r)
	require.NoError(t, err)
	nextLease(t, p, func(l *Lease) bool { return l.Revision == rev })
	require.Eventually(t, func() bool { runtime.GC(); return probe.Value() == nil }, 5*time.Second, 10*time.Millisecond)
	assert.Equal(t, []byte{255, 0, 0, 255}, old.Snapshot.Scene.Textures[0].RGBA)
	runtime.KeepAlive(old)
}

func TestCapacityRetryWaitsForRealCacheHeadroom(t *testing.T) {
	old, tile := view.TileID{}, view.TileID{Z: 1}
	s := state{p: &Producer{limits: Limits{CacheBytes: 1000}}, entries: map[view.TileID]*entry{
		old: {usage: CacheUsage{Raw: 100, Fragments: 700, Prepared: 200}},
	}, failures: make(map[view.TileID]failure)}
	s.recordCache(old, s.entries[old])
	next := &entry{usage: CacheUsage{Raw: 100, Fragments: 300, Prepared: 100}}
	s.failCapacity(tile, next)
	require.True(t, s.failures[tile].capacity)
	assert.Equal(t, uint64(600), s.failures[tile].maximumFixed)
	// Prepared was already excluded from the minimum admission charge.
	s.entries[old].usage.Prepared = 0
	s.recordCache(old, s.entries[old])
	s.retryCapacity()
	assert.Contains(t, s.failures, tile)
	assert.Zero(t, s.stats.CapacityRetries)
	s.entries[old].usage.Fragments = 600
	s.recordCache(old, s.entries[old])
	s.retryCapacity()
	assert.Contains(t, s.failures, tile)
	s.entries[old].usage.Fragments = 500
	s.recordCache(old, s.entries[old])
	s.retryCapacity()
	assert.NotContains(t, s.failures, tile)
	assert.Equal(t, uint64(1), s.stats.CapacityRetries)
	s.retryCapacity()
	assert.Equal(t, uint64(1), s.stats.CapacityRetries)
	next.usage.Fragments = 2000
	s.failCapacity(tile, next)
	assert.False(t, s.failures[tile].capacity, "indivisible oversized input stays terminal")
}

func TestCapacityEvictsNonDesiredContinuityBeforeFailing(t *testing.T) {
	target, visible, stale := view.TileID{Z: 1}, view.TileID{Z: 1, X: 1}, view.TileID{Z: 1, Y: 1}
	r := testRequest(t, target, visible, stale)
	s := churnState(t, r)
	for _, tile := range []view.TileID{target, visible, stale} {
		e := &entry{raw: tilePBF(), source: r.Style.Source}
		s.entries[tile] = e
		s.build(tile, e)
		require.NotNil(t, s.entries[tile].fragment, "build installs a replacement entry")
	}
	// visible is acknowledged Current continuity; stale is pinned only by an
	// unconfirmed lease. Neither is requested any more. Fixed charges: 400 each,
	// plus the shared 1024-byte style profile.
	s.current.cover = []view.TileID{visible}
	s.desired = map[view.TileID]bool{target: true}
	for _, tile := range []view.TileID{visible, stale} {
		s.entries[tile].usage = CacheUsage{Fragments: 400}
	}
	s.entries[target].usage = CacheUsage{Fragments: 100}
	s.recordCache(target, s.entries[target])
	s.p.limits.CacheBytes = 3000
	next := *s.entries[target]
	next.usage = CacheUsage{Fragments: 1300, Prepared: 50}
	require.True(t, s.admitPrepared(target, &next), "dropping the lease-only tile admits the desired replacement")
	assert.NotContains(t, s.entries, stale)
	assert.Contains(t, s.entries, visible, "visible continuity outlives lease-only pins")
	assert.Equal(t, uint64(1), s.stats.ContinuityEvictions)
	assert.Equal(t, uint64(400), s.stats.ContinuityBytesFreed)
	assert.True(t, s.dirty)
	_, err := s.set.Select([]view.TileID{stale}, nil, r.Camera, 0)
	require.NoError(t, err)
	selected, _ := view.SelectCover([]view.TileID{stale}, nil, func(tile view.TileID) bool { return s.entries[tile] != nil })
	assert.Empty(t, selected, "evicted continuity is no longer selectable")
	assert.Equal(t, uint64(50), next.usage.Prepared, "incoming preparation survives when continuity eviction suffices")
	next.usage = CacheUsage{Fragments: 1800}
	require.True(t, s.admitPrepared(target, &next), "visible continuity yields when nothing else can")
	assert.NotContains(t, s.entries, visible)
	assert.Equal(t, uint64(2), s.stats.ContinuityEvictions)
	next.usage = CacheUsage{Fragments: 2500}
	assert.False(t, s.admitPrepared(target, &next), "an indivisible oversized input remains an explicit failure")
	assert.Equal(t, uint64(2), s.stats.ContinuityEvictions, "desired tiles are never evicted for admission")
}
