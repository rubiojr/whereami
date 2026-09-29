package compiler

import (
	"errors"
	"math"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/mvt"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func dashedLayers() ([]style.CompiledLayer, map[string][]mvt.Feature) {
	layers := tileLayers("line", "line", "line", "line", "line", "line", "line", "line")
	width := []any{"interpolate", []any{"linear"}, []any{"zoom"}, 8.0, 1.0, 12.0, 9.0}
	for i, layer := range []struct {
		id    string
		paint map[string]any
		cap   string
	}{
		{"plain", map[string]any{"line-width": width}, ""},
		{"dashed", map[string]any{"line-color": "#ff0000", "line-width": width, "line-dasharray": []any{2.0, 1.0}}, ""},
		{"odd", map[string]any{"line-width": width, "line-dasharray": []any{3.0}}, "unknown"},
		{"round", map[string]any{"line-width": width, "line-dasharray": []any{2.0, 1.0}}, "round"},
		{"square", map[string]any{"line-width": width, "line-dasharray": []any{2.0, 1.0}}, "square"},
		{"offset", map[string]any{"line-width": width, "line-dasharray": []any{2.0, 1.0}, "line-offset": 3.0}, ""},
		{"long", map[string]any{"line-width": width, "line-dasharray": []any{1.0, 1.0, 2.0, 1.0, 3.0, 1.0}}, ""},
		{"gapfirst", map[string]any{"line-width": width, "line-dasharray": []any{0.0, 2.0}}, ""},
	} {
		layers[i].ID, layers[i].Paint = layer.id, layer.paint
		if layer.cap != "" {
			layers[i].Layout = map[string]any{"line-cap": layer.cap}
		}
	}
	bend := mvt.Feature{GeometryType: mvt.LineStringType, Lines: [][]geometry.Point{{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 14, Y: 9}}}}
	return layers, map[string][]mvt.Feature{"source": {bend}}
}

func compileDashed(t *testing.T, options LayerOptions) map[string]Primitive {
	t.Helper()
	layers, sources := dashedLayers()
	result := map[string]Primitive{}
	require.NoError(t, CompileTile(layers, sources, options, func(p Primitive) error {
		require.NotContains(t, result, p.LayerID)
		result[p.LayerID] = p
		return nil
	}, nil))
	return result
}

func TestShaderDashedTileLines(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		options := LayerOptions{SourceZoom: 9, Zoom: 10, Indexed: indexed}
		baked := compileDashed(t, options)
		// A first dash of zero never draws.
		require.Len(t, baked, 7)
		require.NotContains(t, baked, "gapfirst")
		for _, p := range baked {
			assert.Nil(t, p.Distances, "the existing output is unchanged")
			assert.Zero(t, p.DashUnit)
			assert.Zero(t, p.Dashes)
		}
		for _, extrude := range []bool{false, true} {
			options.ExtrudeLines, options.ShaderDashes, options.Zoom = extrude, true, 10
			dashed := compileDashed(t, options)
			require.Len(t, dashed, len(baked))
			options.Zoom = 10.0625
			next := compileDashed(t, options)
			for id, p := range dashed {
				assert.Equal(t, baked[id].Order, p.Order)
				assert.Equal(t, baked[id].Color, p.Color)
				switch id {
				case "dashed", "odd":
					want, err := geometry.TessellateDashedLines([][]geometry.Point{{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 14, Y: 9}}}, 16, indexed)
					require.NoError(t, err)
					require.Len(t, p.Mesh.Vertices, len(want.Vertices))
					assert.Equal(t, want.Indices, p.Mesh.Indices)
					for i, vertex := range want.Vertices {
						assert.Equal(t, vertex, geometry.DashedVertex{Anchor: p.Mesh.Vertices[i], Direction: p.Directions[i], Distance: p.Distances[i]})
					}
					assert.False(t, p.Dynamic)
					assert.Equal(t, 2.5, p.HalfWidth, "half of the width interpolated at zoom 10")
					assert.Equal(t, 2.5, p.DashUnit, "the width in tile units at twice the source scale")
					if id == "odd" {
						assert.Equal(t, geometry.DashPattern{3, 3}, p.Dashes)
					} else {
						assert.Equal(t, geometry.DashPattern{2, 1}, p.Dashes)
					}
					// Another style zoom changes the width and the dash unit only.
					assert.Equal(t, p.Mesh, next[id].Mesh)
					assert.Equal(t, p.Directions, next[id].Directions)
					assert.Equal(t, p.Distances, next[id].Distances)
					assert.Equal(t, p.Dashes, next[id].Dashes)
					assert.InDelta(t, 2.5625, next[id].HalfWidth, 1e-12)
					assert.InDelta(t, 5.125/math.Exp2(1.0625), next[id].DashUnit, 1e-12)
				case "plain":
					assert.Nil(t, p.Distances)
					assert.Equal(t, extrude, p.Directions != nil)
				default:
					assert.Equal(t, baked[id].Mesh, p.Mesh, "%s keeps its baked geometry", id)
					assert.Nil(t, p.Distances)
					assert.Nil(t, p.Directions)
					assert.Equal(t, extrude, p.Dynamic)
					assert.NotEqual(t, p.Mesh.Vertices, next[id].Mesh.Vertices)
				}
			}
		}
	}
}

