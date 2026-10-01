package placement

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rubiojr/whereami/pkg/vecmap/glyph"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
)

// ExpandTokens substitutes at most 256 {property} tokens with the existing style
// primitive string conversion. Substituted braces are not recursively expanded.
// Properties are borrowed synchronously and normally contain MVT primitives.
// Oversized/invalid text returns empty; an unmatched brace remains literal.
func ExpandTokens(text string, properties style.Properties) string {
	const maximumExpansions = 256
	if len(text) > glyph.MaxTextBytes || !utf8.ValidString(text) {
		return ""
	}
	offset := 0
	for range maximumExpansions {
		startOffset := strings.IndexByte(text[offset:], '{')
		if startOffset < 0 {
			return BoundedText(text)
		}
		start := offset + startOffset
		endOffset := strings.IndexByte(text[start+1:], '}')
		if endOffset < 0 {
			return BoundedText(text)
		}
		end := start + endOffset + 1
		name := text[start+1 : end]
		var value any
		if properties != nil {
			value, _ = properties.Get(name)
		}
		replacement := style.String(value)
		retainedBytes := len(text) - (end + 1 - start)
		if len(replacement) > glyph.MaxTextBytes-retainedBytes {
			return ""
		}
		text = text[:start] + replacement + text[end+1:]
		offset = start + len(replacement)
	}
	return BoundedText(text)
}

// BoundedText preserves the existing 4096-byte/256-rune UTF-8 text ceiling.
func BoundedText(text string) string {
	if len(text) > glyph.MaxTextBytes || !utf8.ValidString(text) || utf8.RuneCountInString(text) > glyph.MaxTextRunes {
		return ""
	}
	return text
}

// NormalizeText maps CR/LF to LF (coalescing CRLF) and other Unicode whitespace
// to ASCII spaces. It does not collapse adjacent spaces. Callers bound its input;
// PrepareSymbols calls it only after bounded token expansion.
func NormalizeText(text string) string {
	var normalized strings.Builder
	normalized.Grow(len(text))
	previousCarriageReturn := false
	for _, codePoint := range text {
		if codePoint == '\n' && previousCarriageReturn {
			previousCarriageReturn = false
			continue
		}
		previousCarriageReturn = codePoint == '\r'
		switch {
		case codePoint == '\r' || codePoint == '\n':
			normalized.WriteByte('\n')
		case unicode.IsSpace(codePoint):
			normalized.WriteByte(' ')
		default:
			normalized.WriteRune(codePoint)
		}
	}
	return normalized.String()
}
