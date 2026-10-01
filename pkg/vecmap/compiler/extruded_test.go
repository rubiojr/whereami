package compiler

import (
	"errors"
	"math"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/mvt"
	"github.com/rubiojr/whereami/pkg/vecmap/placement"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/sprite"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func extrusionLayers() ([]style.CompiledLayer, map[string]mvt.FeatureSlice) {
	layers := tileLayers("fill", "line", "line", "line", "line", "line")
	width := []any{"interpolate", []any{"linear"}, []any{"zoom"}, 8.0, 1.0, 12.0, 9.0}
	layers[0].Paint = map[string]any{"fill-color": "#102030", "fill-outline-color": "#405060"}
	layers[1].ID, layers[1].Paint = "plain", map[string]any{"line-color": "#ff0000", "line-width": width}
	layers[1].Layout = map[string]any{"line-cap": "round", "line-join": "round"}
	layers[2].ID, layers[2].Paint = "dashed", map[string]any{"line-width": width, "line-dasharray": []any{2.0, 1.0}}
	layers[3].ID, layers[3].Paint = "offset", map[string]any{"line-width": width, "line-offset": 3.0}
	layers[4].ID, layers[4].Paint = "gap", map[string]any{"line-width": width, "line-gap-width": 2.0}
	layers[5].ID, layers[5].Paint = "hairline", map[string]any{"line-width": 1e-12}
	bend := mvt.Feature{GeometryType: mvt.LineStringType, Lines: [][]geometry.Point{{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 14, Y: 9}}}}
	return layers, map[string]mvt.FeatureSlice{"source": {polygonFeature(nil), bend}}
}

func compilePrimitives(t *testing.T, options LayerOptions) []Primitive {
	t.Helper()
	layers, sources := extrusionLayers()
	var result []Primitive
	require.NoError(t, CompileTile(layers, sources, options, func(p Primitive) error { result = append(result, p); return nil }, nil))
	return result
}

func TestExtrudedTileLinesMatchBakedOutput(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		options := LayerOptions{SourceZoom: 9, Zoom: 10, Indexed: indexed}
		baked := compilePrimitives(t, options)
		options.ExtrudeLines = true
		extruded := compilePrimitives(t, options)
		require.Len(t, extruded, len(baked))
		// fill, its outline, plain line, dashed, offset and both gap sides.
		require.Len(t, baked, 7)
		scale := 2.0 // 2^(10-9)
		kinds := map[string]int{}
		for i, p := range extruded {
			legacy := baked[i]
			assert.Nil(t, legacy.Directions, "the existing output is unchanged")
			assert.False(t, legacy.Dynamic)
			assert.Zero(t, legacy.HalfWidth)
			assert.Equal(t, legacy.Order, p.Order)
			assert.Equal(t, legacy.LayerID, p.LayerID)
			assert.Equal(t, legacy.Color, p.Color)
			assert.Equal(t, legacy.Mesh.Indices, p.Mesh.Indices)
			require.Len(t, p.Mesh.Vertices, len(legacy.Mesh.Vertices))
			switch {
			case p.Directions != nil:
				kinds["extruded"]++
				assert.False(t, p.Dynamic)
				require.Len(t, p.Directions, len(p.Mesh.Vertices))
				for j, anchor := range p.Mesh.Vertices {
					halfWidth := p.HalfWidth / scale
					assert.InDelta(t, legacy.Mesh.Vertices[j].X, anchor.X+p.Directions[j].X*halfWidth, 1e-9)
					assert.InDelta(t, legacy.Mesh.Vertices[j].Y, anchor.Y+p.Directions[j].Y*halfWidth, 1e-9)
				}
			case p.Dynamic:
				kinds["dynamic"]++
				assert.Equal(t, legacy.Mesh, p.Mesh, "dashed and offset lines keep their baked geometry")
				assert.Zero(t, p.HalfWidth)
			default:
				kinds["stable"]++
				assert.Equal(t, legacy.Mesh, p.Mesh)
			}
		}
		assert.Equal(t, map[string]int{"stable": 1, "extruded": 2, "dynamic": 4}, kinds)
		assert.Equal(t, 0.5, extruded[1].HalfWidth, "fill outlines are one logical pixel wide")
		assert.Equal(t, "plain", extruded[2].LayerID)
		assert.Equal(t, 2.5, extruded[2].HalfWidth, "half of the width interpolated at zoom 10")

		// Another style zoom changes paint values only.
		options.Zoom = 10.0625
		next := compilePrimitives(t, options)
		require.Len(t, next, len(extruded))
		for i, p := range next {
			if p.Dynamic {
				assert.NotEqual(t, extruded[i].Mesh.Vertices, p.Mesh.Vertices, "baked geometry follows the zoom")
				continue
			}
			assert.Equal(t, extruded[i].Mesh, p.Mesh, "primitive %d stays resident", i)
			assert.Equal(t, extruded[i].Directions, p.Directions)
		}
		assert.InDelta(t, 2.5625, next[2].HalfWidth, 1e-12)
		assert.Equal(t, 0.5, next[1].HalfWidth)
	}
}

