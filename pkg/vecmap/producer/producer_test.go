package producer

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/rubiojr/whereami/pkg/vecmap/glyph"
	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
	"github.com/rubiojr/whereami/pkg/vecmap/tiles"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type answer struct {
	data []byte
	err  error
}
type call struct {
	key   Key
	ctx   context.Context
	reply chan answer
}
type controlled struct {
	calls chan call
	stop  chan struct{}
	once  sync.Once
}

func (c *controlled) close() { c.once.Do(func() { close(c.stop) }) }

func newControlled(t *testing.T, limits Limits) (*Producer, *controlled) {
	t.Helper()
	c := &controlled{calls: make(chan call, 8), stop: make(chan struct{})}
	p, err := New(func(ctx context.Context, key Key, dst io.Writer) error {
		request := call{key: key, ctx: ctx, reply: make(chan answer, 1)}
		select {
		case c.calls <- request:
		case <-c.stop:
			return context.Canceled
		}
		// Deliberately ignore ctx cancellation: fixed-worker limits and stale
		// success rejection must hold even for a badly behaved transport.
		select {
		case a := <-request.reply:
			if a.err != nil {
				return a.err
			}
			_, err := dst.Write(a.data)
			return err
		case <-c.stop:
			return context.Canceled
		}
	}, limits)
	require.NoError(t, err)
	t.Cleanup(func() { p.Close(); c.close(); waitDone(t, p.Done()) })
	return p, c
}

func waitDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("producer did not join")
	}
}

func nextCall(t *testing.T, c *controlled) call {
	t.Helper()
	select {
	case call := <-c.calls:
		return call
	case <-time.After(5 * time.Second):
		t.Fatal("no loader call")
		return call{}
	}
}

func waitStatus(t *testing.T, p *Producer, check func(Status) bool) Status {
	t.Helper()
	require.Eventually(t, func() bool { return check(p.Status()) }, 5*time.Second, time.Millisecond, "status: %+v", p.Status())
	return p.Status()
}

func nextLease(t *testing.T, p *Producer, check func(*Lease) bool) *Lease {
	t.Helper()
	var result *Lease
	ok := assert.Eventually(t, func() bool {
		l, ok := p.Next()
		if !ok {
			return false
		}
		if check(l) {
			result = l
			return true
		}
		l.Release()
		return false
	}, 5*time.Second, time.Millisecond)
	require.True(t, ok, "status: %+v", p.Status())
	t.Cleanup(result.Release)
	return result
}

func testRequest(t *testing.T, targets ...view.TileID) Request {
	t.Helper()
	layers, err := style.Parse([]byte(`{"version":8,"layers":[{"id":"background","type":"background","paint":{"background-color":"#ff0000"}}]}`))
	require.NoError(t, err)
	return Request{Camera: view.NewCamera(view.Coordinate{}, 2, 0, 256, 256), Targets: targets,
		Style:  &Style{Source: "test", Epoch: 1, Layers: layers, Options: tiles.PrepareOptions{Zoom: 2, Indexed: true, TriangleLimit: 100, CandidateLimit: 16, ElementLimit: 4096, DrawLimit: 64}, Bytes: 1024},
		Assets: &Assets{Epoch: 1, Bytes: 1}}
}

// A small valid MVT with one point in a labels layer; no external fixture needed.
func tilePBF() []byte {
	field := func(n uint64, value []byte) []byte {
		out := binary.AppendUvarint(nil, n<<3|2)
		out = binary.AppendUvarint(out, uint64(len(value)))
		return append(out, value...)
	}
	feature := append([]byte{24, 1}, field(4, []byte{9, 128, 2, 128, 2})...)
	layer := append(field(1, []byte("labels")), field(2, feature)...)
	layer = append(layer, 40, 128, 2, 120, 2)
	return field(3, layer)
}

