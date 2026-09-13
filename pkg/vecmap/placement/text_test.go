package placement

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/rubiojr/whereami/pkg/vecmap/glyph"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandTokensPreservesBoundedNonrecursiveBehavior(t *testing.T) {
	properties := map[string]any{"name": "Madrid", "n": 12.5, "b": true, "nested": "{name}", "": "empty"}
	for _, tt := range []struct{ in, want string }{
		{"{name}/{missing}/{n}/{b}", "Madrid//12.5/true"}, {"{}", "empty"},
		{"{nested}", "{name}"}, {"{name", "{name"}, {"literal", "literal"},
		{strings.Repeat("{missing}", 257), "{missing}"},
		{strings.Repeat("A", 257), ""}, {strings.Repeat("A", 4097), ""}, {"\xff", ""},
	} {
		assert.Equal(t, tt.want, ExpandTokens(tt.in, properties))
	}
	properties["big"] = strings.Repeat("A", 4097)
	assert.Empty(t, ExpandTokens("{big}", properties))
	properties["big"] = strings.Repeat("界", 257)
	assert.Empty(t, ExpandTokens("{big}", properties))
	properties["bad"] = "\xff"
	assert.Empty(t, ExpandTokens("{bad}", properties))
	assert.Equal(t, strings.Repeat("界", 256), BoundedText(strings.Repeat("界", 256)))
	assert.Empty(t, BoundedText(strings.Repeat("A", 4097)))
	assert.Empty(t, BoundedText("\xff"))
}

func TestNormalizeText(t *testing.T) {
	assert.Equal(t, " Madrid  東京\nSeoul\n\nA ", NormalizeText(" Madrid\t\u3000東京\r\nSeoul\r\rA\t"))
	assert.Equal(t, "\n\n", NormalizeText("\n\n"))
	assert.Empty(t, NormalizeText(""))
}

func FuzzSymbolText(f *testing.F) {
	f.Add("{name}", "Madrid")
	f.Add("{name}\r\n{name}", "{nested}")
	f.Fuzz(func(t *testing.T, text, value string) {
		expanded := ExpandTokens(text, map[string]any{"name": value})
		for _, result := range []string{expanded, BoundedText(NormalizeText(expanded))} {
			require.LessOrEqual(t, len(result), glyph.MaxTextBytes)
			require.True(t, utf8.ValidString(result))
			require.LessOrEqual(t, utf8.RuneCountInString(result), glyph.MaxTextRunes)
		}
	})
}
