package glyph

import (
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func layoutGlyphs() map[uint32]Glyph {
	a, b := bitmapGlyph(6, 8, 100), bitmapGlyph(7, 8, 120)
	a.ID, a.Top, a.Advance = 'A', -8, 10
	b.ID, b.Left, b.Top, b.Advance = 'B', 1, -8, 12
	return map[uint32]Glyph{'A': a, 'B': b, ' ': {ID: ' ', Advance: 5}}
}

func TestLayoutTextMetricsHaloAndOwnership(t *testing.T) {
	glyphs := layoutGlyphs()
	a := glyphs['A']
	layout, ok := LayoutText("AB", "font", glyphs, LayoutOptions{
		TextSize: 24, LineHeight: 1, MaximumWidth: 10, Anchor: "center", Justify: "auto", HaloWidth: 2, HaloBlur: 1,
	})
	require.True(t, ok)
	assert.Equal(t, 1.0, layout.Scale)
	require.Len(t, layout.Glyphs, 2)
	assert.Equal(t, -11.0, layout.Glyphs[0].X)
	assert.Equal(t, -1.0, layout.Glyphs[1].X)
	assert.Equal(t, TextBounds{Left: -14, Top: -12, Right: 10, Bottom: 2}, layout.Bounds)
	assert.Equal(t, Key{FontStack: "font", ID: 'A'}, layout.Glyphs[0].Key)
	assert.Equal(t, a, layout.Glyphs[0].Glyph)
	assert.Same(t, &a.Bitmap[0], &layout.Glyphs[0].Glyph.Bitmap[0])
	clear(glyphs) // Map ownership is separate; only immutable bitmaps are borrowed.
	assert.Equal(t, a, layout.Glyphs[0].Glyph)
	other, ok := LayoutText("AB", "font", layoutGlyphs(), LayoutOptions{TextSize: 24})
	require.True(t, ok)
	other.Glyphs[0].X = 999
	assert.Equal(t, -11.0, layout.Glyphs[0].X)
}

func TestLayoutTextAnchorAndJustification(t *testing.T) {
	for _, tt := range []struct {
		anchor, justify        string
		firstX, shortX, firstY float64
	}{
		{"center", "left", -10, -10, -29},
		{"center", "center", -10, -5, -29},
		{"center", "right", -10, 0, -29},
		{"top-left", "auto", 0, 0, -5},
		{"bottom-right", "auto", -20, -10, -53},
		{"left", "right", 0, 10, -29},
		{"unknown", "unknown", -10, -5, -29},
	} {
		t.Run(tt.anchor+"/"+tt.justify, func(t *testing.T) {
			layout, ok := LayoutText("AA\nA", "font", layoutGlyphs(), LayoutOptions{TextSize: 24, LineHeight: 1, Anchor: tt.anchor, Justify: tt.justify})
			require.True(t, ok)
			require.Len(t, layout.Glyphs, 3)
			assert.Equal(t, tt.firstX, layout.Glyphs[0].X)
			assert.Equal(t, tt.shortX, layout.Glyphs[2].X)
			assert.Equal(t, tt.firstY, layout.Glyphs[0].Y)
			assert.Equal(t, tt.firstY+24, layout.Glyphs[2].Y)
		})
	}
	// Legacy anchor matching uses substrings, with left/top taking precedence.
	h, v := anchorAlignment("bottom-top-right-left")
	assert.Equal(t, 0.0, h)
	assert.Equal(t, 0.0, v)
}

func TestLayoutTextSpacingScaleHaloAndEmptyInk(t *testing.T) {
	options := LayoutOptions{TextSize: 12, LetterSpacing: 0.5, HaloWidth: 99, HaloBlur: 2}
	layout, ok := LayoutText("AA", "font", layoutGlyphs(), options)
	require.True(t, ok)
	assert.Equal(t, 0.5, layout.Scale)
	assert.Equal(t, 22.0, layout.Glyphs[1].X-layout.Glyphs[0].X)
	plain, ok := LayoutText("AA", "font", layoutGlyphs(), LayoutOptions{TextSize: 12, LetterSpacing: 0.5, HaloWidth: -1, HaloBlur: -1})
	require.True(t, ok)
	assert.Equal(t, plain.Bounds.Left-3.5, layout.Bounds.Left) // 3 texels * .5 scale + blur 2.
	assert.Equal(t, plain.Bounds.Right+3.5, layout.Bounds.Right)
	assert.Equal(t, plain.Bounds.Top-3.5, layout.Bounds.Top)
	assert.Equal(t, plain.Bounds.Bottom+3.5, layout.Bounds.Bottom)
	defaults, ok := LayoutText("A\nA", "font", layoutGlyphs(), LayoutOptions{TextSize: 24, LineHeight: -1})
	require.True(t, ok)
	assert.InDelta(t, 28.8, defaults.Glyphs[1].Y-defaults.Glyphs[0].Y, 1e-12)
	empty, ok := LayoutText(".", "font", map[uint32]Glyph{'.': {Advance: 5}}, LayoutOptions{TextSize: 24, LineHeight: 1})
	require.True(t, ok)
	assert.Equal(t, TextBounds{Left: -2.5, Top: -12, Right: 2.5, Bottom: 12}, empty.Bounds)
	// Missing space is harmless when wrapping removes it, but fails atomically
	// if it would remain inside a rendered line.
	glyphs := layoutGlyphs()
	delete(glyphs, ' ')
	_, ok = LayoutText("A A", "font", glyphs, LayoutOptions{TextSize: 24, MaximumWidth: 1})
	assert.True(t, ok)
	failed, ok := LayoutText("A A", "font", glyphs, LayoutOptions{TextSize: 24})
	assert.False(t, ok)
	assert.Equal(t, TextLayout{}, failed)
}

func TestLayoutTextRejectsInvalidInputsAndOverflow(t *testing.T) {
	for _, text := range []string{"", " \t\r\n", "AC", "\xff", strings.Repeat("A", MaxTextRunes+1), strings.Repeat("A", MaxTextBytes+1)} {
		layout, ok := LayoutText(text, "font", layoutGlyphs(), LayoutOptions{TextSize: 24})
		assert.False(t, ok)
		assert.Equal(t, TextLayout{}, layout)
	}
	for _, size := range []float64{0, -1} {
		layout, ok := LayoutText("A", "font", layoutGlyphs(), LayoutOptions{TextSize: size})
		assert.False(t, ok)
		assert.Equal(t, TextLayout{}, layout)
	}
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		for field := range 6 {
			options := LayoutOptions{TextSize: 24}
			fields := []*float64{&options.TextSize, &options.LetterSpacing, &options.MaximumWidth, &options.LineHeight, &options.HaloWidth, &options.HaloBlur}
			*fields[field] = value
			layout, ok := LayoutText("A", "font", layoutGlyphs(), options)
			assert.False(t, ok)
			assert.Equal(t, TextLayout{}, layout)
		}
	}
	for _, tt := range []struct {
		text    string
		options LayoutOptions
	}{
		{"A", LayoutOptions{TextSize: 24, MaximumWidth: math.MaxFloat64}},
		{"AAAA", LayoutOptions{TextSize: 24, LetterSpacing: math.MaxFloat64 / 48}},
		{"A\nA\nA\nA", LayoutOptions{TextSize: 24, LineHeight: math.MaxFloat64 / 48}},
		{"A", LayoutOptions{TextSize: math.MaxFloat64, HaloBlur: math.MaxFloat64}},
	} {
		layout, ok := LayoutText(tt.text, "font", layoutGlyphs(), tt.options)
		assert.False(t, ok)
		assert.Equal(t, TextLayout{}, layout)
	}
	for _, text := range []string{strings.Repeat("A", MaxTextRunes), strings.Repeat("界", MaxTextRunes)} {
		glyphs := layoutGlyphs()
		glyphs['界'] = glyphs['A']
		layout, ok := LayoutText(text, "font", glyphs, LayoutOptions{TextSize: 24})
		require.True(t, ok)
		assert.Len(t, layout.Glyphs, MaxTextRunes)
	}
}

