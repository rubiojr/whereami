package style

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTypedLayerValues(t *testing.T) {
	layer := CompiledLayer{Paint: map[string]any{
		"number": 2.5, "string": "", "bool": true, "wrong": map[string]any{},
		"nan": math.NaN(), "inf": math.Inf(1), "color": "#01020304", "bad-color": "invalid",
		"failed": []any{"unknown", true},
	}}
	ctx := Context{}
	assert.Equal(t, 2.5, layer.NumberValue("number", ctx, 7))
	for _, name := range []string{"missing", "wrong", "string", "nan", "inf", "failed"} {
		assert.Equal(t, 7.0, layer.NumberValue(name, ctx, 7), name)
	}
	assert.Equal(t, "", layer.StringValue("string", ctx, "fallback"))
	assert.Equal(t, "fallback", layer.StringValue("missing", ctx, "fallback"))
	assert.Equal(t, "fallback", layer.StringValue("number", ctx, "fallback"))
	assert.True(t, layer.BoolValue("bool", ctx, false))
	assert.True(t, layer.BoolValue("missing", ctx, true))
	assert.False(t, layer.BoolValue("number", ctx, false))
	fallback := Color{Red: 9, Alpha: 255}
	color, ok := layer.ColorValue("color", ctx, fallback)
	assert.True(t, ok)
	assert.Equal(t, Color{Red: 1, Green: 2, Blue: 3, Alpha: 4}, color)
	for _, name := range []string{"missing", "failed"} {
		color, ok = layer.ColorValue(name, ctx, fallback)
		assert.True(t, ok)
		assert.Equal(t, fallback, color)
	}
	color, ok = layer.ColorValue("bad-color", ctx, fallback)
	assert.False(t, ok)
	assert.Equal(t, Color{}, color)
}

func TestNumberArrayValuesRetainComponentSemantics(t *testing.T) {
	values := []any{float64(-1), math.Inf(1), math.NaN()}
	layer := CompiledLayer{Layout: map[string]any{"offset": []any{"get", "value"}}}
	ctx := Context{Properties: map[string]any{"value": values}}
	got := layer.NumberArrayValue("offset", ctx)
	require.Len(t, got, 3)
	assert.Equal(t, -1.0, got[0])
	assert.True(t, math.IsInf(got[1], 1))
	assert.True(t, math.IsNaN(got[2]))
	got[0] = 99
	assert.Equal(t, -1.0, values[0])
	assert.Nil(t, layer.NumberArrayValue("missing", ctx))
	for _, value := range []any{nil, "bad", []float64{1, 2}, []any{1.0, "bad"}} {
		ctx.Properties["value"] = value
		assert.Nil(t, layer.NumberArrayValue("offset", ctx))
	}
	ctx.Properties["value"] = []any{}
	assert.NotNil(t, layer.NumberArrayValue("offset", ctx))
	assert.Empty(t, layer.NumberArrayValue("offset", ctx))
}

func TestFontStackValues(t *testing.T) {
	layer := CompiledLayer{Layout: map[string]any{}}
	for _, value := range []any{nil, "font", []any{}, []any{1.0, "  "}} {
		layer.Layout["text-font"] = value
		family, stack := layer.FontStack(Context{})
		assert.Equal(t, "Noto Sans Regular", family)
		assert.Equal(t, family, stack)
	}
	delete(layer.Layout, "text-font")
	family, stack := layer.FontStack(Context{})
	assert.Equal(t, "Noto Sans Regular", family)
	assert.Equal(t, family, stack)
	layer.Layout["text-font"] = []any{"get", "fonts"}
	family, stack = layer.FontStack(Context{Properties: map[string]any{"fonts": []any{" A ", 1.0, "", "B", "A"}}})
	assert.Equal(t, "A", family)
	assert.Equal(t, "A,B,A", stack)
}
