package placement

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/mvt"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func collectSymbols(t *testing.T, features mvt.Features, layer style.CompiledLayer, options SymbolOptions) []Symbol {
	t.Helper()
	var symbols []Symbol
	require.NoError(t, PrepareSymbols(features, layer, options, func(s Symbol) error { symbols = append(symbols, s); return nil }))
	return symbols
}

func TestPrepareSymbolsDefaultsAndSelection(t *testing.T) {
	features := mvt.FeatureSlice{
		{GeometryType: mvt.PointType, Properties: mvt.Properties{"name": "skip"}, Points: []geometry.Point{{X: 1}}},
		{GeometryType: mvt.PointType, Properties: mvt.Properties{"name": "Madrid"}, Points: []geometry.Point{{X: 2}, {X: 3}}},
	}
	layer := style.CompiledLayer{Order: 7, ID: "labels", Filter: []any{"!=", []any{"get", "name"}, "skip"}, Layout: map[string]any{"text-field": "{name}"}}
	symbols := collectSymbols(t, features, layer, SymbolOptions{SourceZoom: 9, Zoom: 10, Limit: 10})
	require.Len(t, symbols, 2)
	want := Symbol{
		Order: 7, LayerID: "labels", Anchor: geometry.Point{X: 2},
		ViewportAligned: true, IconViewportAligned: true, TextPadding: 2, IconPadding: 2,
		Text: "Madrid", FontFamily: "Noto Sans Regular", FontStack: "Noto Sans Regular",
		TextSize: 16, TextColor: style.Color{Alpha: 255}, LineHeight: 1.2, MaximumWidth: 10,
		TextAnchor: "center", TextJustify: "auto", IconSize: 1, IconColor: style.Color{Alpha: 255},
		IconOpacity: 1, IconAnchor: "center",
	}
	assert.Equal(t, want, symbols[0])
	want.Anchor.X = 3
	assert.Equal(t, want, symbols[1])
	features[1].Properties.(mvt.Properties)["name"] = "changed"
	features[1].Points[0].X = 100
	assert.Equal(t, "Madrid", symbols[0].Text)
	assert.Equal(t, 2.0, symbols[0].Anchor.X)
	assert.Empty(t, collectSymbols(t, features, style.CompiledLayer{}, SymbolOptions{Limit: 0}))
}

func TestPrepareSymbolsEvaluatesTextIconPaint(t *testing.T) {
	feature := mvt.Feature{GeometryType: mvt.LineStringType, Properties: mvt.Properties{"name": " a\u3000b\r\nc "}, Lines: [][]geometry.Point{{{X: 10}, {}}}}
	layer := style.CompiledLayer{Order: 4, ID: "styled", Layout: map[string]any{
		"text-field": "{name}", "text-transform": "uppercase", "symbol-placement": "line-center",
		"symbol-sort-key": 5.0, "text-allow-overlap": true, "icon-allow-overlap": true,
		"text-optional": true, "icon-optional": true, "text-padding": 3.0, "icon-padding": 4.0,
		"text-font": []any{" First ", "Second"}, "text-size": []any{"zoom"},
		"text-letter-spacing": -0.1, "text-line-height": 2.0, "text-max-width": 20.0,
		"text-anchor": "left", "text-justify": "right", "text-offset": []any{1.0, -2.0}, "text-rotate": 90.0,
		"icon-image": "icon", "icon-size": 2.0, "icon-opacity": 0.5, "icon-anchor": "bottom",
		"icon-offset": []any{-3.0, 4.0}, "icon-rotate": 180.0,
		"text-keep-upright": false, "icon-keep-upright": true,
		"text-rotation-alignment": "viewport", "icon-rotation-alignment": "map",
	}, Paint: map[string]any{
		"text-color": "#123456", "text-opacity": 0.5, "text-halo-color": "white", "text-halo-width": 2.0,
		"text-halo-blur": 3.0, "icon-color": "#010203", "text-padding": 6.0,
	}}
	symbols := collectSymbols(t, mvt.FeatureSlice{feature}, layer, SymbolOptions{Zoom: 12, SourceZoom: 9, Limit: 10})
	require.Len(t, symbols, 1)
	assert.Equal(t, Symbol{
		Order: 4, LayerID: "styled", Anchor: geometry.Point{X: 5}, LineAngle: math.Pi, IconLineAngle: 2 * math.Pi,
		ViewportAligned: true, IconViewportAligned: false, SortKey: 5,
		TextAllowsOverlap: true, IconAllowsOverlap: true, TextOptional: true, IconOptional: true,
		TextPadding: 6, IconPadding: 4, Text: " A B\nC ", FontFamily: "First", FontStack: "First,Second",
		TextSize: 12, TextColor: style.Color{Red: 0x12, Green: 0x34, Blue: 0x56, Alpha: 128},
		HaloColor: style.Color{Red: 255, Green: 255, Blue: 255, Alpha: 255}, HaloWidth: 2, HaloBlur: 3,
		LetterSpacing: -0.1, LineHeight: 2, MaximumWidth: 20, TextAnchor: "left", TextJustify: "right",
		TextOffset: geometry.Point{X: 1, Y: -2}, TextRotate: math.Pi / 2,
		IconName: "icon", IconSize: 2, IconColor: style.Color{Red: 1, Green: 2, Blue: 3, Alpha: 255},
		IconOpacity: 0.5, IconAnchor: "bottom", IconOffset: geometry.Point{X: -3, Y: 4}, IconRotate: math.Pi,
	}, symbols[0])
	layer.Layout["text-transform"] = "lowercase"
	layer.Layout["text-rotation-alignment"] = "map"
	layer.Layout["icon-rotation-alignment"] = "viewport"
	layer.Paint["text-color"] = "bad"
	symbols = collectSymbols(t, mvt.FeatureSlice{feature}, layer, SymbolOptions{Limit: 1})
	assert.Equal(t, " a b\nc ", symbols[0].Text)
	assert.False(t, symbols[0].ViewportAligned)
	assert.True(t, symbols[0].IconViewportAligned)
	assert.Equal(t, style.Color{}, symbols[0].TextColor)
}

