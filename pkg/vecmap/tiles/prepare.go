package tiles

import (
	"errors"
	"iter"
	"math"
	"slices"
	"sort"

	"github.com/rubiojr/whereami/pkg/vecmap/compiler"
	"github.com/rubiojr/whereami/pkg/vecmap/glyph"
	"github.com/rubiojr/whereami/pkg/vecmap/mvt"
	"github.com/rubiojr/whereami/pkg/vecmap/placement"
	"github.com/rubiojr/whereami/pkg/vecmap/sprite"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
)

const MaxStyleLayers = 1024

// PrepareOptions fixes the tile's source identity, evaluated style zoom and output
// topology. Zero limits select existing compiler/placement ceilings; positive
// values may lower them. Source zoom is <=14; evaluated zoom is finite in [0,20].
// Reprepare on style-zoom changes; changing the view transform alone does not
// reevaluate line widths, style visibility or symbol paint.
type PrepareOptions struct {
	Tile                          view.TileID
	Zoom                          float64
	Indexed                       bool
	TriangleLimit, CandidateLimit int
	ElementLimit, DrawLimit       int
	// Coarser is the number of zoom levels the tile and the style zoom lie below
	// the camera zoom the tile is drawn at, at most view.MaxCoarser. Zero draws a
	// tile view.TileSize units wide at its own zoom. One draws it twice as wide,
	// as MapLibre does, so a view needs fewer tiles and the style is evaluated
	// one zoom lower. Pair it with view.VisibleTileCoverAt and view.StyleZoomAt.
	// Zoom stays the evaluated style zoom; pixel widths, pattern sizes and
	// symbol spacing are converted to tile units at Zoom plus Coarser.
	Coarser int
	// ResidentGeometry keeps geometry that does not depend on the evaluated zoom
	// byte-identical across style-zoom changes. Undashed, unoffset lines and fill
	// outlines are packed as width-independent extrusions whose half width is a
	// per-draw value, and the fragment is split into compiler.StableMesh (fills,
	// patterns, extruded lines) and compiler.DynamicMesh (dashed or offset lines,
	// symbols). A retained store can then keep the stable mesh resident while only
	// draws and the dynamic mesh change. The backend must scale map-aligned
	// offsets by Material.OffsetScale. False preserves the single-mesh output.
	ResidentGeometry bool
	// ResidentSymbols keeps icon and text quads byte-identical across style-zoom
	// changes that leave symbol layout alone. Quads are packed at a base size in
	// compiler.SymbolMesh and the evaluated icon or text size is a per-draw
	// Material.OffsetScale. Candidates, text bounds and collision input are
	// unchanged. Anchors, texts, rotation and em-relative layout still follow the
	// style zoom, so a layout change replaces the symbol mesh. The backend must
	// scale vertex offsets by OffsetScale. False preserves the existing output.
	ResidentSymbols bool
	// ResidentDashes keeps butt-capped, unoffset dashed lines with at most four
	// dash entries byte-identical across style-zoom changes. They are packed as
	// one width-independent quad per path segment with the distance along the
	// path in vertex U; the width and the dash pattern travel on scene.Dashed
	// draws. With ResidentGeometry they join compiler.StableMesh. Other dashed
	// lines stay baked. The backend must implement scene.Dashed. False preserves
	// the existing output.
	ResidentDashes bool
	// CompactVertices packs fills and patterns as scene.PositionVertex and
	// extruded lines as scene.OffsetVertex, in sections of the same meshes.
	// Vertices lose only attributes that are zero, so the rendered output does
	// not change, and a dense tile keeps about a quarter less vertex storage.
	// The backend must implement scene.Draw.Layout. False preserves the
	// existing output.
	CompactVertices bool
}

// Prepared owns reusable primitives and evaluated candidates, not source features
// or style maps. It is immutable after Prepare. Build can be repeated for new
// immutable asset snapshots without decoding or tessellating again. Concurrent
// Builds require concurrency-safe asset callbacks and caller-owned job budgets.
type Prepared struct {
	options    PrepareOptions
	orders     []int
	primitives []compiler.Primitive
	symbols    []placement.Symbol
	limits     []mvt.LayerLimits
	valid      bool
}

// Assets supplies already decoded immutable font maps (which may merge multiple
// glyph ranges) and synchronous sprite readiness/lookups. Callbacks must not do
// I/O or mutate assets. Nil callbacks mean unavailable. FallbackEligible is an
// optional collision-readiness predicate for non-SDF text; it does not draw native
// fallback. Passing glyph.LegacyFallbackEligible preserves the fixture's policy.
type Assets struct {
	Fonts            map[string]map[uint32]glyph.Glyph
	Sprite           compiler.SpriteLookup
	SpriteEntry      func(string) (sprite.Entry, bool)
	FallbackEligible func(string) bool
}

type BuildResult struct {
	Fragment     *Fragment
	MissingFonts []string
	Limits       []mvt.LayerLimits
}