func TestBreakLinesPreservesWrappingRules(t *testing.T) {
	glyphs := map[uint32]Glyph{'A': {Advance: 10}, 'B': {Advance: 10}, ' ': {Advance: 5}}
	for _, tt := range []struct {
		text           string
		spacing, width float64
		want           [][]rune
	}{
		{"A B", 0, 20, [][]rune{{'A'}, {'B'}}},
		{"AB", 0, 15, [][]rune{{'A'}, {'B'}}},
		{"A B", 2, 29, [][]rune{{'A', ' ', 'B'}}},
		{"A B", 2, 28, [][]rune{{'A'}, {'B'}}},
		{"A BBB", 0, 20, [][]rune{{'A'}, {'B', 'B'}, {'B'}}},
		{"A B", 0, 0, [][]rune{{'A', ' ', 'B'}}},
		{"A B", -2, 21, [][]rune{{'A', ' ', 'B'}}},
		{" A\t\u3000B \r\n\nA", 0, 100, [][]rune{{'A', ' ', 'B'}, nil, {'A'}}},
		{"\n", 0, 10, [][]rune{nil, nil}},
		{"AC", 0, 10, [][]rune{{'A'}, {'C'}}},
	} {
		assert.Equal(t, tt.want, breakLines(tt.text, glyphs, tt.spacing, tt.width), tt.text)
	}
	assert.Equal(t, 0.0, measureLine(nil, glyphs, 5))
	assert.True(t, math.IsInf(measureLine([]rune("AC"), glyphs, 0), 1))
}

func FuzzLayoutText(f *testing.F) {
	f.Add("A B", 24.0, 0.0, 1.0)
	f.Add("\nAAAA", 12.0, 0.2, 3.0)
	f.Fuzz(func(t *testing.T, text string, size, spacing, width float64) {
		layout, ok := LayoutText(text, "font", layoutGlyphs(), LayoutOptions{TextSize: size, LetterSpacing: spacing, MaximumWidth: width})
		if !ok {
			require.Equal(t, TextLayout{}, layout)
			return
		}
		require.LessOrEqual(t, len(layout.Glyphs), MaxTextRunes)
		for _, positioned := range layout.Glyphs {
			require.True(t, finite(positioned.X) && finite(positioned.Y))
		}
		require.True(t, finite(layout.Bounds.Left) && finite(layout.Bounds.Top) && finite(layout.Bounds.Right) && finite(layout.Bounds.Bottom))
	})
}
