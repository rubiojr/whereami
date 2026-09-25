package producer

import (
	"context"
	"slices"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/sprite"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
	"github.com/rubiojr/whereami/pkg/vecmap/tiles"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Drive owner transitions without a scheduling race. A queued input at build's
// phase boundary models input arriving while Prepare is running.
func churnState(t *testing.T, r Request) *state {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	p := &Producer{ctx: ctx, cancel: cancel, limits: DefaultLimits(), wake: make(chan struct{}, 1), leases: make(map[*Lease]struct{})}
	set, err := tiles.New(p.limits.Store)
	require.NoError(t, err)
	s := &state{p: p, set: set, entries: make(map[view.TileID]*entry), running: make(map[view.TileID]job), failures: make(map[view.TileID]failure)}
	_, err = p.Submit(r)
	require.NoError(t, err)
	s.inputs()
	return s
}

func TestCurrentSequenceDoesNotRepublishUnchangedCoverage(t *testing.T) {
	r := testRequest(t, view.TileID{})
	s := churnState(t, r)
	s.entries[view.TileID{}] = &entry{raw: tilePBF(), source: r.Style.Source}
	require.True(t, s.compile())
	s.publish()
	l, ok := s.p.Next()
	require.True(t, ok)
	t.Cleanup(l.Release)
	require.True(t, s.p.Current(1, 1, l))
	s.inputs()
	require.True(t, s.dirty, "first Current establishes continuity")
	s.publish()
	if output, ok := s.p.Next(); ok {
		output.Release()
	}
	selections := s.stats.Selecting.Count
	for sequence := uint64(2); sequence < 100; sequence++ {
		require.True(t, s.p.Current(1, sequence, l))
		s.inputs()
		s.publish()
		s.report()
		assert.Equal(t, sequence, s.p.Status().CurrentSequence)
		assert.False(t, s.dirty)
	}
	assert.Equal(t, selections, s.stats.Selecting.Count)
	assert.Equal(t, uint64(98), s.stats.UnchangedCurrent)
	_, ok = s.p.Next()
	assert.False(t, ok, "upload acknowledgements don't generate CPU lease churn")
	// Reset clears continuity and must still trigger selection once.
	require.True(t, s.p.ResetCurrent(2))
	s.inputs()
	assert.True(t, s.dirty)
}

func TestQueuedDemandBetweenPreparationAndBuild(t *testing.T) {
	for _, kind := range []string{"style", "source", "clear", "assets", "camera"} {
		t.Run("test"+kind, func(t *testing.T) {
			r := testRequest(t, view.TileID{})
			s := churnState(t, r)
			old := &entry{raw: tilePBF(), source: r.Style.Source}
			s.entries[view.TileID{}] = old
			switch kind {
			case "style", "source":
				next := *r.Style
				next.Epoch++
				next.Options.Zoom++
				if kind == "source" {
					next.Source = "replacement"
				}
				r.Style = &next
			case "clear":
				r.Targets = []view.TileID{}
			case "assets":
				r.Assets = &Assets{Epoch: 2, Bytes: 1}
			case "camera":
				r.Camera.Bearing++
			}
			_, err := s.p.Submit(r)
			require.NoError(t, err)
			s.build(view.TileID{}, old)
			assert.Equal(t, uint64(1), s.stats.Prepares)
			if kind == "assets" || kind == "camera" {
				assert.Equal(t, uint64(1), s.stats.Builds)
				assert.Zero(t, s.stats.Rejected)
				assert.Equal(t, s.assets, s.entries[view.TileID{}].assets)
			} else {
				assert.Zero(t, s.stats.Builds)
				assert.Equal(t, uint64(1), s.stats.SkippedBuilds)
				assert.Equal(t, uint64(1), s.stats.Rejected)
				assert.Same(t, old, s.entries[view.TileID{}])
				assert.Nil(t, old.fragment)
			}
		})
	}
}

func TestCoherencePreflightPreservesExactSelectedCover(t *testing.T) {
	left, right := view.TileID{Z: 1}, view.TileID{Z: 1, X: 1}
	r := testRequest(t, left, right)
	s := churnState(t, r)
	for _, tile := range []view.TileID{{}, left, right} {
		e := &entry{raw: tilePBF(), source: r.Style.Source}
		s.entries[tile] = e
		s.build(tile, e)
	}
	s.current.cover = []view.TileID{left, right}
	// A refreshed parent cannot replace old acknowledged children merely to
	// make publication coherent. Readiness includes those old fragments too.
	s.style++
	s.entries[view.TileID{}].style = s.style
	s.dirty = true
	s.publish()
	assert.Zero(t, s.stats.Selecting.Count)
	assert.Equal(t, uint64(1), s.stats.DeferredSelections)
	assert.Nil(t, s.p.output)
	assert.False(t, s.dirty, "wait for a real input/build change, don't spin")
	s.entries[left].style = s.style
	assert.False(t, s.coherentCover(), "mixed siblings cannot publish")
	s.entries[right].style = s.style
	s.dirty = true
	s.publish()
	require.NotNil(t, s.p.output)
	assert.Equal(t, []view.TileID{left, right}, s.p.output.Snapshot.Cover)
	assert.Equal(t, uint64(1), s.stats.Selecting.Count)
	s.p.output.Release()
}

func TestUsefulLoadSurvivesStyleChangeAndUsesLatestPaint(t *testing.T) {
	limits := DefaultLimits()
	limits.Workers = 1
	limits.RawBytes = 128
	p, c := newControlled(t, limits)
	r := testRequest(t, view.TileID{})
	_, err := p.Submit(r)
	require.NoError(t, err)
	load := nextCall(t, c)
	layers, err := style.Parse([]byte(`{"version":8,"layers":[{"id":"background","type":"background","paint":{"background-color":"#00ff00"}}]}`))
	require.NoError(t, err)
	next := *r.Style
	next.Epoch++
	next.Layers = layers
	r.Style = &next
	rev, err := p.Submit(r)
	require.NoError(t, err)
	waitStatus(t, p, func(s Status) bool { return s.Revision == rev })
	assert.NoError(t, load.ctx.Err())
	load.reply <- answer{data: tilePBF()}
	l := nextLease(t, p, func(l *Lease) bool { return l.Revision == rev && len(l.Snapshot.Cover) == 1 })
	assert.Equal(t, []byte{0, 255, 0, 255}, byteColor(l))
	assert.Equal(t, uint64(1), p.Status().Loads)
	assert.Equal(t, uint64(1), p.Status().LoadStyleReuses)
	assert.Zero(t, p.Status().Rejected)
}

func TestAssetRefreshBetweenPhasesUsesLatestCallbacks(t *testing.T) {
	r := testRequest(t, view.TileID{})
	layers, err := style.Parse([]byte(`{"version":8,"layers":[{"id":"icon","type":"symbol","source-layer":"labels","layout":{"icon-image":"dot"}}]}`))
	require.NoError(t, err)
	r.Style.Layers = layers
	oldCalls, newCalls := 0, 0
	r.Assets.Value.SpriteEntry = func(string) (sprite.Entry, bool) { oldCalls++; return sprite.Entry{}, false }
	s := churnState(t, r)
	r.Assets = &Assets{Epoch: 2, Bytes: 1, Value: tiles.Assets{SpriteEntry: func(string) (sprite.Entry, bool) { newCalls++; return sprite.Entry{}, false }}}
	_, err = s.p.Submit(r)
	require.NoError(t, err)
	e := &entry{raw: tilePBF(), source: r.Style.Source}
	s.entries[view.TileID{}] = e
	s.build(view.TileID{}, e)
	assert.Zero(t, oldCalls)
	assert.Positive(t, newCalls)
	assert.Equal(t, uint64(1), s.stats.Prepares)
	assert.Equal(t, uint64(1), s.stats.Builds)
}

func TestSourceChangeStillCancelsInFlightRaw(t *testing.T) {
	limits := DefaultLimits()
	limits.Workers = 1
	limits.RawBytes = 128
	p, c := newControlled(t, limits)
	r := testRequest(t, view.TileID{})
	_, err := p.Submit(r)
	require.NoError(t, err)
	old := nextCall(t, c)
	next := *r.Style
	next.Epoch++
	next.Source = "different-data"
	r.Style = &next
	rev, err := p.Submit(r)
	require.NoError(t, err)
	waitDone(t, old.ctx.Done())
	old.reply <- answer{data: tilePBF()} // ignores cancellation
	fresh := nextCall(t, c)
	assert.Equal(t, next.Source, fresh.key.Source)
	assert.Greater(t, fresh.key.Generation, old.key.Generation)
	fresh.reply <- answer{data: tilePBF()}
	nextLease(t, p, func(l *Lease) bool { return l.Revision == rev && len(l.Snapshot.Cover) == 1 })
	assert.Equal(t, uint64(1), p.Status().Rejected)
	assert.Equal(t, uint64(1), p.Status().Prepares)
	assert.Zero(t, p.Status().LoadStyleReuses)
	assert.Equal(t, 1, p.Status().PeakJobs)
}

func TestRefreshPrioritizesSelectableCoverBeforeRefinement(t *testing.T) {
	a, missing, b := view.TileID{Z: 2}, view.TileID{Z: 2, X: 1}, view.TileID{Z: 2, X: 2}
	parentA, _ := a.Parent()
	parentB, _ := b.Parent()
	r := testRequest(t, a, missing, b)
	s := churnState(t, r)
	for _, tile := range []view.TileID{parentA, parentB, a, b} {
		e := &entry{raw: tilePBF(), source: r.Style.Source}
		s.entries[tile] = e
		s.build(tile, e)
	}
	s.entries[missing] = &entry{raw: tilePBF(), source: r.Style.Source}
	s.style++
	s.entries[parentA].style = s.style
	s.entries[a].style = s.style
	s.pinned[parentB] = true // old native Current may still borrow this parent
	s.refine()
	assert.False(t, s.desired[parentB], "installed sibling hides its parent even during epoch refresh")
	s.evict()
	require.NotNil(t, s.entries[parentB], "a hidden pinned parent is retained, not rebuilt")
	builds := s.stats.Builds
	require.True(t, s.compile())
	assert.Equal(t, s.style, s.entries[b].style)
	assert.Nil(t, s.entries[missing].fragment, "refresh selected b before nearer unfinished sibling")
	assert.Equal(t, builds+1, s.stats.Builds)
	s.publish()
	require.NotNil(t, s.p.output)
	assert.Equal(t, []view.TileID{parentA, b}, s.p.output.Snapshot.Cover)
	s.p.output.Release()
	require.True(t, s.compile())
	s.publish()
	require.NotNil(t, s.p.output)
	assert.Equal(t, []view.TileID{a, missing, b}, s.p.output.Snapshot.Cover)
	s.p.output.Release()
	assert.False(t, s.compile(), "no obsolete fallback compilation after refinement")
}

func TestReturningStyleReusesUnmodifiedTileAndResourceIdentities(t *testing.T) {
	r := testRequest(t, view.TileID{})
	originalStyle := r.Style
	s := churnState(t, r)
	s.entries[view.TileID{}] = &entry{raw: tilePBF(), source: r.Style.Source}
	require.True(t, s.compile())
	s.publish()
	first, ok := s.p.Next()
	require.True(t, ok)
	t.Cleanup(first.Release)
	entry := s.entries[view.TileID{}]
	prepared := entry.prepared
	usage := s.stats.Cache
	// The owner observes B, then returns to A's exact style inputs before this
	// tile was rebuilt. Epoch still advances, but immutable pixels didn't change.
	for _, zoom := range []float64{3, 2} {
		next := *r.Style
		next.Epoch++
		next.Options.Zoom = zoom
		r.Style = &next
		_, err := s.p.Submit(r)
		require.NoError(t, err)
		s.inputs()
	}
	assert.Equal(t, uint64(1), s.stats.StyleReuses)
	assert.False(t, s.compile())
	assert.Same(t, prepared, entry.prepared)
	assert.Same(t, originalStyle, entry.styleSnapshot, "original borrowed profile remains charged")
	assert.Equal(t, s.style, entry.style)
	s.publish()
	second, ok := s.p.Next()
	require.True(t, ok)
	t.Cleanup(second.Release)
	assert.Same(t, first.Snapshot, second.Snapshot, "no Apply means no artificial resource revision")
	assert.Equal(t, usage, s.stats.Cache)
	assert.Equal(t, uint64(1), s.stats.Prepares)
	assert.Equal(t, uint64(1), s.stats.Builds)
	// Assets remain an independent epoch: reuse preparation, rebuild owned output.
	r.Assets = &Assets{Epoch: 2, Bytes: 1}
	_, err := s.p.Submit(r)
	require.NoError(t, err)
	s.inputs()
	require.True(t, s.compile())
	assert.Equal(t, uint64(1), s.stats.Prepares)
	assert.Equal(t, uint64(2), s.stats.Builds)
	assert.Equal(t, []byte{255, 0, 0, 255}, byteColor(first))
}

func TestPreparationIdentityIsConservative(t *testing.T) {
	original := testRequest(t).Style
	for _, tc := range []struct {
		name string
		edit func(*Style)
		want bool
	}{
		{"epoch", func(s *Style) { s.Epoch++ }, true},
		{"source", func(s *Style) { s.Source = "other" }, false},
		{"zoom", func(s *Style) { s.Options.Zoom += 1.0 / 16 }, false},
		{"topology", func(s *Style) { s.Options.Indexed = !s.Options.Indexed }, false},
		{"limits", func(s *Style) { s.Options.ElementLimit-- }, false},
		{"layerCopy", func(s *Style) { s.Layers = slices.Clone(s.Layers) }, false},
		{"layerCount", func(s *Style) { s.Layers = nil }, false},
	} {
		t.Run("test"+tc.name, func(t *testing.T) {
			next := *original
			tc.edit(&next)
			assert.Equal(t, tc.want, samePreparation(original, &next))
		})
	}
	assert.False(t, samePreparation(nil, original))
	assert.False(t, samePreparation(original, nil))
	empty := *original
	empty.Layers = nil
	assert.True(t, samePreparation(&empty, &empty))
}