func TestParentFirstRefinementAndAcknowledgedContinuity(t *testing.T) {
	limits := DefaultLimits()
	limits.Workers = 1
	limits.RawBytes = 1024
	p, c := newControlled(t, limits)
	left, right := view.TileID{Z: 2, X: 2, Y: 2}, view.TileID{Z: 2, X: 3, Y: 2}
	bottomLeft, bottomRight := view.TileID{Z: 2, X: 2, Y: 3}, view.TileID{Z: 2, X: 3, Y: 3}
	parent, _ := left.Parent()
	children := []view.TileID{left, right, bottomLeft, bottomRight}
	r := testRequest(t, children...)
	_, err := p.Submit(r)
	require.NoError(t, err)
	load := nextCall(t, c)
	assert.Equal(t, parent, load.key.Tile)
	load.reply <- answer{data: tilePBF()}
	old := nextLease(t, p, func(l *Lease) bool { return len(l.Snapshot.Cover) == 1 && l.Snapshot.Cover[0] == parent })
	require.True(t, p.Current(1, 1, old))
	oldVertices := append([]byte(nil), byteColor(old)...)
	load = nextCall(t, c)
	assert.Equal(t, left, load.key.Tile)
	load.reply <- answer{data: tilePBF()}
	waitStatus(t, p, func(s Status) bool { return s.Builds == 2 })
	partial := nextLease(t, p, func(l *Lease) bool { return len(l.Snapshot.Cover) == 1 })
	assert.Equal(t, []view.TileID{parent}, partial.Snapshot.Cover)
	partial.Release()
	load = nextCall(t, c)
	assert.Equal(t, right, load.key.Tile)
	load.reply <- answer{data: tilePBF()}
	for _, tile := range []view.TileID{bottomLeft, bottomRight} {
		load = nextCall(t, c)
		assert.Equal(t, tile, load.key.Tile)
		load.reply <- answer{data: tilePBF()}
	}
	fine := nextLease(t, p, func(l *Lease) bool { return len(l.Snapshot.Cover) == 4 })
	assert.Equal(t, children, fine.Snapshot.Cover)
	assert.Equal(t, oldVertices, byteColor(old), "old snapshot remains immutable")
	require.True(t, p.Current(1, 2, fine))
	old.Release()
	waitStatus(t, p, func(s Status) bool { return s.Cached == 4 })
	// Zoom out: detailed acknowledged coverage wins while requesting the parent.
	r.Targets = []view.TileID{parent}
	r.Camera.Zoom = 1
	revision, err := p.Submit(r)
	require.NoError(t, err)
	zoomed := nextLease(t, p, func(l *Lease) bool { return l.Revision == revision })
	assert.Equal(t, children, zoomed.Snapshot.Cover)
	assert.False(t, p.Current(1, 1, old))
	assert.True(t, p.Current(2, 1, nil))
	assert.False(t, p.Current(1, 100, fine), "old native generation cannot restore continuity")
	status := waitStatus(t, p, func(s Status) bool { return s.Revision == revision })
	assert.LessOrEqual(t, status.PeakJobs, limits.Workers)
	assert.LessOrEqual(t, status.PeakCacheBytes, limits.CacheBytes)
}

func byteColor(l *Lease) []byte {
	c := l.Snapshot.Scene.Draws[0].Material.Color
	return []byte{byte(c[0] * 255), byte(c[1] * 255), byte(c[2] * 255), byte(c[3] * 255)}
}

func TestCancellationKeepsWorkerChargedAndRejectsLateSuccess(t *testing.T) {
	limits := DefaultLimits()
	limits.Workers = 1
	limits.RawBytes = 1024
	p, c := newControlled(t, limits)
	r := testRequest(t, view.TileID{Z: 3, X: 1, Y: 1})
	_, err := p.Submit(r)
	require.NoError(t, err)
	stale := nextCall(t, c)
	for i := uint32(2); i < 8; i++ {
		r.Targets = []view.TileID{{Z: 3, X: i, Y: 6}}
		_, err = p.Submit(r)
		require.NoError(t, err)
	}
	waitDone(t, stale.ctx.Done())
	status := waitStatus(t, p, func(s Status) bool { return s.Revision == 7 })
	assert.Equal(t, 1, status.Jobs)
	assert.Equal(t, uint64(1024), status.ReservedRawBytes)
	select {
	case unexpected := <-c.calls:
		t.Fatalf("spawned beyond limit: %+v", unexpected.key)
	default:
	}
	stale.reply <- answer{data: tilePBF()}
	fresh := nextCall(t, c)
	assert.Greater(t, fresh.key.Generation, stale.key.Generation)
	assert.Equal(t, view.TileID{Z: 2, X: 3, Y: 3}, fresh.key.Tile)
	fresh.reply <- answer{data: tilePBF()}
	nextLease(t, p, func(l *Lease) bool { return len(l.Snapshot.Cover) == 1 })
	status = p.Status()
	assert.Equal(t, uint64(1), status.Rejected)
	assert.Equal(t, 1, status.PeakJobs)
	assert.Equal(t, uint64(1), status.Prepares, "late raw result was never decoded")
}

