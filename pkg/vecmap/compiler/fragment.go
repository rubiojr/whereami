package compiler

import (
	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/glyph"
	"github.com/rubiojr/whereami/pkg/vecmap/placement"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
)

const MaxFragmentDraws = 65536

type DrawPart uint8

const (
	BaseDraw DrawPart = iota
	IconDraw
	TextDraw
)

// DrawSource preserves the producer identity of one packed draw. PatternPeriod is
// the original float64 tile-local period before packing into Material.PatternSize.
// Candidate is a caller-owned symbol index for IconDraw/TextDraw, ignored for BaseDraw.
type DrawSource struct {
	Layer         int
	Part          DrawPart
	Candidate     int
	PatternPeriod [2]float64
}

// FragmentBuilder packs the same geometry/material/pass math as SceneBuilder,
// with explicit draw-source boundaries. Draws never merge across primitives or
// symbol candidates. Construct with NewFragmentBuilder; zero builders reject use.
// Geometry is owned, image pixels are immutable borrows. The builder is single-owner.
type FragmentBuilder struct {
	packing SceneBuilder
	sources []DrawSource
}

func NewFragmentBuilder(indexed bool, maximumElements, maximumDraws int) *FragmentBuilder {
	b := &FragmentBuilder{packing: *NewSceneBuilder(indexed, maximumElements)}
	if maximumDraws == 0 {
		maximumDraws = MaxFragmentDraws
	}
	if (maximumDraws < 0 || maximumDraws > MaxFragmentDraws) && b.packing.err == nil {
		b.packing.err = geometry.ErrGeometryLimit
	}
	b.packing.drawLimit = maximumDraws
	return b
}

func (b *FragmentBuilder) GlyphAtlas(atlas *glyph.Atlas) uint64 { return b.packing.GlyphAtlas(atlas) }

// Split separates zoom-stable from zoom-dependent geometry as SceneBuilder.Split.
func (b *FragmentBuilder) Split() { b.packing.Split() }

// ResidentSymbols packs size-independent symbols as SceneBuilder.ResidentSymbols.
func (b *FragmentBuilder) ResidentSymbols() { b.packing.ResidentSymbols() }

// CompactVertices packs vertices without unused attributes as
// SceneBuilder.CompactVertices.
func (b *FragmentBuilder) CompactVertices() { b.packing.CompactVertices() }

// Reserve makes room for the geometry of primitives before packing them, as
// SceneBuilder.Reserve. A primitive that packs nothing (a missing sprite) only
// leaves room unused until Finish. Call after Split and CompactVertices.
func (b *FragmentBuilder) Reserve(primitives []Primitive) {
	// Vertices and indices by mesh (stable, dynamic) and layout.
	var counts [2][3][2]int
	for _, primitive := range primitives {
		mesh := 0
		if primitive.Dynamic && b.packing.split {
			mesh = 1
		}
		room := &counts[mesh][b.packing.Layout(primitive.Directions != nil, primitive.Distances != nil)]
		indices := len(primitive.Mesh.Indices)
		if primitive.Mesh.Indices == nil {
			indices = len(primitive.Mesh.Vertices)
		}
		if b.packing.indexed {
			room[0] += len(primitive.Mesh.Vertices)
		} else {
			room[0] += indices
		}
		room[1] += indices
	}
	for mesh := range counts {
		for layout, room := range counts[mesh] {
			if room[0] > 0 {
				b.packing.Reserve(mesh == 1, scene.Layout(layout), room[0], room[1])
			}
		}
	}
}

// Primitive packs base geometry at wrap zero and preserves its exact period for
// later world instancing. Tile and layer identity must be valid. Missing sprites
// and empty geometry produce no draw-source entries.
func (b *FragmentBuilder) Primitive(tile view.TileID, primitive Primitive, lookup SpriteLookup) {
	if !b.packing.ready() {
		return
	}
	if !tile.Valid() || primitive.Order < 0 {
		b.packing.err = ErrPackingInput
		return
	}
	start := b.begin()
	period := b.packing.primitive(tile, 0, primitive, lookup)
	b.capture(start, DrawSource{Layer: primitive.Order, Part: BaseDraw, PatternPeriod: period})
}

// SymbolLayer preserves icons/all halos/all fills ordering. first is the global
// candidate index corresponding to symbolAt(0). Counts and first+count are bounded
// by placement.MaxSymbols. Acceptance and complete atlas coverage remain caller
// policy; a retained producer normally packs all potentially drawable candidates.
func (b *FragmentBuilder) SymbolLayer(first, count int, symbolAt func(int) RenderSymbol, atlas uint64, lookup SpriteLookup) int {
	if !b.packing.ready() {
		return 0
	}
	if first < 0 || count < 0 || count > placement.MaxSymbols || first > placement.MaxSymbols-count {
		b.packing.err = geometry.ErrGeometryLimit
		return 0
	}
	return b.packing.symbolLayer(count, symbolAt, func(index int, kind scene.Kind, item RenderSymbol) bool {
		if item.Symbol.Order < 0 {
			b.packing.err = ErrPackingInput
			return false
		}
		source := DrawSource{Layer: item.Symbol.Order, Part: TextDraw, Candidate: first + index}
		if kind == scene.Image {
			source.Part = IconDraw
		}
		start := b.begin()
		label := b.packing.symbolPass(kind, item, atlas, lookup)
		b.capture(start, source)
		return label && len(b.packing.result.Draws) > start
	})
}

func (b *FragmentBuilder) begin() int {
	b.packing.breakDraw = true
	return len(b.packing.result.Draws)
}

func (b *FragmentBuilder) capture(start int, source DrawSource) {
	for i := start; i < len(b.packing.result.Draws); i++ {
		b.sources = append(b.sources, source)
	}
}

// Finish seals the builder, returning immutable scene data and one source record
// per draw. Empty fragments succeed with no mesh/textures. All errors return nil
// outputs. Repeat Finish is allowed until another mutating method is attempted.
func (b *FragmentBuilder) Finish() (*scene.Scene, []DrawSource, error) {
	result, err := b.packing.finish(true)
	if err != nil {
		return nil, nil, err
	}
	return result, b.sources, nil
}
