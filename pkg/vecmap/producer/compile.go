package producer

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/rubiojr/whereami/pkg/vecmap/style"
	"github.com/rubiojr/whereami/pkg/vecmap/tiles"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
)

// compileJob prepares and builds one tile on a compiler goroutine. Its inputs
// are immutable while it runs. The owner cancels it when the result could no
// longer be installed, which skips a Build not yet started.
type compileJob struct {
	ctx      context.Context
	cancel   context.CancelFunc
	tile     view.TileID
	source   string
	raw      []byte           // the cached response, never modified
	prepared *tiles.Prepared  // a reusable preparation, or nil to prepare raw
	decoded  *tiles.Source    // raw already decoded, or nil to decode it
	stable   tiles.StableBase // a fragment whose StableMesh the preparation may borrow
	layers   []style.CompiledLayer
	options  tiles.PrepareOptions
	style    uint64 // owner style epoch the job prepares for
	limit    uint64 // texture bytes the build may produce
	cause    prepareCause
	place    tilePlace
}

// tilePlace is where a desired tile lies relative to the camera's view.
type tilePlace uint8

const (
	placeVisible tilePlace = iota
	placeRing              // a prefetch target around the visible tiles
	placeParent            // a fallback parent that is not a target
)

type prepareCause uint8

const (
	prepareFirst prepareCause = iota
	prepareStyleZoom
	prepareStyle
	prepareRepeat
)

// prepareCauseOf says why an entry needs preparing for style: how the inputs of
// its last installed preparation differ.
func prepareCauseOf(e *entry, style *Style) prepareCause {
	last := e.styleSnapshot
	switch {
	case last == nil:
		return prepareFirst
	case samePreparation(last, style):
		return prepareRepeat
	}
	zoomed := *last
	zoomed.Options.Zoom = style.Options.Zoom
	if samePreparation(&zoomed, style) {
		return prepareStyleZoom
	}
	return prepareStyle
}

func (c *PrepareCauses) add(cause prepareCause, place tilePlace) {
	switch cause {
	case prepareFirst:
		c.First++
	case prepareStyleZoom:
		c.StyleZoom++
	case prepareStyle:
		c.Style++
	default:
		c.Repeat++
	}
	switch place {
	case placeRing:
		c.Ring++
	case placeParent:
		c.Parent++
	}
}

// placeOf classifies a desired tile against visible, the camera's cover without
// a prefetch ring.
func (s *state) placeOf(tile view.TileID, visible []view.TileID) tilePlace {
	switch {
	case slices.Contains(visible, tile):
		return placeVisible
	case slices.Contains(s.targets, tile):
		return placeRing
	}
	return placeParent
}

// visibleCover is the latest request's cover without a prefetch ring.
func (s *state) visibleCover() []view.TileID {
	return view.VisibleTileCoverRing(s.request.Camera, s.request.Style.Options.Coarser, 0)
}

type compileResult struct {
	job                 compileJob
	prepared            *tiles.Prepared
	decoded             *tiles.Source // decoded by this job
	fragment            *tiles.Fragment
	borrowed            bool   // the build borrowed the job's stable base
	assets              uint64 // owner asset epoch of the build
	ranPrepare, built   bool
	preparing, building time.Duration
	stage               string // where err happened
	err                 error
}

// buildAssets are the assets builds use, with their owner epoch. Compilers read
// the newest at Build time, so a preparation finished during an asset change
// is built with the new assets instead of being discarded.
type buildAssets struct {
	value tiles.Assets
	epoch uint64
}

func (p *Producer) compiler(wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case <-p.ctx.Done():
			return
		case j := <-p.compileJobs:
			r := prepareTile(j)
			if r.err == nil && j.ctx.Err() == nil {
				r = p.buildTile(r)
			}
			select {
			case p.compiled <- r:
			case <-p.ctx.Done():
				return
			}
		}
	}
}

// prepareTile runs the job's preparation unless it reuses one, decoding raw
// unless the job carries a decoded source.
func prepareTile(j compileJob) compileResult {
	r := compileResult{job: j, prepared: j.prepared, stage: "prepare"}
	if r.prepared == nil {
		started := time.Now()
		source := j.decoded
		if source == nil {
			source, r.err = tiles.Decode(j.raw, j.options.Indexed)
			r.decoded = source
		}
		if r.err == nil {
			r.prepared, r.err = tiles.PrepareSourceReusing(source, j.layers, j.options, j.stable)
		}
		r.preparing, r.ranPrepare = time.Since(started), true
	}
	return r
}

// buildTile packs a preparation with the newest assets.
func (p *Producer) buildTile(r compileResult) compileResult {
	assets := p.buildAssets.Load()
	started := time.Now()
	built, err := r.prepared.BuildOwned(assets.value, r.job.limit)
	r.building, r.built, r.assets = time.Since(started), true, assets.epoch
	r.stage, r.err = "build", err
	if err == nil {
		r.fragment, r.borrowed = built.Fragment, built.Borrowed
	}
	return r
}

