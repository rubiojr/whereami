package placement

import (
	"errors"
	"math"
	"strings"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/mvt"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
)

const MaxSymbols = 10_000

var ErrSymbolLimit = errors.New("symbol candidate limit exceeded")

// Symbol is evaluated text/icon paint at one anchor, before glyph layout or
// collision. Strings may borrow immutable style/feature data; all other fields
// are values. It has no toolkit handles, property maps or native font objects.
type Symbol struct {
	Order               int
	LayerID             string
	Anchor              geometry.Point
	LineAngle           float64
	IconLineAngle       float64
	ViewportAligned     bool
	IconViewportAligned bool
	SortKey             float64
	TextAllowsOverlap   bool
	IconAllowsOverlap   bool
	TextOptional        bool
	IconOptional        bool
	TextPadding         float64
	IconPadding         float64

	Text          string
	FontFamily    string
	FontStack     string
	TextSize      float64
	TextColor     style.Color
	HaloColor     style.Color
	HaloWidth     float64
	HaloBlur      float64
	LetterSpacing float64
	LineHeight    float64
	MaximumWidth  float64
	TextAnchor    string
	TextJustify   string
	TextOffset    geometry.Point
	TextRotate    float64

	IconName    string
	IconSize    float64
	IconColor   style.Color
	IconOpacity float64
	IconAnchor  string
	IconOffset  geometry.Point
	IconRotate  float64
}

// SymbolOptions supplies tile/source zoom, evaluated style zoom and remaining
// capacity shared across the caller's tile layers. Limit is capped at MaxSymbols.
type SymbolOptions struct {
	SourceZoom uint32
	Zoom       float64
	Limit      int
}

// PrepareSymbols emits candidates in feature/anchor order for one already-selected
// symbol layer. Caller selection owns zoom visibility, hidden state and layer type.
// It reuses the existing filter, text, font, paint and alignment behavior.
//
// The sink runs synchronously. Errors stop emission but do not undo earlier sink
// calls, preserving legacy partial-budget behavior. Limit exhaustion occurs only
// when another candidate would be emitted. Use remaining capacity across layers;
// the function neither stores an output slice nor owns logging/cache/I/O policy.
// Inputs are immutable bounded MVT features and application-owned style values.
func PrepareSymbols(features []mvt.Feature, layer style.CompiledLayer, options SymbolOptions, emit func(Symbol) error) error {
	if emit == nil {
		return errors.New("symbol sink is nil")
	}
	if options.Limit < 0 {
		return ErrSymbolLimit
	}
	remaining := min(options.Limit, MaxSymbols)
	for _, feature := range features {
		context := style.Context{Zoom: options.Zoom, GeometryType: feature.GeometryType, Properties: feature.Properties}
		if !layer.Matches(context) {
			continue
		}
		text := ExpandTokens(layer.StringValue("text-field", context, ""), feature.Properties)
		switch layer.StringValue("text-transform", context, "none") {
		case "uppercase":
			text = strings.ToUpper(text)
		case "lowercase":
			text = strings.ToLower(text)
		}
		text = BoundedText(NormalizeText(text))
		iconName := ExpandTokens(layer.StringValue("icon-image", context, ""), feature.Properties)
		if text == "" && iconName == "" {
			continue
		}
		mode := layer.StringValue("symbol-placement", context, "point")
		spacing := SymbolSpacing(layer.NumberValue("symbol-spacing", context, 250), options.SourceZoom, options.Zoom)
		fontFamily, fontStack := layer.FontStack(context)
		for _, anchor := range FeatureAnchors(feature, mode, spacing) {
			if remaining == 0 {
				return ErrSymbolLimit
			}
			symbol := evaluatedSymbol(layer, context, anchor, mode, text, iconName, fontFamily, fontStack)
			if err := emit(symbol); err != nil {
				return err
			}
			remaining--
		}
	}
	return nil
}

