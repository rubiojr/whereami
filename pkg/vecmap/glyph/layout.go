package glyph

import (
	"math"
	"strings"
	"unicode/utf8"
)

const (
	EmSize          = 24.0
	DefaultBaseline = -17.0
	MaxTextBytes    = 4096
	MaxTextRunes    = 256
)

// LayoutOptions is evaluated paint/layout for the existing glyph-metric layout
// algorithm. TextSize and halo values use logical pixels; LetterSpacing,
// MaximumWidth and LineHeight use ems. Nonpositive MaximumWidth disables wrapping;
// nonpositive LineHeight uses 1.2 ems. Anchor/Justify retain Liberty's defaults.
type LayoutOptions struct {
	TextSize, LetterSpacing, MaximumWidth, LineHeight float64
	Anchor, Justify                                   string
	HaloWidth, HaloBlur                               float64
}

// PositionedGlyph borrows the glyph bitmap and stores its origin in unscaled
// 24-unit em coordinates. Glyph metrics are copied, not linked to the input map.
type PositionedGlyph struct {
	Key   Key
	Glyph Glyph
	X, Y  float64
}

// TextBounds is the logical-pixel ink/halo rectangle relative to the label anchor.
type TextBounds struct{ Left, Top, Right, Bottom float64 }

// TextLayout owns its positioned slice and borrows immutable glyph bitmaps.
// Scale converts positioned coordinates to logical pixels. Publication is
// immutable; atlas-dependent quads and collision/placement are caller-owned.
type TextLayout struct {
	Glyphs []PositionedGlyph
	Bounds TextBounds
	Scale  float64
}

// LayoutText reuses the existing line breaking, metric layout and halo bounds.
// It does not perform OpenType shaping, bidi, kerning or script eligibility checks.
// Callers choose eligible text and provide validated glyph metrics (normally from
// DecodeRange). Missing required glyphs, empty text, invalid UTF-8, excessive text
// and invalid/nonfinite layout arithmetic return a zero layout and false.
// The input map is read synchronously and is not retained or modified.
func LayoutText(text, fontStack string, glyphs map[uint32]Glyph, options LayoutOptions) (TextLayout, bool) {
	if !validLayoutInput(text, options) {
		return TextLayout{}, false
	}
	spacing := options.LetterSpacing * EmSize
	maximumWidth := options.MaximumWidth * EmSize
	lines := breakLines(text, glyphs, spacing, maximumWidth)
	lineHeight := options.LineHeight * EmSize
	if lineHeight <= 0 {
		lineHeight = 1.2 * EmSize
	}
	horizontalAlign, verticalAlign := anchorAlignment(options.Anchor)
	justify := textJustification(options.Justify, horizontalAlign)
	positioned := make([]PositionedGlyph, 0, len([]rune(text)))
	maximumLineWidth := 0.0
	for lineIndex, line := range lines {
		lineWidth := measureLine(line, glyphs, spacing)
		maximumLineWidth = max(maximumLineWidth, lineWidth)
		x := -justify * lineWidth
		y := float64(lineIndex)*lineHeight + DefaultBaseline
		for index, codePoint := range line {
			glyph, exists := glyphs[uint32(codePoint)]
			if !exists {
				return TextLayout{}, false
			}
			positioned = append(positioned, PositionedGlyph{
				Key: Key{FontStack: fontStack, ID: uint32(codePoint)}, Glyph: glyph, X: x, Y: y,
			})
			x += float64(glyph.Advance)
			if index+1 < len(line) {
				x += spacing
			}
		}
	}
	if len(positioned) == 0 {
		return TextLayout{}, false
	}
	blockHeight := float64(len(lines)) * lineHeight
	shiftX := (justify - horizontalAlign) * maximumLineWidth
	shiftY := -verticalAlign*blockHeight + 0.5*lineHeight
	for index := range positioned {
		positioned[index].X += shiftX
		positioned[index].Y += shiftY
		if !finite(positioned[index].X) || !finite(positioned[index].Y) {
			return TextLayout{}, false
		}
	}
	scale := options.TextSize / EmSize
	renderedHaloWidth := min(max(0, options.HaloWidth), PBFBorder*scale)
	haloExtent := renderedHaloWidth + max(0, options.HaloBlur)
	bounds := positionedGlyphBounds(positioned, scale)
	if math.IsInf(bounds.Left, 1) {
		bounds = TextBounds{
			Left:   -horizontalAlign * maximumLineWidth * scale,
			Top:    -verticalAlign * blockHeight * scale,
			Right:  (1 - horizontalAlign) * maximumLineWidth * scale,
			Bottom: (1 - verticalAlign) * blockHeight * scale,
		}
	}
	bounds.Left -= haloExtent
	bounds.Top -= haloExtent
	bounds.Right += haloExtent
	bounds.Bottom += haloExtent
	if !finite(bounds.Left) || !finite(bounds.Top) || !finite(bounds.Right) || !finite(bounds.Bottom) {
		return TextLayout{}, false
	}
	return TextLayout{Glyphs: positioned, Scale: scale, Bounds: bounds}, true
}

func validLayoutInput(text string, options LayoutOptions) bool {
	if text == "" || len(text) > MaxTextBytes || !utf8.ValidString(text) || utf8.RuneCountInString(text) > MaxTextRunes || options.TextSize <= 0 {
		return false
	}
	for _, value := range [...]float64{options.TextSize, options.LetterSpacing * EmSize, options.MaximumWidth * EmSize, options.LineHeight * EmSize, options.HaloWidth, options.HaloBlur} {
		if !finite(value) {
			return false
		}
	}
	return true
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func positionedGlyphBounds(glyphs []PositionedGlyph, scale float64) TextBounds {
	bounds := TextBounds{Left: math.Inf(1), Top: math.Inf(1), Right: math.Inf(-1), Bottom: math.Inf(-1)}
	for _, positioned := range glyphs {
		if positioned.Glyph.Width == 0 || positioned.Glyph.Height == 0 {
			continue
		}
		left := (positioned.X + float64(positioned.Glyph.Left)) * scale
		top := (positioned.Y - float64(positioned.Glyph.Top)) * scale
		bounds.Left = min(bounds.Left, left)
		bounds.Top = min(bounds.Top, top)
		bounds.Right = max(bounds.Right, left+float64(positioned.Glyph.Width)*scale)
		bounds.Bottom = max(bounds.Bottom, top+float64(positioned.Glyph.Height)*scale)
	}
	return bounds
}

func anchorAlignment(anchor string) (float64, float64) {
	horizontal, vertical := 0.5, 0.5
	if strings.Contains(anchor, "left") {
		horizontal = 0
	} else if strings.Contains(anchor, "right") {
		horizontal = 1
	}
	if strings.Contains(anchor, "top") {
		vertical = 0
	} else if strings.Contains(anchor, "bottom") {
		vertical = 1
	}
	return horizontal, vertical
}

func textJustification(justify string, horizontalAlign float64) float64 {
	switch justify {
	case "left":
		return 0
	case "right":
		return 1
	case "center":
		return 0.5
	default:
		return horizontalAlign
	}
}
