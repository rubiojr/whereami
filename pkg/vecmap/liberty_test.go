package vecmap

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"sort"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/liberty"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLibertyStyleAdapterSharesLayers(t *testing.T) {
	layers, err := compiledLibertyLayers()
	require.NoError(t, err)
	require.Len(t, layers, 111)
	assert.Equal(t, "background", layers[0].ID)
	assert.Equal(t, "background", layers[0].Kind)
	assert.Equal(t, "label_country_1", layers[len(layers)-1].ID)
	shared, err := liberty.Layers()
	require.NoError(t, err)
	assert.Same(t, &shared[0], &layers[0])
}

func TestLibertySpriteAdapterSharesPixels(t *testing.T) {
	sprite, ok := libertySprite("airport", mapColor{Red: 1, Green: 2, Blue: 3, Alpha: 255}, 1)
	assert.True(t, ok)
	assert.NotEmpty(t, sprite.pixels)
	shared, ok := liberty.Sprite("airport", mapColor{Red: 1, Green: 2, Blue: 3, Alpha: 255}, 1)
	require.True(t, ok)
	assert.Same(t, &shared.Pixels[0], &sprite.pixels[0])
	assert.Equal(t, shared.Width, sprite.width)
	assert.Equal(t, shared.Height, sprite.height)
	assert.Equal(t, shared.PixelRatio, sprite.pixelRatio)
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

func TestAppendLibertyDiskProducesClosedOctagon(t *testing.T) {
	var triangles []roadPoint
	err := appendLibertyDisk(roadPoint{X: 4, Y: 5}, 2, func(first, second, third roadPoint) error {
		triangles = append(triangles, first, second, third)
		return nil
	})

	require.NoError(t, err)
	require.Len(t, triangles, libertyDiskSections*3)
	assert.Equal(t, triangles[1], triangles[len(triangles)-1])
	assert.InDelta(t, 8*math.Sqrt2, triangleMeshArea(triangles), 1e-12)
}

func TestLibertyExpressionsAreSupported(t *testing.T) {
	data, err := os.ReadFile("liberty/liberty_style.json")
	require.NoError(t, err)
	var document style.Document
	require.NoError(t, json.Unmarshal(data, &document))

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
		Exterior: []roadPoint{{X: 0, Y: 0}, {X: 32, Y: 0}, {X: 32, Y: 32}, {X: 0, Y: 32}},
		Vertices: backgroundTriangles(),
	}
	bucket := &tileBucket{
		tile: vectorTileID{X: 0, Y: 0, Z: 12},
		sourceLayers: map[string][]vectorFeature{
			"landcover": {{
				GeometryType: mvtPolygonType,
				Properties:   featureProperties{"class": "wetland"},
				Polygons:     []vectorPolygon{polygon},
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

func TestCompileLibertyTileDoesNotPublishFailedGeometry(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		bucket := &tileBucket{
			tile: vectorTileID{Z: 12}, compiled: true,
			liberty: []libertyRenderPrimitive{{layerID: "old"}},
			sourceLayers: map[string][]vectorFeature{"landcover": {{
				GeometryType: mvtPolygonType, Properties: featureProperties{"class": "wetland"},
				Polygons: []vectorPolygon{{Vertices: []roadPoint{{}, {}, {}}, Indices: []uint32{0, 1, 3}}},
			}}},
		}
		assert.ErrorIs(t, compileLibertyTileGeometry(bucket, 12, indexed), errFeatureResourceLimit)
		assert.False(t, bucket.compiled)
		assert.Nil(t, bucket.liberty, "earlier background geometry must not be published")
	}
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
		{value: "#f8f4f0", expected: mapColor{Red: 248, Green: 244, Blue: 240, Alpha: 255}},
		{value: "rgba(95, 208, 100, 0.5)", expected: mapColor{Red: 95, Green: 208, Blue: 100, Alpha: 128}},
		{value: "hsla(0, 0%, 100%, 0.25)", expected: mapColor{Red: 255, Green: 255, Blue: 255, Alpha: 64}},
	}
	for _, test := range tests {
		actual, ok := parseLibertyColor(test.value)
		assert.True(t, ok, test.value)
		assert.Equal(t, test.expected, actual, test.value)
	}
}
