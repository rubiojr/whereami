package producer

import (
	"cmp"
	"slices"

	"github.com/rubiojr/whereami/pkg/vecmap/tiles"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
)

// admitPrepared fits a replacement into the cache budget. Reconstructible
// preparation is reclaimed first; every lease and Set identity of desired tiles
// remain untouched. If the fixed charge still cannot fit, cached continuity that
// no longer belongs to the requested cover is dropped from the cache and Set.
// Leases keep their own snapshot payload, so acknowledged Current still renders;
// only future selections lose that fallback. Without this, a detailed Current
// could pin the whole budget while the coarser cover that must replace it can
// never be admitted, so neither side ever advances.
func (s *state) admitPrepared(tile view.TileID, next *entry) bool {
	if s.cacheCharge(tile, next) <= s.p.limits.CacheBytes {
		return true
	}
	if s.reclaimPrepared(tile, next) {
		return true
	}
	if !s.evictContinuity(tile, next) {
		return false
	}
	return s.cacheCharge(tile, next) <= s.p.limits.CacheBytes || s.reclaimPrepared(tile, next)
}

// fixedCharge excludes every reclaimable Prepared object from the would-be cache.
func (s *state) fixedCharge(tile view.TileID, next *entry) uint64 {
	usage := s.cacheUsage(tile, next)
	return usage.Total() - usage.Prepared
}

// evictContinuity drops non-desired cached tiles, least valuable first, until the
// fixed charge fits. Tiles outside acknowledged Current go before visible
// continuity; within each class the oldest build goes first. Desired tiles are
// never evicted here. It reports whether the fixed charge now fits.
func (s *state) evictContinuity(tile view.TileID, next *entry) bool {
	limit := s.p.limits.CacheBytes
	if s.fixedCharge(tile, next) <= limit {
		return true
	}
	var candidates []view.TileID
	for key := range s.entries {
		if key != tile && !s.desired[key] {
			candidates = append(candidates, key)
		}
	}
	slices.SortFunc(candidates, func(a, b view.TileID) int {
		return cmp.Or(cmp.Compare(boolInt(slices.Contains(s.current.cover, a)), boolInt(slices.Contains(s.current.cover, b))),
			cmp.Compare(s.entries[a].preparedUse, s.entries[b].preparedUse),
			cmp.Compare(a.Z, b.Z), cmp.Compare(a.Y, b.Y), cmp.Compare(a.X, b.X))
	})
	for _, key := range candidates {
		e := s.entries[key]
		if e.fragment != nil {
			if err := s.set.Apply([]tiles.Change{{Tile: key}}); err != nil {
				s.error(err)
				return false
			}
		}
		delete(s.entries, key)
		s.recordCache(key, nil)
		s.stats.ContinuityEvictions++
		s.stats.ContinuityBytesFreed += e.usage.Total()
		s.dirty = true
		if s.fixedCharge(tile, next) <= limit {
			return true
		}
	}
	return false
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// reclaimPrepared evicts least-recently-built preparation, or omits the incoming
// one, until the charge fits. It never churns preparation when even reclaiming
// all of it cannot admit the immutable fragments/raw/profile union.
func (s *state) reclaimPrepared(tile view.TileID, next *entry) bool {
	charge := s.cacheCharge(tile, next)
	if charge <= s.p.limits.CacheBytes {
		return true
	}
	reclaimable := next.usage.Prepared
	for key, e := range s.entries {
		if key != tile {
			reclaimable += e.usage.Prepared
		}
	}
	if charge-reclaimable > s.p.limits.CacheBytes {
		return false
	}
	var candidates []view.TileID
	for key, e := range s.entries {
		if key != tile && e.prepared != nil {
			candidates = append(candidates, key)
		}
	}
	slices.SortFunc(candidates, func(a, b view.TileID) int {
		return cmp.Or(cmp.Compare(s.entries[a].preparedUse, s.entries[b].preparedUse),
			cmp.Compare(a.Z, b.Z), cmp.Compare(a.Y, b.Y), cmp.Compare(a.X, b.X))
	})
	for _, key := range candidates {
		s.dropPrepared(s.entries[key])
		s.recordCache(key, s.entries[key])
		if s.cacheCharge(tile, next) <= s.p.limits.CacheBytes {
			return true
		}
	}
	if next.prepared != nil {
		s.stats.UncachedPreparations++
		next.prepared = nil
		next.usage.Prepared = 0
	}
	return s.cacheCharge(tile, next) <= s.p.limits.CacheBytes
}

func (s *state) dropPrepared(e *entry) {
	s.stats.PreparationEvictions++
	s.stats.PreparationBytesFreed += e.usage.Prepared
	e.prepared = nil
	e.usage.Prepared = 0
}

// Retry a cache-capacity rejection only after enough non-reconstructible storage
// has actually left the cache (normally after native Current/lease retirement).
// Dropping Prepared alone cannot trigger retries: it was already excluded from
// the failed minimum charge. No polling retry loop or optimistic release credit.
func (s *state) failCapacity(tile view.TileID, next *entry) {
	f := failure{attempts: 3}
	usage := s.cacheUsage(tile, next)
	fixed := s.stats.Cache.Raw + s.stats.Cache.Fragments + s.stats.Cache.Profiles
	minimum := usage.Total() - usage.Prepared
	if minimum > s.p.limits.CacheBytes {
		shortfall := minimum - s.p.limits.CacheBytes
		if shortfall <= fixed {
			f.capacity = true
			f.fixedAt = fixed
			f.maximumFixed = fixed - shortfall
		}
	}
	s.failures[tile] = f
}

func (s *state) retryCapacity() {
	fixed := s.stats.Cache.Raw + s.stats.Cache.Fragments + s.stats.Cache.Profiles
	for tile, f := range s.failures {
		if f.capacity && fixed < f.fixedAt && fixed <= f.maximumFixed {
			delete(s.failures, tile)
			s.stats.CapacityRetries++
		}
	}
}
