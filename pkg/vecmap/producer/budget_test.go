package producer

import (
	"context"
	"fmt"
	"io"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/sprite"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdmissionFailuresStayWithinBudgets(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Limits)
		load   bool
	}{
		{"raw", func(l *Limits) { l.CacheBytes = 2*uint64(len(tilePBF())) - 1 }, true},
		{"prepared", func(l *Limits) { l.CacheBytes = 256 }, true},
		{"store", func(l *Limits) { l.Store.Bytes = 1 }, true},
		{"snapshot", func(l *Limits) { l.SnapshotBytes = 1 }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limits := DefaultLimits()
			limits.RawBytes = 128
			limits.Workers = 1
			tc.change(&limits)
			p, c := newControlled(t, limits)
			_, err := p.Submit(testRequest(t, view.TileID{}))
			require.NoError(t, err)
			if tc.load {
				nextCall(t, c).reply <- answer{data: tilePBF()}
			}
			status := waitStatus(t, p, func(s Status) bool {
				return s.Jobs == 0 && s.LastError != "" && (tc.name == "raw" || !tc.load || s.Prepares > 0)
			})
			assert.LessOrEqual(t, status.PeakCacheBytes, limits.CacheBytes)
			assert.LessOrEqual(t, status.PeakLeaseBytes, uint64(limits.Leases)*limits.SnapshotBytes)
			if tc.name == "snapshot" {
				_, ok := p.Next()
				assert.False(t, ok)
			}
		})
	}
}

func TestPinnedContinuityCannotBeEvictedForAdmission(t *testing.T) {
	limits := DefaultLimits()
	limits.Tiles = 1
	limits.Workers = 1
	limits.RawBytes = 128
	p, c := newControlled(t, limits)
	r := testRequest(t, view.TileID{})
	_, err := p.Submit(r)
	require.NoError(t, err)
	nextCall(t, c).reply <- answer{data: tilePBF()}
	old := nextLease(t, p, func(l *Lease) bool { return len(l.Snapshot.Cover) == 1 })
	require.True(t, p.Current(1, 1, old))
	old.Release()
	r.Targets = []view.TileID{{Z: 2, X: 2, Y: 2}}
	rev, err := p.Submit(r)
	require.NoError(t, err)
	status := waitStatus(t, p, func(s Status) bool { return s.Revision == rev && s.LastError != "" })
	assert.Equal(t, 1, status.Cached)
	continuity := nextLease(t, p, func(l *Lease) bool { return l.Revision == rev })
	assert.Equal(t, []view.TileID{{}}, continuity.Snapshot.Cover)
	assert.False(t, p.Current(1, 2, old), "released token cannot become Current")
	continuity.Release()
	require.True(t, p.Current(2, 1, nil))
	load := nextCall(t, c)
	assert.Equal(t, view.TileID{Z: 1, X: 1, Y: 1}, load.key.Tile)
	load.reply <- answer{data: tilePBF()}
	status = waitStatus(t, p, func(s Status) bool { return s.Builds == 2 })
	assert.Equal(t, 1, status.PeakCached)
}

