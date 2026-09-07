package quick

/*
#include "textnode.h"
*/
import "C"

import (
	"math"
	"runtime"
	"strings"

	qt "github.com/mappu/miqt/qt6"
)

// TextAnchor identifies which side of a text box is attached to its origin.
type TextAnchor int

const (
	TextAnchorLeading  TextAnchor = -1
	TextAnchorCenter   TextAnchor = 0
	TextAnchorTrailing TextAnchor = 1
)

// NewQSGTextNode lays out text in Go through public Qt bindings and creates the
// final public scene-graph node through the minimal Qt Quick bridge.
func NewQSGTextNode(
	item *QQuickItem,
	text, family string,
	pixelSize, letterSpacing, lineHeight, maximumWidth float32,
	color, haloColor [4]int,
	haloWidth float32,
	horizontalAnchor, verticalAnchor TextAnchor,
) *QSGNode {
	if item == nil || text == "" || family == "" || pixelSize <= 0 {
		return nil
	}
	familyName, weight, italic := textFont(family)
	font := qt.NewQFont3(textFontFamilies(familyName))
	if font == nil {
		return nil
	}
	defer font.Delete()
	font.SetWeight(weight)
	font.SetItalic(italic)
	font.SetPixelSize(int(math.Round(float64(pixelSize))))
	font.SetLetterSpacing(qt.QFont__AbsoluteSpacing, float64(letterSpacing))

	layout := qt.NewQTextLayout3(text, font)
	if layout == nil {
		return nil
	}
	defer layout.Delete()
	option := qt.NewQTextOption2(qt.AlignHCenter)
	option.SetWrapMode(qt.QTextOption__WrapAtWordBoundaryOrAnywhere)
	layout.SetTextOption(option)
	option.Delete()

	layout.BeginLayout()
	y := float64(0)
	for {
		line := layout.CreateLine()
		if line == nil {
			break
		}
		if !line.IsValid() {
			runtime.SetFinalizer(line, nil)
			line.Delete()
			break
		}
		width := float64(maximumWidth)
		if width <= 0 {
			width = 100000
		}
		line.SetLineWidth(width)
		position := qt.NewQPointF3(0, y)
		line.SetPosition(position)
		position.Delete()
		if lineHeight > 0 {
			y += float64(pixelSize * lineHeight)
		} else {
			y += line.Height()
		}
		runtime.SetFinalizer(line, nil)
		line.Delete()
	}
	layout.EndLayout()

	bounds := layout.BoundingRect()
	x := -bounds.Width() / 2
	if horizontalAnchor < 0 {
		x = 0
	} else if horizontalAnchor > 0 {
		x = -bounds.Width()
	}
	y = -bounds.Height() / 2
	if verticalAnchor < 0 {
		y = 0
	} else if verticalAnchor > 0 {
		y = -bounds.Height()
	}
	runtime.SetFinalizer(bounds, nil)
	bounds.Delete()

	textColor := qt.NewQColor11(color[0], color[1], color[2], color[3])
	if textColor == nil {
		return nil
	}
	defer textColor.Delete()
	var outlineColor *qt.QColor
	if haloWidth > 0 && haloColor[3] > 0 {
		outlineColor = qt.NewQColor11(haloColor[0], haloColor[1], haloColor[2], haloColor[3])
		if outlineColor != nil {
			defer outlineColor.Delete()
		}
	}
	var outlinePointer *C.QColor
	if outlineColor != nil {
		outlinePointer = (*C.QColor)(outlineColor.UnsafePointer())
	}
	node := C.QQuickItem_newTextNode(
		(*C.QQuickItem)(item.UnsafePointer()),
		(*C.QTextLayout)(layout.UnsafePointer()),
		C.float(x),
		C.float(y),
		(*C.QColor)(textColor.UnsafePointer()),
		outlinePointer,
	)
	runtime.KeepAlive(item)
	runtime.KeepAlive(layout)
	runtime.KeepAlive(textColor)
	runtime.KeepAlive(outlineColor)
	return newQSGNode(node)
}

func textFontFamilies(family string) []string {
	if family == "KlokanTech Noto Sans" {
		return []string{family, "Noto Sans"}
	}
	return []string{family}
}

func textFont(family string) (string, qt.QFont__Weight, bool) {
	suffixes := []struct {
		suffix string
		weight qt.QFont__Weight
		italic bool
	}{
		{suffix: " Bold", weight: qt.QFont__Bold},
		{suffix: " Medium", weight: qt.QFont__Medium},
		{suffix: " Italic", weight: qt.QFont__Normal, italic: true},
		{suffix: " Regular", weight: qt.QFont__Normal},
	}
	for _, suffix := range suffixes {
		if strings.HasSuffix(family, suffix.suffix) {
			return strings.TrimSuffix(family, suffix.suffix), suffix.weight, suffix.italic
		}
	}
	return family, qt.QFont__Normal, false
}
