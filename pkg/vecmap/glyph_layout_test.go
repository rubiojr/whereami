package vecmap

import (
	"math"
	"strings"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/glyph"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShapeSDFTextUsesGlyphMetricsAndHalo(t *testing.T) {
	glyphs := map[uint32]sdfGlyph{
		'A': testSDFGlyph('A', 6, 8, 0, -8, 10, 100),
		'B': testSDFGlyph('B', 7, 8, 1, -8, 12, 120),
		' ': {ID: ' ', Advance: 5},
	}
	candidate := libertySymbolCandidate{
		text:          "AB",
		fontStack:     "Noto Sans Regular",
		textSize:      24,
		letterSpacing: 0,
		lineHeight:    1,
		maximumWidth:  10,
		textAnchor:    "center",
		textJustify:   "auto",
		haloWidth:     2,
		haloBlur:      1,
	}

	layout := shapeSDFText(candidate, glyphs)
	require.NotNil(t, layout)
	assert.Equal(t, 1.0, layout.scale)
	assert.Len(t, layout.glyphs, 2)
	assert.InDelta(t, -11, layout.glyphs[0].X, 0.001)
	assert.InDelta(t, -1, layout.glyphs[1].X, 0.001)
	assert.InDelta(t, -14, layout.bounds.left, 0.001)
	assert.InDelta(t, 10, layout.bounds.right, 0.001)
	assert.InDelta(t, -12, layout.bounds.top, 0.001)
	assert.InDelta(t, 2, layout.bounds.bottom, 0.001)
}

func TestSDFAtlasPreservesGuardsAndProducesQuads(t *testing.T) {
	glyph := testSDFGlyph('A', 2, 2, 0, -2, 4, 90)
	key := sdfGlyphKey{FontStack: "Noto Sans Regular", ID: 'A'}
	atlas := buildSDFAtlas(map[sdfGlyphKey]sdfGlyph{key: glyph})
	require.NotNil(t, atlas)
	rectangle := atlas.Positions[key]
	assert.Equal(t, 10, rectangle.Width)
	assert.Equal(t, 10, rectangle.Height)
	assert.Equal(t, byte(0), atlas.Pixels[rectangle.Y*atlas.Width+rectangle.X])
	firstBitmapPixel := (rectangle.Y+1)*atlas.Width + rectangle.X + 1
	assert.Equal(t, byte(90), atlas.Pixels[firstBitmapPixel])

	layout := &sdfTextLayout{
		glyphs: []sdfPositionedGlyph{{Key: key, Glyph: glyph, X: 0, Y: 0}},
		scale:  1,
	}
	vertices := sdfLayoutVertices(layout, atlas)
	assert.Len(t, vertices, 24)
	assert.Equal(t, float32(-4), vertices[0])
	assert.Equal(t, float32(-2), vertices[1])
	assert.Equal(t, float32(6), vertices[4])
	assert.Equal(t, float32(-2), vertices[5])
}

func TestBuildSDFSceneRejectsInvalidQuadGeometry(t *testing.T) {
	g := testSDFGlyph('A', 2, 2, 0, -2, 4, 90)
	key := sdfGlyphKey{FontStack: "test", ID: 'A'}
	atlas := buildSDFAtlas(map[sdfGlyphKey]sdfGlyph{key: g})
	layout := &sdfTextLayout{glyphs: []sdfPositionedGlyph{{Key: key, Glyph: g, X: math.MaxFloat64}}, scale: 1}
	for _, indexed := range []bool{false, true} {
		scene, err := buildSDFSceneGeometry(map[libertySDFLayoutKey]*sdfTextLayout{{}: layout}, atlas, indexed)
		assert.ErrorIs(t, err, glyph.ErrLayoutGeometry)
		assert.Nil(t, scene)
		assert.Nil(t, layout.vertices)
		assert.Nil(t, layout.indexedVertices)
		assert.Nil(t, layout.indices)
	}
}

func TestSDFTextEligibilityRejectsComplexShaping(t *testing.T) {
	assert.True(t, sdfTextEligible("Madrid 東京"))
	assert.False(t, sdfTextEligible("مرحبا"))
	assert.False(t, sdfTextEligible("e\u0301"))
	assert.False(t, sdfTextEligible("\U0001f600"))
	assert.False(t, sdfTextEligible(strings.Repeat("A", maximumSymbolTextRunes+1)))
}

