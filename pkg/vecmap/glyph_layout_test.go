package vecmap

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShapeSDFTextUsesGlyphMetricsAndHalo(t *testing.T) {
	glyphs := map[uint32]sdfGlyph{
		'A': testSDFGlyph('A', 6, 8, 0, -8, 10, 100),
		'B': testSDFGlyph('B', 7, 8, 1, -8, 12, 120),
		' ': {id: ' ', advance: 5},
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
	assert.InDelta(t, -11, layout.glyphs[0].x, 0.001)
	assert.InDelta(t, -1, layout.glyphs[1].x, 0.001)
	assert.InDelta(t, -14, layout.bounds.left, 0.001)
	assert.InDelta(t, 10, layout.bounds.right, 0.001)
	assert.InDelta(t, -12, layout.bounds.top, 0.001)
	assert.InDelta(t, 2, layout.bounds.bottom, 0.001)
}

func TestSDFAtlasPreservesGuardsAndProducesQuads(t *testing.T) {
	glyph := testSDFGlyph('A', 2, 2, 0, -2, 4, 90)
	key := sdfGlyphKey{fontStack: "Noto Sans Regular", id: 'A'}
	atlas := buildSDFAtlas(map[sdfGlyphKey]sdfGlyph{key: glyph})
	require.NotNil(t, atlas)
	rectangle := atlas.positions[key]
	assert.Equal(t, 10, rectangle.width)
	assert.Equal(t, 10, rectangle.height)
	assert.Equal(t, byte(0), atlas.pixels[rectangle.y*atlas.width+rectangle.x])
	firstBitmapPixel := (rectangle.y+1)*atlas.width + rectangle.x + 1
	assert.Equal(t, byte(90), atlas.pixels[firstBitmapPixel])

	layout := &sdfTextLayout{
		glyphs: []sdfPositionedGlyph{{key: key, glyph: glyph, x: 0, y: 0}},
		scale:  1,
	}
	vertices := sdfLayoutVertices(layout, atlas)
	assert.Len(t, vertices, 24)
	assert.Equal(t, float32(-4), vertices[0])
	assert.Equal(t, float32(-2), vertices[1])
	assert.Equal(t, float32(6), vertices[4])
	assert.Equal(t, float32(-2), vertices[5])
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
}

func TestSDFTextWaitsForItsLayoutInsteadOfUsingQtFallback(t *testing.T) {
	assert.False(t, libertyTextRenderable("Madrid", nil))
	assert.True(t, libertyTextRenderable("Madrid", &sdfTextLayout{}))
	assert.True(t, libertyTextRenderable("مرحبا", nil))
	assert.False(t, libertyTextRenderable("ދިވެހި", nil))
}

func TestNormalizeSDFTextUsesFetchedSeparators(t *testing.T) {
	assert.Equal(t, "Madrid 東京\nSeoul", normalizeLibertySymbolText("Madrid\u3000東京\r\nSeoul"))
}

func TestBreakSDFLinesUsesMaximumWidth(t *testing.T) {
	glyphs := map[uint32]sdfGlyph{
		'A': {id: 'A', advance: 10},
		'B': {id: 'B', advance: 10},
		' ': {id: ' ', advance: 5},
	}
	assert.Equal(t, [][]rune{{'A'}, {'B'}}, breakSDFLines("A B", glyphs, 0, 20))
	assert.Equal(t, [][]rune{{'A'}, {'B'}}, breakSDFLines("AB", glyphs, 0, 15))
	assert.Equal(t, [][]rune{{'A', ' ', 'B'}}, breakSDFLines("A B", glyphs, 2, 29))
	assert.Equal(t, [][]rune{{'A'}, {'B'}}, breakSDFLines("A B", glyphs, 2, 28))
}

func TestPackSDFGlyphsReturnsDeterministicPartialAtlas(t *testing.T) {
	glyphs := make(map[sdfGlyphKey]sdfGlyph)
	keys := make([]sdfGlyphKey, 100)
	for index := range keys {
		key := sdfGlyphKey{fontStack: "test", id: uint32(index)}
		keys[index] = key
		glyphs[key] = sdfGlyph{width: maximumGlyphDimension, height: maximumGlyphDimension}
	}

	positions, fits := packSDFGlyphs(keys, glyphs, maximumSDFAtlasSize)
	assert.False(t, fits)
	assert.Len(t, positions, 49)
	assert.Contains(t, positions, keys[0])
	assert.NotContains(t, positions, keys[len(keys)-1])
}

func TestBuildSDFSceneRejectsLayoutsWithoutDrawableGlyphs(t *testing.T) {
	key := libertySDFLayoutKey{tile: vectorTileID{Z: 1}, index: 2}
	layout := &sdfTextLayout{
		glyphs: []sdfPositionedGlyph{{
			key:   sdfGlyphKey{fontStack: "test", id: ' '},
			glyph: sdfGlyph{id: ' ', advance: 5},
		}},
		scale: 1,
	}
	atlas := &sdfGlyphAtlas{width: 256, height: 256, positions: make(map[sdfGlyphKey]sdfAtlasRect)}
	assert.Nil(t, buildSDFScene(map[libertySDFLayoutKey]*sdfTextLayout{key: layout}, atlas))
}

func TestSDFAtlasCacheMatchesGlyphSets(t *testing.T) {
	first := sdfGlyphKey{fontStack: "test", id: 'A'}
	second := sdfGlyphKey{fontStack: "test", id: 'B'}
	glyphs := map[sdfGlyphKey]sdfGlyph{first: {}, second: {}}
	keys := sdfGlyphKeySet(glyphs)

	assert.True(t, sameSDFGlyphKeys(keys, map[sdfGlyphKey]struct{}{second: {}, first: {}}))
	assert.False(t, sameSDFGlyphKeys(keys, map[sdfGlyphKey]struct{}{first: {}}))
	assert.False(t, sdfAtlasNeedsRebuild(
		&sdfGlyphAtlas{positions: map[sdfGlyphKey]sdfAtlasRect{first: {}, second: {}}},
		glyphs,
	))
	assert.True(t, sdfAtlasNeedsRebuild(
		&sdfGlyphAtlas{positions: map[sdfGlyphKey]sdfAtlasRect{first: {}}},
		glyphs,
	))
}

func TestBuildRetainedSDFAtlasKeepsPriorGlyphsWhenTheyFit(t *testing.T) {
	firstKey := sdfGlyphKey{fontStack: "test", id: 'A'}
	secondKey := sdfGlyphKey{fontStack: "test", id: 'B'}
	first := testSDFGlyph('A', 2, 2, 0, -2, 4, 90)
	second := testSDFGlyph('B', 2, 2, 0, -2, 4, 100)

	atlas, retained := buildRetainedSDFAtlas(
		map[sdfGlyphKey]sdfGlyph{secondKey: second},
		map[sdfGlyphKey]sdfGlyph{firstKey: first},
	)
	require.NotNil(t, atlas)
	assert.Contains(t, atlas.positions, firstKey)
	assert.Contains(t, atlas.positions, secondKey)
	assert.Contains(t, retained, firstKey)
	assert.Contains(t, retained, secondKey)
}

func testSDFGlyph(id rune, width, height uint32, left, top int32, advance uint32, distance byte) sdfGlyph {
	bitmap := make([]byte, int(width+2*glyphPBFBorder)*int(height+2*glyphPBFBorder))
	for index := range bitmap {
		bitmap[index] = distance
	}
	return sdfGlyph{
		id:      uint32(id),
		bitmap:  bitmap,
		width:   width,
		height:  height,
		left:    left,
		top:     top,
		advance: advance,
	}
}