func TestRetriesMissingMalformedAndOversized(t *testing.T) {
	for _, tc := range []struct {
		name      string
		responses []answer
		attempts  uint64
		success   bool
	}{
		{"retry", []answer{{err: errors.New("temporary")}, {err: errors.New("temporary")}, {data: tilePBF()}}, 3, true},
		{"exhausted", []answer{{err: errors.New("offline")}, {err: errors.New("offline")}, {err: errors.New("offline")}}, 3, false},
		{"missing", []answer{{err: ErrMissing}}, 1, false},
		{"permanent", []answer{{err: ErrPermanent}}, 1, false},
		{"malformed", []answer{{data: []byte{255}}}, 1, false},
		{"oversized", []answer{{data: make([]byte, 129)}}, 1, false},
		{"blank", []answer{{data: []byte{26, 11, 10, 4, 'l', 'a', 'n', 'd', 40, 128, 2, 120, 2}}}, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limits := DefaultLimits()
			limits.RawBytes = 128
			limits.RetryDelay = time.Millisecond
			p, c := newControlled(t, limits)
			_, err := p.Submit(testRequest(t, view.TileID{}))
			require.NoError(t, err)
			for _, response := range tc.responses {
				nextCall(t, c).reply <- response
			}
			status := waitStatus(t, p, func(s Status) bool {
				return s.Jobs == 0 && s.Loads == tc.attempts && (s.Builds > 0 || s.LastError != "")
			})
			if tc.success {
				nextLease(t, p, func(l *Lease) bool { return len(l.Snapshot.Cover) == 1 })
			} else {
				assert.NotEmpty(t, status.LastError)
			}
			assert.Equal(t, tc.attempts, status.Loads)
		})
	}
}

func TestAssetsRebuildStyleReprepareAndCameraReuse(t *testing.T) {
	limits := DefaultLimits()
	limits.Workers = 1
	limits.RawBytes = 1024
	p, c := newControlled(t, limits)
	r := testRequest(t, view.TileID{})
	layers, err := style.Parse([]byte(`{"version":8,"layers":[{"id":"labels","type":"symbol","source-layer":"labels","layout":{"text-field":"A","text-font":["Test"],"text-size":16},"paint":{"text-color":"black"}}]}`))
	require.NoError(t, err)
	r.Style.Layers = layers
	_, err = p.Submit(r)
	require.NoError(t, err)
	nextCall(t, c).reply <- answer{data: tilePBF()}
	missing := nextLease(t, p, func(l *Lease) bool { return len(l.Snapshot.Cover) == 1 })
	assert.Empty(t, missing.Snapshot.Scene.Draws)
	r.Assets = &Assets{Epoch: 2, Bytes: 1024, Value: tiles.Assets{Fonts: map[string]map[uint32]glyph.Glyph{
		"Test": {'A': {ID: 'A', Width: 2, Height: 2, Advance: 4, Bitmap: bytes.Repeat([]byte{90}, 64)}},
	}}}
	rev, err := p.Submit(r)
	require.NoError(t, err)
	ready := nextLease(t, p, func(l *Lease) bool { return l.Revision == rev })
	require.NotEmpty(t, ready.Snapshot.Scene.Draws, "accepted=%+v spaces=%+v", ready.Snapshot.Accepted, ready.Snapshot.TileSpaces)
	status := waitStatus(t, p, func(s Status) bool { return s.Builds == 2 })
	assert.Equal(t, uint64(1), status.Prepares)
	assert.Equal(t, uint64(1), status.Loads)
	assert.Empty(t, missing.Snapshot.Scene.Draws)
	missing.Release()
	r.Camera.Bearing = 1
	rev, err = p.Submit(r)
	require.NoError(t, err)
	moved := nextLease(t, p, func(l *Lease) bool { return l.Revision == rev })
	assert.Same(t, ready.Snapshot, moved.Snapshot)
	assert.NotEqual(t, ready.Snapshot.Frame(view.NewCamera(view.Coordinate{}, 2, 0, 256, 256)).Transforms, moved.Snapshot.Frame(r.Camera).Transforms)
	moved.Release()
	ready.Release()
	newStyle := *r.Style
	newStyle.Epoch++
	newStyle.Options.Zoom = 3
	r.Style = &newStyle
	rev, err = p.Submit(r)
	require.NoError(t, err)
	nextLease(t, p, func(l *Lease) bool { return l.Revision == rev })
	status = waitStatus(t, p, func(s Status) bool { return s.Builds == 3 })
	assert.Equal(t, uint64(1), status.Loads, "style refresh reuses cached raw MVT")
}

