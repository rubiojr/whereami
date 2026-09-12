package vecmap

type tileTargetGroup struct {
	parent    vectorTileID
	hasParent bool
	targets   []vectorTileID
}

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
		if !group.hasParent {
			continue
		}
		allTargetsLoaded := true
		for _, tile := range group.targets {
			if s.loaded[tile] == nil {
				allTargetsLoaded = false
				break
			}
		}
		if !allTargetsLoaded {
			continue
		}
		drop[group.parent] = struct{}{}
		delete(s.desired, group.parent)
		delete(s.loaded, group.parent)
		delete(s.failed, group.parent)
		delete(s.attempts, group.parent)
		if load, loading := s.inFlight[group.parent]; loading {
			load.cancel()
			delete(s.inFlight, group.parent)
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

func (s *tileSchedulerState) targetGroups() []tileTargetGroup {
	groups := make([]tileTargetGroup, 0, len(s.order))
	indexes := make(map[vectorTileID]int, len(s.order))
	for _, tile := range s.order {
		parent, hasParent := tile.Parent()
		if !hasParent {
			groups = append(groups, tileTargetGroup{targets: []vectorTileID{tile}})
			continue
		}
		index, exists := indexes[parent]
		if !exists {
			index = len(groups)
			indexes[parent] = index
			groups = append(groups, tileTargetGroup{parent: parent, hasParent: true})
		}
		groups[index].targets = append(groups[index].targets, tile)
	}
	return groups
}

func (s *tileSchedulerState) continuityCover(tile vectorTileID) ([]vectorTileID, bool) {
	var nearestAncestor vectorTileID
	hasAncestor := false
	descendants := make([]vectorTileID, 0, 4)
	maximumZoom := tile.Z
	for _, retained := range s.rendered {
		if _, exists := s.continuity[retained]; !exists || s.loaded[retained] == nil {
			continue
		}
		if retained == tile {
			return []vectorTileID{retained}, true
		}
		if retained.Z < tile.Z && tileContains(retained, tile) {
			if !hasAncestor || retained.Z > nearestAncestor.Z {
				nearestAncestor = retained
				hasAncestor = true
			}
			continue
		}
		if retained.Z > tile.Z && tileContains(tile, retained) {
			descendants = append(descendants, retained)
			maximumZoom = max(maximumZoom, retained.Z)
		}
	}
	if hasAncestor {
		return []vectorTileID{nearestAncestor}, true
	}
	if len(descendants) == 0 {
		return nil, false
	}
	coveredArea := uint64(0)
	for _, descendant := range descendants {
		coveredArea += uint64(1) << (2 * (maximumZoom - descendant.Z))
	}
	targetArea := uint64(1) << (2 * (maximumZoom - tile.Z))
	return descendants, coveredArea == targetArea
}

func (s *tileSchedulerState) renderSelection() ([]loadedRoadTile, int) {
	selected := make([]loadedRoadTile, 0, len(s.order))
	fallbacks := 0
	appendTile := func(tile vectorTileID, fallback bool) bool {
		roads := s.loaded[tile]
		if roads == nil {
			return false
		}
		for _, existing := range selected {
			if tilesOverlap(existing.id, tile) {
				return false
			}
		}
		selected = append(selected, loadedRoadTile{id: tile, roads: roads})
		if fallback {
			fallbacks++
		}
		return true
	}

	for _, group := range s.targetGroups() {
		allTargetsLoaded := true
		for _, tile := range group.targets {
			if s.loaded[tile] == nil {
				allTargetsLoaded = false
				break
			}
		}
		if allTargetsLoaded {
			for _, tile := range group.targets {
				appendTile(tile, false)
			}
			continue
		}
		continuity := make([][]vectorTileID, len(group.targets))
		allTargetsRetained := true
		for index, tile := range group.targets {
			var complete bool
			continuity[index], complete = s.continuityCover(tile)
			if !complete {
				allTargetsRetained = false
			}
		}
		if allTargetsRetained {
			detailedContinuity := true
			for index, cover := range continuity {
				for _, tile := range cover {
					if tile.Z < group.targets[index].Z {
						detailedContinuity = false
					}
				}
			}
			if detailedContinuity {
				for index, cover := range continuity {
					for _, tile := range cover {
						appendTile(tile, tile != group.targets[index])
					}
				}
				continue
			}
		}
		if group.hasParent && appendTile(group.parent, true) {
			continue
		}
		if allTargetsRetained {
			for index, cover := range continuity {
				for _, tile := range cover {
					appendTile(tile, tile != group.targets[index])
				}
			}
			continue
		}

		for index, tile := range group.targets {
			if appendTile(tile, false) {
				continue
			}
			for _, retained := range continuity[index] {
				// Partial descendant continuity is preferable to a completely blank target.
				appendTile(retained, retained != tile)
			}
		}
	}
	return selected, fallbacks
}
