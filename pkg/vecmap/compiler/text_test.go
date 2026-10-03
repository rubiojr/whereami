package compiler

import (
	"bytes"
	"iter"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/glyph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func textRequest(text, font string) TextRequest {
	return TextRequest{Text: text, FontStack: font, Options: glyph.LayoutOptions{
		TextSize: 24, LineHeight: 1, MaximumWidth: 10, Anchor: "left", Justify: "left",
	}}
}

func textFont() map[uint32]glyph.Glyph {
	return map[uint32]glyph.Glyph{
		'A': {ID: 'A', Width: 2, Height: 2, Advance: 4, Bitmap: bytes.Repeat([]byte{90}, 64)},
		' ': {ID: ' ', Advance: 2},
	}
}

func TestFontDiscoveryPreservesIdentity(t *testing.T) {
	requests := []TextRequest{textRequest("A", "z"), textRequest("", "unused"), textRequest("unsupported مرحبا", "a"), textRequest("A", "z"), textRequest("A", ""), textRequest("A", " a ")}
	assert.Equal(t, []string{"", " a ", "a", "z"}, FontStacks(slices.All(requests)))
	assert.NotNil(t, FontStacks[int](nil))
	assert.Empty(t, FontStacks[int](nil))
}

func TestPrepareTextLayoutsAvailabilityAndMetrics(t *testing.T) {
	available := textFont()
	fonts := map[string]map[uint32]glyph.Glyph{"good": available}
	requests := []TextRequest{
		textRequest("A A\r\nA", "good"), textRequest("", "empty"), textRequest("B", "good"),
		textRequest("A", "z-missing"), textRequest("A", "a-missing"), textRequest("مرحبا", "unsupported"),
		textRequest("\xff", "invalid"), textRequest("A", "good"),
	}
	requests[7].Options.TextSize = -1
	layouts, missing, err := PrepareTextLayouts(slices.All(requests), fonts)
	require.NoError(t, err)
	require.Len(t, layouts, 1)
	assert.Equal(t, []string{"a-missing", "good", "invalid", "unsupported", "z-missing"}, missing)
	want, ok := glyph.LayoutText(requests[0].Text, "good", available, requests[0].Options)
	require.True(t, ok)
	assert.Equal(t, want, layouts[0].TextLayout)
	assert.Empty(t, layouts[0].LayoutMesh)
	assert.Same(t, &available['A'].Bitmap[0], &layouts[0].Glyphs[0].Glyph.Bitmap[0])
	layouts[0].Glyphs[0].Glyph.Advance = 99
	assert.Equal(t, uint32(4), available['A'].Advance)
	clear(fonts)
	assert.NotEmpty(t, layouts[0].Glyphs)
	// Complete glyph coverage with invalid layout options is not a missing font.
	invalid := textRequest("A", "good")
	invalid.Options.TextSize = math.NaN()
	layouts, missing, err = PrepareTextLayouts(slices.All([]TextRequest{invalid}), map[string]map[uint32]glyph.Glyph{"good": available})
	require.NoError(t, err)
	assert.Empty(t, layouts)
	assert.Empty(t, missing)
}

func TestTextCompleteOriginalTextPolicy(t *testing.T) {
	font := textFont()
	for _, text := range []string{"A", "A\r\nA", " A "} {
		assert.True(t, TextComplete(text, font), text)
	}
	for _, text := range []string{"", "B", "\xff", "مرحبا", "e\u0301", strings.Repeat("A", glyph.MaxTextRunes+1)} {
		assert.False(t, TextComplete(text, font), text)
	}
	assert.False(t, TextComplete("A", nil))
	delete(font, ' ')
	assert.False(t, TextComplete(" A ", font), "coverage precedes whitespace trimming")
	assert.True(t, TextComplete("A\n", font))
}

func TestTextLayoutIterationLimitsAndKeys(t *testing.T) {
	font := map[string]map[uint32]glyph.Glyph{"font": textFont()}
	request := textRequest("A", "font")
	visits := 0
	seq := func(count int) iter.Seq2[string, TextRequest] {
		return func(yield func(string, TextRequest) bool) {
			for range count {
				visits++
				if !yield("same-key", TextRequest{}) {
					return
				}
			}
		}
	}
	layouts, missing, err := PrepareTextLayouts(seq(glyph.MaxPreparedLayouts), font)
	require.NoError(t, err)
	assert.NotNil(t, layouts)
	assert.Empty(t, layouts)
	assert.Empty(t, missing)
	visits = 0
	layouts, missing, err = PrepareTextLayouts(seq(glyph.MaxPreparedLayouts+10), font)
	assert.ErrorIs(t, err, geometry.ErrGeometryLimit)
	assert.Equal(t, glyph.MaxPreparedLayouts+1, visits)
	assert.Nil(t, layouts)
	assert.Nil(t, missing)
	duplicate := func(yield func(string, TextRequest) bool) {
		if !yield("label", request) {
			return
		}
		request.Options.TextSize = 12
		if !yield("label", request) {
			return
		}
		yield("label", textRequest("missing", "absent"))
	}
	layouts, missing, err = PrepareTextLayouts(duplicate, font)
	require.NoError(t, err)
	require.Len(t, layouts, 1)
	assert.Equal(t, 0.5, layouts["label"].Scale)
	assert.Equal(t, []string{"absent"}, missing)
	layouts, missing, err = PrepareTextLayouts[string](nil, nil)
	require.NoError(t, err)
	assert.NotNil(t, layouts)
	assert.NotNil(t, missing)
}

func TestDecodeFontRangeSet(t *testing.T) {
	// A valid empty stack named f, range 0-255. No test-only parser is needed.
	data := []byte{0x0a, 10, 0x0a, 1, 'f', 0x12, 5, '0', '-', '2', '5', '5'}
	input := map[string][]byte{"exact requested identity": data}
	fonts, err := DecodeFontRanges(input, 0)
	require.NoError(t, err)
	require.Contains(t, fonts, "exact requested identity")
	assert.NotNil(t, fonts["exact requested identity"])
	clear(input)
	clear(data)
	assert.Len(t, fonts, 1)
	fonts, err = DecodeFontRanges(map[string][]byte{"bad": {0xff}}, 0)
	require.ErrorContains(t, err, `font "bad"`)
	assert.Nil(t, fonts)
	fonts, err = DecodeFontRanges(nil, 0)
	require.NoError(t, err)
	assert.NotNil(t, fonts)
}

func TestPinnedFontRangeSetHeadless(t *testing.T) {
	dir := os.Getenv("WHEREAMI_VECTOR_GLYPH_FIXTURE_DIR")
	if dir == "" {
		t.Skip("set WHEREAMI_VECTOR_GLYPH_FIXTURE_DIR")
	}
	ranges := make(map[string][]byte)
	for _, face := range []string{"Regular", "Bold", "Italic"} {
		data, err := os.ReadFile(filepath.Join(dir, "Noto%20Sans%20"+face+".pbf"))
		require.NoError(t, err)
		ranges["Noto Sans "+face] = data
	}
	fonts, err := DecodeFontRanges(ranges, 0)
	require.NoError(t, err)
	for font, data := range ranges {
		want, err := glyph.DecodeRange(data, font, 0)
		require.NoError(t, err)
		assert.Equal(t, want.Glyphs, fonts[font])
		clear(data)
		assert.Equal(t, want.Glyphs, fonts[font])
	}
}

// Identical requests share one layout; any difference lays the text out again.
func TestPrepareTextLayoutsSharesIdenticalRequests(t *testing.T) {
	fonts := map[string]map[uint32]glyph.Glyph{"good": textFont()}
	larger := textRequest("A", "good")
	larger.Options.TextSize = 30
	requests := []TextRequest{textRequest("A", "good"), textRequest("A A", "good"), textRequest("A", "good"), larger, textRequest("B", "good"), textRequest("B", "good")}
	layouts, missing, err := PrepareTextLayouts(slices.All(requests), fonts)
	require.NoError(t, err)
	assert.Equal(t, []string{"good"}, missing)
	require.Len(t, layouts, 4)
	assert.Same(t, layouts[0], layouts[2])
	assert.NotSame(t, layouts[0], layouts[1])
	assert.NotSame(t, layouts[0], layouts[3])
	for i, request := range requests[:4] {
		want, ok := glyph.LayoutText(request.Text, request.FontStack, fonts["good"], request.Options)
		require.True(t, ok)
		assert.Equal(t, want, layouts[i].TextLayout, i)
	}
}
