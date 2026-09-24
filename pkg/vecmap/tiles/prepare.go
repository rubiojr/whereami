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

// Prepare decodes bounded MVT input and compiles caller-supplied style layers using
// the existing geometry/candidate algorithms. Layers must have strictly increasing
// nonnegative Order values (as produced by style.Compile). Styles are trusted,
// bounded application data; expression width/work remains caller-owned. Every
// failure returns nil, including failures after earlier successful layer emissions.
func Prepare(data []byte, layers []style.CompiledLayer, options PrepareOptions) (*Prepared, error) {
	if err := options.validate(layers); err != nil {
		return nil, err
	}
	if options.CandidateLimit == 0 {
		options.CandidateLimit = placement.MaxSymbols
	}
	source, err := mvt.DecodeTile(data, options.Indexed)
	if err != nil {
		return nil, err
	}
	p := &Prepared{options: options, limits: source.Limits, orders: make([]int, len(layers))}
	for i, layer := range layers {
		p.orders[i] = layer.Order
	}
	err = compiler.CompileTile(layers, source.Layers, compiler.LayerOptions{
		SourceZoom: int(options.Tile.Z), Zoom: options.Zoom, Indexed: options.Indexed, TriangleLimit: options.TriangleLimit,
	}, func(primitive compiler.Primitive) error {
		p.primitives = append(p.primitives, primitive)
		return nil
	}, func(layer style.CompiledLayer) error {
		return placement.PrepareSymbols(source.Layers[layer.SourceLayer], layer,
			placement.SymbolOptions{SourceZoom: options.Tile.Z, Zoom: options.Zoom, Limit: options.CandidateLimit - len(p.symbols)},
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
	if !validTile(o.Tile) || math.IsNaN(o.Zoom) || o.Zoom < 0 || o.Zoom > 20 {
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
	renderable, err := glyph.PrepareLayouts(layouts, atlas, p.options.Indexed)
	if err != nil {
		return nil, err
	}
	packing := compiler.NewFragmentBuilder(p.options.Indexed, p.options.ElementLimit, p.options.DrawLimit)
	var atlasID uint64
	if len(renderable) > 0 {
		atlasID = packing.GlyphAtlas(atlas)
	}
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