// SymbolSpacing converts screen-pixel spacing to source-tile units at style zoom.
func SymbolSpacing(screenPixels float64, sourceZoom uint32, styleZoom float64) float64 {
	return screenPixels * math.Exp2(float64(sourceZoom)-styleZoom)
}

func evaluatedSymbol(layer style.CompiledLayer, context style.Context, anchor Anchor, mode, text, iconName, family, stack string) Symbol {
	candidate := Symbol{
		Order: layer.Order, LayerID: layer.ID, Anchor: anchor.Point,
		LineAngle: anchor.Angle, IconLineAngle: anchor.RawAngle,
		ViewportAligned: mode == "point", IconViewportAligned: mode == "point",
		SortKey:           layer.NumberValue("symbol-sort-key", context, 0),
		TextAllowsOverlap: layer.BoolValue("text-allow-overlap", context, false),
		IconAllowsOverlap: layer.BoolValue("icon-allow-overlap", context, false),
		TextOptional:      layer.BoolValue("text-optional", context, false),
		IconOptional:      layer.BoolValue("icon-optional", context, false),
		TextPadding:       layer.NumberValue("text-padding", context, 2),
		IconPadding:       layer.NumberValue("icon-padding", context, 2),
		Text:              text, FontFamily: family, FontStack: stack,
		TextSize:      layer.NumberValue("text-size", context, 16),
		LetterSpacing: layer.NumberValue("text-letter-spacing", context, 0),
		LineHeight:    layer.NumberValue("text-line-height", context, 1.2),
		MaximumWidth:  layer.NumberValue("text-max-width", context, 10),
		TextAnchor:    layer.StringValue("text-anchor", context, "center"),
		TextJustify:   layer.StringValue("text-justify", context, "auto"),
		TextOffset:    evaluatedPoint(layer, "text-offset", context),
		TextRotate:    layer.NumberValue("text-rotate", context, 0) * math.Pi / 180,
		IconName:      iconName,
		IconSize:      layer.NumberValue("icon-size", context, 1),
		IconOpacity:   layer.NumberValue("icon-opacity", context, 1),
		IconAnchor:    layer.StringValue("icon-anchor", context, "center"),
		IconOffset:    evaluatedPoint(layer, "icon-offset", context),
		IconRotate:    layer.NumberValue("icon-rotate", context, 0) * math.Pi / 180,
	}
	if !layer.BoolValue("text-keep-upright", context, true) {
		candidate.LineAngle = anchor.RawAngle
	}
	if layer.BoolValue("icon-keep-upright", context, false) {
		candidate.IconLineAngle = anchor.Angle
	}
	candidate.TextColor, _ = layer.ColorValue("text-color", context, style.Color{Alpha: 255})
	candidate.TextColor = style.ColorWithOpacity(candidate.TextColor, layer.NumberValue("text-opacity", context, 1))
	candidate.HaloColor, _ = layer.ColorValue("text-halo-color", context, style.Color{})
	candidate.HaloWidth = layer.NumberValue("text-halo-width", context, 0)
	candidate.HaloBlur = layer.NumberValue("text-halo-blur", context, 0)
	candidate.IconColor, _ = layer.ColorValue("icon-color", context, style.Color{Alpha: 255})
	if alignment := layer.StringValue("text-rotation-alignment", context, "auto"); alignment == "viewport" {
		candidate.ViewportAligned = true
	} else if alignment == "map" {
		candidate.ViewportAligned = false
	}
	if alignment := layer.StringValue("icon-rotation-alignment", context, "auto"); alignment == "viewport" {
		candidate.IconViewportAligned = true
	} else if alignment == "map" {
		candidate.IconViewportAligned = false
	}
	return candidate
}

func evaluatedPoint(layer style.CompiledLayer, name string, context style.Context) geometry.Point {
	values := layer.NumberArrayValue(name, context)
	if len(values) != 2 {
		return geometry.Point{}
	}
	return geometry.Point{X: values[0], Y: values[1]}
}
