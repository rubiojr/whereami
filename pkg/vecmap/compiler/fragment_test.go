package compiler

import (
	"math"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/glyph"
	"github.com/rubiojr/whereami/pkg/vecmap/placement"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/sprite"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFragmentProvenance(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		b := NewFragmentBuilder(indexed, 0, 0)
		plain := NewSceneBuilder(indexed, 0)
		p := Primitive{Order: 2, Mesh: BackgroundGeometry(indexed), Color: style.Color{Alpha: 255}}
		for _, order := range []int{2, 3} {
			p.Order = order
			b.Primitive(view.TileID{}, p, nil)
			plain.Primitive(view.TileID{}, 0, p, nil)
		}
		result, sources, err := b.Finish()
		require.NoError(t, err)
		ordinary, err := plain.Finish()
		require.NoError(t, err)
		assert.Len(t, ordinary.Draws, 1, "ordinary fixture packing still coalesces")
		require.Len(t, result.Draws, 2)
		assert.Equal(t, []DrawSource{{Layer: 2}, {Layer: 3}}, sources)
		assert.Equal(t, ordinary.Meshes, result.Meshes)
		again, againSources, err := b.Finish()
		require.NoError(t, err)
		assert.Same(t, result, again)
		assert.Equal(t, sources, againSources)
		b.Primitive(view.TileID{}, p, nil)
		failed, metadata, err := b.Finish()
		assert.ErrorIs(t, err, ErrPackingClosed)
		assert.Nil(t, failed)
		assert.Nil(t, metadata)
		assert.Len(t, result.Draws, 2)
	}
}

func TestFragmentPatternPeriod(t *testing.T) {
	b := NewFragmentBuilder(false, 0, 0)
	p := Primitive{Mesh: BackgroundGeometry(false), PatternName: "dots", PatternScale: 0.5, Opacity: 1}
	b.Primitive(view.TileID{}, p, nil) // missing sprite emits no orphan metadata
	image := sprite.Image{Width: 8, Height: 6, PixelRatio: 3, Pixels: make([]byte, 8*6*4)}
	b.Primitive(view.TileID{}, p, func(string, style.Color, float64) (sprite.Image, bool) { return image, true })
	result, sources, err := b.Finish()
	require.NoError(t, err)
	require.Len(t, result.Draws, 1)
	require.Len(t, sources, 1)
	assert.Equal(t, [2]float64{8.0 / 3 * 0.5, 1}, sources[0].PatternPeriod)
	assert.NotEqual(t, float64(result.Draws[0].Material.PatternSize[0]), sources[0].PatternPeriod[0])
	assert.Same(t, &image.Pixels[0], &result.Textures[0].RGBA[0])
}

func TestFragmentSymbolBoundaries(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		quad := geometry.TextQuad(0, 0, 5, 5, 0, 0, 1, 1)
		indices := []uint32{0, 1, 2, 0, 2, 3}
		layout := &glyph.PreparedLayout{TextLayout: glyph.TextLayout{Scale: 1}}
		if indexed {
			layout.Vertices, layout.Indices = quad[:], indices
		} else {
			for _, index := range indices {
				v := quad[index]
				layout.Expanded = append(layout.Expanded, v.X, v.Y, v.U, v.V)
			}
		}
		item := RenderSymbol{Symbol: placement.Symbol{Order: 8, IconName: "dot", IconSize: 1, IconOpacity: 1,
			TextColor: style.Color{Alpha: 255}, HaloColor: style.Color{Alpha: 255}, HaloWidth: 1},
			Layout: layout, Accepted: placement.Accepted{Text: true, Icon: true}}
		b := NewFragmentBuilder(indexed, 0, 0)
		atlas := b.GlyphAtlas(&glyph.Atlas{Width: 1, Height: 1, Pixels: []byte{90}})
		labels := b.SymbolLayer(7, 2, func(i int) RenderSymbol {
			copy := item
			copy.Symbol.Anchor.X = float64(i * 20)
			return copy
		}, atlas, func(string, style.Color, float64) (sprite.Image, bool) {
			return sprite.Image{Width: 1, Height: 1, PixelRatio: 1, Pixels: []byte{255, 255, 255, 255}}, true
		})
		assert.Equal(t, 2, labels)
		result, sources, err := b.Finish()
		require.NoError(t, err)
		require.Len(t, result.Draws, 6, "identical materials must not merge different candidates")
		for i, kind := range []scene.Kind{scene.Image, scene.Image, scene.SDFHalo, scene.SDFHalo, scene.SDFFill, scene.SDFFill} {
			assert.Equal(t, kind, result.Draws[i].Material.Kind)
			assert.Equal(t, 7+i%2, sources[i].Candidate)
			assert.Equal(t, 8, sources[i].Layer)
			if i < 2 {
				assert.Equal(t, IconDraw, sources[i].Part)
			} else {
				assert.Equal(t, TextDraw, sources[i].Part)
			}
		}
		b.SymbolLayer(0, 0, nil, 0, nil)
		_, _, err = b.Finish()
		assert.ErrorIs(t, err, ErrPackingClosed)
	}
}