func TestQtTextFallbackRejectsUnsupportedThaana(t *testing.T) {
	assert.True(t, qtTextFallbackEligible("مرحبا"))
	assert.False(t, qtTextFallbackEligible("ދިވެހި"))
	assert.False(t, qtTextFallbackEligible("\xff"))
}

func TestSDFTextWaitsForItsLayoutInsteadOfUsingQtFallback(t *testing.T) {
	assert.False(t, libertyTextRenderable("Madrid", nil))
	assert.True(t, libertyTextRenderable("Madrid", &sdfTextLayout{}))
	assert.True(t, libertyTextRenderable("مرحبا", nil))
	assert.False(t, libertyTextRenderable("ދިވެހި", nil))
	assert.False(t, libertyTextRenderable("\xff", nil))
}

func TestNormalizeSDFTextUsesFetchedSeparators(t *testing.T) {
	assert.Equal(t, "Madrid 東京\nSeoul", normalizeLibertySymbolText("Madrid\u3000東京\r\nSeoul"))
}

func TestBuildSDFSceneRejectsLayoutsWithoutDrawableGlyphs(t *testing.T) {
	key := libertySDFLayoutKey{tile: vectorTileID{Z: 1}, index: 2}
	layout := &sdfTextLayout{
		glyphs: []sdfPositionedGlyph{{
			Key:   sdfGlyphKey{FontStack: "test", ID: ' '},
			Glyph: sdfGlyph{ID: ' ', Advance: 5},
		}},
		scale: 1,
	}
	atlas := &sdfGlyphAtlas{Width: 256, Height: 256, Positions: make(map[sdfGlyphKey]sdfAtlasRect)}
	assert.Nil(t, buildSDFScene(map[libertySDFLayoutKey]*sdfTextLayout{key: layout}, atlas))
}

func TestSDFAtlasCacheMatchesGlyphSets(t *testing.T) {
	first := sdfGlyphKey{FontStack: "test", ID: 'A'}
	second := sdfGlyphKey{FontStack: "test", ID: 'B'}
	glyphs := map[sdfGlyphKey]sdfGlyph{first: {}, second: {}}
	keys := sdfGlyphKeySet(glyphs)

	assert.True(t, sameSDFGlyphKeys(keys, map[sdfGlyphKey]struct{}{second: {}, first: {}}))
	assert.False(t, sameSDFGlyphKeys(keys, map[sdfGlyphKey]struct{}{first: {}}))
	assert.False(t, sdfAtlasNeedsRebuild(
		&sdfGlyphAtlas{Positions: map[sdfGlyphKey]sdfAtlasRect{first: {}, second: {}}},
		glyphs,
	))
	assert.True(t, sdfAtlasNeedsRebuild(
		&sdfGlyphAtlas{Positions: map[sdfGlyphKey]sdfAtlasRect{first: {}}},
		glyphs,
	))
}

func TestBuildRetainedSDFAtlasKeepsPriorGlyphsWhenTheyFit(t *testing.T) {
	firstKey := sdfGlyphKey{FontStack: "test", ID: 'A'}
	secondKey := sdfGlyphKey{FontStack: "test", ID: 'B'}
	first := testSDFGlyph('A', 2, 2, 0, -2, 4, 90)
	second := testSDFGlyph('B', 2, 2, 0, -2, 4, 100)

	atlas, retained := buildRetainedSDFAtlas(
		map[sdfGlyphKey]sdfGlyph{secondKey: second},
		map[sdfGlyphKey]sdfGlyph{firstKey: first},
	)
	require.NotNil(t, atlas)
	assert.Contains(t, atlas.Positions, firstKey)
	assert.Contains(t, atlas.Positions, secondKey)
	assert.Contains(t, retained, firstKey)
	assert.Contains(t, retained, secondKey)
}

func testSDFGlyph(id rune, width, height uint32, left, top int32, advance uint32, distance byte) sdfGlyph {
	bitmap := make([]byte, int(width+2*glyphPBFBorder)*int(height+2*glyphPBFBorder))
	for index := range bitmap {
		bitmap[index] = distance
	}
	return sdfGlyph{
		ID:      uint32(id),
		Bitmap:  bitmap,
		Width:   width,
		Height:  height,
		Left:    left,
		Top:     top,
		Advance: advance,
	}
}
