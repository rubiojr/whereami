package vecmap

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"sort"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmbeddedLibertyStyleIsPinned(t *testing.T) {
	checksum := sha256.Sum256(libertyStyleJSON)
	assert.Equal(t, libertyStyleSHA256, hex.EncodeToString(checksum[:]))

	layers, err := compiledLibertyLayers()
	require.NoError(t, err)
	require.Len(t, layers, 111)
	assert.Equal(t, "background", layers[0].id)
	assert.Equal(t, "background", layers[0].kind)
	assert.Equal(t, "label_country_1", layers[len(layers)-1].id)
}

func TestEmbeddedLibertySpritesArePinned(t *testing.T) {
	jsonChecksum := sha256.Sum256(libertySpriteJSON)
	pngChecksum := sha256.Sum256(libertySpritePNG)
	assert.Equal(t, libertySpriteJSONSHA256, hex.EncodeToString(jsonChecksum[:]))
	assert.Equal(t, libertySpritePNGSHA256, hex.EncodeToString(pngChecksum[:]))
	require.NoError(t, loadLibertySprites())
	assert.Len(t, libertySpriteIndex, 264)

	sprite, ok := libertySprite("airport", mapColor{red: 1, green: 2, blue: 3, alpha: 255}, 1)
	assert.True(t, ok)
	assert.NotEmpty(t, sprite.pixels)
}

func TestTessellateLibertyLinesProducesWidthAndDashes(t *testing.T) {
	path := [][]roadPoint{{{X: 0, Y: 0}, {X: 20, Y: 0}}}
	solid, err := tessellateLibertyLines(path, libertyLinePaint{width: 4, lineCap: "butt"}, 100)
	require.NoError(t, err)
	assert.InDelta(t, 80, triangleMeshArea(solid), 1e-9)

	dashed, err := tessellateLibertyLines(path, libertyLinePaint{width: 2, dashes: []float64{2, 2}, lineCap: "butt"}, 100)
	require.NoError(t, err)
	assert.Less(t, triangleMeshArea(dashed), triangleMeshArea(solid))

	tiny, err := tessellateLibertyLines(path, libertyLinePaint{width: 1e-300, dashes: []float64{1, 1}}, 100)
	require.NoError(t, err)
	assert.Empty(t, tiny)

	zeroDash, err := tessellateLibertyLines(path, libertyLinePaint{width: 2, dashes: []float64{0, 2}}, 100)
	require.NoError(t, err)
	assert.Empty(t, zeroDash)

	zeroPattern, err := tessellateLibertyLines(path, libertyLinePaint{width: 2, dashes: []float64{0, 0}}, 100)
	require.NoError(t, err)
	assert.NotEmpty(t, zeroPattern)

	_, err = tessellateLibertyLines(path, libertyLinePaint{width: 2, dashes: []float64{0.1, 0.1}}, 2)
	assert.ErrorIs(t, err, errFeatureResourceLimit)

	miter, err := tessellateLibertyLines(
		[][]roadPoint{{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}}},
		libertyLinePaint{width: 4, lineJoin: "miter"},
		100,
	)
	require.NoError(t, err)
	assert.Len(t, miter, 18)

	offset := offsetLibertyLine([]roadPoint{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}}, 2)
	assert.InDelta(t, 8, offset[1].X, 1e-12)
	assert.InDelta(t, 2, offset[1].Y, 1e-12)
}

func TestLibertySpriteCacheIsBounded(t *testing.T) {
	cache := libertySpriteImageCache{}
	for index := range maxLibertySpriteCacheEntries + 100 {
		cache.put(strconv.Itoa(index), libertySpriteImage{pixels: []byte{byte(index)}})
	}
	assert.Len(t, cache.entries, maxLibertySpriteCacheEntries)
	assert.Len(t, cache.order, maxLibertySpriteCacheEntries)
}

func TestLibertyExpressionsAreSupported(t *testing.T) {
	var document libertyStyleDocument
	require.NoError(t, json.Unmarshal(libertyStyleJSON, &document))

	operators := make(map[string]struct{})
	for _, layer := range document.Layers {
		collectLibertyOperators(layer.Filter, operators)
		for _, value := range layer.Paint {
			var decoded any
			require.NoError(t, json.Unmarshal(value, &decoded))
			collectLibertyOperators(decoded, operators)
		}
		for _, value := range layer.Layout {
			var decoded any
			require.NoError(t, json.Unmarshal(value, &decoded))
			collectLibertyOperators(decoded, operators)
		}
	}

	unsupported := make([]string, 0)
	for operator := range operators {
		if _, exists := supportedLibertyOperators[operator]; !exists {
			unsupported = append(unsupported, operator)
		}
	}
	sort.Strings(unsupported)
	assert.Empty(t, unsupported)
}

var styleExpressionOperators = map[string]struct{}{
	"!": {}, "!=": {}, "*": {}, "+": {}, "-": {}, "/": {}, "<": {}, "<=": {}, "==": {}, ">": {}, ">=": {},
	"all": {}, "any": {}, "array": {}, "at": {}, "boolean": {}, "case": {}, "coalesce": {}, "concat": {},
	"downcase": {}, "format": {}, "geometry-type": {}, "get": {}, "has": {}, "image": {}, "in": {},
	"index-of": {}, "interpolate": {}, "length": {}, "let": {}, "literal": {}, "match": {}, "number": {},
	"object": {}, "slice": {}, "step": {}, "string": {}, "to-boolean": {}, "to-number": {}, "to-string": {},
	"typeof": {}, "upcase": {}, "var": {}, "zoom": {},
}

