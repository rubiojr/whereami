package compiler

import (
	"errors"
	"math"
	"os"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/mvt"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type emission struct {
	mesh           geometry.Mesh
	color          style.Color
	name           string
	scale, opacity float64
}

func collect(t *testing.T, features []mvt.Feature, layer style.CompiledLayer, options LayerOptions) []emission {
	t.Helper()
	var result []emission
	solid := func(mesh geometry.Mesh, color style.Color) error {
		result = append(result, emission{mesh: mesh, color: color})
		return nil
	}
	pattern := func(mesh geometry.Mesh, name string, scale, opacity float64) error {
		result = append(result, emission{mesh: mesh, name: name, scale: scale, opacity: opacity})
		return nil
	}
	var err error
	if layer.Kind == "line" {
		err = CompileLine(features, layer, options, solid)
	} else {
		err = CompileFill(features, layer, options, solid, pattern)
	}
	require.NoError(t, err)
	return result
}

func polygonFeature(properties mvt.Properties) mvt.Feature {
	points := []geometry.Point{{X: 1, Y: 1}, {X: 9, Y: 1}, {X: 1, Y: 9}}
	return mvt.Feature{GeometryType: mvt.PolygonType, Properties: properties,
		Polygons: []mvt.Polygon{{Exterior: points, Vertices: points}}}
}

func lineFeature(properties mvt.Properties) mvt.Feature {
	return mvt.Feature{GeometryType: mvt.LineStringType, Properties: properties,
		Lines: [][]geometry.Point{{{X: 0, Y: 0}, {X: 10, Y: 0}}}}
}

func TestFillOrderingPaintAndOwnership(t *testing.T) {
	layer := style.CompiledLayer{Kind: "fill", Filter: []any{"!=", []any{"get", "skip"}, true}, Paint: map[string]any{
		"fill-color": []any{"get", "color"}, "fill-pattern": []any{"get", "pattern"},
		"fill-opacity": 0.5, "fill-outline-color": "#0000ff",
	}}
	features := []mvt.Feature{
		polygonFeature(mvt.Properties{"pattern": "dots"}),
		polygonFeature(mvt.Properties{"color": "#ff0000"}),
		polygonFeature(mvt.Properties{"color": "#00ff00"}),
		polygonFeature(mvt.Properties{"color": "#ff0000"}),
		polygonFeature(mvt.Properties{"pattern": "dots"}),
		polygonFeature(mvt.Properties{"color": "invalid"}),
		polygonFeature(mvt.Properties{"skip": true}), lineFeature(nil),
	}
	got := collect(t, features, layer, LayerOptions{SourceZoom: 9, Zoom: 10})
	require.Len(t, got, 4)
	assert.Equal(t, style.Color{Red: 255, Alpha: 128}, got[0].color)
	assert.Equal(t, style.Color{Green: 255, Alpha: 128}, got[1].color)
	assert.Len(t, got[0].mesh.Vertices, 6)
	assert.Len(t, got[1].mesh.Vertices, 3)
	assert.Equal(t, "dots", got[2].name)
	assert.Equal(t, 0.5, got[2].scale)
	assert.Equal(t, 0.5, got[2].opacity)
	assert.Len(t, got[2].mesh.Vertices, 6)
	assert.Equal(t, style.Color{Blue: 255, Alpha: 128}, got[3].color)
	assert.NotEmpty(t, got[3].mesh.Vertices)
	got[0].mesh.Vertices[0].X = 999
	assert.Equal(t, float64(1), features[1].Polygons[0].Vertices[0].X)
	got[3].mesh.Vertices[0].X = 999
	assert.Equal(t, float64(1), features[0].Polygons[0].Exterior[0].X)
}

func TestExtrusionAndOutlineDefaults(t *testing.T) {
	layer := style.CompiledLayer{Kind: "fill-extrusion", Paint: map[string]any{
		"fill-extrusion-color": "#ff0000", "fill-extrusion-opacity": 0.25,
		"fill-color": "#00ff00", "fill-opacity": 0.5, "fill-outline-color": "#0000ff",
	}}
	feature := polygonFeature(nil)
	feature.Polygons[0].Holes = [][]geometry.Point{{{X: 2, Y: 2}, {X: 3, Y: 2}, {X: 2, Y: 3}}, nil}
	got := collect(t, []mvt.Feature{feature}, layer, LayerOptions{})
	require.Len(t, got, 2)
	assert.Equal(t, style.Color{Red: 255, Alpha: 64}, got[0].color)
	assert.Equal(t, style.Color{Blue: 255, Alpha: 128}, got[1].color)
	for _, outline := range []any{"bad", "transparent"} {
		layer.Paint["fill-outline-color"] = outline
		assert.Len(t, collect(t, []mvt.Feature{feature}, layer, LayerOptions{}), 1)
	}
}