// compile hands tiles that need building at the working epoch to free
// compilers. At most Compilers jobs are unfinished, so the buffer has room.
func (s *state) compile() {
	if s.request == nil || len(s.compiling) >= s.p.limits.Compilers {
		return
	}
	cover := s.selectedCover() // starting a job does not change it
	visible := s.visibleCover()
	for len(s.compiling) < s.p.limits.Compilers {
		tile, e, ok := s.nextCompile(cover)
		if !ok {
			return
		}
		s.p.compileJobs <- s.startCompile(tile, e, s.placeOf(tile, visible))
	}
}

// nextCompile picks the next cached tile to build at the working epoch. It
// refreshes the currently selectable cover before refinements that cannot yet
// be published, keeping LoadOrder within each class. This does not turn a CPU
// target into continuity; coverage still uses acknowledged Current.
func (s *state) nextCompile(cover []view.TileID) (view.TileID, *entry, bool) {
	if s.request == nil {
		return view.TileID{}, nil, false
	}
	for _, selected := range []bool{true, false} {
		for _, tile := range s.order {
			if _, running := s.compiling[tile]; running || !s.desired[tile] || slices.Contains(cover, tile) != selected {
				continue
			}
			e := s.entries[tile]
			if e == nil || e.source != s.working.Style.Source || (e.style == s.style && e.assets == s.assets) || s.failures[tile].attempts >= 3 ||
				s.deferred(e, selected) {
				continue
			}
			return tile, e, true
		}
	}
	return view.TileID{}, nil, false
}

// deferred reports whether Limits.DeferHiddenRefresh postpones compiling a
// desired entry that isn't at the working epoch. selected is whether the tile
// is in the selected cover.
func (s *state) deferred(e *entry, selected bool) bool {
	return s.p.limits.DeferHiddenRefresh && !selected && e != nil && e.fragment != nil && e.source == s.working.Style.Source
}

// startCompile registers a job for a cached tile. A preparation made for the
// working style is reused, so an asset change only builds again.
func (s *state) startCompile(tile view.TileID, e *entry, place tilePlace) compileJob {
	ctx, cancel := context.WithCancel(s.p.ctx)
	options := s.working.Style.Options
	options.Tile = tile
	j := compileJob{ctx: ctx, cancel: cancel, tile: tile, source: e.source, raw: e.raw, layers: s.working.Style.Layers,
		options: options, style: s.style, limit: max(s.p.limits.CacheBytes/uint64(s.p.limits.Compilers), 1), place: place}
	if e.style == s.style {
		j.prepared = e.prepared
	}
	if j.prepared == nil {
		j.cause = prepareCauseOf(e, s.working.Style)
		j.decoded = e.decoded
		if s.p.limits.ReuseStable {
			j.stable = e.stable
		}
	}
	s.compiling[tile] = j
	s.stats.PeakCompiling = max(s.stats.PeakCompiling, len(s.compiling))
	return j
}

// compiled installs a finished job, unless newer demand made it obsolete:
// another style or source, a tile that left the cover, or a build with assets
// that are no longer the working ones.
func (s *state) compiled(r compileResult) {
	j := r.job
	defer j.cancel()
	delete(s.compiling, j.tile)
	if r.ranPrepare {
		s.stats.Preparing.observe(r.preparing)
		s.stats.Prepares++
		s.stats.PrepareCauses.add(j.cause, j.place)
		if j.decoded != nil {
			s.stats.PrepareCauses.Decoded++
		}
		s.workingPrepares++
	}
	if r.built {
		s.stats.Building.observe(r.building)
		s.stats.Builds++
		if r.borrowed {
			s.stats.PrepareCauses.Stable++
		}
	}
	old := s.entries[j.tile]
	if s.p.ctx.Err() != nil || j.ctx.Err() != nil || j.style != s.style || !s.desired[j.tile] || old == nil || old.source != j.source ||
		(r.built && r.assets != s.assets) {
		s.stats.Rejected++
		if r.ranPrepare {
			s.stats.PrepareCauses.Wasted++
		}
		if r.err == nil && !r.built {
			s.stats.SkippedBuilds++
		}
		return
	}
	next := *old
	stage, err := r.stage, r.err
	if err == nil {
		stage = "cache/compiled"
		next.prepared, next.fragment = r.prepared, r.fragment
		if s.p.limits.DiscardPreparation {
			next.prepared = nil
		}
		if r.decoded != nil && s.p.limits.ReuseDecoded {
			next.decoded = r.decoded
		}
		if s.p.limits.ReuseStable {
			next.stable = r.fragment.StableBase()
		}
		next.preparedUse = s.stats.Builds
		next.usage = entryUsage(j.tile, &next)
		next.styleSnapshot = s.working.Style
		if !s.admitPrepared(j.tile, &next) {
			err = ErrLimit
		}
	}
	if err == nil {
		stage = "store"
		err = s.set.Apply([]tiles.Change{{Tile: j.tile, Fragment: next.fragment}})
	}
	if err != nil {
		s.errorAt(stage, err)
		if stage == "cache/compiled" {
			s.failCapacity(j.tile, &next)
		} else {
			s.failures[j.tile] = failure{attempts: 3}
		}
		return
	}
	next.style, next.assets = s.style, s.assets
	s.entries[j.tile] = &next
	s.recordCache(j.tile, &next)
	s.dirty = true
}