func TestSnapshotLeaseBackpressureAndShutdown(t *testing.T) {
	limits := DefaultLimits()
	limits.Workers = 1
	limits.Leases = 1
	limits.RawBytes = 1024
	p, c := newControlled(t, limits)
	r := testRequest(t, view.TileID{})
	_, err := p.Submit(r)
	require.NoError(t, err)
	nextCall(t, c).reply <- answer{data: tilePBF()}
	old := nextLease(t, p, func(l *Lease) bool { return len(l.Snapshot.Cover) == 1 })
	r.Camera.Bearing = 20
	rev, err := p.Submit(r)
	require.NoError(t, err)
	waitStatus(t, p, func(s Status) bool { return s.Revision == rev })
	_, ok := p.Next()
	assert.False(t, ok, "consumer-held lease backpressures publication")
	old.Release()
	old.Release()
	nextLease(t, p, func(l *Lease) bool { return l.Revision == rev })
	status := p.Status()
	assert.Equal(t, 1, status.PeakLeases)
	assert.LessOrEqual(t, status.PeakLeaseBytes, limits.SnapshotBytes)
	// New transport ignores cancel. Close returns immediately, Done waits for it.
	r.Style = &Style{Source: "other", Epoch: 2, Layers: r.Style.Layers, Options: r.Style.Options, Bytes: 1024}
	_, err = p.Submit(r)
	require.NoError(t, err)
	outstanding := nextCall(t, c)
	p.Close()
	waitDone(t, outstanding.ctx.Done())
	select {
	case <-p.Done():
		t.Fatal("joined before ignored-cancel loader returned")
	default:
	}
	outstanding.reply <- answer{data: tilePBF()}
	waitDone(t, p.Done())
	_, err = p.Submit(r)
	require.ErrorIs(t, err, ErrClosed)
	assert.False(t, p.Current(1, 1, nil))
}

func TestWorkerKeepsSceneAssociatedLeaseThroughPartialUpload(t *testing.T) {
	limits := DefaultLimits()
	limits.Workers = 1
	limits.RawBytes = 1024
	p, c := newControlled(t, limits)
	left, right := view.TileID{Z: 1}, view.TileID{Z: 1, X: 1}
	_, err := p.Submit(testRequest(t, left, right))
	require.NoError(t, err)
	nextCall(t, c).reply <- answer{data: tilePBF()}
	parent := nextLease(t, p, func(l *Lease) bool { return len(l.Snapshot.Cover) == 1 })
	w, err := retained.NewWorkerWithData[*Lease](retained.ResidencyLimits{}, retained.Budget{Bytes: 1 << 20, Resources: 1})
	require.NoError(t, err)
	t.Cleanup(func() { w.Close(); waitDone(t, w.Done()) })
	generation, err := w.RestartWithData(parent.Snapshot.Scene, parent)
	require.NoError(t, err)
	packet := driveCurrent(t, w, parent)
	require.True(t, p.Current(generation, packet.Sequence, packet.CurrentData))
	require.True(t, w.Acknowledge(packet.Generation, packet.Sequence, true))
	nextCall(t, c).reply <- answer{data: tilePBF()}
	nextCall(t, c).reply <- answer{data: tilePBF()}
	fine := nextLease(t, p, func(l *Lease) bool { return len(l.Snapshot.Cover) == 2 })
	require.True(t, w.SetTargetWithData(generation, fine.Snapshot.Scene, fine))
	packet = workerPacket(t, w)
	require.NotNil(t, packet.Batch)
	assert.Same(t, parent, packet.CurrentData)
	assert.Same(t, parent.Snapshot.Scene, packet.Current)
	assert.Len(t, packet.CurrentData.Snapshot.Frame(testRequest(t).Camera).Transforms, len(parent.Snapshot.TileSpaces))
	require.True(t, w.Acknowledge(packet.Generation, packet.Sequence, true))
	packet = driveCurrent(t, w, fine)
	require.True(t, p.Current(generation, packet.Sequence, packet.CurrentData))
	// Outstanding native work never prevents producer shutdown.
	p.Close()
	waitDone(t, p.Done())
	assert.Same(t, fine.Snapshot.Scene, packet.Current)
	w.Close()
	waitDone(t, w.Done())
	parent.Release()
}

