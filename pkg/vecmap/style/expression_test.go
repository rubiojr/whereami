package style

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvaluate(t *testing.T) {
	context := Context{Zoom: 12, GeometryType: 2, Properties: map[string]any{"class": "primary", "rank": int64(2)}}
	tests := []struct {
		name       string
		expression any
		want       any
		ok         bool
	}{
		{"property", []any{"get", "class"}, "primary", true},
		{"missing", []any{"get", "missing"}, nil, true},
		{"has", []any{"has", "rank"}, true, true},
		{"hasMissing", []any{"has", "missing"}, false, true},
		{"badHasArity", []any{"has"}, false, true},
		{"badHasKey", []any{"has", 12}, false, true},
		{"badGetArity", []any{"get"}, nil, false},
		{"badGetKey", []any{"get", 12}, nil, false},
		{"filter", []any{"all", []any{"==", []any{"geometry-type"}, "LineString"}, []any{"<", []any{"get", "rank"}, float64(3)}}, true, true},
		{"allShortCircuit", []any{"all", false, []any{"get"}}, false, true},
		{"allFailure", []any{"all", []any{"get"}}, false, false},
		{"comparisonFailure", []any{"==", []any{"get"}, true}, false, true},
		{"matchList", []any{"match", []any{"get", "class"}, []any{"primary", "secondary"}, "road", "other"}, "road", true},
		{"matchScalar", []any{"match", "primary", "primary", "road", "other"}, "road", true},
		{"matchFallback", []any{"match", []any{"get", "missing"}, []any{"bridge", "tunnel"}, false, true}, true, true},
		{"badMatch", []any{"match"}, nil, false},
		{"case", []any{"case", false, "no", true, "yes", "fallback"}, "yes", true},
		{"caseFallback", []any{"case", []any{"get"}, "no", "fallback"}, "fallback", true},
		{"bareCaseLiteral", []any{"case"}, []any{"case"}, true},
		{"coalesceEmpty", []any{"coalesce", "", "fallback"}, "", true},
		{"coalesceMissing", []any{"coalesce", []any{"get", "missing"}, "fallback"}, "fallback", true},
		{"coalesceFailure", []any{"coalesce", nil, []any{"get"}}, nil, false},
		{"concat", []any{"concat", "rank=", []any{"get", "rank"}, []any{"get"}, true}, "rank=2true", true},
		{"string", []any{"to-string", []any{"zoom"}}, "12", true},
		{"missingOperand", []any{"to-string"}, "", false},
		{"step", []any{"step", []any{"zoom"}, "small", float64(10), "medium", float64(14), "large"}, "medium", true},
		{"interpolate", []any{"interpolate", []any{"linear"}, []any{"zoom"}, float64(10), float64(2), float64(14), float64(6)}, float64(4), true},
		{"missingInequality", []any{"!=", []any{"get", "brunnel"}, "tunnel"}, true, true},
		{"literal", 42, 42, true},
		{"emptyArray", []any{}, []any{}, true},
		{"numericArray", []any{1, 2}, []any{1, 2}, true},
		{"fontArray", []any{"Noto Sans Regular", "Noto Sans Bold"}, []any{"Noto Sans Regular", "Noto Sans Bold"}, true},
		{"unknownMixedArray", []any{"unknown", true}, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, ok := Evaluate(tt.expression, context)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, value)
		})
	}
	for code, want := range []string{"Unknown", "Point", "LineString", "Polygon", "Unknown"} {
		value, ok := Evaluate([]any{"geometry-type"}, Context{GeometryType: uint32(code)})
		require.True(t, ok)
		assert.Equal(t, want, value)
	}
}

func TestDepthAndBorrowedValues(t *testing.T) {
	var expression any = true
	for range MaxExpressionDepth - 1 {
		expression = []any{"!", expression}
	}
	_, ok := Evaluate(expression, Context{})
	require.True(t, ok)
	expression = []any{"!", expression}
	_, ok = Evaluate(expression, Context{})
	assert.False(t, ok)
	cycle := []any{"!", nil}
	cycle[1] = cycle
	_, ok = Evaluate(cycle, Context{})
	assert.False(t, ok)
	fonts := []any{"Noto Sans Regular"}
	properties := map[string]any{"fonts": fonts}
	result, ok := Evaluate([]any{"get", "fonts"}, Context{Properties: properties})
	require.True(t, ok)
	assert.Same(t, &fonts[0], &result.([]any)[0])
	assert.Equal(t, []any{"Noto Sans Regular"}, fonts)
	result, ok = Evaluate(fonts, Context{})
	require.True(t, ok)
	assert.Same(t, &fonts[0], &result.([]any)[0])
}

