package producer

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rubiojr/whereami/pkg/vecmap/sprite"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// iconRequest draws the labels layer's points as icons, so building a tile
// asks the assets for a sprite entry, where a test can hold the build.
func iconRequest(t *testing.T, targets ...view.TileID) Request {
	t.Helper()
	r := testRequest(t, targets...)
	layers, err := style.Parse([]byte(`{"version":8,"layers":[{"id":"background","type":"background","paint":{"background-color":"#ff0000"}},{"id":"icon","type":"symbol","source-layer":"labels","layout":{"icon-image":"dot"}}]}`))
	require.NoError(t, err)
	next := *r.Style
	next.Layers = layers
	r.Style = &next
	r.Assets = &Assets{Epoch: 1, Bytes: 1}
	return r
}

func TestCompilersBuildSeveralTilesAtOnce(t *testing.T) {
	limits := DefaultLimits()
	limits.Compilers, limits.Parents = 3, false
	p, c := newControlled(t, limits)
	targets := []view.TileID{{Z: 1}, {Z: 1, X: 1}, {Z: 1, Y: 1}}
	r := iconRequest(t, targets...)
	// Nobody leaves the callback before three builds are inside it at once.
	var inside atomic.Int32
	var once sync.Once
	all, abort := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(abort) })
	r.Assets.Value.SpriteEntry = func(string) (sprite.Entry, bool) {
		if inside.Add(1) == int32(len(targets)) {
			once.Do(func() { close(all) })
		}
		defer inside.Add(-1)
		select {
		case <-all:
		case <-abort:
		}
		return sprite.Entry{}, false
	}
	revision, err := p.Submit(r)
	require.NoError(t, err)
	for range targets {
		nextCall(t, c).reply <- answer{data: tilePBF()}
	}
	select {
	case <-all:
	case <-time.After(5 * time.Second):
		t.Fatalf("builds at once: got %d, want %d", inside.Load(), len(targets))
	}
	lease, status := settle(t, p, revision)
	require.NotNil(t, lease)
	assert.ElementsMatch(t, targets, lease.Snapshot.Cover)
	assert.Equal(t, len(targets), status.PeakCompiling)
	assert.Zero(t, status.Compiling)
	assert.Equal(t, uint64(len(targets)), status.Builds)
}

func TestOwnerTakesRequestsWhileATileBuilds(t *testing.T) {
	p, c := newControlled(t, DefaultLimits())
	tile := view.TileID{}
	r := iconRequest(t, tile)
	var once sync.Once
	entered, release := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	r.Assets.Value.SpriteEntry = func(string) (sprite.Entry, bool) {
		once.Do(func() { close(entered) })
		<-release
		return sprite.Entry{}, false
	}
	_, err := p.Submit(r)
	require.NoError(t, err)
	nextCall(t, c).reply <- answer{data: tilePBF()}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the tile was never built")
	}

	// A camera change arrives while the only compiler is busy.
	r.Camera.Bearing = 10
	revision, err := p.Submit(r)
	require.NoError(t, err)
	status := waitStatus(t, p, func(s Status) bool { return s.Revision == revision })
	assert.Equal(t, 1, status.Compiling)
	assert.Zero(t, status.Builds, "the build is still running")

	close(release)
	lease, status := settle(t, p, revision)
	require.NotNil(t, lease)
	assert.Equal(t, []view.TileID{tile}, lease.Snapshot.Cover)
	assert.Equal(t, uint64(1), status.Builds, "a camera change does not discard the build")
}