func TestInvalidInputsAndOverflow(t *testing.T) {
	load := func(context.Context, Key, io.Writer) error { return nil }
	for _, change := range []func(*Limits){
		func(l *Limits) { l.Workers = 0 }, func(l *Limits) { l.Workers = 5 },
		func(l *Limits) { l.Tiles = 0 }, func(l *Limits) { l.Leases = 9 },
		func(l *Limits) { l.RawBytes = 0 }, func(l *Limits) { l.CacheBytes = 1<<30 + 1 },
		func(l *Limits) { l.SnapshotBytes = 0 }, func(l *Limits) { l.ProfileBytes = 0 },
		func(l *Limits) { l.RetryDelay = 0 },
	} {
		limits := DefaultLimits()
		change(&limits)
		_, err := New(load, limits)
		assert.ErrorIs(t, err, ErrInput)
	}
	_, err := New(nil, DefaultLimits())
	assert.ErrorIs(t, err, ErrInput)
	limits := DefaultLimits()
	limits.Store.Fragments = -1
	_, err = New(load, limits)
	assert.ErrorIs(t, err, retained.ErrLimit)
	p, _ := newControlled(t, DefaultLimits())
	for _, change := range []func(*Request){
		func(r *Request) { r.Style = nil }, func(r *Request) { r.Assets = nil },
		func(r *Request) { r.Style.Epoch = 0 }, func(r *Request) { r.Assets.Bytes = 0 },
		func(r *Request) { r.Style.Source = "" }, func(r *Request) { r.Style.Bytes = 1 << 31 },
		func(r *Request) { r.Assets.Bytes = 1 << 31 }, func(r *Request) { r.Style.Options.Zoom = math.NaN() },
		func(r *Request) { r.Camera.Width = math.Inf(1) }, func(r *Request) { r.Camera.Height = 0 },
		func(r *Request) { r.Targets = []view.TileID{{Z: 15}} },
		func(r *Request) { r.Targets = []view.TileID{{X: 1}} },
		func(r *Request) { r.Targets = []view.TileID{{}, {}} },
		func(r *Request) { r.Targets = []view.TileID{{}, {Z: 1}} },
	} {
		r := testRequest(t, view.TileID{})
		change(&r)
		_, err := p.Submit(r)
		assert.ErrorIs(t, err, ErrInput)
	}
	assert.False(t, p.Current(0, 1, nil))
	assert.False(t, p.Current(1, 0, nil))
	assert.False(t, p.ResetCurrent(0))
	assert.True(t, p.ResetCurrent(1))
	assert.False(t, p.ResetCurrent(1))
	assert.True(t, p.Current(1, 1, nil), "reset does not consume the first publication sequence")
	p.mu.Lock()
	p.revision = math.MaxUint64
	p.mu.Unlock()
	_, err = p.Submit(testRequest(t))
	assert.ErrorIs(t, err, ErrLimit)
	var nilLease *Lease
	nilLease.Release()
}

func TestWriterLatchesOverflowEvenWhenLoaderIgnoresError(t *testing.T) {
	w := responseWriter{limit: 3, data: make([]byte, 0, 3)}
	n, err := w.Write([]byte("ab"))
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	n, err = w.Write([]byte("cd"))
	assert.ErrorIs(t, err, ErrLimit)
	assert.Zero(t, n)
	n, err = w.Write([]byte("c"))
	assert.ErrorIs(t, err, ErrLimit)
	assert.Zero(t, n)
	assert.Equal(t, "ab", string(w.data))
	limits := DefaultLimits()
	limits.RawBytes = 1
	p, err := New(func(_ context.Context, _ Key, w io.Writer) error { _, _ = w.Write([]byte("bad")); return nil }, limits)
	require.NoError(t, err)
	t.Cleanup(func() { p.Close(); waitDone(t, p.Done()) })
	_, err = p.Submit(testRequest(t, view.TileID{}))
	require.NoError(t, err)
	waitStatus(t, p, func(s Status) bool { return s.Jobs == 0 && s.LastError == ErrLimit.Error() })
	assert.Zero(t, p.Status().Prepares)
}

func TestClearCoalescingAndBoundedError(t *testing.T) {
	p, c := newControlled(t, DefaultLimits())
	r := testRequest(t, view.TileID{})
	_, err := p.Submit(r)
	require.NoError(t, err)
	nextCall(t, c).reply <- answer{data: tilePBF()}
	ready := nextLease(t, p, func(l *Lease) bool { return len(l.Snapshot.Cover) == 1 })
	ready.Release()
	r.Targets = []view.TileID{}
	rev, err := p.Submit(r)
	require.NoError(t, err)
	clear := nextLease(t, p, func(l *Lease) bool { return l.Revision == rev })
	assert.Empty(t, clear.Snapshot.Cover)
	waitStatus(t, p, func(s Status) bool { return s.Cached == 0 })
	// Let several updates overwrite only the unconsumed output slot.
	for range 10 {
		r.Camera.Bearing++
		rev, err = p.Submit(r)
		require.NoError(t, err)
	}
	waitStatus(t, p, func(s Status) bool { return s.Revision == rev })
	assert.LessOrEqual(t, p.Status().Leases, 2)
	assert.False(t, p.Current(1, 1, &Lease{}))
	// No timer-based retry goroutines are added; Close also stops the ticker.
	p.Close()
	waitDone(t, p.Done())
	clear.Release()
	select {
	case <-p.Done():
	case <-time.After(time.Second):
		t.Fatal("closed Done changed")
	}
}