func TestShaderDashedTileLineErrors(t *testing.T) {
	layers, sources := dashedLayers()
	sentinel := errors.New("sink failed")
	err := CompileTile(layers[1:2], sources, LayerOptions{ShaderDashes: true}, func(Primitive) error { return sentinel }, nil)
	assert.ErrorIs(t, err, sentinel)
	// Two quads need four triangles.
	err = CompileTile(layers[1:2], sources, LayerOptions{ShaderDashes: true, TriangleLimit: 3}, func(Primitive) error { return nil }, nil)
	assert.ErrorIs(t, err, mvt.ErrFeatureResourceLimit)
	require.NoError(t, CompileTile(layers[1:2], sources, LayerOptions{ShaderDashes: true, TriangleLimit: 4}, func(Primitive) error { return nil }, nil))
	// The baked walk of the same line is limited by its dash count.
	err = CompileTile(layers[1:2], sources, LayerOptions{TriangleLimit: 4}, func(Primitive) error { return nil }, nil)
	assert.ErrorIs(t, err, mvt.ErrFeatureResourceLimit)
}

func dashedPrimitive(indexed bool) Primitive {
	mesh, _ := geometry.TessellateDashedLines([][]geometry.Point{{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 5}}}, 16, indexed)
	p := Primitive{Order: 2, Color: style.Color{Red: 255, Alpha: 255}, HalfWidth: 1.5, DashUnit: 0.75, Dashes: geometry.DashPattern{2, 1, 0.5, 4}}
	for _, vertex := range mesh.Vertices {
		p.Mesh.Vertices = append(p.Mesh.Vertices, vertex.Anchor)
		p.Directions = append(p.Directions, vertex.Direction)
		p.Distances = append(p.Distances, vertex.Distance)
	}
	p.Mesh.Indices = mesh.Indices
	return p
}

func TestDashedPacking(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		fill, _, baked := splitPrimitives(indexed)
		dashed := dashedPrimitive(indexed)
		for _, split := range []bool{false, true} {
			pack := func(p Primitive) *scene.Scene {
				b := NewFragmentBuilder(indexed, 0, 0)
				if split {
					b.Split()
				}
				for _, primitive := range []Primitive{fill, p, baked} {
					b.Primitive(view.TileID{}, primitive, nil)
				}
				result, sources, err := b.Finish()
				require.NoError(t, err)
				require.Len(t, sources, 3)
				assert.Equal(t, DrawSource{Layer: 2}, sources[1])
				return result
			}
			result := pack(dashed)
			draw := result.Draws[1]
			assert.Equal(t, uint64(StableMesh), draw.Mesh, "shader dashes are zoom-stable geometry")
			assert.Equal(t, scene.Material{Kind: scene.Dashed, Color: [4]float32{1, 0, 0, 1}, MapAligned: true, OffsetScale: 1.5, DashUnit: 0.75, Dashes: [4]float32{2, 1, 0.5, 4}}, draw.Material)
			assert.Equal(t, [4]float32{0, 0, view.TileSize, view.TileSize}, draw.Clip)
			assert.Equal(t, uint32(12), draw.Count)
			mesh := result.Meshes[0]
			elements := meshElements(mesh)[draw.First : draw.First+draw.Count]
			assert.Equal(t, scene.Vertex{X: 0, Y: 0, OffsetX: 0, OffsetY: 1}, elements[0])
			assert.Equal(t, scene.Vertex{X: 10, Y: 0, OffsetX: 0, OffsetY: 1, U: 10}, elements[2])
			assert.Equal(t, scene.Vertex{X: 10, Y: 5, OffsetX: 1, OffsetY: 0, U: 15}, elements[11])
			// Width, dash unit and pattern are draw state.
			wider := dashed
			wider.HalfWidth, wider.DashUnit, wider.Dashes = 4, 2, geometry.DashPattern{1, 1}
			changed := pack(wider)
			assert.Equal(t, result.Meshes[0], changed.Meshes[0])
			assert.Equal(t, scene.Material{Kind: scene.Dashed, Color: [4]float32{1, 0, 0, 1}, MapAligned: true, OffsetScale: 4, DashUnit: 2, Dashes: [4]float32{1, 1}}, changed.Draws[1].Material)
		}
	}
}

func TestDashedPackingRejections(t *testing.T) {
	dashed := dashedPrimitive(false)
	for name, change := range map[string]func(*Primitive){
		"distances":  func(p *Primitive) { p.Distances = p.Distances[:1] },
		"directions": func(p *Primitive) { p.Directions = p.Directions[:1] },
		"half width": func(p *Primitive) { p.HalfWidth = 0 },
		"zero unit":  func(p *Primitive) { p.DashUnit = 0 },
		"nan unit":   func(p *Primitive) { p.DashUnit = math.NaN() },
		"huge unit":  func(p *Primitive) { p.DashUnit = math.Inf(1) },
		"first dash": func(p *Primitive) { p.Dashes[0] = 0 },
		"nan dash":   func(p *Primitive) { p.Dashes[0] = math.NaN() },
	} {
		bad := dashed
		change(&bad)
		b := NewFragmentBuilder(false, 0, 0)
		b.Primitive(view.TileID{}, bad, nil)
		_, _, err := b.Finish()
		assert.ErrorIs(t, err, ErrPackingInput, name)
	}
	bad := dashed
	bad.Dashes[1] = -1
	b := NewFragmentBuilder(false, 0, 0)
	b.Primitive(view.TileID{}, bad, nil)
	_, _, err := b.Finish()
	assert.Error(t, err, "a negative gap fails final validation")
	b = NewFragmentBuilder(false, len(dashed.Mesh.Vertices)-1, 0)
	b.Primitive(view.TileID{}, dashed, nil)
	_, _, err = b.Finish()
	assert.ErrorIs(t, err, geometry.ErrGeometryLimit)
	closed := NewSceneBuilder(false, 0)
	_, err = closed.finish(true)
	require.NoError(t, err)
	closed.Dashed(dashed.Mesh, dashed.Directions, dashed.Distances, 1, 1, dashed.Dashes, scene.Material{}, [4]float32{})
	_, err = closed.Finish()
	assert.ErrorIs(t, err, ErrPackingClosed)
}
