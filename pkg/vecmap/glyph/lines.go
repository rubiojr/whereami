package glyph

import (
	"math"
	"strings"
)

func breakLines(text string, glyphs map[uint32]Glyph, spacing, maximumWidth float64) [][]rune {
	paragraphs := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	lines := make([][]rune, 0, len(paragraphs))
	for _, paragraph := range paragraphs {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			lines = append(lines, nil)
			continue
		}
		current := make([]rune, 0, len([]rune(paragraph)))
		currentWidth := 0.0
		for _, word := range words {
			wordRunes := []rune(word)
			wordWidth := measureLine(wordRunes, glyphs, spacing)
			if maximumWidth > 0 && wordWidth > maximumWidth {
				if len(current) > 0 {
					lines = append(lines, current)
					current = nil
					currentWidth = 0
				}
				for _, codePoint := range wordRunes {
					glyph, exists := glyphs[uint32(codePoint)]
					glyphWidth := math.Inf(1)
					if exists {
						glyphWidth = float64(glyph.Advance)
					}
					candidateWidth := glyphWidth
					if len(current) > 0 {
						candidateWidth += currentWidth + spacing
					}
					if len(current) > 0 && candidateWidth > maximumWidth {
						lines = append(lines, current)
						current = []rune{codePoint}
						currentWidth = glyphWidth
						continue
					}
					current = append(current, codePoint)
					currentWidth = candidateWidth
				}
				continue
			}
			candidateWidth := wordWidth
			if len(current) > 0 {
				space, exists := glyphs[' ']
				if !exists {
					candidateWidth = math.Inf(1)
				} else {
					candidateWidth += currentWidth + float64(space.Advance) + 2*spacing
				}
			}
			if maximumWidth > 0 && len(current) > 0 && candidateWidth > maximumWidth {
				lines = append(lines, current)
				current = append([]rune(nil), wordRunes...)
				currentWidth = wordWidth
				continue
			}
			if len(current) > 0 {
				current = append(current, ' ')
			}
			current = append(current, wordRunes...)
			currentWidth = candidateWidth
		}
		lines = append(lines, current)
	}
	return lines
}

func measureLine(line []rune, glyphs map[uint32]Glyph, spacing float64) float64 {
	width := 0.0
	for index, codePoint := range line {
		glyph, exists := glyphs[uint32(codePoint)]
		if !exists {
			return math.Inf(1)
		}
		width += float64(glyph.Advance)
		if index+1 < len(line) {
			width += spacing
		}
	}
	return width
}