func TestLineGapScaleAndOrdering(t *testing.T) {
	layer := style.CompiledLayer{Kind: "line", Paint: map[string]any{
		"line-color": []any{"get", "color"}, "line-width": 4.0, "line-gap-width": 8.0,
		"line-offset": 2.0, "line-opacity": 0.5,
	}}
	features := []mvt.Feature{lineFeature(mvt.Properties{"color": "#ff0000"}), lineFeature(mvt.Properties{"color": "#0000ff"}), lineFeature(mvt.Properties{"color": "#ff0000"})}
	got := collect(t, features, layer, LayerOptions{SourceZoom: 9, Zoom: 10})
	require.Len(t, got, 4)
	for i, offset := range []float64{-2, 4, -2, 4} {
		paths := features[0].Lines
		if i < 2 {
			paths = append(append([][]geometry.Point{}, paths...), paths...)
		}
		want, err := geometry.TessellateLines(paths, geometry.LineStyle{Width: 2, Offset: offset, Cap: "butt", Join: "miter"}, MaxTriangles, false)
		require.NoError(t, err)
		assert.Equal(t, want, got[i].mesh)
	}
	assert.Equal(t, style.Color{Red: 255, Alpha: 128}, got[0].color)
	assert.Equal(t, got[0].color, got[1].color)
	assert.Equal(t, style.Color{Blue: 255, Alpha: 128}, got[2].color)
	got[0].mesh.Vertices[0].X = 999
	assert.Zero(t, features[0].Lines[0][0].X)
}

func TestLineFilteringDashesAndPolygonPaths(t *testing.T) {
	layer := style.CompiledLayer{Kind: "line", Filter: []any{"!=", []any{"get", "skip"}, true}, Paint: map[string]any{
		"line-color": []any{"get", "color"}, "line-width": []any{"get", "width"},
		"line-dasharray": []any{2.0, 1.0},
	}, Layout: map[string]any{"line-cap": "round", "line-join": "round"}}
	feature := polygonFeature(mvt.Properties{"color": "#ff0000", "width": 2.0})
	feature.Polygons[0].Holes = [][]geometry.Point{{{X: 2, Y: 2}, {X: 3, Y: 2}, {X: 2, Y: 3}}}
	features := []mvt.Feature{lineFeature(mvt.Properties{"skip": true}), lineFeature(mvt.Properties{"color": "bad"}),
		lineFeature(mvt.Properties{"color": "transparent"}), lineFeature(mvt.Properties{"color": "#ff0000", "width": -1.0}), feature}
	got := collect(t, features, layer, LayerOptions{})
	require.Len(t, got, 1)
	paths := [][]geometry.Point{
		{{X: 1, Y: 1}, {X: 9, Y: 1}, {X: 1, Y: 9}, {X: 1, Y: 1}},
		{{X: 2, Y: 2}, {X: 3, Y: 2}, {X: 2, Y: 3}, {X: 2, Y: 2}},
	}
	want, err := geometry.TessellateLines(paths, geometry.LineStyle{Width: 2, Dashes: []float64{2, 1}, Cap: "round", Join: "round"}, MaxTriangles, false)
	require.NoError(t, err)
	assert.Equal(t, want, got[0].mesh)
	// Point features can retain empty batches; the parent filters empty emissions.
	got = collect(t, []mvt.Feature{{GeometryType: mvt.PointType}}, style.CompiledLayer{Kind: "line"}, LayerOptions{})
	require.Len(t, got, 1)
	assert.Empty(t, got[0].mesh.Vertices)
}

