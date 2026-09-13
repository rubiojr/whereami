package vecmap

import (
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLibertySymbolSpacingUsesScreenPixels(t *testing.T) {
	assert.InDelta(t, 25, libertySymbolSpacing(200, 14, 17), 1e-12)
	assert.InDelta(t, 200, libertySymbolSpacing(200, 14, 14), 1e-12)
}

func TestExpandLibertyTokensDoesNotRescanPropertyValues(t *testing.T) {
	properties := featureProperties{"name": "{name}", "other": "value"}
	assert.Equal(t, "{name} value", expandLibertyTokens("{name} {other}", properties))
}

func TestExpandLibertyTokensRejectsOversizedRemoteText(t *testing.T) {
	properties := featureProperties{"name": strings.Repeat("A", maximumSymbolTextRunes+1)}
	assert.Empty(t, expandLibertyTokens("{name}", properties))
	assert.Empty(t, boundedLibertySymbolText(strings.Repeat("界", maximumSymbolTextRunes+1)))
}

func TestLibertyLineAnchorRetainsRawDirection(t *testing.T) {
	anchor, ok := libertyLineAnchor([]roadPoint{{X: 10}, {X: 0}}, 0.5)
	require.True(t, ok)
	assert.InDelta(t, math.Pi, anchor.rawAngle, 1e-12)
	assert.InDelta(t, 0, math.Sin(anchor.angle), 1e-12)
	assert.Greater(t, math.Cos(anchor.angle), 0.0)
}

func TestLibertyViewportAlignedSymbolIgnoresLineAngle(t *testing.T) {
	assert.InDelta(t, 0.25, libertyRenderedSymbolAngle(1.5, 0.25, true), 1e-12)
	assert.InDelta(t, 1.75, libertyRenderedSymbolAngle(1.5, 0.25, false), 1e-12)
}

func TestLibertyDesktopFontUsesBundledOpenMapTilesFamily(t *testing.T) {
	assert.Equal(t, "KlokanTech Noto Sans", libertyDesktopFont("Noto Sans"))
	assert.Equal(t, "KlokanTech Noto Sans Regular", libertyDesktopFont("Noto Sans Regular"))
	assert.Equal(t, "KlokanTech Noto Sans Bold", libertyDesktopFont("Noto Sans Bold"))
	assert.Equal(t, "Open Sans Regular", libertyDesktopFont("Open Sans Regular"))
}

func TestLibertyEvaluatedFontsPreservesOrderedStack(t *testing.T) {
	layer := compiledLibertyLayer{Layout: map[string]any{
		"text-font": []any{"Noto Sans Regular", "Noto Sans CJK TC Regular"},
	}}
	family, stack := libertyEvaluatedFonts(layer, libertyEvaluation{})
	assert.Equal(t, "Noto Sans Regular", family)
	assert.Equal(t, "Noto Sans Regular,Noto Sans CJK TC Regular", stack)
}

func TestLibertyCandidateCollisionUsesSDFMetrics(t *testing.T) {
	candidate := libertySymbolCandidate{
		anchor:      roadPoint{X: 100, Y: 100},
		text:        "label",
		textColor:   mapColor{Alpha: 255},
		textSize:    10,
		textOffset:  roadPoint{X: 1, Y: 2},
		textPadding: 2,
	}
	layout := &sdfTextLayout{bounds: libertyCollisionBox{left: -5, top: -6, right: 7, bottom: 8}}
	textBox, present, visible, _, _, _ := libertyCandidateCollisionBoxes(
		affineTransform{M11: 1, M22: 1},
		candidate,
		layout,
		256,
		256,
	)
	assert.True(t, present)
	assert.True(t, visible)
	assert.Equal(t, libertyCollisionBox{left: 103, top: 112, right: 119, bottom: 130}, textBox)
}

func TestLibertyCandidateCollisionRotatesSDFMetrics(t *testing.T) {
	candidate := libertySymbolCandidate{
		anchor:      roadPoint{X: 100, Y: 100},
		lineAngle:   math.Pi / 2,
		text:        "label",
		textColor:   mapColor{Alpha: 255},
		textSize:    10,
		textPadding: 0,
	}
	layout := &sdfTextLayout{bounds: libertyCollisionBox{left: -5, top: -2, right: 5, bottom: 2}}
	textBox, _, visible, _, _, _ := libertyCandidateCollisionBoxes(
		affineTransform{M11: 1, M22: 1},
		candidate,
		layout,
		256,
		256,
	)
	require.True(t, visible)
	assert.InDelta(t, 98, textBox.left, 1e-12)
	assert.InDelta(t, 95, textBox.top, 1e-12)
	assert.InDelta(t, 102, textBox.right, 1e-12)
	assert.InDelta(t, 105, textBox.bottom, 1e-12)
}

func TestSetLibertyCounterTransformRejectsMissingNode(t *testing.T) {
	assert.False(t, setLibertyCounterTransform(nil, Camera{}, vectorTileID{}, roadPoint{}, roadPoint{}, 0, false))
}

func TestLibertyLayerHasAcceptedSymbol(t *testing.T) {
	tile := vectorTileID{X: 1, Y: 2, Z: 3}
	candidates := []libertySymbolCandidate{{order: 4}, {order: 7}}
	accepted := map[libertySymbolKey]libertyAcceptedSymbol{
		{tile: tile, wrap: 1, index: 1}: {text: true},
	}

	assert.True(t, libertyLayerHasAcceptedSymbol(tile, 1, candidates, 7, accepted))
	assert.False(t, libertyLayerHasAcceptedSymbol(tile, 1, candidates, 4, accepted))
	assert.False(t, libertyLayerHasAcceptedSymbol(tile, 0, candidates, 7, accepted))
}

func TestLibertySymbolRangeAtOrderPreservesCandidateIndexes(t *testing.T) {
	candidates := []libertySymbolCandidate{{order: 1}, {order: 3}, {order: 3}, {order: 8}}

	start, end := libertySymbolRangeAtOrder(candidates, 3)
	assert.Equal(t, 1, start)
	assert.Equal(t, 3, end)
	start, end = libertySymbolRangeAtOrder(candidates, 2)
	assert.Equal(t, start, end)
}

func TestAcceptedLibertySymbolsUsesLayerPriority(t *testing.T) {
	camera := NewCamera(Coordinate{}, 0, 0, 256, 256)
	candidate := libertySymbolCandidate{
		anchor:       roadPoint{X: 128, Y: 128},
		text:         "label",
		textColor:    mapColor{Alpha: 255},
		textSize:     16,
		lineHeight:   1.2,
		maximumWidth: 10,
	}
	lower := candidate
	lower.order = 1
	higher := candidate
	higher.order = 2
	tiles := []loadedRoadTile{{
		id:    vectorTileID{Z: 0},
		roads: &tileBucket{symbols: []libertySymbolCandidate{lower, higher}},
	}}
	layouts := map[libertySDFLayoutKey]*sdfTextLayout{
		{tile: vectorTileID{Z: 0}, index: 0}: {bounds: libertyCollisionBox{left: -40, top: -10, right: 40, bottom: 10}},
		{tile: vectorTileID{Z: 0}, index: 1}: {bounds: libertyCollisionBox{left: -40, top: -10, right: 40, bottom: 10}},
	}

	accepted := acceptedLibertySymbols(camera, tiles, layouts)

	acceptedIndexes := make(map[int]struct{})
	for key := range accepted {
		acceptedIndexes[key.index] = struct{}{}
	}
	assert.NotContains(t, acceptedIndexes, 0)
	assert.Contains(t, acceptedIndexes, 1)
}

func TestAcceptedLibertySymbolsKeepsOptionalIcon(t *testing.T) {
	camera := NewCamera(Coordinate{}, 0, 0, 256, 256)
	blocker := libertySymbolCandidate{
		order:        2,
		anchor:       roadPoint{X: 128, Y: 128},
		text:         "blocking label",
		textColor:    mapColor{Alpha: 255},
		textSize:     16,
		lineHeight:   1.2,
		maximumWidth: 10,
	}
	optional := blocker
	optional.order = 1
	optional.text = "airport"
	optional.iconName = "airport"
	optional.textOptional = true
	optional.iconAllowsOverlap = true
	tiles := []loadedRoadTile{{
		id:    vectorTileID{Z: 0},
		roads: &tileBucket{symbols: []libertySymbolCandidate{optional, blocker}},
	}}

	accepted := acceptedLibertySymbols(camera, tiles, nil)
	var placement libertyAcceptedSymbol
	for key, candidatePlacement := range accepted {
		if key.index == 0 {
			placement = candidatePlacement
		}
	}
	assert.False(t, placement.text)
	assert.True(t, placement.icon)
}

func TestAcceptedLibertySymbolsWaitsForRequiredSDFText(t *testing.T) {
	camera := NewCamera(Coordinate{}, 0, 0, 256, 256)
	candidate := libertySymbolCandidate{
		anchor:       roadPoint{X: 128, Y: 128},
		text:         "airport",
		textColor:    mapColor{Alpha: 255},
		textSize:     16,
		lineHeight:   1.2,
		maximumWidth: 10,
		iconName:     "airport",
	}
	tiles := []loadedRoadTile{{
		id:    vectorTileID{Z: 0},
		roads: &tileBucket{symbols: []libertySymbolCandidate{candidate}},
	}}

	accepted := acceptedLibertySymbols(camera, tiles, nil)

	assert.Empty(t, accepted)
}

func TestAcceptedLibertySymbolsReflowsWhenCameraZoomChanges(t *testing.T) {
	candidate := libertySymbolCandidate{
		text:         "label",
		textColor:    mapColor{Alpha: 255},
		textSize:     16,
		lineHeight:   1.2,
		maximumWidth: 10,
	}
	first := candidate
	first.anchor = roadPoint{X: 55, Y: 128}
	second := candidate
	second.anchor = roadPoint{X: 65, Y: 128}
	tiles := []loadedRoadTile{{
		id:    vectorTileID{Z: 0},
		roads: &tileBucket{symbols: []libertySymbolCandidate{first, second}},
	}}
	layouts := map[libertySDFLayoutKey]*sdfTextLayout{
		{tile: vectorTileID{Z: 0}, index: 0}: {bounds: libertyCollisionBox{left: -25, top: -10, right: 25, bottom: 10}},
		{tile: vectorTileID{Z: 0}, index: 1}: {bounds: libertyCollisionBox{left: -25, top: -10, right: 25, bottom: 10}},
	}
	center := Coordinate{Longitude: -90}
	zoomedIn := NewCamera(center, 3, 0, 256, 256)
	zoomedOut := NewCamera(center, 0, 0, 256, 256)

	before := acceptedLibertySymbols(zoomedIn, tiles, layouts)
	after := acceptedLibertySymbols(zoomedOut, tiles, layouts)

	_, firstVisibleBefore := before[libertySymbolKey{tile: vectorTileID{Z: 0}, wrap: 0, index: 0}]
	_, firstVisibleAfter := after[libertySymbolKey{tile: vectorTileID{Z: 0}, wrap: 0, index: 0}]
	assert.True(t, firstVisibleBefore)
	assert.False(t, firstVisibleAfter)
	assert.False(t, sameLibertyAcceptedSymbols(before, after))
	assert.True(t, sameLibertyAcceptedSymbols(after, after))
}