func TestInterpolationAndStepBoundaries(t *testing.T) {
	for _, tt := range []struct {
		input float64
		want  float64
	}{{-1, 2}, {0, 2}, {2, 4}, {4, 6}, {5, 6}} {
		value, ok := Evaluate([]any{"interpolate", []any{"linear"}, tt.input, 0, 2, 4, 6}, Context{})
		require.True(t, ok)
		number, valid := Number(value)
		require.True(t, valid)
		assert.Equal(t, tt.want, number)
	}
	value, ok := Evaluate([]any{"interpolate", []any{"exponential", 2}, 1, 0, 0, 2, 9}, Context{})
	require.True(t, ok)
	assert.Equal(t, float64(3), value)
	value, ok = Evaluate([]any{"interpolate", []any{"linear"}, 1, 0, "#000", 2, "#fff"}, Context{})
	require.True(t, ok)
	assert.Equal(t, Color{Red: 128, Green: 128, Blue: 128, Alpha: 255}, value)
	assert.Equal(t, 0.0, libertyInterpolationFactor(2, 1, 1, 1))
	assert.Equal(t, "first", libertyInterpolateValue("first", "second", .49))
	assert.Equal(t, "second", libertyInterpolateValue("first", "second", .5))
	for _, expression := range [][]any{
		{"interpolate"}, {"interpolate", []any{"linear"}, "bad", 0, 0, 1, 1},
		{"interpolate", []any{"linear"}, 1, 0, 0, "bad", 1},
		{"step"}, {"step", "bad", 0},
	} {
		_, ok := Evaluate(expression, Context{})
		assert.False(t, ok)
	}
	value, ok = Evaluate([]any{"step", 3, 0, 1, 5, 2, 9}, Context{})
	require.True(t, ok)
	assert.Equal(t, 9, value)
	value, ok = Evaluate([]any{"step", 3, 0, "invalid", 5}, Context{})
	require.True(t, ok)
	assert.Equal(t, 0, value)
}

func TestComparisons(t *testing.T) {
	for _, values := range [][2]any{{float64(1), int64(2)}, {"a", "b"}} {
		for _, tt := range []struct {
			operator string
			want     bool
		}{
			{"==", false}, {"!=", true}, {"<", true}, {"<=", true}, {">", false}, {">=", false},
		} {
			value, ok := Evaluate([]any{tt.operator, values[0], values[1]}, Context{})
			require.True(t, ok)
			assert.Equal(t, tt.want, value)
		}
	}
	assert.True(t, libertyCompare("==", nil, nil))
	assert.True(t, libertyCompare("==", true, true))
	assert.True(t, libertyCompare("!=", nil, false))
	assert.False(t, libertyCompare("==", math.NaN(), math.NaN()))
	assert.True(t, libertyCompare("!=", math.NaN(), math.NaN()))
	// These values previously panicked at interface equality. This is scalar
	// comparison, not deep equality, including composites containing interfaces.
	for _, value := range []any{[]any{1}, map[string]any{"x": 1}, struct{ X any }{X: []int{1}}} {
		for _, op := range []string{"==", "!="} {
			got, ok := Evaluate([]any{op, []any{"get", "x"}, []any{"get", "x"}}, Context{Properties: map[string]any{"x": value}})
			require.True(t, ok)
			assert.Equal(t, op == "!=", got)
		}
	}
}

func TestPrimitiveConversions(t *testing.T) {
	for _, value := range []any{float64(2), float32(2), int(2), int64(2), uint64(2), json.Number("2")} {
		number, ok := Number(value)
		require.True(t, ok)
		assert.Equal(t, float64(2), number)
	}
	_, ok := Number(json.Number("invalid"))
	assert.False(t, ok)
	_, ok = Number("2")
	assert.False(t, ok)
	for _, tt := range []struct {
		value any
		want  string
	}{{"text", "text"}, {float64(2.5), "2.5"}, {true, "true"}, {nil, ""}, {int64(2), "2"}} {
		assert.Equal(t, tt.want, String(tt.value))
	}
	for _, value := range []any{false, nil, "", float64(0)} {
		assert.False(t, Truthy(value))
	}
	for _, value := range []any{true, "x", float64(1), int64(0), []any{}} {
		assert.True(t, Truthy(value))
	}
}

func FuzzEvaluateJSON(f *testing.F) {
	f.Add([]byte(`["==", {}, {}]`))
	f.Add([]byte(`["interpolate", ["linear"], ["zoom"], 10, 2, 14, 6]`))
	f.Add([]byte(`["all", ["has", "rank"], ["<", ["get", "rank"], 4]]`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			return
		}
		var expression any
		if json.Unmarshal(data, &expression) != nil {
			return
		}
		Evaluate(expression, Context{Zoom: 12, GeometryType: 2, Properties: map[string]any{"rank": int64(2)}})
	})
}

func BenchmarkEvaluateFilter(b *testing.B) {
	var expression any = []any{"all", []any{"==", []any{"geometry-type"}, "LineString"}, []any{"<", []any{"get", "rank"}, float64(3)}}
	context := Context{GeometryType: 2, Properties: map[string]any{"rank": int64(2)}}
	b.ReportAllocs()
	for b.Loop() {
		value, ok := Evaluate(expression, context)
		if !ok || value != true {
			b.Fatal("unexpected evaluation")
		}
	}
}