// Source is a decoded tile: features with triangulated fills, independent of
// style and style zoom. It is immutable, so concurrent preparations may share
// it, and preparing it at another style zoom skips decoding and triangulation.
type Source struct {
	tile    *mvt.Tile
	indexed bool
}

// Decode decodes bounded MVT input for PrepareSource. indexed must match the
// PrepareOptions.Indexed of every preparation that uses the source.
func Decode(data []byte, indexed bool) (*Source, error) {
	tile, err := mvt.DecodeTile(data, indexed)
	if err != nil {
		return nil, err
	}
	return &Source{tile: tile, indexed: indexed}, nil
}

// Prepare decodes bounded MVT input and compiles caller-supplied style layers using
// the existing geometry/candidate algorithms. Layers must have strictly increasing
// nonnegative Order values (as produced by style.Compile). Styles are trusted,
// bounded application data; expression width/work remains caller-owned. Every
// failure returns nil, including failures after earlier successful layer emissions.
func Prepare(data []byte, layers []style.CompiledLayer, options PrepareOptions) (*Prepared, error) {
	if err := options.validate(layers); err != nil {
		return nil, err
	}
	source, err := Decode(data, options.Indexed)
	if err != nil {
		return nil, err
	}
	return PrepareSource(source, layers, options)
}

// PrepareSource is Prepare for an already decoded tile, with identical output.
func PrepareSource(source *Source, layers []style.CompiledLayer, options PrepareOptions) (*Prepared, error) {
	if err := options.validate(layers); err != nil {
		return nil, err
	}
	if source == nil || source.indexed != options.Indexed {
		return nil, ErrInput
	}
	if options.CandidateLimit == 0 {
		options.CandidateLimit = placement.MaxSymbols
	}
	tile := source.tile
	p := &Prepared{options: options, limits: tile.Limits, orders: make([]int, len(layers))}
	for i, layer := range layers {
		p.orders[i] = layer.Order
	}
	err := compiler.CompileTile(layers, tile.Layers, compiler.LayerOptions{
		SourceZoom: int(options.Tile.Z), Zoom: options.Zoom, Indexed: options.Indexed, TriangleLimit: options.TriangleLimit,
		ExtrudeLines: options.ResidentGeometry, ShaderDashes: options.ResidentDashes, Coarser: options.Coarser,
	}, func(primitive compiler.Primitive) error {
		p.primitives = append(p.primitives, primitive)
		return nil
	}, func(layer style.CompiledLayer) error {
		return placement.PrepareSymbols(tile.Layers[layer.SourceLayer], layer,
			placement.SymbolOptions{SourceZoom: options.Tile.Z, Zoom: options.Zoom, Coarser: options.Coarser, Limit: options.CandidateLimit - len(p.symbols)},
			func(symbol placement.Symbol) error { p.symbols = append(p.symbols, symbol); return nil })
	})
	if errors.Is(err, placement.ErrSymbolLimit) {
		err = mvt.ErrFeatureResourceLimit
	}
	if err != nil {
		return nil, err
	}
	p.valid = true
	return p, nil
}

func (o PrepareOptions) validate(layers []style.CompiledLayer) error {
	if !validTile(o.Tile) || math.IsNaN(o.Zoom) || o.Zoom < 0 || o.Zoom > 20 || o.Coarser < 0 || o.Coarser > view.MaxCoarser {
		return ErrInput
	}
	if len(layers) > MaxStyleLayers {
		return ErrLimit
	}
	for i, layer := range layers {
		if layer.Order < 0 || (i > 0 && layer.Order <= layers[i-1].Order) {
			return ErrInput
		}
	}
	for _, limit := range [][2]int{{o.TriangleLimit, compiler.MaxTriangles}, {o.CandidateLimit, placement.MaxSymbols}, {o.ElementLimit, compiler.MaxSceneElements}, {o.DrawLimit, compiler.MaxFragmentDraws}} {
		if limit[0] < 0 || limit[0] > limit[1] {
			return ErrLimit
		}
	}
	return nil
}

// TextRequests exposes immutable text/font/options with original candidate keys.
// A loader can discover font stacks and Unicode ranges without retaining source
// features or invoking Build. Iteration must not mutate Prepared's reachable data.
func (p *Prepared) TextRequests() iter.Seq2[int, compiler.TextRequest] {
	return compiler.SymbolTextRequests(p.symbols)
}

// Limits returns owned metadata describing MVT resource degradation during Prepare.
func (p *Prepared) Limits() []mvt.LayerLimits { return slices.Clone(p.limits) }

// Build packs every potentially drawable candidate before camera-dependent
// placement. Text uses the shared SDF-only completeness/atlas policy. Metric-ready
// layouts remain available to collision even if atlas coverage omits their meshes.
// Output owns packed geometry/metadata and converted glyph RGBA, borrowing sprite
// pixels. It does not retain asset maps. Errors return nil without altering p or
// previously built fragments. Empty output is a valid ready blank tile.
func (p *Prepared) Build(assets Assets) (*BuildResult, error) {
	return p.build(assets, 0)
}

