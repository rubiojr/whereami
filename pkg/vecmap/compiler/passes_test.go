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

func TestPrimitivePatternMaterialAndPhase(t *testing.T) {
	b := NewSceneBuilder(false, 0)
	primitive := Primitive{Mesh: BackgroundGeometry(false), PatternName: "dots", PatternScale: 0.5, Opacity: 0.25}
	image := sprite.Image{Pixels: make([]byte, 8*6*4), Width: 8, Height: 6, PixelRatio: 2}
	lookup := func(name string, color style.Color, opacity float64) (sprite.Image, bool) {
		assert.Equal(t, "dots", name)
		assert.Equal(t, style.Color{Red: 255, Green: 255, Blue: 255, Alpha: 255}, color)
		assert.Equal(t, 1.0, opacity)
		return image, true
	}
	tile := view.TileID{X: 3, Y: 2, Z: 3}
	b.Primitive(tile, 1, primitive, nil)
	b.Primitive(tile, 1, primitive, func(string, style.Color, float64) (sprite.Image, bool) { return sprite.Image{}, false })
	b.Primitive(tile, 1, primitive, lookup)
	b.Primitive(tile, 1, Primitive{Mesh: BackgroundGeometry(false), Color: style.Color{Red: 255, Alpha: 255}}, nil)
	result, err := b.Finish()
	require.NoError(t, err)
	require.Len(t, result.Draws, 2)
	x, y := view.PatternPhase(tile, 1, 2, 1.5)
	assert.Equal(t, scene.Material{Kind: scene.Pattern, Texture: 1, Color: [4]float32{1, 1, 1, 0.25}, PatternSize: [2]float32{2, 1.5}, PatternPhase: [2]float32{float32(x), float32(y)}}, result.Draws[0].Material)
	assert.Equal(t, [4]float32{0, 0, 256, 256}, result.Draws[0].Clip)
	assert.Same(t, &image.Pixels[0], &result.Textures[0].RGBA[0])
	b.Primitive(tile, 0, primitive, lookup)
	_, err = b.Finish()
	assert.ErrorIs(t, err, ErrPackingClosed)
}

func TestSymbolPassOrderAndPaint(t *testing.T) {
	var scenes []*scene.Scene
	for _, indexed := range []bool{false, true} {
		quad := geometry.TextQuad(-2, -3, 4, 5, 0, 0, 1, 1)
		indices := []uint32{0, 1, 2, 0, 2, 3}
		layout := &glyph.PreparedLayout{TextLayout: glyph.TextLayout{Scale: 1}}
		if indexed {
			layout.LayoutMesh = glyph.LayoutMesh{Vertices: quad[:], Indices: indices}
		} else {
			for _, index := range indices {
				v := quad[index]
				layout.Expanded = append(layout.Expanded, v.X, v.Y, v.U, v.V)
			}
		}
		first := RenderSymbol{Symbol: placement.Symbol{Anchor: geometry.Point{X: 10, Y: 20},
			IconName: "airport", IconSize: 2, IconOpacity: 0.5, IconColor: style.Color{Red: 42, Alpha: 255},
			IconOffset: geometry.Point{X: 1, Y: 2}, IconAnchor: "top-left", IconRotate: math.Pi / 2, IconViewportAligned: true,
			TextSize: 12, TextColor: style.Color{Red: 255, Alpha: 255}, HaloColor: style.Color{Blue: 255, Alpha: 255},
			HaloWidth: 1, HaloBlur: 0.5, LineAngle: math.Pi / 2, TextOffset: geometry.Point{X: 1, Y: 2}},
			Accepted: placement.Accepted{Text: true, Icon: true}, Layout: layout}
		second := first
		second.Symbol.Anchor.X = 30
		second.Symbol.TextColor = style.Color{Green: 255, Alpha: 255}
		second.Symbol.HaloColor = style.Color{Alpha: 255}
		b := NewSceneBuilder(indexed, 0)
		atlas := b.GlyphAtlas(&glyph.Atlas{Width: 1, Height: 1, Pixels: []byte{90}})
		visits, calls := 0, 0
		items := []RenderSymbol{first, second}
		at := func(i int) RenderSymbol { visits++; return items[i] }
		labels := b.SymbolLayer(len(items), at, atlas, func(name string, color style.Color, opacity float64) (sprite.Image, bool) {
			calls++
			assert.Equal(t, "airport", name)
			assert.Equal(t, first.Symbol.IconColor, color)
			assert.Equal(t, 0.5, opacity)
			return sprite.Image{Width: 1, Height: 1, PixelRatio: 2, Pixels: []byte{42, 0, 0, 128}}, true
		})
		assert.Equal(t, 2, labels)
		assert.Equal(t, 6, visits)
		assert.Equal(t, 2, calls)
		result, err := b.Finish()
		require.NoError(t, err)
		require.Len(t, result.Draws, 5)
		assert.Equal(t, []scene.Kind{scene.Image, scene.SDFHalo, scene.SDFHalo, scene.SDFFill, scene.SDFFill}, []scene.Kind{result.Draws[0].Material.Kind, result.Draws[1].Material.Kind, result.Draws[2].Material.Kind, result.Draws[3].Material.Kind, result.Draws[4].Material.Kind})
		assert.False(t, result.Draws[0].Material.MapAligned)
		assert.True(t, result.Draws[1].Material.MapAligned)
		assert.Equal(t, float32(0.5), result.Draws[1].Material.HaloBlur)
		assert.Equal(t, PackedColor(first.Symbol.TextColor), result.Draws[3].Material.Color)
		require.Len(t, result.Textures, 2)
		scenes = append(scenes, result)
	}
	assert.Equal(t, scenes[0].Draws, scenes[1].Draws)
	for i, index := range scenes[1].Meshes[0].Indices {
		assert.Equal(t, scenes[0].Meshes[0].Vertices[i], scenes[1].Meshes[0].Vertices[index])
	}
}

