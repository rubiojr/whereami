package tiles

import (
	"maps"
	"math"
	"slices"
	"sort"

	"github.com/rubiojr/whereami/pkg/vecmap/placement"
	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
)

type SymbolKey struct {
	Tile      view.TileID
	Wrap      int
	Candidate int
}

// Snapshot is immutable after publication. Cover is canonical nonoverlapping tile
// coverage; TileSpaces includes world instances and supplies the scene's own slots.
// Accepted includes metric-ready candidates even if they have no drawable glyphs.
type Snapshot struct {
	Scene      *scene.Scene
	Cover      []view.TileID
	TileSpaces []scene.TileSpace
	Fallbacks  int
	Accepted   map[SymbolKey]placement.Accepted
}

// Frame reprojects this snapshot's slots with the latest camera. Selection/placement
// remain those of Select; rerun Select when the producer's placement policy requires
// it. Callers set DevicePixelRatio for their backend.
func (s *Snapshot) Frame(camera view.Camera) scene.Frame {
	document := scene.Document{TileSpaces: s.TileSpaces}
	frame := document.FrameAt(camera)
	frame.Scene = s.Scene
	return frame
}

// Select composes CPU-ready tiles for a requested cover, reusing view.SelectCover
// and placement's collision policy. previous is explicit eligible continuity,
// normally from the acknowledged Current snapshot. This method does not update it.
// Targets must be unique at one source zoom; previous must be nonoverlapping.
// Each list is capped at MaxTiles; all tiles are canonical and at source zoom <=14.
//
// The producer runs this off GUI/render threads. Projection is capped at 100,000
// references, instanced source draws at the Store's draw limit (before filtering),
// and collision work at workLimit (zero chooses placement's default). Any error
// returns nil output and preserves the last cache. If coverage, wraps and acceptance
// are unchanged since the last Apply, it returns the same snapshot for camera-only
// updates, avoiding resource validation/upload planning on every camera tick.
func (s *Set) Select(targets, previous []view.TileID, camera view.Camera, workLimit int) (*Snapshot, error) {
	if s.store == nil {
		return nil, ErrInput
	}
	if err := validateSelection(targets, previous, camera); err != nil {
		return nil, err
	}
	camera = camera.Normalized()
	cover, fallbacks := view.SelectCover(targets, previous, func(tile view.TileID) bool { _, ok := s.tiles[tile]; return ok })
	spaces, err := s.spaces(cover, view.WorldWraps(camera))
	if err != nil {
		return nil, err
	}
	accepted, err := s.place(spaces, camera, workLimit)
	if err != nil {
		return nil, err
	}
	if old := s.cached; old != nil && old.Fallbacks == fallbacks && slices.Equal(old.Cover, cover) && slices.Equal(old.TileSpaces, spaces) && maps.Equal(old.Accepted, accepted) {
		return old, nil
	}
	packed, err := s.compose(spaces, accepted)
	if err != nil {
		return nil, err
	}
	s.cached = &Snapshot{Scene: packed, Cover: cover, TileSpaces: spaces, Fallbacks: fallbacks, Accepted: accepted}
	return s.cached, nil
}

func validateSelection(targets, previous []view.TileID, camera view.Camera) error {
	if len(targets) > MaxTiles || len(previous) > MaxTiles {
		return ErrLimit
	}
	for _, dimension := range []float64{camera.Width, camera.Height} {
		if dimension <= 0 || dimension > placement.MaxCollisionViewport || math.IsNaN(dimension) {
			return ErrInput
		}
	}
	for i, tile := range targets {
		if !validTile(tile) || tile.Z != targets[0].Z || slices.Contains(targets[:i], tile) {
			return ErrInput
		}
	}
	for i, tile := range previous {
		if !validTile(tile) {
			return ErrInput
		}
		for _, earlier := range previous[:i] {
			if view.TilesOverlap(tile, earlier) {
				return ErrInput
			}
		}
	}
	return nil
}

func (s *Set) spaces(cover []view.TileID, wraps []int) ([]scene.TileSpace, error) {
	draws, symbols := 0, 0
	for _, tile := range cover {
		f := s.tiles[tile]
		draws += len(f.draws) * len(wraps)
		symbols += len(f.symbols) * len(wraps)
	}
	if draws > s.maxDraws || symbols > placement.MaxCollisionReferences {
		return nil, ErrLimit
	}
	spaces := make([]scene.TileSpace, 0, len(cover)*len(wraps))
	for _, tile := range cover {
		for _, wrap := range wraps {
			spaces = append(spaces, scene.TileSpace{Tile: tile, Wrap: wrap})
		}
	}
	return spaces, nil
}

func (s *Set) place(spaces []scene.TileSpace, camera view.Camera, workLimit int) (map[SymbolKey]placement.Accepted, error) {
	var references []placement.CollisionReference[SymbolKey]
	for _, space := range spaces {
		transform := view.TileTransform(camera, space.Tile, space.Wrap)
		symbols := s.tiles[space.Tile].symbols
		// Preserve production tile/wrap/reverse-candidate tie order.
		for i := len(symbols) - 1; i >= 0; i-- {
			item := &symbols[i]
			context := placement.ProjectionContext{Transform: transform, Width: camera.Width, Height: camera.Height, TextReady: item.TextReady}
			if item.HasTextBounds {
				context.TextBounds = &item.TextBounds
			}
			if item.HasSprite {
				context.Sprite = &item.Sprite
			}
			projected := placement.ProjectSymbol(&item.Candidate, context)
			references = append(references, placement.CollisionReference[SymbolKey]{Key: SymbolKey{space.Tile, space.Wrap, i}, Order: item.Candidate.Order, SortKey: item.Candidate.SortKey, Text: projected.Text, Icon: projected.Icon})
		}
	}
	return placement.SelectSymbols(references, placement.CollisionOptions{Width: camera.Width, Height: camera.Height, WorkLimit: workLimit})
}

type drawRange struct {
	selection retained.Range
	metadata  Draw
}

func (s *Set) compose(spaces []scene.TileSpace, accepted map[SymbolKey]placement.Accepted) (*scene.Scene, error) {
	var draws []drawRange
	for slot, space := range spaces {
		f := s.tiles[space.Tile]
		for i, metadata := range f.draws {
			decision := accepted[SymbolKey{space.Tile, space.Wrap, metadata.Candidate}]
			if (metadata.Part == Icon && !decision.Icon) || (metadata.Part == Text && !decision.Text) {
				continue
			}
			draws = append(draws, drawRange{retained.Range{Key: f.key, First: i, Count: 1, Transform: slot}, metadata})
		}
	}
	// Stable sorting preserves tile/wrap/source-draw order within each layer.
	sort.SliceStable(draws, func(i, j int) bool { return draws[i].metadata.Layer < draws[j].metadata.Layer })
	order := make([]retained.Range, len(draws))
	for i, draw := range draws {
		order[i] = draw.selection
	}
	packed, err := s.store.Snapshot(order)
	if err != nil {
		return nil, err
	}
	for i, draw := range draws {
		if period := draw.metadata.PatternPeriod; period != [2]float64{} {
			space := spaces[draw.selection.Transform]
			x, y := view.PatternPhase(space.Tile, space.Wrap, period[0], period[1])
			packed.Draws[i].Material.PatternPhase = [2]float32{float32(x), float32(y)}
		}
	}
	return packed, nil
}