// BuildOwned retains the same geometry, provenance and placement metrics as Build,
// but owns compact sprite pixel buffers instead of borrowing asset backing. Glyph
// atlas RGBA is already owned and is not copied again. maximumTextureBytes bounds
// aggregate output RGBA before sprite copies (positive, at most 1 GiB). This lets
// producers drop obsolete asset snapshots while retaining immutable fragments.
func (p *Prepared) BuildOwned(assets Assets, maximumTextureBytes uint64) (*BuildResult, error) {
	if maximumTextureBytes == 0 || maximumTextureBytes > 1<<30 {
		return nil, ErrLimit
	}
	return p.build(assets, maximumTextureBytes)
}

func (p *Prepared) build(assets Assets, maximumTextureBytes uint64) (*BuildResult, error) {
	if p == nil || !p.valid {
		return nil, ErrInput
	}
	layouts, missing, err := compiler.PrepareTextLayouts(p.TextRequests(), assets.Fonts)
	if err != nil {
		return nil, err
	}
	atlas, _, err := glyph.BuildRetainedAtlas(glyph.LayoutGlyphs(layouts), nil)
	if err != nil {
		return nil, err
	}
	prepare := glyph.PrepareLayouts[int]
	if p.options.ResidentSymbols {
		prepare = glyph.PrepareUnitLayouts[int]
	}
	renderable, err := prepare(layouts, atlas, p.options.Indexed)
	if err != nil {
		return nil, err
	}
	packing := compiler.NewFragmentBuilder(p.options.Indexed, p.options.ElementLimit, p.options.DrawLimit)
	if p.options.ResidentGeometry {
		packing.Split()
	}
	if p.options.ResidentSymbols {
		packing.ResidentSymbols()
	}
	if p.options.CompactVertices {
		packing.CompactVertices()
	}
	var atlasID uint64
	if len(renderable) > 0 {
		atlasID = packing.GlyphAtlas(atlas)
	}
	packing.Reserve(p.primitives)
	p.pack(packing, renderable, atlasID, assets.Sprite)
	packed, sources, err := packing.Finish()
	if err != nil {
		return nil, err
	}
	if maximumTextureBytes != 0 {
		var bytes uint64
		for _, texture := range packed.Textures {
			if uint64(len(texture.RGBA)) > maximumTextureBytes-bytes {
				return nil, ErrLimit
			}
			bytes += uint64(len(texture.RGBA))
		}
		for i, texture := range packed.Textures {
			if texture.ID != atlasID {
				pixels := make([]byte, len(texture.RGBA))
				copy(pixels, texture.RGBA)
				packed.Textures[i].RGBA = pixels
			}
		}
	}
	fragment := &Fragment{Scene: packed, Draws: sources, Symbols: p.metrics(layouts, assets)}
	return &BuildResult{Fragment: fragment, MissingFonts: missing, Limits: p.Limits()}, nil
}

func (p *Prepared) pack(b *compiler.FragmentBuilder, layouts map[int]*glyph.PreparedLayout, atlas uint64, lookup compiler.SpriteLookup) {
	for _, order := range p.orders {
		start := sort.Search(len(p.primitives), func(i int) bool { return p.primitives[i].Order >= order })
		for i := start; i < len(p.primitives) && p.primitives[i].Order == order; i++ {
			b.Primitive(p.options.Tile, p.primitives[i], lookup)
		}
		first := sort.Search(len(p.symbols), func(i int) bool { return p.symbols[i].Order >= order })
		end := sort.Search(len(p.symbols), func(i int) bool { return p.symbols[i].Order > order })
		b.SymbolLayer(first, end-first, func(offset int) compiler.RenderSymbol {
			i := first + offset
			return compiler.RenderSymbol{Symbol: p.symbols[i], Accepted: placement.Accepted{Text: true, Icon: true}, Layout: layouts[i]}
		}, atlas, lookup)
	}
}

func (p *Prepared) metrics(layouts map[int]*glyph.PreparedLayout, assets Assets) []Symbol {
	symbols := make([]Symbol, len(p.symbols))
	for i, candidate := range p.symbols {
		s := Symbol{Candidate: candidate}
		if layout := layouts[i]; layout != nil {
			s.TextReady, s.HasTextBounds = true, true
			s.TextBounds = placement.Box{Left: layout.Bounds.Left, Top: layout.Bounds.Top, Right: layout.Bounds.Right, Bottom: layout.Bounds.Bottom}
		}
		if !glyph.TextEligible(candidate.Text) && assets.FallbackEligible != nil {
			s.TextReady = assets.FallbackEligible(candidate.Text)
		}
		s.TextReady = s.TextReady && candidate.Text != "" && candidate.TextColor.Alpha > 0
		if candidate.IconName != "" && assets.SpriteEntry != nil {
			if entry, ok := assets.SpriteEntry(candidate.IconName); ok && entry.PixelRatio > 0 {
				s.HasSprite = true
				s.Sprite = placement.SpriteMetrics{Width: entry.Width, Height: entry.Height, PixelRatio: entry.PixelRatio}
			}
		}
		symbols[i] = s
	}
	return symbols
}
