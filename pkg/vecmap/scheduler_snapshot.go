package vecmap

type loadedRoadTile struct {
	id     vectorTileID
	roads  *tileBucket
	source *tileBucket
}

func (t loadedRoadTile) contentIdentity() *tileBucket {
	if t.source != nil {
		return t.source
	}
	return t.roads
}

type roadTileSnapshot struct {
	revision        uint64
	contentRevision uint64
	requested       int
	loading         int
	errors          int
	fallbacks       int
	lastError       string
	tiles           []loadedRoadTile
}

func (s *tileSchedulerState) relevantFailures() (int, string) {
	count := 0
	lastError := ""
	seen := make(map[vectorTileID]struct{}, len(s.failed))
	add := func(tile vectorTileID) {
		failure, exists := s.failed[tile]
		if !exists {
			return
		}
		if _, duplicate := seen[tile]; duplicate {
			return
		}
		seen[tile] = struct{}{}
		count++
		if lastError == "" {
			lastError = failure.message
		}
	}
	for _, group := range s.targetGroups() {
		missingTarget := false
		for _, tile := range group.targets {
			if s.loaded[tile] == nil {
				missingTarget = true
			}
			add(tile)
		}
		if group.hasParent && missingTarget {
			add(group.parent)
		}
	}
	return count, lastError
}

func (s *tileSchedulerState) publish(publish func(*roadTileSnapshot)) {
	if publish == nil {
		return
	}
	tiles, fallbacks := s.renderSelection()
	errors, lastError := s.relevantFailures()
	s.revision++
	if !sameLoadedRoadTiles(tiles, s.published) {
		s.contentRevision++
		s.published = append(s.published[:0], tiles...)
	}
	snapshot := &roadTileSnapshot{
		revision:        s.revision,
		contentRevision: s.contentRevision,
		requested:       len(s.order),
		loading:         len(s.pending) + len(s.inFlight) + s.retryingCount(),
		errors:          errors,
		fallbacks:       fallbacks,
		lastError:       lastError,
		tiles:           tiles,
	}
	s.rendered = s.rendered[:0]
	selected := make(map[vectorTileID]struct{}, len(tiles))
	for _, tile := range tiles {
		s.rendered = append(s.rendered, tile.id)
		selected[tile.id] = struct{}{}
	}
	for tile := range s.continuity {
		_, rendered := selected[tile]
		_, wanted := s.desired[tile]
		if !rendered && !wanted {
			delete(s.loaded, tile)
			delete(s.continuity, tile)
		}
	}
	publish(snapshot)
}

func sameLoadedRoadTiles(first, second []loadedRoadTile) bool {
	if len(first) != len(second) {
		return false
	}
	for index := range first {
		if first[index].id != second[index].id || first[index].roads != second[index].roads {
			return false
		}
	}
	return true
}
