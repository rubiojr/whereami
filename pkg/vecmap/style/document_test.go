package style

import (
	"encoding/json"
	"math"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLayers(t *testing.T) {
	data := []byte(`{"version":8,"sources":{"ignored":{}},"layers":[
		{"id":"duplicate","type":"unknown","source-layer":"roads","paint":{"width":["zoom"]}},
		{"id":"duplicate","type":"line","minzoom":2,"maxzoom":4,"filter":["==",["get","class"],"primary"],"layout":{"visibility":"none"}}
	]}`)
	layers, err := Parse(data)
	require.NoError(t, err)
	require.Len(t, layers, 2)
	clear(data) // Parsed expressions and metadata must not borrow the input bytes.
	assert.Equal(t, "duplicate", layers[0].ID)
	assert.Equal(t, "duplicate", layers[1].ID)
	assert.Equal(t, "unknown", layers[0].Kind)
	assert.Equal(t, "roads", layers[0].SourceLayer)
	assert.Equal(t, 0, layers[0].Order)
	assert.Equal(t, 1, layers[1].Order)
	assert.Equal(t, float64(0), layers[0].MinZoom)
	assert.True(t, math.IsInf(layers[0].MaxZoom, 1))
	assert.True(t, layers[0].Matches(Context{}))
	assert.True(t, layers[1].Matches(Context{Properties: MapProperties{"class": "primary"}}))
	assert.False(t, layers[1].Matches(Context{Properties: MapProperties{"class": "secondary"}}))
	assert.False(t, layers[1].Matches(Context{}))
	assert.True(t, layers[1].Hidden())
	value, ok := layers[0].Value("width", Context{Zoom: 3.5})
	assert.True(t, ok)
	assert.Equal(t, 3.5, value)
	for _, tt := range []struct {
		zoom    float64
		visible bool
	}{
		{1.999, false}, {2, true}, {3.999, true}, {4, false},
		{math.NaN(), false}, {math.Inf(1), false}, {math.Inf(-1), false},
	} {
		assert.Equal(t, tt.visible, layers[1].VisibleAt(tt.zoom))
	}
	layers, err = Parse([]byte(`{"version":8}`))
	require.NoError(t, err)
	assert.NotNil(t, layers)
	assert.Empty(t, layers)
}

func TestCompilationErrorsAreAtomic(t *testing.T) {
	for _, data := range []string{`{`, `null`, `{"version":7}`, `{"version":8,"layers":[{"minzoom":"bad"}]}`, `{"version":8} {}`} {
		layers, err := Parse([]byte(data))
		assert.Error(t, err)
		assert.Nil(t, layers)
	}
	for _, section := range []string{"paint", "layout"} {
		t.Run(section, func(t *testing.T) {
			bad := Layer{ID: "bad"}
			raw := map[string]json.RawMessage{"width": json.RawMessage(`[`)}
			if section == "paint" {
				bad.Paint = raw
			} else {
				bad.Layout = raw
			}
			layers, err := Compile(Document{Version: 8, Layers: []Layer{{ID: "good"}, bad}})
			assert.ErrorContains(t, err, "decode layer bad "+section+" width")
			var syntax *json.SyntaxError
			assert.ErrorAs(t, err, &syntax)
			assert.Nil(t, layers)
		})
	}
}

func TestCompileOwnership(t *testing.T) {
	minZoom, maxZoom := 2.0, 5.0
	filter := []any{"has", "class"}
	raw := json.RawMessage(`["Noto Sans Regular"]`)
	document := Document{Version: 8, Layers: []Layer{{
		MinZoom: &minZoom, MaxZoom: &maxZoom, Filter: filter,
		Layout: map[string]json.RawMessage{"text-font": raw},
	}}}
	layers, err := Compile(document)
	require.NoError(t, err)
	minZoom, maxZoom = 10, 20
	clear(raw)
	delete(document.Layers[0].Layout, "text-font")
	assert.Equal(t, 2.0, layers[0].MinZoom)
	assert.Equal(t, 5.0, layers[0].MaxZoom)
	fonts, ok := layers[0].Value("text-font", Context{})
	assert.True(t, ok)
	assert.Equal(t, []any{"Noto Sans Regular"}, fonts)
	assert.Same(t, &filter[0], &layers[0].Filter.([]any)[0])
	assert.Same(t, &layers[0].Layout["text-font"].([]any)[0], &fonts.([]any)[0])
}

func TestLayerEvaluationPrecedence(t *testing.T) {
	layer := CompiledLayer{
		Paint:  map[string]any{"null": nil, "invalid": []any{"unknown", true}, "shared": "paint"},
		Layout: map[string]any{"null": "fallback", "invalid": "fallback", "shared": "layout", "only-layout": []any{"geometry-type"}},
	}
	for _, tt := range []struct {
		name string
		want any
		ok   bool
	}{
		{"null", nil, true}, {"invalid", nil, false}, {"shared", "paint", true},
		{"only-layout", "LineString", true}, {"missing", nil, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			value, ok := layer.Value(tt.name, Context{GeometryType: 2})
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, value)
		})
	}
	layer.Filter = []any{"unknown", true}
	assert.False(t, layer.Matches(Context{}))
	layer.Filter = float64(0)
	assert.False(t, layer.Matches(Context{}))
	layer.Filter = "truthy"
	assert.True(t, layer.Matches(Context{}))
	assert.False(t, layer.Hidden())
	for _, visibility := range []any{nil, "visible", []any{"none"}, map[string]any{}, false} {
		layer.Layout["visibility"] = visibility
		assert.False(t, layer.Hidden())
	}
	layer.Layout["visibility"] = "none"
	assert.True(t, layer.Hidden())
}

func TestPinnedDocumentHeadless(t *testing.T) {
	// Reuse the caller's pinned asset without embedding a second production copy.
	data, err := os.ReadFile("../liberty/liberty_style.json")
	require.NoError(t, err)
	layers, err := Parse(data)
	require.NoError(t, err)
	require.Len(t, layers, 111)
	assert.Equal(t, "background", layers[0].ID)
	assert.Equal(t, "label_country_1", layers[110].ID)
	for order, layer := range layers {
		assert.Equal(t, order, layer.Order)
	}
}