func TestCompilerOptionsAndFailurePolicy(t *testing.T) {
	sentinel := errors.New("sink stopped")
	noop := func(geometry.Mesh, style.Color) error { return nil }
	pattern := func(geometry.Mesh, string, float64, float64) error { return nil }
	for _, options := range []LayerOptions{{Zoom: math.NaN()}, {Zoom: math.Inf(1)}, {Zoom: math.Inf(-1)}, {Zoom: 2000}, {Zoom: -2000}, {TriangleLimit: -1}, {TriangleLimit: MaxTriangles + 1}} {
		assert.ErrorIs(t, CompileFill(nil, style.CompiledLayer{}, options, noop, pattern), ErrOptions)
		assert.ErrorIs(t, CompileLine(nil, style.CompiledLayer{}, options, noop), ErrOptions)
	}
	assert.ErrorIs(t, CompileFill(nil, style.CompiledLayer{}, LayerOptions{}, nil, pattern), ErrOptions)
	assert.ErrorIs(t, CompileFill(nil, style.CompiledLayer{}, LayerOptions{}, noop, nil), ErrOptions)
	assert.ErrorIs(t, CompileLine(nil, style.CompiledLayer{}, LayerOptions{}, nil), ErrOptions)
	layer := style.CompiledLayer{Paint: map[string]any{"fill-color": []any{"get", "color"}}}
	features := []mvt.Feature{polygonFeature(mvt.Properties{"color": "#ff0000"}), polygonFeature(mvt.Properties{"color": "#0000ff"})}
	calls := 0
	stop := func(geometry.Mesh, style.Color) error {
		calls++
		if calls == 2 {
			return sentinel
		}
		return nil
	}
	assert.ErrorIs(t, CompileFill(features, layer, LayerOptions{}, stop, pattern), sentinel)
	assert.Equal(t, 2, calls)
	layer.Paint = map[string]any{"fill-pattern": "dots"}
	assert.ErrorIs(t, CompileFill(features[:1], layer, LayerOptions{}, noop, func(geometry.Mesh, string, float64, float64) error { return sentinel }), sentinel)
	// Limit failures while accumulating fills emit nothing, even after valid input.
	for _, paint := range []map[string]any{nil, {"fill-pattern": "dots"}} {
		calls = 0
		layer.Paint = paint
		err := CompileFill(features, layer, LayerOptions{TriangleLimit: 1}, stop, pattern)
		assert.ErrorIs(t, err, mvt.ErrFeatureResourceLimit)
		assert.Zero(t, calls)
	}
	// Outline failure occurs after a valid solid emission; streaming is not atomic.
	calls = 0
	layer.Paint = map[string]any{"fill-outline-color": "#ff0000"}
	assert.ErrorIs(t, CompileFill(features[:1], layer, LayerOptions{TriangleLimit: 1}, stop, pattern), mvt.ErrFeatureResourceLimit)
	assert.Equal(t, 1, calls)
	assert.ErrorIs(t, CompileLine([]mvt.Feature{lineFeature(nil)}, style.CompiledLayer{}, LayerOptions{}, func(geometry.Mesh, style.Color) error { return sentinel }), sentinel)
	assert.ErrorIs(t, CompileLine([]mvt.Feature{lineFeature(nil)}, style.CompiledLayer{}, LayerOptions{TriangleLimit: 1}, noop), mvt.ErrFeatureResourceLimit)
}

func TestDashValuesAndPaintKeyCompatibility(t *testing.T) {
	for _, value := range []any{nil, "bad", []any{1.0, -1.0}, []any{1.0, true}} {
		layer := style.CompiledLayer{Paint: map[string]any{"dash": value}}
		assert.Nil(t, evaluatedNumbers(layer, "dash", style.Context{}))
	}
	assert.Nil(t, evaluatedNumbers(style.CompiledLayer{}, "dash", style.Context{}))
	values := []any{0.0, math.Copysign(0, -1), math.NaN(), math.Inf(1)}
	layer := style.CompiledLayer{Paint: map[string]any{"dash": values}}
	got := evaluatedNumbers(layer, "dash", style.Context{})
	require.Len(t, got, 4)
	assert.True(t, math.Signbit(got[1]))
	assert.True(t, math.IsNaN(got[2]))
	assert.Equal(t, "0,-0,NaN,+Inf,", numberListKey(got))
	got[0] = 5
	assert.Equal(t, 0.0, values[0])
	first := linePaint{width: 1.0000000001}
	second := linePaint{width: 1.0000000002}
	assert.Equal(t, linePaintKey(first), linePaintKey(second), "legacy nine-digit keys deliberately merge near-identical paint")
}

