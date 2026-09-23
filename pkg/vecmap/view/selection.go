package view

// TileGroup preserves the first-seen order of requested siblings. At zoom zero
// there is no parent. Targets within each group retain their input order.
type TileGroup struct {
	Parent    TileID
	HasParent bool
	Targets   []TileID
}

// GroupTiles groups trusted canonical requests by immediate parent without
// sorting, deduplication or retaining the input slice. Callers bound request count.
func GroupTiles(targets []TileID) []TileGroup {
	groups := make([]TileGroup, 0, len(targets))
	indexes := make(map[TileID]int, len(targets))
	for _, tile := range targets {
		parent, hasParent := tile.Parent()
		if !hasParent {
			groups = append(groups, TileGroup{Targets: []TileID{tile}})
			continue
		}
		index, exists := indexes[parent]
		if !exists {
			index = len(groups)
			indexes[parent] = index
			groups = append(groups, TileGroup{Parent: parent, HasParent: true})
		}
		groups[index].Targets = append(groups[index].Targets, tile)
	}
	return groups
}

// SelectCover reuses vecmap's sibling-atomic fallback policy. Fully ready requested
// siblings replace their parent together. Detailed previous coverage wins over a
// coarser parent; partial descendants can fill otherwise blank targets. Selected
// canonical tiles never overlap. The second result counts selected fallback tiles.
//
// Inputs are trusted, bounded canonical tiles at source zoom <=14. Targets are
// unique and at one zoom; previous is the eligible continuity selection, normally
// nonoverlapping. ready is synchronous and must not change during this call. The
// caller owns readiness (CPU preparation versus native publication), bounds and
// filtering of previous. Nil ready selects nothing. All returned slices are owned.
func SelectCover(targets, previous []TileID, ready func(TileID) bool) ([]TileID, int) {
	if ready == nil {
		return nil, 0
	}
	s := coverSelector{ready: ready, previous: previous, selected: make([]TileID, 0, len(targets))}
	for _, group := range GroupTiles(targets) {
		s.group(group)
	}
	return s.selected, s.fallbacks
}

type coverSelector struct {
	ready     func(TileID) bool
	previous  []TileID
	selected  []TileID
	fallbacks int
}

func (s *coverSelector) append(tile TileID, fallback bool) bool {
	if !s.ready(tile) {
		return false
	}
	for _, existing := range s.selected {
		if TilesOverlap(existing, tile) {
			return false
		}
	}
	s.selected = append(s.selected, tile)
	if fallback {
		s.fallbacks++
	}
	return true
}

func (s *coverSelector) continuity(tile TileID) ([]TileID, bool) {
	var ancestor TileID
	hasAncestor := false
	descendants := make([]TileID, 0, 4)
	maximumZoom := tile.Z
	for _, retained := range s.previous {
		if !s.ready(retained) {
			continue
		}
		if retained == tile {
			return []TileID{retained}, true
		}
		if retained.Z < tile.Z && TileContains(retained, tile) {
			if !hasAncestor || retained.Z > ancestor.Z {
				ancestor, hasAncestor = retained, true
			}
			continue
		}
		if retained.Z > tile.Z && TileContains(tile, retained) {
			descendants = append(descendants, retained)
			maximumZoom = max(maximumZoom, retained.Z)
		}
	}
	if hasAncestor {
		return []TileID{ancestor}, true
	}
	if len(descendants) == 0 {
		return nil, false
	}
	var area uint64
	for _, child := range descendants {
		area += uint64(1) << (2 * (maximumZoom - child.Z))
	}
	return descendants, area == uint64(1)<<(2*(maximumZoom-tile.Z))
}

func (s *coverSelector) group(group TileGroup) {
	allReady := true
	for _, tile := range group.Targets {
		if !s.ready(tile) {
			allReady = false
			break
		}
	}
	if allReady {
		for _, tile := range group.Targets {
			s.append(tile, false)
		}
		return
	}
	covers, complete, detailed := s.groupContinuity(group.Targets)
	if complete && detailed {
		s.appendContinuity(group.Targets, covers)
		return
	}
	if group.HasParent && s.append(group.Parent, true) {
		return
	}
	if complete {
		s.appendContinuity(group.Targets, covers)
		return
	}
	for i, tile := range group.Targets {
		if !s.append(tile, false) {
			for _, retained := range covers[i] {
				s.append(retained, retained != tile)
			}
		}
	}
}

func (s *coverSelector) groupContinuity(targets []TileID) ([][]TileID, bool, bool) {
	covers := make([][]TileID, len(targets))
	complete, detailed := true, true
	for i, tile := range targets {
		var covered bool
		covers[i], covered = s.continuity(tile)
		complete = complete && covered
		for _, retained := range covers[i] {
			if retained.Z < tile.Z {
				detailed = false
			}
		}
	}
	return covers, complete, detailed
}

func (s *coverSelector) appendContinuity(targets []TileID, covers [][]TileID) {
	for i, cover := range covers {
		for _, tile := range cover {
			s.append(tile, tile != targets[i])
		}
	}
}