var supportedLibertyOperators = map[string]struct{}{
	"!": {}, "!=": {}, "<": {}, "<=": {}, "==": {}, ">": {}, ">=": {}, "all": {}, "case": {},
	"coalesce": {}, "concat": {}, "geometry-type": {}, "get": {}, "has": {}, "interpolate": {}, "match": {},
	"step": {}, "to-string": {}, "zoom": {},
}

func collectLibertyOperators(value any, operators map[string]struct{}) {
	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		return
	}
	if operator, ok := items[0].(string); ok {
		if _, isOperator := styleExpressionOperators[operator]; isOperator {
			operators[operator] = struct{}{}
		}
	}
	for _, item := range items {
		collectLibertyOperators(item, operators)
	}
}

func TestLibertyEvaluator(t *testing.T) {
	evaluation := libertyEvaluation{
		zoom:       12,
		geometryID: mvtLineStringType,
		properties: featureProperties{"class": "primary", "rank": int64(2)},
	}
	tests := []struct {
		name       string
		expression any
		expected   any
	}{
		{name: "property", expression: []any{"get", "class"}, expected: "primary"},
		{name: "filter", expression: []any{"all", []any{"==", []any{"geometry-type"}, "LineString"}, []any{"<", []any{"get", "rank"}, float64(3)}}, expected: true},
		{name: "match", expression: []any{"match", []any{"get", "class"}, []any{"primary", "secondary"}, "road", "other"}, expected: "road"},
		{name: "step", expression: []any{"step", []any{"zoom"}, "small", float64(10), "medium", float64(14), "large"}, expected: "medium"},
		{name: "interpolate", expression: []any{"interpolate", []any{"linear"}, []any{"zoom"}, float64(10), float64(2), float64(14), float64(6)}, expected: float64(4)},
		{name: "missing property inequality", expression: []any{"!=", []any{"get", "brunnel"}, "tunnel"}, expected: true},
		{name: "missing property match fallback", expression: []any{"match", []any{"get", "brunnel"}, "bridge", false, "tunnel", false, true}, expected: true},
		{name: "coalesce accepts empty string", expression: []any{"coalesce", "", "fallback"}, expected: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, ok := evaluateLibertyExpression(test.expression, evaluation)
			assert.True(t, ok)
			assert.Equal(t, test.expected, actual)
		})
	}
}

func TestLibertyEvaluatorRejectsUnknownAndDeepExpressions(t *testing.T) {
	_, ok := evaluateLibertyExpression([]any{"not-a-style-operator", true}, libertyEvaluation{})
	assert.False(t, ok)

	var expression any = true
	for range maxLibertyExpressionDepth + 1 {
		expression = []any{"!", expression}
	}
	_, ok = evaluateLibertyExpression(expression, libertyEvaluation{})
	assert.False(t, ok)
}

func TestCompileLibertyPatternFill(t *testing.T) {
	polygon := vectorPolygon{
		exterior:  []roadPoint{{X: 0, Y: 0}, {X: 32, Y: 0}, {X: 32, Y: 32}, {X: 0, Y: 32}},
		triangles: backgroundTriangles(),
	}
	bucket := &tileBucket{
		tile: vectorTileID{X: 0, Y: 0, Z: 12},
		sourceLayers: map[string][]vectorFeature{
			"landcover": {{
				geometryID: mvtPolygonType,
				properties: featureProperties{"class": "wetland"},
				polygons:   []vectorPolygon{polygon},
			}},
		},
	}

	require.NoError(t, compileLibertyTile(bucket, 12))
	found := false
	for _, primitive := range bucket.liberty {
		if primitive.layerID == "landcover_wetland" {
			assert.Equal(t, "wetland_bg_11", primitive.patternName)
			found = true
		}
	}
	assert.True(t, found)
}

func TestValidateNaturalEarthRaster(t *testing.T) {
	imageData := image.NewRGBA(image.Rect(0, 0, int(tileSize), int(tileSize)))
	imageData.SetRGBA(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	var encoded bytes.Buffer
	require.NoError(t, png.Encode(&encoded, imageData))
	assert.NoError(t, validateNaturalEarthRaster(encoded.Bytes()))
	raster, err := decodeNaturalEarthRaster(encoded.Bytes())
	require.NoError(t, err)
	assert.Equal(t, int(tileSize), raster.width)
	assert.Equal(t, int(tileSize), raster.height)
	assert.Len(t, raster.rgba, int(tileSize*tileSize*4))
	assert.Equal(t, []byte{1, 2, 3, 255}, raster.rgba[:4])
	assert.Error(t, validateNaturalEarthRaster([]byte("not a PNG")))
	var tooSmall bytes.Buffer
	require.NoError(t, png.Encode(&tooSmall, image.NewRGBA(image.Rect(0, 0, 64, 64))))
	assert.ErrorIs(t, validateNaturalEarthRaster(tooSmall.Bytes()), errRasterData)
}

func TestParseLibertyColors(t *testing.T) {
	tests := []struct {
		value    string
		expected mapColor
	}{
		{value: "#f8f4f0", expected: mapColor{red: 248, green: 244, blue: 240, alpha: 255}},
		{value: "rgba(95, 208, 100, 0.5)", expected: mapColor{red: 95, green: 208, blue: 100, alpha: 128}},
		{value: "hsla(0, 0%, 100%, 0.25)", expected: mapColor{red: 255, green: 255, blue: 255, alpha: 64}},
	}
	for _, test := range tests {
		actual, ok := parseLibertyColor(test.value)
		assert.True(t, ok, test.value)
		assert.Equal(t, test.expected, actual, test.value)
	}
}
