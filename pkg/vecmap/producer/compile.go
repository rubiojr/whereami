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
	raw      []byte          // the cached response, never modified
	prepared *tiles.Prepared // a reusable preparation, or nil to prepare raw
	layers   []style.CompiledLayer
	options  tiles.PrepareOptions
	style    uint64 // owner style epoch the job prepares for
	limit    uint64 // texture bytes the build may produce
}

type compileResult struct {
	job                 compileJob
	prepared            *tiles.Prepared
	fragment            *tiles.Fragment
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

// prepareTile runs the job's preparation unless it reuses one.
func prepareTile(j compileJob) compileResult {
	r := compileResult{job: j, prepared: j.prepared, stage: "prepare"}
	if r.prepared == nil {
		started := time.Now()
		r.prepared, r.err = tiles.Prepare(j.raw, j.layers, j.options)
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
		r.fragment = built.Fragment
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
	for len(s.compiling) < s.p.limits.Compilers {
		tile, e, ok := s.nextCompile(cover)
		if !ok {
			return
		}
		s.p.compileJobs <- s.startCompile(tile, e)
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
			if e == nil || e.source != s.working.Style.Source || (e.style == s.style && e.assets == s.assets) || s.failures[tile].attempts >= 3 {
				continue
			}
			return tile, e, true
		}
	}
	return view.TileID{}, nil, false
}

// startCompile registers a job for a cached tile. A preparation made for the
// working style is reused, so an asset change only builds again.
func (s *state) startCompile(tile view.TileID, e *entry) compileJob {
	ctx, cancel := context.WithCancel(s.p.ctx)
	options := s.working.Style.Options
	options.Tile = tile
	j := compileJob{ctx: ctx, cancel: cancel, tile: tile, source: e.source, raw: e.raw, layers: s.working.Style.Layers,
		options: options, style: s.style, limit: max(s.p.limits.CacheBytes/uint64(s.p.limits.Compilers), 1)}
	if e.style == s.style {
		j.prepared = e.prepared
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
		s.workingPrepares++
	}
	if r.built {
		s.stats.Building.observe(r.building)
		s.stats.Builds++
	}
	old := s.entries[j.tile]
	if s.p.ctx.Err() != nil || j.ctx.Err() != nil || j.style != s.style || !s.desired[j.tile] || old == nil || old.source != j.source ||
		(r.built && r.assets != s.assets) {
		s.stats.Rejected++
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
