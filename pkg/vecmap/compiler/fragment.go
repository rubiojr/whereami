package compiler

import (
	"slices"

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
	// runDraws is, per StablePlan run, the index of the draw its primitive
	// made, or -1.
	runDraws []int
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
	if run := primitive.StableRun; run > 0 && b.packing.err == nil {
		for len(b.runDraws) < run {
			b.runDraws = append(b.runDraws, -1)
		}
		if len(b.packing.result.Draws) > start {
			b.runDraws[run-1] = start
		}
	}
	b.capture(start, DrawSource{Layer: primitive.Order, Part: BaseDraw, PatternPeriod: period})
}

// Borrow publishes mesh, the StableMesh of an earlier build of the same tile,
// as this build's StableMesh. Primitives that a CompileTilePlanned borrowing
// plan emitted draw from it with the ranges plan records; other stable
// geometry is an error, and Finish fails with ErrStableMismatch when a pattern
// sprite's presence would change the mesh. Call after Split, ResidentSymbols
// and CompactVertices, before packing; mesh must not be modified.
func (b *FragmentBuilder) Borrow(mesh scene.Mesh, plan *StablePlan) {
	if !b.packing.unused() {
		return
	}
	if !b.packing.split || plan == nil || mesh.ID != StableMesh {
		b.packing.err = ErrPackingInput
		return
	}
	b.packing.borrowed, b.packing.stablePlan = &mesh, plan
}

// StablePlan completes compiled, the plan of the CompileTilePlanned call whose
// primitives were packed, with the draw of each run, so a later build can
// borrow this build's StableMesh. A borrowing build returns its plan. Call
// after a successful Finish of a split builder.
func (b *FragmentBuilder) StablePlan(compiled *StablePlan) *StablePlan {
	if b.packing.stablePlan != nil {
		return b.packing.stablePlan
	}
	if compiled == nil || !b.packing.closed || b.packing.err != nil || !b.packing.split {
		return nil
	}
	plan := &StablePlan{runs: slices.Clone(compiled.runs)}
	for i, draw := range b.runDraws {
		if draw >= 0 && i < len(plan.runs) {
			published := b.packing.result.Draws[draw]
			run := &plan.runs[i]
			run.drawn, run.first, run.count, run.layout = true, published.First, published.Count, published.Layout
		}
	}
	return plan
}

// ReserveSymbols makes room for the icon and text quads that SymbolLayer packs
// for these symbols, as Reserve does for primitives; labels otherwise arrive one
// at a time and grow the mesh by doubling. Call once with every symbol of the
// fragment, after Split, ResidentSymbols and CompactVertices.
func (b *FragmentBuilder) ReserveSymbols(count int, symbolAt func(int) RenderSymbol) {
	b.packing.reserveSymbols(count, symbolAt)
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