func TestExtrudedTileLineErrors(t *testing.T) {
	layers, sources := extrusionLayers()
	sentinel := errors.New("sink failed")
	calls := 0
	err := CompileTile(layers, sources, LayerOptions{ExtrudeLines: true}, func(p Primitive) error {
		calls++
		if p.Directions != nil {
			return sentinel
		}
		return nil
	}, nil)
	assert.ErrorIs(t, err, sentinel)
	assert.Equal(t, 2, calls, "the fill is accepted before its outline fails")
	// One triangle admits the fill but not its extruded outline.
	err = CompileTile(layers[:1], sources, LayerOptions{ExtrudeLines: true, TriangleLimit: 1}, func(Primitive) error { return nil }, nil)
	assert.ErrorIs(t, err, mvt.ErrFeatureResourceLimit)
	err = CompileTile(layers[1:2], sources, LayerOptions{ExtrudeLines: true, TriangleLimit: 1}, func(Primitive) error { return nil }, nil)
	assert.ErrorIs(t, err, mvt.ErrFeatureResourceLimit)
	assert.ErrorIs(t, compileFill(nil, layers[0], LayerOptions{}, func(geometry.Mesh, style.Color) error { return nil },
		func(geometry.Mesh, string, float64, float64) error { return nil }, lineSinks{}), ErrOptions)
	assert.ErrorIs(t, compileLine(nil, layers[1], LayerOptions{}, lineSinks{}), ErrOptions)
}

func splitPrimitives(indexed bool) (fill, line, dashed Primitive) {
	fill = Primitive{Order: 1, Mesh: BackgroundGeometry(indexed), Color: style.Color{Alpha: 255}}
	mesh, _ := geometry.TessellateExtrudedLines([][]geometry.Point{{{X: 0, Y: 0}, {X: 10, Y: 0}}}, geometry.ExtrudedLineStyle{}, 16, indexed)
	line = Primitive{Order: 2, Color: style.Color{Red: 255, Alpha: 255}, HalfWidth: 1.5}
	for _, vertex := range mesh.Vertices {
		line.Mesh.Vertices = append(line.Mesh.Vertices, vertex.Anchor)
		line.Directions = append(line.Directions, vertex.Direction)
	}
	line.Mesh.Indices = mesh.Indices
	baked, _ := geometry.TessellateLines([][]geometry.Point{{{X: 0, Y: 5}, {X: 10, Y: 5}}}, geometry.LineStyle{Width: 1}, 16, indexed)
	dashed = Primitive{Order: 3, Mesh: baked, Color: style.Color{Blue: 255, Alpha: 255}, Dynamic: true}
	return fill, line, dashed
}

func TestSplitFragmentMeshes(t *testing.T) {
	label := func(int) RenderSymbol {
		return RenderSymbol{Symbol: placement.Symbol{Order: 4, IconName: "dot", IconSize: 1}, Accepted: placement.Accepted{Icon: true}}
	}
	image := sprite.Image{Pixels: make([]byte, 4*4*4), Width: 4, Height: 4, PixelRatio: 1}
	lookup := func(string, style.Color, float64) (sprite.Image, bool) { return image, true }
	for _, indexed := range []bool{false, true} {
		fill, line, dashed := splitPrimitives(indexed)
		pack := func(split bool) (*scene.Scene, []DrawSource) {
			b := NewFragmentBuilder(indexed, 0, 0)
			if split {
				b.Split()
			}
			for _, p := range []Primitive{fill, line, dashed, fill} {
				b.Primitive(view.TileID{}, p, nil)
			}
			b.SymbolLayer(0, 1, label, 0, lookup)
			result, sources, err := b.Finish()
			require.NoError(t, err)
			return result, sources
		}
		single, singleSources := pack(false)
		split, splitSources := pack(true)
		assert.Equal(t, singleSources, splitSources, "provenance and draw order are unchanged")
		require.Len(t, single.Meshes, 1)
		require.Len(t, split.Meshes, 2)
		assert.Equal(t, uint64(StableMesh), split.Meshes[0].ID)
		assert.Equal(t, uint64(DynamicMesh), split.Meshes[1].ID)
		assert.Equal(t, single.Meshes[0].BufferBytes(), split.Meshes[0].BufferBytes()+split.Meshes[1].BufferBytes())
		require.Len(t, split.Draws, len(single.Draws))
		meshes := make([]uint64, len(split.Draws))
		for i, draw := range split.Draws {
			meshes[i] = draw.Mesh
			assert.Equal(t, single.Draws[i].Material, draw.Material)
			assert.Equal(t, single.Draws[i].Count, draw.Count)
			assert.Equal(t, single.Draws[i].Clip, draw.Clip)
		}
		assert.Equal(t, []uint64{StableMesh, StableMesh, DynamicMesh, StableMesh, DynamicMesh}, meshes)
		// The second stable fill follows the extruded line in the stable mesh, not
		// the dashed line it follows in draw order.
		assert.Equal(t, split.Draws[1].First+split.Draws[1].Count, split.Draws[3].First)
		material := split.Draws[1].Material
		assert.True(t, material.MapAligned)
		assert.Equal(t, float32(1.5), material.OffsetScale)
		assert.Zero(t, split.Draws[0].Material.OffsetScale)
		assert.False(t, split.Draws[2].Material.MapAligned, "baked lines are ordinary geometry")
		stable := split.Meshes[0]
		element := func(i uint32) scene.Vertex {
			if indexed {
				return stable.Vertices[stable.Indices[i]]
			}
			return stable.Vertices[i]
		}
		first := element(split.Draws[1].First)
		assert.Equal(t, scene.Vertex{X: 0, Y: 0, OffsetX: 0, OffsetY: 1}, first, "anchors and unit directions")

		// A width change leaves both meshes byte-identical.
		line.HalfWidth = 4
		wider, _ := pack(true)
		assert.Equal(t, split.Meshes, wider.Meshes)
		assert.Equal(t, float32(4), wider.Draws[1].Material.OffsetScale)
	}
}

