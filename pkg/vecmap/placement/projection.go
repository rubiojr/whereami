package placement

import (
	"math"
	"strings"
	"unicode/utf8"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
)

// SpriteMetrics describes the caller's available sprite, without its pixel data
// or native texture. Dimensions are image pixels; PixelRatio converts to logical
// pixels. A nil sprite or nonpositive/NaN pixel ratio means unavailable.
type SpriteMetrics struct {
	Width, Height int
	PixelRatio    float64
}

// ProjectionContext supplies synchronous readiness/metric snapshots. TextReady
// is caller policy; a bounds pointer alone does not mark text ready. TextBounds
// normally comes from glyph layout, already in logical pixels including halo.
// Ready text without bounds uses the existing fallback size estimate.
type ProjectionContext struct {
	Transform     view.Affine
	Width, Height float64
	TextReady     bool
	TextBounds    *Box
	Sprite        *SpriteMetrics
}

type ProjectedSymbol struct{ Text, Icon CollisionPart }

// ProjectSymbol reuses the existing projected text/icon box arithmetic. It neither
// fetches resources nor shapes text. Inputs are evaluated bounded symbols and
// caller-owned affine/metric snapshots; no pointers or strings are retained.
// Text/icon offsets and sizes stay in logical pixels during map-camera scaling.
// SelectSymbols provides finite-input/work validation for visible output parts.
func ProjectSymbol(symbol *Symbol, context ProjectionContext) ProjectedSymbol {
	if symbol == nil {
		return ProjectedSymbol{}
	}
	anchor := geometry.Point(context.Transform.MapPoint(view.ScreenPoint(symbol.Anchor)))
	return ProjectedSymbol{Text: projectText(symbol, context, anchor), Icon: projectIcon(symbol, context, anchor)}
}

func projectText(symbol *Symbol, context ProjectionContext, anchor geometry.Point) CollisionPart {
	part := CollisionPart{Present: symbol.Text != "" && symbol.TextColor.Alpha > 0,
		AllowsOverlap: symbol.TextAllowsOverlap, Optional: symbol.TextOptional}
	if !part.Present || !context.TextReady {
		return part
	}
	var bounds Box
	if context.TextBounds != nil {
		bounds = *context.TextBounds
	} else {
		bounds = fallbackTextBounds(symbol)
	}
	part.Box = RotatedBox(
		geometry.Point{X: anchor.X + symbol.TextOffset.X*symbol.TextSize, Y: anchor.Y + symbol.TextOffset.Y*symbol.TextSize},
		bounds, ScreenSymbolAngle(context.Transform, symbol.LineAngle, symbol.TextRotate, symbol.ViewportAligned), symbol.TextPadding,
	)
	part.Visible = part.Box.Visible(context.Width, context.Height)
	return part
}

func fallbackTextBounds(symbol *Symbol) Box {
	// Keep the count in floating point. Tiny positive maximum widths can make
	// the estimate exceed int, even for bounded text, especially on 386.
	lineCount := float64(max(1, strings.Count(symbol.Text, "\n")+1))
	characterCount := max(1, utf8.RuneCountInString(symbol.Text))
	textWidth := float64(characterCount) * symbol.TextSize * (0.58 + symbol.LetterSpacing)
	maximumWidth := symbol.MaximumWidth * symbol.TextSize
	if maximumWidth > 0 && textWidth > maximumWidth {
		lineCount = max(lineCount, math.Ceil(textWidth/maximumWidth))
		textWidth = maximumWidth
	}
	haloExtent := symbol.HaloWidth + symbol.HaloBlur
	textWidth += 2 * haloExtent
	textHeight := lineCount*symbol.TextSize*symbol.LineHeight + 2*haloExtent
	x, y := AnchoredOrigin(symbol.TextAnchor, textWidth, textHeight)
	return Box{Left: x, Top: y, Right: x + textWidth, Bottom: y + textHeight}
}

func projectIcon(symbol *Symbol, context ProjectionContext, anchor geometry.Point) CollisionPart {
	part := CollisionPart{AllowsOverlap: symbol.IconAllowsOverlap, Optional: symbol.IconOptional}
	sprite := context.Sprite
	if symbol.IconName == "" || sprite == nil || !(sprite.PixelRatio > 0) {
		return part
	}
	part.Present = true
	width := float64(sprite.Width) / sprite.PixelRatio * symbol.IconSize
	height := float64(sprite.Height) / sprite.PixelRatio * symbol.IconSize
	x, y := AnchoredOrigin(symbol.IconAnchor, width, height)
	part.Box = RotatedBox(
		geometry.Point{X: anchor.X + symbol.IconOffset.X*symbol.IconSize, Y: anchor.Y + symbol.IconOffset.Y*symbol.IconSize},
		Box{Left: x, Top: y, Right: x + width, Bottom: y + height},
		ScreenSymbolAngle(context.Transform, symbol.IconLineAngle, symbol.IconRotate, symbol.IconViewportAligned), symbol.IconPadding,
	)
	part.Visible = part.Box.Visible(context.Width, context.Height)
	return part
}

// RenderedSymbolAngle omits the source line direction for viewport-aligned paint.
func RenderedSymbolAngle(lineAngle, rotate float64, viewportAligned bool) float64 {
	if viewportAligned {
		return rotate
	}
	return lineAngle + rotate
}

// ScreenSymbolAngle applies the affine direction transform only for map alignment.
func ScreenSymbolAngle(transform view.Affine, lineAngle, rotate float64, viewportAligned bool) float64 {
	localAngle := RenderedSymbolAngle(lineAngle, rotate, viewportAligned)
	if viewportAligned {
		return localAngle
	}
	directionX := transform.M11*math.Cos(localAngle) + transform.M12*math.Sin(localAngle)
	directionY := transform.M21*math.Cos(localAngle) + transform.M22*math.Sin(localAngle)
	return math.Atan2(directionY, directionX)
}

// RotatedBox transforms all four corners, then applies the original signed padding.
func RotatedBox(origin geometry.Point, box Box, angle, padding float64) Box {
	cosAngle, sinAngle := math.Cos(angle), math.Sin(angle)
	result := Box{Left: math.Inf(1), Top: math.Inf(1), Right: math.Inf(-1), Bottom: math.Inf(-1)}
	for _, point := range [...]geometry.Point{
		{X: box.Left, Y: box.Top}, {X: box.Right, Y: box.Top}, {X: box.Right, Y: box.Bottom}, {X: box.Left, Y: box.Bottom},
	} {
		x := origin.X + point.X*cosAngle - point.Y*sinAngle
		y := origin.Y + point.X*sinAngle + point.Y*cosAngle
		result.Left = min(result.Left, x)
		result.Top = min(result.Top, y)
		result.Right = max(result.Right, x)
		result.Bottom = max(result.Bottom, y)
	}
	return Box{Left: result.Left - padding, Top: result.Top - padding, Right: result.Right + padding, Bottom: result.Bottom + padding}
}

// Visible retains the inclusive viewport-edge test, independently of collision.
func (box Box) Visible(width, height float64) bool {
	return box.Right >= 0 && box.Bottom >= 0 && box.Left <= width && box.Top <= height
}

// AnchoredOrigin retains substring matching and the exact center/edge arithmetic.
func AnchoredOrigin(anchor string, width, height float64) (float64, float64) {
	x, y := -width/2, -height/2
	if strings.Contains(anchor, "left") {
		x = 0
	} else if strings.Contains(anchor, "right") {
		x = -width
	}
	if strings.Contains(anchor, "top") {
		y = 0
	} else if strings.Contains(anchor, "bottom") {
		y = -height
	}
	return x, y
}
