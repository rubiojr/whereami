package producer

import (
	"cmp"
	"slices"

	"github.com/rubiojr/whereami/pkg/vecmap/view"
)

// Only reconstructible preparation is reclaimable. Fragments, Set identity,
// acknowledged continuity and every lease remain untouched. Asset refresh may
// reprepare from the retained raw response when pressure evicted preparation.
func (s *state) admitPrepared(tile view.TileID, next *entry) bool {
	charge := s.cacheCharge(tile, next)
	if charge <= s.p.limits.CacheBytes {
		return true
	}
	// Do not churn useful preparation if even reclaiming all of it cannot admit
	// the immutable fragments/raw/profile union. This remains an explicit error.
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
