package glyph

import (
	"unicode"
	"unicode/utf8"
)

// LegacyFallbackEligible preserves the existing native text fallback gate for
// adapters and offline collision compatibility. It is not a shaping capability
// guarantee. Thaana is excluded because the legacy Qt path repeatedly searches
// installed fonts without finding OpenType shaping support.
func LegacyFallbackEligible(text string) bool {
	if text == "" || !utf8.ValidString(text) || utf8.RuneCountInString(text) > MaxTextRunes {
		return false
	}
	for _, codePoint := range text {
		if unicode.Is(unicode.Thaana, codePoint) {
			return false
		}
	}
	return true
}