func TestPinnedLayersHeadless(t *testing.T) {
	path := os.Getenv("WHEREAMI_VECTOR_TILE_FIXTURE")
	if path == "" {
		t.Skip("set WHEREAMI_VECTOR_TILE_FIXTURE")
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	styleData, err := os.ReadFile("../liberty_style.json")
	require.NoError(t, err)
	layers, err := style.Parse(styleData)
	require.NoError(t, err)
	expanded, err := mvt.DecodeTile(data, false)
	require.NoError(t, err)
	indexed, err := mvt.DecodeTile(data, true)
	require.NoError(t, err)
	for _, zoom := range []float64{10, 10.5} {
		for _, layer := range layers {
			if !layer.VisibleAt(zoom) || layer.Hidden() || (layer.Kind != "fill" && layer.Kind != "fill-extrusion" && layer.Kind != "line") {
				continue
			}
			want := collect(t, expanded.Layers[layer.SourceLayer], layer, LayerOptions{SourceZoom: 9, Zoom: zoom})
			got := collect(t, indexed.Layers[layer.SourceLayer], layer, LayerOptions{SourceZoom: 9, Zoom: zoom, Indexed: true})
			require.Len(t, got, len(want), layer.ID)
			for i := range got {
				assert.Equal(t, want[i].color, got[i].color)
				assert.Equal(t, want[i].name, got[i].name)
				assert.Equal(t, want[i].scale, got[i].scale)
				assert.Equal(t, want[i].opacity, got[i].opacity)
				require.Len(t, got[i].mesh.Indices, len(want[i].mesh.Vertices))
				for j, index := range got[i].mesh.Indices {
					p, q := want[i].mesh.Vertices[j], got[i].mesh.Vertices[index]
					require.Equal(t, math.Float64bits(p.X), math.Float64bits(q.X))
					require.Equal(t, math.Float64bits(p.Y), math.Float64bits(q.Y))
				}
			}
		}
	}
}

func TestIndexedFillWithoutFixture(t *testing.T) {
	feature := polygonFeature(nil)
	feature.Polygons[0].Vertices = append(feature.Polygons[0].Vertices, geometry.Point{X: 9, Y: 9})
	feature.Polygons[0].Indices = []uint32{0, 1, 2, 2, 1, 3}
	for _, paint := range []map[string]any{nil, {"fill-pattern": "dots"}} {
		layer := style.CompiledLayer{Paint: paint}
		expanded := collect(t, []mvt.Feature{feature}, layer, LayerOptions{})
		indexed := collect(t, []mvt.Feature{feature}, layer, LayerOptions{Indexed: true})
		require.Len(t, indexed, 1)
		require.Len(t, expanded, 1)
		require.Len(t, indexed[0].mesh.Indices, 6)
		for i, index := range indexed[0].mesh.Indices {
			assert.Equal(t, expanded[0].mesh.Vertices[i], indexed[0].mesh.Vertices[index])
		}
		indexed[0].mesh.Indices[0] = 3
		assert.Zero(t, feature.Polygons[0].Indices[0])
	}
}

func FuzzLayerCompilation(f *testing.F) {
	f.Add([]byte{1, 2, 3, 4}, 2.0, 1.0, 0.0, 10.0, false)
	f.Add([]byte{255, 0}, math.MaxFloat64, math.NaN(), math.Inf(1), -2000.0, true)
	f.Fuzz(func(t *testing.T, data []byte, width, opacity, offset, zoom float64, indexed bool) {
		if len(data) > 64 {
			return
		}
		features := make([]mvt.Feature, 0, len(data))
		for _, value := range data {
			properties := mvt.Properties{"color": "#ff0000"}
			if value%2 == 0 {
				properties["color"] = "#0000ff"
			}
			feature := lineFeature(properties)
			if value%3 == 0 {
				feature = polygonFeature(properties)
			}
			features = append(features, feature)
		}
		options := LayerOptions{SourceZoom: 9, Zoom: zoom, Indexed: indexed, TriangleLimit: 256}
		check := func(mesh geometry.Mesh, _ style.Color) error {
			count := len(mesh.Vertices)
			if mesh.Indices != nil {
				count = len(mesh.Indices)
				for _, index := range mesh.Indices {
					require.Less(t, int(index), len(mesh.Vertices))
				}
			}
			require.LessOrEqual(t, count, 256*3)
			require.Zero(t, count%3)
			return nil
		}
		line := style.CompiledLayer{Paint: map[string]any{"line-color": []any{"get", "color"}, "line-width": width, "line-opacity": opacity, "line-offset": offset}}
		fill := style.CompiledLayer{Paint: map[string]any{"fill-color": []any{"get", "color"}, "fill-opacity": opacity}}
		for _, err := range []error{
			CompileLine(features, line, options, check),
			CompileFill(features, fill, options, check, func(mesh geometry.Mesh, _ string, _, _ float64) error { return check(mesh, style.Color{}) }),
		} {
			if err != nil {
				require.True(t, errors.Is(err, ErrOptions) || errors.Is(err, mvt.ErrFeatureResourceLimit))
			}
		}
	})
}
