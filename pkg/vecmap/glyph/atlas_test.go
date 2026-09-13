package glyph

import (
	"bytes"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func bitmapGlyph(width, height uint32, distance byte) Glyph {
	return Glyph{Width: width, Height: height,
		Bitmap: bytes.Repeat([]byte{distance}, int(width+2*PBFBorder)*int(height+2*PBFBorder))}
}

func TestBuildAtlasGuardsOrderingAndOwnership(t *testing.T) {
	a, b, c, d, e := Key{"z", 1}, Key{"a", 2}, Key{"a", 1}, Key{"narrow", 1}, Key{"short", 1}
	glyphs := map[Key]Glyph{a: bitmapGlyph(2, 3, 90), b: bitmapGlyph(2, 3, 91),
		c: bitmapGlyph(2, 3, 92), d: bitmapGlyph(1, 3, 93), e: bitmapGlyph(4, 2, 94)}
	atlas, err := BuildAtlas(glyphs)
	require.NoError(t, err)
	require.NotNil(t, atlas)
	assert.Equal(t, MinAtlasSize, atlas.Width)
	assert.Equal(t, atlas.Width, atlas.Height)
	assert.Len(t, atlas.Pixels, atlas.Width*atlas.Height)
	assert.Equal(t, 0, atlas.Positions[c].X)
	assert.Equal(t, 10, atlas.Positions[b].X)
	assert.Equal(t, 20, atlas.Positions[a].X)
	assert.Equal(t, 30, atlas.Positions[d].X)
	assert.Equal(t, 39, atlas.Positions[e].X)
	for key, rect := range atlas.Positions {
		g := glyphs[key]
		assert.Equal(t, int(g.Width)+2*AtlasPadding, rect.Width)
		assert.Equal(t, int(g.Height)+2*AtlasPadding, rect.Height)
		for y := range rect.Height {
			for x := range rect.Width {
				want := g.Bitmap[0]
				if x == 0 || y == 0 || x == rect.Width-1 || y == rect.Height-1 {
					want = 0
				}
				require.Equal(t, want, atlas.Pixels[(rect.Y+y)*atlas.Width+rect.X+x])
			}
		}
	}
	for range 5 {
		next, err := BuildAtlas(glyphs)
		require.NoError(t, err)
		assert.Equal(t, atlas, next)
	}
	saved := append([]byte(nil), atlas.Pixels...)
	for key, g := range glyphs {
		clear(g.Bitmap)
		delete(glyphs, key)
	}
	assert.Equal(t, saved, atlas.Pixels)
	assert.Len(t, atlas.Positions, 5)
}

func TestBuildAtlasGrowthAndDeterministicPartialOutput(t *testing.T) {
	g := bitmapGlyph(MaxDimension, MaxDimension, 77)
	input := map[Key]Glyph{{"font", 0}: g}
	atlas, err := BuildAtlas(input)
	require.NoError(t, err)
	assert.Equal(t, 512, atlas.Width)
	for id := range 100 {
		input[Key{"font", uint32(id)}] = g
	}
	atlas, err = BuildAtlas(input)
	require.NoError(t, err)
	assert.Equal(t, MaxAtlasSize, atlas.Width)
	assert.Len(t, atlas.Positions, 49)
	for id := range 49 {
		assert.Contains(t, atlas.Positions, Key{"font", uint32(id)})
	}
	assert.NotContains(t, atlas.Positions, Key{"font", 49})
	assert.Equal(t, Rect{X: 0, Y: 263, Width: 263, Height: 263}, atlas.Positions[Key{"font", 7}])
	other, err := BuildAtlas(input)
	require.NoError(t, err)
	assert.Equal(t, atlas, other)
}

func TestBuildAtlasRejectsInvalidDataBeforePacking(t *testing.T) {
	for _, g := range []Glyph{{Width: math.MaxUint32}, {Height: math.MaxUint32}, {Advance: 256},
		{Left: -129}, {Top: 128}, {Width: 1, Height: 1}, {Bitmap: []byte{1}}} {
		atlas, err := BuildAtlas(map[Key]Glyph{{"valid", 1}: bitmapGlyph(2, 2, 9), {"invalid", 2}: g})
		assert.Error(t, err)
		assert.Nil(t, atlas)
	}
	for _, input := range []map[Key]Glyph{nil, {}, {{"space", 32}: {Advance: 6}}, {{"empty", 1}: {Height: 255}}} {
		atlas, err := BuildAtlas(input)
		require.NoError(t, err)
		assert.Nil(t, atlas)
	}
}

func TestPackRejectsOversizeAndReturnsPartialPositions(t *testing.T) {
	key := Key{"test", 1}
	for _, g := range []Glyph{{Width: MaxDimension}, {Height: MaxDimension}} {
		positions, fits := pack([]Key{key}, map[Key]Glyph{key: g}, MinAtlasSize)
		assert.Nil(t, positions)
		assert.False(t, fits)
	}
	glyphs := make(map[Key]Glyph)
	keys := make([]Key, 100)
	for index := range keys {
		keys[index] = Key{"test", uint32(index)}
		glyphs[keys[index]] = Glyph{Width: MaxDimension, Height: MaxDimension}
	}
	positions, fits := pack(keys, glyphs, MaxAtlasSize)
	assert.False(t, fits)
	assert.Len(t, positions, 49)
	assert.Contains(t, positions, keys[0])
	assert.NotContains(t, positions, keys[len(keys)-1])
}
