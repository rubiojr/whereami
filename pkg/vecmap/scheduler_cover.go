package vecmap

import "github.com/rubiojr/whereami/pkg/vecmap/view"

func (s *tileSchedulerState) sameCover(tiles []vectorTileID) bool {
	if len(tiles) != len(s.targets) {
		return false
	}
	for _, tile := range tiles {
		if _, exists := s.targets[tile]; !exists {
			return false
		}
	}
	return true
}

func (s *tileSchedulerState) setCover(tiles []vectorTileID) {
	targets := make(map[vectorTileID]struct{}, len(tiles))
	order := make([]vectorTileID, 0, len(tiles))
	for _, tile := range tiles {
		if _, duplicate := targets[tile]; duplicate {
			continue
		}
		targets[tile] = struct{}{}
		order = append(order, tile)
	}
	desired := make(map[vectorTileID]struct{}, len(order)*2)
	resourceOrder := make([]vectorTileID, 0, len(order)*2)
	// Load coarse fallback coverage before detailed targets so camera flights
	// fill the viewport before refining it.
	for _, tile := range order {
		if parent, exists := tile.Parent(); exists {
			if _, duplicate := desired[parent]; !duplicate {
				desired[parent] = struct{}{}
				resourceOrder = append(resourceOrder, parent)
			}
		}
	}
	for _, tile := range order {
		if _, duplicate := desired[tile]; !duplicate {
			desired[tile] = struct{}{}
			resourceOrder = append(resourceOrder, tile)
		}
	}
	continuity := make(map[vectorTileID]struct{}, len(s.rendered))
	for _, tile := range s.rendered {
		if s.loaded[tile] != nil {
			continuity[tile] = struct{}{}
		}
	}
	for tile, load := range s.inFlight {
		if _, wanted := desired[tile]; !wanted {
			load.cancel()
			delete(s.inFlight, tile)
		}
	}
	for tile := range s.loaded {
		_, wanted := desired[tile]
		_, retained := continuity[tile]
		if !wanted && !retained {
			delete(s.loaded, tile)
		}
	}
	for tile := range s.failed {
		if _, wanted := desired[tile]; !wanted {
			delete(s.failed, tile)
		}
	}
	for tile := range s.attempts {
		if _, wanted := desired[tile]; !wanted {
			delete(s.attempts, tile)
		}
	}

	s.order = order
	s.resourceOrder = resourceOrder
	s.targets = targets
	s.desired = desired
	s.continuity = continuity
	s.pending = s.pending[:0]
	for _, tile := range resourceOrder {
		_, loaded := s.loaded[tile]
		_, loading := s.inFlight[tile]
		_, failed := s.failed[tile]
		if !loaded && !loading && !failed {
			s.pending = append(s.pending, tile)
		}
	}
	s.dropSatisfiedParents()
}

func (s *tileSchedulerState) dropSatisfiedParents() {
	drop := make(map[vectorTileID]struct{})
	for _, group := range s.targetGroups() {
		if !group.HasParent {
			continue
		}
		allTargetsLoaded := true
		for _, tile := range group.Targets {
			if s.loaded[tile] == nil {
				allTargetsLoaded = false
				break
			}
		}
		if !allTargetsLoaded {
			continue
		}
		drop[group.Parent] = struct{}{}
		delete(s.desired, group.Parent)
		delete(s.loaded, group.Parent)
		delete(s.failed, group.Parent)
		delete(s.attempts, group.Parent)
		if load, loading := s.inFlight[group.Parent]; loading {
			load.cancel()
			delete(s.inFlight, group.Parent)
		}
	}
	if len(drop) == 0 {
		return
	}
	if len(s.pending) > 0 {
		pending := s.pending[:0]
		for _, tile := range s.pending {
			if _, remove := drop[tile]; !remove {
				pending = append(pending, tile)
			}
		}
		s.pending = pending
	}
	resources := s.resourceOrder[:0]
	for _, tile := range s.resourceOrder {
		if _, remove := drop[tile]; !remove {
			resources = append(resources, tile)
		}
	}
	s.resourceOrder = resources
}

func (s *tileSchedulerState) targetGroups() []view.TileGroup {
	return view.GroupTiles(s.order)
}

func (s *tileSchedulerState) renderSelection() ([]loadedRoadTile, int) {
	previous := make([]vectorTileID, 0, len(s.rendered))
	for _, tile := range s.rendered {
		if _, eligible := s.continuity[tile]; eligible {
			previous = append(previous, tile)
		}
	}
	cover, fallbacks := view.SelectCover(s.order, previous, func(tile view.TileID) bool { return s.loaded[tile] != nil })
	selected := make([]loadedRoadTile, len(cover))
	for i, tile := range cover {
		selected[i] = loadedRoadTile{id: tile, roads: s.loaded[tile]}
	}
	return selected, fallbacks
}