func TestSupersessionDuringBuildDoesNotPublishStaleFragment(t *testing.T) {
	limits := DefaultLimits()
	limits.Workers = 1
	limits.RawBytes = 128
	p, c := newControlled(t, limits)
	r := testRequest(t, view.TileID{})
	layers, err := style.Parse([]byte(`{"version":8,"layers":[{"id":"icon","type":"symbol","source-layer":"labels","layout":{"icon-image":"dot"}}]}`))
	require.NoError(t, err)
	r.Style.Layers = layers
	entered, release := make(chan struct{}), make(chan struct{})
	r.Assets.Value.SpriteEntry = func(string) (sprite.Entry, bool) { close(entered); <-release; return sprite.Entry{}, false }
	// Ensure cleanup releases the trusted synchronous callback even on failure.
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	_, err = p.Submit(r)
	require.NoError(t, err)
	nextCall(t, c).reply <- answer{data: tilePBF()}
	waitDone(t, entered)
	r.Targets = []view.TileID{}
	rev, err := p.Submit(r)
	require.NoError(t, err)
	close(release)
	l := nextLease(t, p, func(l *Lease) bool { return l.Revision == rev })
	assert.Empty(t, l.Snapshot.Cover)
	status := waitStatus(t, p, func(s Status) bool { return s.Cached == 0 && s.Rejected == 1 })
	assert.Equal(t, uint64(1), status.Builds)
}

func TestCanceledAndQueuedResultsShareFixedRawSlots(t *testing.T) {
	limits := DefaultLimits()
	limits.Workers = 4
	limits.RawBytes = 128
	p, c := newControlled(t, limits)
	r := testRequest(t, view.TileID{Z: 4, X: 0}, view.TileID{Z: 4, X: 2}, view.TileID{Z: 4, X: 4}, view.TileID{Z: 4, X: 6})
	_, err := p.Submit(r)
	require.NoError(t, err)
	old := make([]call, 4)
	for i := range old {
		old[i] = nextCall(t, c)
	}
	r.Targets = []view.TileID{{Z: 4, X: 14, Y: 14}}
	rev, err := p.Submit(r)
	require.NoError(t, err)
	for _, job := range old {
		waitDone(t, job.ctx.Done())
	}
	status := waitStatus(t, p, func(s Status) bool { return s.Revision == rev })
	assert.Equal(t, 4, status.Jobs)
	assert.Equal(t, uint64(512), status.ReservedRawBytes)
	for _, job := range old {
		job.reply <- answer{data: tilePBF()}
	}
	fresh := nextCall(t, c)
	fresh.reply <- answer{data: tilePBF()}
	status = waitStatus(t, p, func(s Status) bool { return s.Rejected == 4 })
	assert.Equal(t, 4, status.PeakJobs)
	assert.LessOrEqual(t, status.ReservedRawBytes, uint64(512))
}

func TestUsefulLoadsSurviveCoverAndAssetUpdates(t *testing.T) {
	limits := DefaultLimits()
	limits.Workers = 1
	limits.RawBytes = 128
	p, c := newControlled(t, limits)
	r := testRequest(t, view.TileID{Z: 2, X: 2, Y: 2})
	_, err := p.Submit(r)
	require.NoError(t, err)
	parent := nextCall(t, c)
	r.Targets = append(r.Targets, view.TileID{Z: 2, X: 3, Y: 2})
	r.Assets = &Assets{Epoch: 2, Bytes: 1}
	rev, err := p.Submit(r)
	require.NoError(t, err)
	waitStatus(t, p, func(s Status) bool { return s.Revision == rev })
	assert.NoError(t, parent.ctx.Err(), "still-needed parent I/O must survive a pan or font update")
	parent.reply <- answer{data: tilePBF()}
	l := nextLease(t, p, func(l *Lease) bool { return l.Revision == rev && len(l.Snapshot.Cover) == 1 })
	assert.Equal(t, parent.key.Tile, l.Snapshot.Cover[0])
	assert.Greater(t, l.Generation, parent.key.Generation, "registered useful job keeps its original token")
	assert.Zero(t, p.Status().Rejected)
}

