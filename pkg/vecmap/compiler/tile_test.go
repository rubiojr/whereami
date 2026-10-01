package compiler

import (
	"errors"
	"math"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/mvt"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tileLayers(kinds ...string) []style.CompiledLayer {
	layers := make([]style.CompiledLayer, len(kinds))
	for i, kind := range kinds {
		layers[i] = style.CompiledLayer{Order: i, ID: kind, Kind: kind, SourceLayer: "source", MaxZoom: 20}
	}
	return layers
}

func TestTileTraversalAndPrimitiveMetadata(t *testing.T) {
	layers := tileLayers("background", "symbol", "fill", "fill-extrusion", "line", "unknown", "background", "symbol", "background")
	layers[0].Paint = map[string]any{"background-color": "#123456", "background-opacity": 0.5}
	layers[2].Paint = map[string]any{"fill-pattern": "dots", "fill-opacity": 0.25}
	layers[6].Layout = map[string]any{"visibility": "none"}
	layers[7].MinZoom = 11
	layers[8].MaxZoom = 10
	sources := map[string]mvt.FeatureSlice{"source": {polygonFeature(nil), lineFeature(nil)}}
	var sequence []int
	var primitives []Primitive
	err := CompileTile(layers, sources, LayerOptions{SourceZoom: 9, Zoom: 10}, func(p Primitive) error {
		sequence = append(sequence, p.Order)
		primitives = append(primitives, p)
		return nil
	}, func(layer style.CompiledLayer) error {
		sequence = append(sequence, layer.Order)
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, []int{0, 1, 2, 3, 4}, sequence)
	require.Len(t, primitives, 4)
	assert.Equal(t, "background", primitives[0].LayerID)
	assert.Equal(t, style.Color{Red: 18, Green: 52, Blue: 86, Alpha: 128}, primitives[0].Color)
	assert.Equal(t, "dots", primitives[1].PatternName)
	assert.Equal(t, 0.5, primitives[1].PatternScale)
	assert.Equal(t, 0.25, primitives[1].Opacity)
	primitives[1].Mesh.Vertices[0].X = 1000
	assert.Equal(t, float64(1), sources["source"][0].Polygons[0].Vertices[0].X)
}

func TestTileBudgetAndStreamingErrors(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		calls := 0
		emit := func(Primitive) error { calls++; return nil }
		layers := tileLayers("background", "symbol", "background")
		symbols := 0
		err := CompileTile[mvt.FeatureSlice](layers, nil, LayerOptions{Indexed: indexed, TriangleLimit: 3}, emit, func(style.CompiledLayer) error { symbols++; return nil })
		assert.ErrorIs(t, err, mvt.ErrFeatureResourceLimit)
		assert.Equal(t, 1, calls)
		assert.Equal(t, 1, symbols)
		calls = 0
		require.NoError(t, CompileTile[mvt.FeatureSlice](layers, nil, LayerOptions{Indexed: indexed, TriangleLimit: 4}, emit, nil))
		assert.Equal(t, 2, calls)
	}
	sentinel := errors.New("sink failed")
	layers := tileLayers("background", "symbol", "background")
	calls := 0
	err := CompileTile[mvt.FeatureSlice](layers, nil, LayerOptions{}, func(Primitive) error { calls++; return nil }, func(style.CompiledLayer) error { return sentinel })
	assert.ErrorIs(t, err, sentinel)
	assert.Equal(t, 1, calls)
	assert.ErrorIs(t, CompileTile[mvt.FeatureSlice](layers, nil, LayerOptions{}, func(Primitive) error { return sentinel }, nil), sentinel)
	assert.ErrorIs(t, CompileTile[mvt.FeatureSlice](nil, nil, LayerOptions{}, nil, nil), ErrOptions)
	assert.ErrorIs(t, CompileTile[mvt.FeatureSlice](nil, nil, LayerOptions{Zoom: math.NaN()}, func(Primitive) error { return nil }, nil), ErrOptions)
}

func TestTileFilteringBeforeBudget(t *testing.T) {
	layers := tileLayers("background", "background", "fill", "line", "symbol", "background")
	layers[0].Paint = map[string]any{"background-color": "invalid"}
	layers[1].Paint = map[string]any{"background-opacity": 0.0}
	layers[2].Paint = map[string]any{"fill-pattern": "dots", "fill-opacity": -1.0}
	layers[3].SourceLayer = "missing"
	calls := 0
	err := CompileTile(layers, map[string]mvt.FeatureSlice{"source": {polygonFeature(nil)}}, LayerOptions{TriangleLimit: 2}, func(p Primitive) error {
		calls++
		assert.Equal(t, 5, p.Order)
		return nil
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, calls)
	// Across batch kinds and layers, not just within background geometry.
	layers = tileLayers("fill", "fill")
	layers[1].Paint = map[string]any{"fill-pattern": "dots"}
	err = CompileTile(layers, map[string]mvt.FeatureSlice{"source": {polygonFeature(nil)}}, LayerOptions{TriangleLimit: 1}, func(Primitive) error { return nil }, nil)
	assert.ErrorIs(t, err, mvt.ErrFeatureResourceLimit)
}

func TestAssemblyMalformedAndFilteredMeshes(t *testing.T) {
	a := tileAssembly{remaining: 2, limit: 2, emit: func(Primitive) error { return nil }}
	triangle := geometry.Mesh{Vertices: make([]geometry.Point, 3)}
	for _, p := range []Primitive{
		{}, {Mesh: triangle}, {Mesh: triangle, PatternName: "dots", Opacity: 0},
		{Mesh: triangle, Opacity: 1},
	} {
		require.NoError(t, a.append(p, true))
	}
	assert.Equal(t, 2, a.remaining)
	for _, mesh := range []geometry.Mesh{{Vertices: make([]geometry.Point, 2)}, {Vertices: make([]geometry.Point, 3), Indices: []uint32{0}}} {
		assert.Error(t, a.append(Primitive{Mesh: mesh, Color: style.Color{Alpha: 255}}, false))
		assert.Equal(t, 2, a.remaining)
	}
	require.NoError(t, a.append(Primitive{Mesh: triangle, Color: style.Color{Alpha: 255}}, false))
	assert.Equal(t, 1, a.remaining)
}

func TestBackgroundTopologyAndOwnership(t *testing.T) {
	expanded, indexed := BackgroundGeometry(false), BackgroundGeometry(true)
	assert.Equal(t, []geometry.Point{{X: 0, Y: 0}, {X: 256, Y: 0}, {X: 256, Y: 256}, {X: 0, Y: 0}, {X: 256, Y: 256}, {X: 0, Y: 256}}, expanded.Vertices)
	assert.Nil(t, expanded.Indices)
	assert.Equal(t, []uint32{0, 1, 2, 0, 2, 3}, indexed.Indices)
	for i, index := range indexed.Indices {
		assert.Equal(t, expanded.Vertices[i], indexed.Vertices[index])
	}
	expanded.Vertices[0].X = 1
	indexed.Vertices[0].X = 1
	indexed.Indices[0] = 3
	assert.Zero(t, BackgroundGeometry(false).Vertices[0].X)
	assert.Zero(t, BackgroundGeometry(true).Vertices[0].X)
	assert.Zero(t, BackgroundGeometry(true).Indices[0])
}