func TestSymbolPassFilteringAndLimits(t *testing.T) {
	quad := []float32{0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0}
	base := RenderSymbol{Symbol: placement.Symbol{IconName: "missing", IconSize: 1, TextColor: style.Color{Alpha: 255}},
		Layout: &glyph.PreparedLayout{TextLayout: glyph.TextLayout{Scale: 1}, LayoutMesh: glyph.LayoutMesh{Expanded: quad}}, Accepted: placement.Accepted{Text: true, Icon: true}}
	b := NewSceneBuilder(false, 0)
	assert.Zero(t, b.SymbolLayer(0, nil, 0, nil))
	items := []RenderSymbol{base, {}, {Accepted: placement.Accepted{Text: true}}, {Symbol: base.Symbol, Layout: base.Layout}}
	at := func(i int) RenderSymbol { return items[i] }
	assert.Equal(t, 1, b.SymbolLayer(len(items), at, 0, nil))
	result, err := b.Finish()
	require.NoError(t, err)
	require.Len(t, result.Draws, 1)
	assert.Equal(t, scene.SDFFill, result.Draws[0].Material.Kind)
	assert.Zero(t, b.SymbolLayer(len(items), at, 0, nil))
	_, err = b.Finish()
	assert.ErrorIs(t, err, ErrPackingClosed)
	b = NewSceneBuilder(false, 0)
	visits := 0
	assert.Zero(t, b.SymbolLayer(placement.MaxSymbols+10, func(int) RenderSymbol { visits++; return RenderSymbol{} }, 0, nil))
	assert.Zero(t, visits)
	_, err = b.Finish()
	assert.ErrorIs(t, err, geometry.ErrGeometryLimit)
	for _, ratio := range []float64{0, math.NaN(), math.Inf(1)} {
		b = NewSceneBuilder(false, 0)
		assert.Zero(t, b.SymbolLayer(1, func(int) RenderSymbol { return base }, 0, func(string, style.Color, float64) (sprite.Image, bool) { return sprite.Image{PixelRatio: ratio}, true }))
		_, err = b.Finish()
		assert.ErrorIs(t, err, ErrPackingInput)
	}
	for _, count := range []int{-1, 1} {
		b = NewSceneBuilder(false, 0)
		b.SymbolLayer(count, nil, 0, nil)
		_, err = b.Finish()
		assert.ErrorIs(t, err, ErrPackingInput)
	}
}