func TestFragmentLimitsAndEmpty(t *testing.T) {
	var zero FragmentBuilder
	zero.Primitive(view.TileID{}, Primitive{}, nil)
	assert.Zero(t, zero.SymbolLayer(0, 0, nil, 0, nil))
	_, _, err := zero.Finish()
	assert.ErrorIs(t, err, ErrPackingInput)
	b := NewFragmentBuilder(false, 0, 0)
	b.GlyphAtlas(&glyph.Atlas{Width: 1, Height: 1, Pixels: []byte{90}})
	b.Primitive(view.TileID{}, Primitive{}, nil)
	assert.Zero(t, b.SymbolLayer(0, 1, func(int) RenderSymbol {
		return RenderSymbol{Layout: &glyph.PreparedLayout{TextLayout: glyph.TextLayout{Scale: 1}}, Accepted: placement.Accepted{Text: true}}
	}, 0, nil), "empty text geometry is not a packed label")
	result, sources, err := b.Finish()
	require.NoError(t, err)
	assert.Empty(t, result.Meshes)
	assert.Empty(t, result.Textures, "empty tiles don't retain an unused atlas")
	assert.Empty(t, sources)
	for _, count := range []int{-1, MaxFragmentDraws + 1} {
		_, _, err := NewFragmentBuilder(false, 0, count).Finish()
		assert.ErrorIs(t, err, geometry.ErrGeometryLimit)
	}
	_, _, err = NewFragmentBuilder(false, -1, -1).Finish()
	assert.ErrorIs(t, err, geometry.ErrGeometryLimit)
	b = NewFragmentBuilder(false, 0, 1)
	p := Primitive{Mesh: BackgroundGeometry(false), Color: style.Color{Alpha: 255}}
	b.Primitive(view.TileID{}, p, nil)
	b.Primitive(view.TileID{}, p, nil)
	result, sources, err = b.Finish()
	assert.ErrorIs(t, err, geometry.ErrGeometryLimit)
	assert.Nil(t, result)
	assert.Nil(t, sources)
	for _, bad := range []struct {
		tile      view.TileID
		primitive Primitive
	}{
		{view.TileID{Z: 33}, p}, {view.TileID{}, Primitive{Order: -1}},
	} {
		b := NewFragmentBuilder(false, 0, 0)
		b.Primitive(bad.tile, bad.primitive, nil)
		_, _, err := b.Finish()
		assert.ErrorIs(t, err, ErrPackingInput)
	}
	for _, pair := range [][2]int{{-1, 0}, {0, -1}, {0, placement.MaxSymbols + 1}, {math.MaxInt, 1}} {
		b := NewFragmentBuilder(false, 0, 0)
		b.SymbolLayer(pair[0], pair[1], func(int) RenderSymbol { t.Fatal("invalid range called accessor"); return RenderSymbol{} }, 0, nil)
		_, _, err := b.Finish()
		assert.ErrorIs(t, err, geometry.ErrGeometryLimit)
	}
	b = NewFragmentBuilder(false, 0, 0)
	b.SymbolLayer(0, 1, func(int) RenderSymbol { return RenderSymbol{Symbol: placement.Symbol{Order: -1}} }, 0, nil)
	_, _, err = b.Finish()
	assert.ErrorIs(t, err, ErrPackingInput)
}

func TestSymbolTextRequests(t *testing.T) {
	values := []placement.Symbol{{Text: "A", FontStack: "font", TextSize: 12, LetterSpacing: 2, MaximumWidth: 8, LineHeight: 1.5, TextAnchor: "left", TextJustify: "right", HaloWidth: 1, HaloBlur: 0.5}, {Text: "B"}}
	visits := 0
	for index, request := range SymbolTextRequests(values) {
		visits++
		assert.Zero(t, index)
		assert.Equal(t, TextRequest{Text: "A", FontStack: "font", Options: glyph.LayoutOptions{TextSize: 12, LetterSpacing: 2, MaximumWidth: 8, LineHeight: 1.5, Anchor: "left", Justify: "right", HaloWidth: 1, HaloBlur: 0.5}}, request)
		break
	}
	assert.Equal(t, 1, visits)
}