func workerPacket(t *testing.T, w *retained.WorkerWithData[*Lease]) retained.PacketWithData[*Lease] {
	t.Helper()
	var packet retained.PacketWithData[*Lease]
	require.Eventually(t, func() bool { var ok bool; packet, ok = w.Next(); return ok }, 5*time.Second, time.Millisecond)
	require.NoError(t, packet.Err)
	return packet
}

func driveCurrent(t *testing.T, w *retained.WorkerWithData[*Lease], want *Lease) retained.PacketWithData[*Lease] {
	t.Helper()
	for {
		packet := workerPacket(t, w)
		if packet.CurrentData == want {
			return packet
		}
		require.True(t, w.Acknowledge(packet.Generation, packet.Sequence, true))
	}
}

func TestConcurrentSubmitClose(t *testing.T) {
	p, _ := newControlled(t, DefaultLimits())
	r := testRequest(t, view.TileID{})
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 100 {
				_, _ = p.Submit(r)
				_ = p.Status()
				if l, ok := p.Next(); ok {
					l.Release()
				}
			}
		})
	}
	wg.Wait()
	p.Close()
}

func TestDefaultCoverFollowsCoarser(t *testing.T) {
	var mu sync.Mutex
	zooms := make(map[uint32]int)
	p, err := New(func(_ context.Context, key Key, dst io.Writer) error {
		mu.Lock()
		zooms[key.Tile.Z]++
		mu.Unlock()
		_, err := dst.Write(tilePBF())
		return err
	}, DefaultLimits())
	require.NoError(t, err)
	t.Cleanup(func() { p.Close(); waitDone(t, p.Done()) })
	r := testRequest(t)
	r.Camera = view.NewCamera(view.Coordinate{Latitude: 40.4168, Longitude: -3.7038}, 6, 0, 512, 512)
	r.Style.Options.Zoom, r.Style.Options.Coarser = view.StyleZoomAt(r.Camera.Zoom, 1), 1
	want := view.VisibleTileCoverAt(r.Camera, 1)
	require.Less(t, len(want), len(view.VisibleTileCover(r.Camera)))
	_, err = p.Submit(r)
	require.NoError(t, err)
	l := nextLease(t, p, func(l *Lease) bool { return len(l.Snapshot.Cover) == len(want) })
	assert.ElementsMatch(t, want, l.Snapshot.Cover)
	// An unconsumed publication counts as pending, so wait for the loads instead.
	status := waitStatus(t, p, func(s Status) bool { return s.Jobs == 0 && s.SelectedTiles == len(want) })
	assert.Zero(t, status.Failed)
	assert.Equal(t, len(want), status.Requested)
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, len(want), zooms[5])
	assert.Zero(t, zooms[6], "tiles at the camera zoom must not be loaded")
}

func TestSubmitRejectsCoarserOutOfRange(t *testing.T) {
	p, _ := newControlled(t, DefaultLimits())
	for _, coarser := range []int{-1, view.MaxCoarser + 1} {
		r := testRequest(t, view.TileID{})
		r.Style.Options.Coarser = coarser
		revision, err := p.Submit(r)
		assert.ErrorIs(t, err, ErrInput)
		assert.Zero(t, revision)
	}
	r := testRequest(t, view.TileID{})
	r.Style.Options.Coarser = view.MaxCoarser
	_, err := p.Submit(r)
	assert.NoError(t, err)
}