func TestEpochRefreshPublishesCoherentCoverWithoutBorrowedAssets(t *testing.T) {
	limits := DefaultLimits()
	limits.Workers = 1
	limits.RawBytes = 128
	p, c := newControlled(t, limits)
	left, right := view.TileID{Z: 1}, view.TileID{Z: 1, X: 1}
	r := testRequest(t, left, right)
	r.Assets.Bytes = 1 << 20
	_, err := p.Submit(r)
	require.NoError(t, err)
	for range 3 {
		nextCall(t, c).reply <- answer{data: tilePBF()}
	}
	old := nextLease(t, p, func(l *Lease) bool { return len(l.Snapshot.Cover) == 2 })
	require.True(t, p.Current(1, 1, old))
	status := waitStatus(t, p, func(s Status) bool { return s.Cached == 2 })
	assert.Less(t, status.CacheBytes, uint64(20000), "owned fragments do not borrow the asset profile")
	oldCharge := old.bytes
	assert.Less(t, oldCharge, uint64(1<<20))
	layers, err := style.Parse([]byte(`{"version":8,"layers":[{"id":"background","type":"background","paint":{"background-color":"#00ff00"}},{"id":"icon","type":"symbol","source-layer":"labels","layout":{"icon-image":"dot"}}]}`))
	require.NoError(t, err)
	newStyle := *r.Style
	newStyle.Epoch++
	newStyle.Layers = layers
	r.Style = &newStyle
	gates, abort := make(chan chan struct{}, 2), make(chan struct{})
	t.Cleanup(func() { close(abort) })
	r.Assets = &Assets{Epoch: 2, Bytes: 1}
	r.Assets.Value.SpriteEntry = func(string) (sprite.Entry, bool) {
		gate := make(chan struct{})
		gates <- gate
		select {
		case <-gate:
		case <-abort:
		}
		return sprite.Entry{}, false
	}
	rev, err := p.Submit(r)
	require.NoError(t, err)
	nextGate := func() chan struct{} {
		select {
		case gate := <-gates:
			return gate
		case <-time.After(5 * time.Second):
			t.Fatal("no asset build")
			return nil
		}
	}
	close(nextGate())
	second := nextGate() // first refreshed tile installed; second still compiling
	_, ok := p.Next()
	assert.False(t, ok, "mixed red/green style epochs must not publish")
	assert.Equal(t, []byte{255, 0, 0, 255}, byteColor(old))
	close(second)
	fresh := nextLease(t, p, func(l *Lease) bool { return l.Revision == rev })
	for _, draw := range fresh.Snapshot.Scene.Draws {
		assert.Equal(t, [4]float32{0, 1, 0, 1}, draw.Material.Color)
	}
	assert.Equal(t, oldCharge, old.bytes, "owned old snapshots retain their original charge")
	status = waitStatus(t, p, func(s Status) bool { return s.Builds == 5 })
	assert.GreaterOrEqual(t, status.LeaseBytes, oldCharge+fresh.bytes)
	assert.Less(t, status.CacheBytes, uint64(20000), "old profile is no longer cached after all fragments refresh")
}

func TestUnusedAssetBackingDoesNotChargeOwnedSnapshot(t *testing.T) {
	limits := DefaultLimits()
	limits.RawBytes = 128
	limits.SnapshotBytes = 8192
	p, c := newControlled(t, limits)
	r := testRequest(t, view.TileID{})
	r.Assets.Bytes = 1 << 20
	_, err := p.Submit(r)
	require.NoError(t, err)
	nextCall(t, c).reply <- answer{data: tilePBF()}
	l := nextLease(t, p, func(l *Lease) bool { return len(l.Snapshot.Cover) == 1 })
	status := waitStatus(t, p, func(s Status) bool { return s.Builds == 1 })
	assert.Empty(t, status.LastError)
	assert.Less(t, status.CacheBytes, uint64(1<<20))
	assert.Equal(t, l.Snapshot.RetainedBytes(), l.bytes)
}

func TestErrorMailboxBoundsAndGenerationExhaustion(t *testing.T) {
	p, c := newControlled(t, DefaultLimits())
	_, err := p.Submit(testRequest(t, view.TileID{}))
	require.NoError(t, err)
	nextCall(t, c).reply <- answer{err: fmt.Errorf("%w: %s", ErrPermanent, strings.Repeat("x", 4096))}
	waitStatus(t, p, func(s Status) bool { return s.Jobs == 0 && len(s.LastError) == 512 })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q := &Producer{ctx: ctx, cancel: cancel, latest: new(Request), revision: 1}
	*q.latest = testRequest(t, view.TileID{})
	s := state{p: q, generation: math.MaxUint64}
	s.inputs()
	assert.Equal(t, uint64(math.MaxUint64), s.generation)
	assert.ErrorIs(t, ctx.Err(), context.Canceled)
	assert.Equal(t, ErrLimit.Error(), s.stats.LastError)
}
