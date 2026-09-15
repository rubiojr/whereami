package fixture

import (
	"errors"
	"iter"
	"sort"

	"github.com/rubiojr/whereami/pkg/vecmap/compiler"
	"github.com/rubiojr/whereami/pkg/vecmap/glyph"
	"github.com/rubiojr/whereami/pkg/vecmap/liberty"
	"github.com/rubiojr/whereami/pkg/vecmap/mvt"
	"github.com/rubiojr/whereami/pkg/vecmap/placement"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
)

type preparedTile struct {
	layers     []style.CompiledLayer
	primitives []compiler.Primitive
	symbols    []placement.Symbol
}

func prepareTile(sources map[string][]mvt.Feature, layers []style.CompiledLayer, indexed bool) (*preparedTile, error) {
	p := &preparedTile{layers: layers, primitives: make([]compiler.Primitive, 0, len(layers))}
	err := compiler.CompileTile(layers, sources, compiler.LayerOptions{SourceZoom: 9, Zoom: 10, Indexed: indexed},
		func(primitive compiler.Primitive) error {
			p.primitives = append(p.primitives, primitive)
			return nil
		}, func(layer style.CompiledLayer) error {
			return placement.PrepareSymbols(sources[layer.SourceLayer], layer,
				placement.SymbolOptions{SourceZoom: 9, Zoom: 10, Limit: placement.MaxSymbols - len(p.symbols)},
				func(symbol placement.Symbol) error { p.symbols = append(p.symbols, symbol); return nil })
		})
	if errors.Is(err, placement.ErrSymbolLimit) {
		err = mvt.ErrFeatureResourceLimit
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

func textRequests(symbols []placement.Symbol) iter.Seq2[int, compiler.TextRequest] {
	return func(yield func(int, compiler.TextRequest) bool) {
		for i, s := range symbols {
			if !yield(i, compiler.TextRequest{Text: s.Text, FontStack: s.FontStack, Options: glyph.LayoutOptions{
				TextSize: s.TextSize, LetterSpacing: s.LetterSpacing, MaximumWidth: s.MaximumWidth,
				LineHeight: s.LineHeight, Anchor: s.TextAnchor, Justify: s.TextJustify,
				HaloWidth: s.HaloWidth, HaloBlur: s.HaloBlur,
			}}) {
				return
			}
		}
	}
}

func (p *preparedTile) pack(b *compiler.SceneBuilder, layouts map[int]*glyph.PreparedLayout, accepted map[int]placement.Accepted, atlas uint64) int {
	labels := 0
	for _, layer := range p.layers {
		start := sort.Search(len(p.primitives), func(i int) bool { return p.primitives[i].Order >= layer.Order })
		for i := start; i < len(p.primitives) && p.primitives[i].Order == layer.Order; i++ {
			b.Primitive(Tile(), 0, p.primitives[i], liberty.Sprite)
		}
		if layer.Kind == "symbol" {
			start := sort.Search(len(p.symbols), func(i int) bool { return p.symbols[i].Order >= layer.Order })
			end := sort.Search(len(p.symbols), func(i int) bool { return p.symbols[i].Order > layer.Order })
			labels += b.SymbolLayer(end-start, func(offset int) compiler.RenderSymbol {
				i := start + offset
				return compiler.RenderSymbol{Symbol: p.symbols[i], Accepted: accepted[i], Layout: layouts[i]}
			}, atlas, liberty.Sprite)
		}
	}
	return labels
}