func TestDiscardPreparationPreparesAgainForNewAssets(t *testing.T) {
	layers, err := style.Parse([]byte(`{"version":8,"layers":[{"id":"labels","type":"symbol","source-layer":"labels","layout":{"text-field":"A","text-font":["Test"],"text-size":16},"paint":{"text-color":"black"}}]}`))
	require.NoError(t, err)
	for _, test := range []struct {
		name     string
		discard  bool
		prepares uint64
	}{
		{"kept preparation serves new assets", false, 1},
		{"discarded preparation is rebuilt from the raw response", true, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			limits := DefaultLimits()
			limits.Workers, limits.RawBytes, limits.DiscardPreparation = 1, 1024, test.discard
			p, c := newControlled(t, limits)
			r := testRequest(t, view.TileID{})
			r.Style.Layers = layers
			_, err := p.Submit(r)
			require.NoError(t, err)
			nextCall(t, c).reply <- answer{data: tilePBF()}
			nextLease(t, p, func(l *Lease) bool { return len(l.Snapshot.Cover) == 1 })
			status := waitStatus(t, p, func(s Status) bool { return s.Builds == 1 })
			assert.Equal(t, test.discard, status.Cache.Prepared == 0)
			assert.NotZero(t, status.Cache.Fragments)
			assert.NotZero(t, status.Cache.Raw)

			r.Assets = &Assets{Epoch: 2, Bytes: 1024, Value: tiles.Assets{Fonts: map[string]map[uint32]glyph.Glyph{
				"Test": {'A': {ID: 'A', Width: 2, Height: 2, Advance: 4, Bitmap: bytes.Repeat([]byte{90}, 64)}},
			}}}
			revision, err := p.Submit(r)
			require.NoError(t, err)
			ready := nextLease(t, p, func(l *Lease) bool { return l.Revision == revision && len(l.Snapshot.Scene.Draws) > 0 })
			require.NoError(t, ready.Snapshot.Scene.Validate())
			status = waitStatus(t, p, func(s Status) bool { return s.Builds == 2 })
			assert.Equal(t, test.prepares, status.Prepares)
			assert.Equal(t, uint64(1), status.Loads, "new assets never load the tile again")
			assert.Equal(t, test.discard, status.Cache.Prepared == 0)
			assert.Zero(t, status.Failed)
		})
	}
}

// marginProducer answers every load and counts them by tile.
func marginProducer(t *testing.T, margin float64) (*Producer, func(view.TileID) int) {
	t.Helper()
	var mu sync.Mutex
	loads := make(map[view.TileID]int)
	limits := DefaultLimits()
	limits.DrawMargin = margin
	p, err := New(func(_ context.Context, key Key, dst io.Writer) error {
		mu.Lock()
		loads[key.Tile]++
		mu.Unlock()
		_, err := dst.Write(tilePBF())
		return err
	}, limits)
	require.NoError(t, err)
	t.Cleanup(func() { p.Close(); waitDone(t, p.Done()) })
	return p, func(tile view.TileID) int {
		mu.Lock()
		defer mu.Unlock()
		return loads[tile]
	}
}

func marginRequest(t *testing.T, camera view.Camera) Request {
	t.Helper()
	r := testRequest(t)
	r.Camera = camera
	r.Style.Options.Zoom = view.StyleZoom(camera.Zoom)
	return r
}

// settled waits until every target of the request is compiled. Parents are
// cached for a while too, so the count of cached tiles alone proves nothing.
func settled(t *testing.T, p *Producer, revision uint64, targets []view.TileID, loads func(view.TileID) int) Status {
	t.Helper()
	return waitStatus(t, p, func(s Status) bool {
		for _, tile := range targets {
			if loads(tile) == 0 {
				return false
			}
		}
		// An unconsumed publication is the one pending item left.
		return s.Revision == revision && s.Jobs == 0 && s.Requested == len(targets) && s.Pending <= 1 && s.Failed == 0
	})
}