func TestSplitOmitsEmptyMeshesAndKeepsIdentity(t *testing.T) {
	fill, _, dashed := splitPrimitives(true)
	for _, test := range []struct {
		primitives []Primitive
		ids        []uint64
	}{{[]Primitive{fill}, []uint64{StableMesh}}, {[]Primitive{dashed}, []uint64{DynamicMesh}}, {nil, nil}} {
		b := NewFragmentBuilder(true, 0, 0)
		b.Split()
		for _, p := range test.primitives {
			b.Primitive(view.TileID{}, p, nil)
		}
		result, _, err := b.Finish()
		require.NoError(t, err)
		var ids []uint64
		for _, mesh := range result.Meshes {
			ids = append(ids, mesh.ID)
		}
		assert.Equal(t, test.ids, ids)
		for _, draw := range result.Draws {
			assert.Equal(t, test.ids[0], draw.Mesh)
		}
	}
	// Without Split a Dynamic primitive is ordinary geometry in mesh one.
	b := NewFragmentBuilder(true, 0, 0)
	b.Primitive(view.TileID{}, dashed, nil)
	result, _, err := b.Finish()
	require.NoError(t, err)
	require.Len(t, result.Meshes, 1)
	assert.Equal(t, uint64(StableMesh), result.Meshes[0].ID)
}

func TestSplitAndExtrudedRejections(t *testing.T) {
	fill, line, dashed := splitPrimitives(false)
	b := NewFragmentBuilder(false, 0, 0)
	b.Primitive(view.TileID{}, fill, nil)
	b.Split()
	_, _, err := b.Finish()
	assert.ErrorIs(t, err, ErrPackingInput, "Split must precede packing")
	var zero FragmentBuilder
	zero.Split()
	_, _, err = zero.Finish()
	assert.ErrorIs(t, err, ErrPackingInput)
	for _, halfWidth := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		bad := line
		bad.HalfWidth = halfWidth
		b := NewFragmentBuilder(false, 0, 0)
		b.Primitive(view.TileID{}, bad, nil)
		_, _, err := b.Finish()
		assert.ErrorIs(t, err, ErrPackingInput, "half width %v", halfWidth)
	}
	bad := line
	bad.Directions = bad.Directions[:1]
	b = NewFragmentBuilder(false, 0, 0)
	b.Primitive(view.TileID{}, bad, nil)
	_, _, err = b.Finish()
	assert.ErrorIs(t, err, ErrPackingInput)
	closed := NewSceneBuilder(false, 0)
	_, err = closed.finish(true)
	require.NoError(t, err)
	closed.Extruded(line.Mesh, line.Directions, 1, scene.Material{}, [4]float32{})
	_, err = closed.Finish()
	assert.ErrorIs(t, err, ErrPackingClosed)
	// The element limit bounds both meshes together.
	count := len(fill.Mesh.Vertices) + len(dashed.Mesh.Vertices)
	for _, limit := range []int{count, count - 1} {
		b := NewFragmentBuilder(false, limit, 0)
		b.Split()
		b.Primitive(view.TileID{}, fill, nil)
		b.Primitive(view.TileID{}, dashed, nil)
		_, _, err := b.Finish()
		if limit == count {
			assert.NoError(t, err)
		} else {
			assert.ErrorIs(t, err, geometry.ErrGeometryLimit)
		}
	}
	b = NewFragmentBuilder(false, len(line.Mesh.Vertices)-1, 0)
	b.Primitive(view.TileID{}, line, nil)
	_, _, err = b.Finish()
	assert.ErrorIs(t, err, geometry.ErrGeometryLimit, "extruded geometry respects the element limit")
	over := line
	over.HalfWidth = math.MaxFloat64
	b = NewFragmentBuilder(false, 0, 0)
	b.Primitive(view.TileID{}, over, nil)
	_, _, err = b.Finish()
	assert.Error(t, err, "a half width beyond float32 fails final validation")
}
