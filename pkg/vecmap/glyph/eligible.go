package glyph

import (
	"unicode"
	"unicode/utf8"
)

// TextEligible is the existing SDF metric-layout script subset, not a shaping or
// font-availability guarantee. Combining marks and scripts requiring contextual
// shaping/bidi are excluded; callers choose their toolkit-specific fallback.
func TextEligible(text string) bool {
	if text == "" || len(text) > MaxTextBytes || !utf8.ValidString(text) {
		return false
	}
	runeCount := 0
	for _, codePoint := range text {
		runeCount++
		if runeCount > MaxTextRunes {
			return false
		}
		if codePoint > rune(MaxCodePoint) || unicode.Is(unicode.M, codePoint) {
			return false
		}
		if codePoint == '\n' || codePoint == '\r' || unicode.IsSpace(codePoint) || unicode.Is(unicode.Common, codePoint) {
			continue
		}
		if !unicode.In(codePoint, unicode.Latin, unicode.Greek, unicode.Cyrillic, unicode.Armenian, unicode.Georgian,
			unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul, unicode.Bopomofo) {
			return false
		}
	}
	return true
}