func TestDrawMarginComposesOnlyNearbyTargets(t *testing.T) {
	madrid := view.Coordinate{Latitude: 40.4168, Longitude: -3.7038}
	camera := view.NewCamera(madrid, 16, 0, 800, 600)
	cover := view.VisibleTileCover(camera)
	near := view.TilesNear(camera, cover, view.TileSize)
	require.Less(t, len(near), len(cover))

	p, loads := marginProducer(t, view.TileSize)
	revision, err := p.Submit(marginRequest(t, camera))
	require.NoError(t, err)
	settled(t, p, revision, cover, loads)
	l := nextLease(t, p, func(l *Lease) bool { return l.Revision == revision && len(l.Snapshot.Cover) == len(near) })
	assert.ElementsMatch(t, near, l.Snapshot.Cover)
	for _, tile := range cover {
		assert.Equal(t, 1, loads(tile), "targets beyond the margin are loaded too: %+v", tile)
	}
	l.Release()

	// A pan brings a loaded target within the margin: it is composed, not loaded.
	var moved view.Camera
	var movedCover, movedNear []view.TileID
	entered := 0
	for distance := 50.0; distance <= 2000 && entered == 0; distance += 50 {
		moved = camera.Panned(0, distance)
		movedCover = view.VisibleTileCover(moved)
		movedNear = view.TilesNear(moved, movedCover, view.TileSize)
		for _, tile := range movedNear {
			if slices.Contains(cover, tile) && !slices.Contains(near, tile) {
				entered++
			}
		}
	}
	require.NotZero(t, entered, "no pan brings a loaded target within the margin")
	revision, err = p.Submit(marginRequest(t, moved))
	require.NoError(t, err)
	settled(t, p, revision, movedCover, loads)
	l = nextLease(t, p, func(l *Lease) bool { return l.Revision == revision && len(l.Snapshot.Cover) == len(movedNear) })
	assert.ElementsMatch(t, movedNear, l.Snapshot.Cover)
	for _, tile := range movedNear {
		assert.Equal(t, 1, loads(tile))
	}
}

func TestDrawMarginKeepsCurrentTargetsUntilTwiceAsFar(t *testing.T) {
	madrid := view.Coordinate{Latitude: 40.4168, Longitude: -3.7038}
	camera := view.NewCamera(madrid, 16, 0, 800, 600)
	cover := view.VisibleTileCover(camera)
	near := view.TilesNear(camera, cover, view.TileSize)

	// Find a pan that leaves a drawn target between one and two margins away.
	var moved view.Camera
	var want, movedNear []view.TileID
	for distance := 50.0; distance <= 2000 && len(want) == len(movedNear); distance += 50 {
		moved = camera.Panned(0, distance)
		movedCover := view.VisibleTileCover(moved)
		movedNear = view.TilesNear(moved, movedCover, view.TileSize)
		want = want[:0]
		for _, tile := range view.TilesNear(moved, movedCover, 2*view.TileSize) {
			if slices.Contains(movedNear, tile) || slices.Contains(near, tile) {
				want = append(want, tile)
			}
		}
	}
	require.Greater(t, len(want), len(movedNear), "no pan leaves a drawn target between the margins")

	for _, test := range []struct {
		name         string
		acknowledged bool
		want         []view.TileID
	}{
		{"a target of Current stays", true, want},
		{"without Current only the margin counts", false, movedNear},
	} {
		t.Run(test.name, func(t *testing.T) {
			p, loads := marginProducer(t, view.TileSize)
			revision, err := p.Submit(marginRequest(t, camera))
			require.NoError(t, err)
			settled(t, p, revision, cover, loads)
			l := nextLease(t, p, func(l *Lease) bool { return l.Revision == revision && len(l.Snapshot.Cover) == len(near) })
			if test.acknowledged {
				require.True(t, p.Current(1, 1, l))
			}
			revision, err = p.Submit(marginRequest(t, moved))
			require.NoError(t, err)
			settled(t, p, revision, view.VisibleTileCover(moved), loads)
			next := nextLease(t, p, func(l *Lease) bool { return l.Revision == revision && len(l.Snapshot.Cover) == len(test.want) })
			assert.ElementsMatch(t, test.want, next.Snapshot.Cover)
		})
	}
}

func TestDrawMarginZeroComposesEveryTarget(t *testing.T) {
	camera := view.NewCamera(view.Coordinate{Latitude: 40.4168, Longitude: -3.7038}, 16, 0, 800, 600)
	cover := view.VisibleTileCover(camera)
	p, loads := marginProducer(t, 0)
	revision, err := p.Submit(marginRequest(t, camera))
	require.NoError(t, err)
	settled(t, p, revision, cover, loads)
	l := nextLease(t, p, func(l *Lease) bool { return l.Revision == revision && len(l.Snapshot.Cover) == len(cover) })
	assert.ElementsMatch(t, cover, l.Snapshot.Cover)
}
