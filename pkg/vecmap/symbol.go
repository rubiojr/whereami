package vecmap

import (
	"errors"

	"github.com/rubiojr/whereami/pkg/vecmap/glyph"
	"github.com/rubiojr/whereami/pkg/vecmap/placement"
)

const (
	maxTileSymbols         = placement.MaxSymbols
	maximumSymbolTextBytes = glyph.MaxTextBytes
	maximumSymbolTextRunes = glyph.MaxTextRunes
)

type libertySymbolCandidate struct {
	order               int
	layerID             string
	anchor              roadPoint
	lineAngle           float64
	iconLineAngle       float64
	viewportAligned     bool
	iconViewportAligned bool
	sortKey             float64
	textAllowsOverlap   bool
	iconAllowsOverlap   bool
	textOptional        bool
	iconOptional        bool
	textPadding         float64
	iconPadding         float64

	text          string
	fontFamily    string
	fontStack     string
	textSize      float64
	textColor     mapColor
	haloColor     mapColor
	haloWidth     float64
	haloBlur      float64
	letterSpacing float64
	lineHeight    float64
	maximumWidth  float64
	textAnchor    string
	textJustify   string
	textOffset    roadPoint
	textRotate    float64

	iconName    string
	iconSize    float64
	iconColor   mapColor
	iconOpacity float64
	iconAnchor  string
	iconOffset  roadPoint
	iconRotate  float64
}

func compileLibertySymbolLayer(bucket *tileBucket, layer compiledLibertyLayer, zoom float64) error {
	err := placement.PrepareSymbols(bucket.sourceLayers[layer.SourceLayer], layer, placement.SymbolOptions{
		SourceZoom: bucket.tile.Z, Zoom: zoom, Limit: maxTileSymbols - len(bucket.symbols),
	}, func(symbol placement.Symbol) error {
		bucket.symbols = append(bucket.symbols, legacySymbol(symbol))
		return nil
	})
	if errors.Is(err, placement.ErrSymbolLimit) {
		return errFeatureResourceLimit
	}
	return err
}

// Keep the legacy retained renderer's representation behind one value adapter.
// The streaming producer avoids a second candidate slice and copies no maps or
// string payloads. Other consumers can retain placement.Symbol directly.
func legacySymbol(s placement.Symbol) libertySymbolCandidate {
	return libertySymbolCandidate{
		order: s.Order, layerID: s.LayerID, anchor: s.Anchor,
		lineAngle: s.LineAngle, iconLineAngle: s.IconLineAngle,
		viewportAligned: s.ViewportAligned, iconViewportAligned: s.IconViewportAligned,
		sortKey: s.SortKey, textAllowsOverlap: s.TextAllowsOverlap, iconAllowsOverlap: s.IconAllowsOverlap,
		textOptional: s.TextOptional, iconOptional: s.IconOptional,
		textPadding: s.TextPadding, iconPadding: s.IconPadding,
		text: s.Text, fontFamily: s.FontFamily, fontStack: s.FontStack,
		textSize: s.TextSize, textColor: s.TextColor, haloColor: s.HaloColor,
		haloWidth: s.HaloWidth, haloBlur: s.HaloBlur, letterSpacing: s.LetterSpacing,
		lineHeight: s.LineHeight, maximumWidth: s.MaximumWidth,
		textAnchor: s.TextAnchor, textJustify: s.TextJustify, textOffset: s.TextOffset, textRotate: s.TextRotate,
		iconName: s.IconName, iconSize: s.IconSize, iconColor: s.IconColor, iconOpacity: s.IconOpacity,
		iconAnchor: s.IconAnchor, iconOffset: s.IconOffset, iconRotate: s.IconRotate,
	}
}

// Project only the fields needed by the shared box preparer. This value adapter
// copies no backing data and can disappear when retained scenes use Symbol directly.
func projectionSymbol(c *libertySymbolCandidate) placement.Symbol {
	return placement.Symbol{
		Anchor: c.anchor, LineAngle: c.lineAngle, IconLineAngle: c.iconLineAngle,
		ViewportAligned: c.viewportAligned, IconViewportAligned: c.iconViewportAligned,
		TextAllowsOverlap: c.textAllowsOverlap, IconAllowsOverlap: c.iconAllowsOverlap,
		TextOptional: c.textOptional, IconOptional: c.iconOptional,
		TextPadding: c.textPadding, IconPadding: c.iconPadding,
		Text: c.text, TextSize: c.textSize, TextColor: c.textColor,
		HaloWidth: c.haloWidth, HaloBlur: c.haloBlur, LetterSpacing: c.letterSpacing,
		LineHeight: c.lineHeight, MaximumWidth: c.maximumWidth, TextAnchor: c.textAnchor,
		TextOffset: c.textOffset, TextRotate: c.textRotate,
		IconName: c.iconName, IconSize: c.iconSize, IconAnchor: c.iconAnchor,
		IconOffset: c.iconOffset, IconRotate: c.iconRotate,
	}
}

func libertySymbolSpacing(screenPixels float64, tileZoom uint32, styleZoom float64) float64 {
	return placement.SymbolSpacing(screenPixels, tileZoom, styleZoom)
}

func libertyLineAnchor(line []roadPoint, fraction float64) (placement.Anchor, bool) {
	return placement.LineAnchor(line, fraction)
}

func expandLibertyTokens(text string, properties featureProperties) string {
	return placement.ExpandTokens(text, properties)
}

func boundedLibertySymbolText(text string) string { return placement.BoundedText(text) }

func libertyEvaluatedFonts(layer compiledLibertyLayer, evaluation libertyEvaluation) (string, string) {
	return layer.FontStack(evaluation.context())
}
