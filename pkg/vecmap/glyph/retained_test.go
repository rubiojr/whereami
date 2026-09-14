package glyph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetainedAtlasCurrentWinsAndOwnership(t *testing.T) {
	a, b := Key{"font", 1}, Key{"font", 2}
	current := map[Key]Glyph{a: bitmapGlyph(2, 2, 90)}
	retained := map[Key]Glyph{a: bitmapGlyph(3, 3, 50), b: bitmapGlyph(2, 2, 60)}
	atlas, resident, err := BuildRetainedAtlas(current, retained)
	require.NoError(t, err)
	want, err := BuildAtlas(map[Key]Glyph{a: current[a], b: retained[b]})
	require.NoError(t, err)
	assert.Equal(t, want, atlas)
	require.Len(t, resident, 2)
	assert.Equal(t, current[a], resident[a])
	assert.Equal(t, uint32(3), retained[a].Width)
	assert.Same(t, &current[a].Bitmap[0], &resident[a].Bitmap[0], "resident bitmaps are immutable borrows")
	delete(resident, a)
	assert.Contains(t, current, a)
	clear(current)
	clear(retained)
	assert.Equal(t, want, atlas)
	assert.Contains(t, resident, b)
}

func TestRetainedAtlasEmptyAndInvalid(t *testing.T) {
	key := Key{"font", 1}
	invalid := map[Key]Glyph{key: {Width: 1, Height: 1}}
	for _, current := range []map[Key]Glyph{nil, {}, {key: {Advance: 5}}} {
		atlas, resident, err := BuildRetainedAtlas(current, invalid)
		require.NoError(t, err)
		assert.Nil(t, atlas)
		assert.Nil(t, resident)
	}
	atlas, resident, err := BuildRetainedAtlas(invalid, nil)
	require.Error(t, err)
	assert.Nil(t, atlas)
	assert.Nil(t, resident)
	current := map[Key]Glyph{{"current", 1}: bitmapGlyph(2, 2, 80)}
	atlas, resident, err = BuildRetainedAtlas(current, invalid)
	require.NoError(t, err)
	want, err := BuildAtlas(current)
	require.NoError(t, err)
	assert.Equal(t, want, atlas)
	assert.Equal(t, current, resident)
}

func TestRetainedAtlasRejectsOptionalGrowth(t *testing.T) {
	current := map[Key]Glyph{{"current", 1}: bitmapGlyph(2, 2, 80)}
	retained := map[Key]Glyph{{"old", 1}: bitmapGlyph(255, 255, 90)}
	atlas, resident, err := BuildRetainedAtlas(current, retained)
	require.NoError(t, err)
	assert.Equal(t, MinAtlasSize, atlas.Width)
	assert.Equal(t, current, resident)
}

func TestRetainedAtlasDisplacementAndPartialCurrent(t *testing.T) {
	// 7x7 padded 255px glyphs fit at 2048; an earlier optional key displaces
	// one required glyph at the same size and must be discarded.
	g := bitmapGlyph(255, 255, 70)
	current := make(map[Key]Glyph)
	for i := range 49 {
		current[Key{"current", uint32(i)}] = g
	}
	retained := map[Key]Glyph{{"before", 1}: g}
	atlas, resident, err := BuildRetainedAtlas(current, retained)
	require.NoError(t, err)
	assert.Equal(t, MaxAtlasSize, atlas.Width)
	assert.Len(t, resident, 49)
	assert.False(t, AtlasNeedsRebuild(atlas, current))
	assert.NotContains(t, resident, Key{"before", 1})
	current[Key{"current", 49}] = g
	atlas, resident, err = BuildRetainedAtlas(current, retained)
	require.NoError(t, err)
	assert.Len(t, resident, 49)
	assert.True(t, AtlasNeedsRebuild(atlas, current), "current-only output can still be partial")
	assert.NotContains(t, resident, Key{"current", 49})
	assert.NotContains(t, resident, Key{"before", 1})
}

func TestAtlasCoverageUsesKeysOnly(t *testing.T) {
	key := Key{"font", 1}
	assert.False(t, AtlasNeedsRebuild(nil, nil))
	assert.True(t, AtlasNeedsRebuild(nil, map[Key]Glyph{key: {}}))
	atlas := &Atlas{Positions: map[Key]Rect{key: {}}}
	assert.False(t, AtlasNeedsRebuild(atlas, map[Key]Glyph{key: {}}))
	assert.True(t, AtlasNeedsRebuild(atlas, map[Key]Glyph{{"missing", 1}: {}}))
	// Bitmap-free current keys do not get positions and force the original
	// current-only fallback, even if optional drawable glyphs would fit.
	current := map[Key]Glyph{key: bitmapGlyph(2, 2, 80), {"space", 32}: {Advance: 4}}
	prepared, resident, err := BuildRetainedAtlas(current, map[Key]Glyph{{"old", 1}: bitmapGlyph(2, 2, 90)})
	require.NoError(t, err)
	assert.Len(t, resident, 1)
	assert.True(t, AtlasNeedsRebuild(prepared, current))
}
