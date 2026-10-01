// Package tiles prepares and composes retained tile scenes into ordered map targets.
// It owns no I/O, goroutines, toolkit handles or GPU readiness. A preparation
// worker owns a Set; immutable snapshots can be handed to retained.WorkerWithData.
package tiles

import (
	"errors"
	"fmt"
	"math"

	"github.com/rubiojr/whereami/pkg/vecmap/compiler"
	"github.com/rubiojr/whereami/pkg/vecmap/placement"
	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
)

var (
	ErrInput = errors.New("invalid tile composition input")
	ErrLimit = errors.New("tile composition limit exceeded")
)

const MaxTiles = 128

type Part = compiler.DrawPart

const (
	Base = compiler.BaseDraw
	Icon = compiler.IconDraw
	Text = compiler.TextDraw
)

// Draw describes one scene draw record. Layer is the style's original order.
// Icon/Text records refer to a candidate by index; Base ignores Candidate.
// A coalesced draw must not cross a layer or candidate boundary.
type Draw = compiler.DrawSource

// Symbol pairs evaluated paint with prepared asset readiness and value metrics.
// It does not load or shape text. TextReady retains producer policy independently
// of whether packed SDF draw records exist (metric-ready text may reserve space).
type Symbol struct {
	Candidate     placement.Symbol
	TextReady     bool
	HasTextBounds bool
	TextBounds    placement.Box
	HasSprite     bool
	Sprite        placement.SpriteMetrics
}

// Fragment is one tile's prepared scene and matching draw/candidate metadata.
// Geometry is tile-local in the 256-unit square. Base draws must already be clipped
// inside it; symbols can extend beyond it. All source draw transform slots are zero.
// Inputs follow compiler/placement's bounded evaluated-data contracts, not a new
// untrusted style parser. Geometry and pixels are immutable borrows through Set AND
// every snapshot lifetime. Apply copies scene/draw/symbol metadata, not payloads.
type Fragment struct {
	Scene   *scene.Scene
	Draws   []Draw
	Symbols []Symbol
}

type Change struct {
	Tile     view.TileID
	Fragment *Fragment // nil removes; an empty nonnil scene is a ready blank tile
}

type fragment struct {
	key     string
	draws   []Draw
	symbols []Symbol
}

// Set owns one retained resource namespace. Construct with New. It is single-owner,
// must not be copied, and retains no implicit last-rendered coverage. Keep prepared
// continuity tiles until the corresponding replacement is acknowledged by the backend.
type Set struct {
	store              *retained.Store
	tiles              map[view.TileID]fragment
	maxTiles, maxDraws int
	cached             *Snapshot
	references         []placement.CollisionReference[SymbolKey] // place's storage, reused by each selection
}

func New(limits retained.Limits) (*Set, error) {
	store, err := retained.New(limits)
	if err != nil {
		return nil, err
	}
	if limits.Fragments == 0 {
		limits.Fragments = MaxTiles
	}
	if limits.Draws == 0 {
		limits.Draws = 65536
	}
	return &Set{store: store, tiles: make(map[view.TileID]fragment), maxTiles: limits.Fragments, maxDraws: limits.Draws}, nil
}

// Apply atomically replaces/removes tiles, sharing Store's identity and revision
// contract. Metadata and resource limits reject before copying/validating payloads.
// Total stored candidates and incoming candidates each cannot exceed 100,000;
// each tile is capped at placement.MaxSymbols. Tiles use source zooms 0 through 14.
func (s *Set) Apply(changes []Change) error {
	if s.store == nil {
		return ErrInput
	}
	if err := s.preflight(changes); err != nil {
		return err
	}
	updates := make([]retained.Change, len(changes))
	for i, change := range changes {
		updates[i].Key = tileKey(change.Tile)
		if change.Fragment != nil {
			if err := validateFragment(change.Fragment); err != nil {
				return err
			}
			updates[i].Scene = change.Fragment.Scene
		}
	}
	if err := s.store.Apply(updates); err != nil {
		return err
	}
	for i, change := range changes {
		if change.Fragment == nil {
			delete(s.tiles, change.Tile)
		} else {
			s.tiles[change.Tile] = fragment{key: updates[i].Key, draws: copyValues(change.Fragment.Draws), symbols: copyValues(change.Fragment.Symbols)}
		}
	}
	if len(changes) > 0 {
		s.cached = nil
	}
	return nil
}

// ReusedVersions forwards the Store's count of replaced resources that kept their
// revision under byte-identical payload, such as glyph atlases across style zooms.
func (s *Set) ReusedVersions() uint64 { return s.store.ReusedVersions() }

func (s *Set) preflight(changes []Change) error {
	if len(changes) > 2*s.maxTiles {
		return ErrLimit
	}
	count, incoming, final, incomingDraws := len(s.tiles), 0, 0, 0
	for _, tile := range s.tiles {
		final += len(tile.symbols)
	}
	seen := make(map[view.TileID]bool, len(changes))
	for _, change := range changes {
		if !validTile(change.Tile) || seen[change.Tile] {
			return ErrInput
		}
		seen[change.Tile] = true
		if previous, ok := s.tiles[change.Tile]; ok {
			count--
			final -= len(previous.symbols)
		}
		if f := change.Fragment; f != nil {
			if f.Scene == nil || len(f.Draws) != len(f.Scene.Draws) {
				return ErrInput
			}
			if len(f.Draws) > s.maxDraws || len(f.Symbols) > placement.MaxSymbols {
				return ErrLimit
			}
			count++
			incomingDraws += len(f.Draws)
			incoming += len(f.Symbols)
			final += len(f.Symbols)
		}
	}
	if count > s.maxTiles || incomingDraws > s.maxDraws || incoming > placement.MaxCollisionReferences || final > placement.MaxCollisionReferences {
		return ErrLimit
	}
	return nil
}

func validateFragment(f *Fragment) error {
	for i, metadata := range f.Draws {
		draw := f.Scene.Draws[i]
		if metadata.Layer < 0 || metadata.Part > Text || draw.Transform != 0 {
			return ErrInput
		}
		if metadata.Part == Base {
			if draw.Clip == [4]float32{} || draw.Clip[0] < 0 || draw.Clip[1] < 0 || draw.Clip[2] > view.TileSize || draw.Clip[3] > view.TileSize {
				return ErrInput
			}
		} else if metadata.Candidate < 0 || metadata.Candidate >= len(f.Symbols) || f.Symbols[metadata.Candidate].Candidate.Order != metadata.Layer {
			return ErrInput
		}
		if !validMaterial(metadata, draw.Material) {
			return ErrInput
		}
	}
	return nil
}

func validMaterial(metadata Draw, material scene.Material) bool {
	if (metadata.Part == Icon && material.Kind != scene.Image) ||
		(metadata.Part == Text && material.Kind != scene.SDFHalo && material.Kind != scene.SDFFill) ||
		(metadata.Part != Text && (material.Kind == scene.SDFHalo || material.Kind == scene.SDFFill)) {
		return false
	}
	if material.Kind != scene.Pattern {
		return metadata.PatternPeriod == [2]float64{}
	}
	for i, period := range metadata.PatternPeriod {
		if period <= 0 || math.IsNaN(period) || math.IsInf(period, 0) || float32(period) != material.PatternSize[i] {
			return false
		}
	}
	return true
}

func validTile(tile view.TileID) bool { return tile.Z <= 14 && tile.Valid() }

func tileKey(tile view.TileID) string { return fmt.Sprintf("%d/%d/%d", tile.Z, tile.X, tile.Y) }