func TestPrepareSymbolsSpacingFallbacksAndCallerVisibility(t *testing.T) {
	layer := style.CompiledLayer{MinZoom: 99, Layout: map[string]any{
		"visibility": "none", "text-field": "A", "symbol-placement": "line", "symbol-spacing": 200.0,
		"text-size": "bad", "text-offset": []any{1.0}, "icon-offset": "bad",
	}}
	feature := mvt.Feature{Lines: [][]geometry.Point{{{}, {X: 100}}}}
	symbols := collectSymbols(t, mvt.FeatureSlice{feature}, layer, SymbolOptions{SourceZoom: 14, Zoom: 17, Limit: 10})
	require.Len(t, symbols, 4)
	assert.Equal(t, 12.5, symbols[0].Anchor.X)
	assert.Equal(t, 16.0, symbols[0].TextSize)
	assert.Equal(t, geometry.Point{}, symbols[0].TextOffset)
	assert.Equal(t, geometry.Point{}, symbols[0].IconOffset)
	assert.False(t, symbols[0].ViewportAligned)
	assert.Equal(t, 25.0, SymbolSpacing(200, 14, 17))
	// Spacing follows the zoom the tile is drawn at, not the lower style zoom.
	assert.Equal(t, symbols, collectSymbols(t, mvt.FeatureSlice{feature}, layer, SymbolOptions{SourceZoom: 14, Zoom: 16, Coarser: 1, Limit: 10}))
	assert.Len(t, collectSymbols(t, mvt.FeatureSlice{feature}, layer, SymbolOptions{SourceZoom: 14, Zoom: 16, Limit: 10}), 2)
	layer.Layout["text-field"] = strings.Repeat("A", 257)
	assert.Empty(t, collectSymbols(t, mvt.FeatureSlice{feature}, layer, SymbolOptions{}))
	layer.Layout["icon-image"] = "airport"
	symbols = collectSymbols(t, mvt.FeatureSlice{feature}, layer, SymbolOptions{SourceZoom: 14, Zoom: 14, Limit: 1})
	assert.Empty(t, symbols[0].Text)
	assert.Equal(t, "airport", symbols[0].IconName)
}

func TestPrepareSymbolsBudgetsAndSinkErrors(t *testing.T) {
	layer := style.CompiledLayer{Layout: map[string]any{"text-field": "A"}}
	features := mvt.FeatureSlice{{Points: []geometry.Point{{X: 1}, {X: 2}}}}
	var got []Symbol
	sink := func(s Symbol) error { got = append(got, s); return nil }
	err := PrepareSymbols(features, layer, SymbolOptions{Limit: 1}, sink)
	assert.ErrorIs(t, err, ErrSymbolLimit)
	require.Len(t, got, 1)
	assert.Equal(t, 1.0, got[0].Anchor.X)
	got = nil
	require.NoError(t, PrepareSymbols(features, layer, SymbolOptions{Limit: 2}, sink))
	assert.Len(t, got, 2)
	got = nil
	assert.ErrorIs(t, PrepareSymbols(features, layer, SymbolOptions{Limit: 0}, sink), ErrSymbolLimit)
	assert.Empty(t, got)
	assert.ErrorIs(t, PrepareSymbols(nil, layer, SymbolOptions{Limit: -1}, sink), ErrSymbolLimit)
	assert.ErrorContains(t, PrepareSymbols(nil, layer, SymbolOptions{}, nil), "sink is nil")
	wantErr := errors.New("sink stopped")
	calls := 0
	err = PrepareSymbols(features, layer, SymbolOptions{Limit: 2}, func(Symbol) error { calls++; return wantErr })
	assert.ErrorIs(t, err, wantErr)
	assert.Equal(t, 1, calls)
	features[0].Points = make([]geometry.Point, MaxSymbols+1)
	calls = 0
	err = PrepareSymbols(features, layer, SymbolOptions{Limit: MaxSymbols + 100}, func(Symbol) error { calls++; return nil })
	assert.ErrorIs(t, err, ErrSymbolLimit)
	assert.Equal(t, MaxSymbols, calls)
}
