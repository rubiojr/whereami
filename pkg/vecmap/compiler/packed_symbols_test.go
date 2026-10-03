package compiler

import (
	"fmt"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/glyph"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// packSymbolModes is packSymbols with packed symbols and short indices.
func packSymbolModes(t *testing.T, indexed, resident, packed, short bool, items ...RenderSymbol) (*scene.Scene, []DrawSource) {
	t.Helper()
	b := NewFragmentBuilder(indexed, 0, 0)
	if resident {
		b.ResidentSymbols()
	}
	if packed {
		b.PackedSymbols()
	}
	if short {
		b.ShortIndices()
	}
	b.ReserveSymbols(len(items), func(i int) RenderSymbol { return items[i] })
	atlas := b.GlyphAtlas(&glyph.Atlas{Width: 1, Height: 1, Pixels: []byte{90}})
	b.SymbolLayer(0, len(items), func(i int) RenderSymbol { return items[i] }, atlas, symbolSprite)
	result, sources, err := b.Finish()
	require.NoError(t, err)
	return result, sources
}

// Packed symbols draw icons and text within half a packed step of their float
// vertices, in half the vertex bytes, and a fill shares its halo's packed quads.
func TestPackedSymbolsDrawTheSameLabels(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		for _, resident := range []bool{false, true} {
			for _, short := range []bool{false, true} {
				if short && !indexed {
					continue
				}
				name := fmt.Sprintf("indexed=%t resident=%t short=%t", indexed, resident, short)
				first, second := residentSymbol(indexed, resident, 14, 1.5), residentSymbol(indexed, resident, 14, 1.5)
				second.Symbol.Anchor.X, second.Symbol.Anchor.Y = 200.3, -31.7
				full, fullSources := packSymbolModes(t, indexed, resident, false, short, first, second)
				packed, sources := packSymbolModes(t, indexed, resident, true, short, first, second)
				assert.Equal(t, fullSources, sources, name)
				want, got := expand(t, full), expand(t, packed)
				require.Len(t, got, len(want), name)
				for i := range want {
					assert.Equal(t, want[i].material, got[i].material, "%s draw %d", name, i)
					require.Len(t, got[i].vertices, len(want[i].vertices))
					for k, w := range want[i].vertices {
						g := got[i].vertices[k]
						assert.InDelta(t, w.X, g.X, 0.5/scene.AnchorUnits, "%s draw %d vertex %d", name, i, k)
						assert.InDelta(t, w.Y, g.Y, 0.5/scene.AnchorUnits)
						assert.InDelta(t, w.OffsetX, g.OffsetX, 0.5/scene.PixelUnits)
						assert.InDelta(t, w.OffsetY, g.OffsetY, 0.5/scene.PixelUnits)
						assert.InDelta(t, w.U, g.U, 0.5/scene.TexcoordUnits+1e-7)
						assert.InDelta(t, w.V, g.V, 0.5/scene.TexcoordUnits+1e-7)
					}
				}
				for _, draw := range packed.Draws {
					assert.Equal(t, scene.PackedSymbolLayout, draw.Layout, name)
				}
				// Icons, halos, then fills: each fill draws its halo's range.
				require.Len(t, packed.Draws, 6)
				for i := range 2 {
					halo, fill := packed.Draws[2+i], packed.Draws[4+i]
					assert.Equal(t, [3]uint32{halo.First, halo.Count, halo.Base}, [3]uint32{fill.First, fill.Count, fill.Base}, name)
				}
				var before, after uint64
				for i, mesh := range packed.Meshes {
					assert.Empty(t, mesh.Vertices, name)
					assert.Equal(t, len(mesh.PackedSymbols), cap(mesh.PackedSymbols), "reserved exactly")
					before, after = before+full.Meshes[i].VertexBytes(), after+mesh.VertexBytes()
				}
				assert.Equal(t, before, 2*after, name)
			}
		}
	}
}

// A label whose anchor or offset the packed range can't hold keeps float
// vertices; the others pack.
func TestPackedSymbolsFallBackOutsideTheirRange(t *testing.T) {
	near, far, wide := residentSymbol(true, true, 14, 1), residentSymbol(true, true, 14, 1), residentSymbol(true, true, 14, 1)
	far.Symbol.Anchor.X = 600
	wide.Symbol.TextOffset.X, wide.Symbol.LineAngle = 50, 0 // ems: 1,200 pixels at the unit layout's 24 pixels per em
	for _, item := range []*RenderSymbol{&near, &far, &wide} {
		item.Symbol.IconName, item.Symbol.HaloWidth = "", 0
	}
	packed, _ := packSymbolModes(t, true, true, true, false, near, far, wide)
	var layouts []scene.Layout
	for _, draw := range packed.Draws {
		layouts = append(layouts, draw.Layout)
	}
	assert.Equal(t, []scene.Layout{scene.PackedSymbolLayout, scene.FullLayout, scene.FullLayout}, layouts)
	full, _ := packSymbolModes(t, true, true, false, false, near, far, wide)
	assert.Equal(t, expand(t, full)[1:], expand(t, packed)[1:], "float vertices are unchanged")
}
