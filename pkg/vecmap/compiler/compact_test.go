package compiler

import (
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/glyph"
	"github.com/rubiojr/whereami/pkg/vecmap/placement"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/sprite"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// drawn is what a backend draws for one draw: its material and every vertex in
// draw order with all attributes, whatever section stores it.
type drawn struct {
	mesh     uint64
	material scene.Material
	clip     [4]float32
	vertices []scene.Vertex
}

func expand(t *testing.T, s *scene.Scene) []drawn {
	t.Helper()
	require.NoError(t, s.Validate())
	meshes := make(map[uint64]scene.Mesh)
	for _, mesh := range s.Meshes {
		meshes[mesh.ID] = mesh
	}
	result := make([]drawn, 0, len(s.Draws))
	for _, draw := range s.Draws {
		mesh := meshes[draw.Mesh]
		d := drawn{mesh: draw.Mesh, material: draw.Material, clip: draw.Clip}
		for i := draw.First; i < draw.First+draw.Count; i++ {
			index := int(i)
			if len(mesh.Indices) > 0 {
				index = int(mesh.Indices[i])
			}
			d.vertices = append(d.vertices, mesh.At(draw.Layout, index))
		}
		result = append(result, d)
	}
	return result
}

func compactPrimitives(indexed bool) []Primitive {
	quad := func(x float64) geometry.Mesh {
		mesh := geometry.Mesh{Vertices: []geometry.Point{{X: x}, {X: x + 4}, {X: x + 4, Y: 4}, {X: x, Y: 4}}, Indices: []uint32{0, 1, 2, 0, 2, 3}}
		if !indexed {
			mesh = geometry.Mesh{Vertices: []geometry.Point{{X: x}, {X: x + 4}, {X: x + 4, Y: 4}, {X: x}, {X: x + 4, Y: 4}, {X: x, Y: 4}}}
		}
		return mesh
	}
	directions := func(mesh geometry.Mesh) []geometry.Point {
		values := make([]geometry.Point, len(mesh.Vertices))
		for i := range values {
			values[i] = geometry.Point{X: float64(i%2)*2 - 1, Y: 0.5}
		}
		return values
	}
	distances := func(mesh geometry.Mesh) []float64 {
		values := make([]float64, len(mesh.Vertices))
		for i := range values {
			values[i] = float64(i) * 3
		}
		return values
	}
	red, blue := style.Color{Red: 255, Alpha: 255}, style.Color{Blue: 255, Alpha: 255}
	return []Primitive{
		{Order: 1, Mesh: BackgroundGeometry(indexed), Color: red},
		{Order: 2, Mesh: quad(10), Color: red},
		{Order: 3, Mesh: quad(20), PatternName: "dots", PatternScale: 1, Opacity: 1},
		{Order: 4, Mesh: quad(30), Directions: directions(quad(30)), HalfWidth: 2, Color: blue},
		{Order: 5, Mesh: quad(40), Color: blue},
		{Order: 6, Mesh: quad(50), Directions: directions(quad(50)), Distances: distances(quad(50)), HalfWidth: 1, DashUnit: 2, Dashes: geometry.DashPattern{2, 1}, Color: red},
		{Order: 7, Mesh: quad(60), Color: red, Dynamic: true},
		{Order: 8, Mesh: quad(70), Directions: directions(quad(70)), HalfWidth: 3, Color: blue},
	}
}

func packCompact(t *testing.T, indexed, split, resident, compact, reserve bool) (*scene.Scene, []DrawSource) {
	t.Helper()
	image := sprite.Image{Width: 8, Height: 6, PixelRatio: 3, Pixels: make([]byte, 8*6*4)}
	lookup := func(string, style.Color, float64) (sprite.Image, bool) { return image, true }
	b := NewFragmentBuilder(indexed, 0, 0)
	if split {
		b.Split()
	}
	if resident {
		b.ResidentSymbols()
	}
	if compact {
		b.CompactVertices()
	}
	primitives := compactPrimitives(indexed)
	if reserve {
		b.Reserve(primitives)
	}
	atlas := b.GlyphAtlas(&glyph.Atlas{Width: 1, Height: 1, Pixels: []byte{90}})
	for _, primitive := range primitives {
		b.Primitive(view.TileID{}, primitive, lookup)
	}
	layout := &glyph.PreparedLayout{TextLayout: glyph.TextLayout{Scale: 1}, LayoutMesh: glyph.LayoutMesh{
		Vertices: []geometry.TextVertex{{X: 0, Y: 0, U: 0.1, V: 0.2}, {X: 8, Y: 0, U: 0.3, V: 0.2}, {X: 8, Y: 8, U: 0.3, V: 0.4}, {X: 0, Y: 8, U: 0.1, V: 0.4}},
		Indices:  []uint32{0, 1, 2, 0, 2, 3}}}
	if !indexed {
		v := layout.Vertices
		layout.Vertices, layout.Indices = []geometry.TextVertex{v[0], v[1], v[2], v[0], v[2], v[3]}, nil
	}
	b.SymbolLayer(0, 1, func(int) RenderSymbol {
		return RenderSymbol{Symbol: placement.Symbol{Order: 9, Text: "A", TextSize: 16, TextColor: style.Color{Alpha: 255}, Anchor: geometry.Point{X: 5, Y: 5}},
			Accepted: placement.Accepted{Text: true}, Layout: layout}
	}, atlas, lookup)
	result, sources, err := b.Finish()
	require.NoError(t, err)
	return result, sources
}

func TestCompactVerticesDrawTheSameGeometry(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		for _, split := range []bool{false, true} {
			for _, resident := range []bool{false, true} {
				plain, plainSources := packCompact(t, indexed, split, resident, false, true)
				for _, reserve := range []bool{false, true} {
					compact, sources := packCompact(t, indexed, split, resident, true, reserve)
					assert.Equal(t, expand(t, plain), expand(t, compact), "indexed=%t split=%t resident=%t reserve=%t", indexed, split, resident, reserve)
					assert.Equal(t, plainSources, sources)
					assert.Equal(t, plain.Textures, compact.Textures)
					require.Equal(t, len(plain.Meshes), len(compact.Meshes), "sections add no mesh resource")
					var before, after uint64
					for i, mesh := range compact.Meshes {
						assert.Equal(t, plain.Meshes[i].ID, mesh.ID)
						assert.Equal(t, len(mesh.Vertices), cap(mesh.Vertices))
						assert.Equal(t, len(mesh.Offsets), cap(mesh.Offsets))
						assert.Equal(t, len(mesh.Positions), cap(mesh.Positions))
						assert.Equal(t, len(mesh.Indices), cap(mesh.Indices))
						assert.Equal(t, len(plain.Meshes[i].Indices), len(mesh.Indices))
						before, after = before+plain.Meshes[i].VertexBytes(), after+mesh.VertexBytes()
					}
					assert.Less(t, after, before)
				}
			}
		}
	}
}

func TestCompactVerticesChooseTheSmallestSection(t *testing.T) {
	compact, _ := packCompact(t, true, true, true, true, true)
	layouts := make(map[scene.Kind]map[scene.Layout]int)
	for _, draw := range compact.Draws {
		if layouts[draw.Material.Kind] == nil {
			layouts[draw.Material.Kind] = make(map[scene.Layout]int)
		}
		layouts[draw.Material.Kind][draw.Layout]++
	}
	// Three fills and the baked line carry positions only, two lines are extruded.
	assert.Equal(t, map[scene.Layout]int{scene.PositionLayout: 4, scene.OffsetLayout: 2}, layouts[scene.Solid])
	assert.Equal(t, map[scene.Layout]int{scene.PositionLayout: 1}, layouts[scene.Pattern])
	assert.Equal(t, map[scene.Layout]int{scene.FullLayout: 1}, layouts[scene.Dashed], "the distance along a dashed line travels in U")
	assert.Equal(t, map[scene.Layout]int{scene.FullLayout: 1}, layouts[scene.SDFFill])

	plain, _ := packCompact(t, true, true, true, false, true)
	for _, draw := range plain.Draws {
		assert.Equal(t, scene.FullLayout, draw.Layout)
	}
	for _, mesh := range plain.Meshes {
		assert.Empty(t, mesh.Offsets)
		assert.Empty(t, mesh.Positions)
	}

	late := NewSceneBuilder(true, 0)
	late.Geometry(BackgroundGeometry(true), scene.Material{Color: [4]float32{0, 0, 0, 1}}, [4]float32{})
	late.CompactVertices()
	_, err := late.Finish()
	assert.ErrorIs(t, err, ErrPackingInput, "the layout is chosen before packing")
}

func TestFinishedBuilderKeepsNoBufferOfItsOwn(t *testing.T) {
	// A scene points into its builder, so whatever the builder still holds
	// stays allocated for as long as the scene does.
	for _, compact := range []bool{false, true} {
		b := NewSceneBuilder(true, 0)
		if compact {
			b.CompactVertices()
		}
		quad := geometry.Mesh{Vertices: []geometry.Point{{}, {X: 4}, {X: 4, Y: 4}, {Y: 4}}, Indices: []uint32{0, 1, 2, 0, 2, 3}}
		b.Geometry(quad, scene.Material{Color: [4]float32{0, 0, 0, 1}}, [4]float32{})
		b.Extruded(quad, []geometry.Point{{X: 1}, {X: 1}, {X: 1}, {X: 1}}, 2, scene.Material{Color: [4]float32{0, 0, 0, 1}}, [4]float32{})
		result, err := b.Finish()
		require.NoError(t, err)
		require.Len(t, result.Meshes, 1)
		require.Len(t, result.Meshes[0].Indices, 12)
		for _, s := range []*sections{&b.mesh, &b.dynamic, &b.symbols} {
			assert.Zero(t, cap(s.full.Vertices)+cap(s.offsets.Vertices)+cap(s.positions.Vertices), "compact=%t", compact)
			assert.Zero(t, cap(s.full.Indices)+cap(s.offsets.Indices)+cap(s.positions.Indices), "compact=%t", compact)
		}
		again, err := b.Finish()
		require.NoError(t, err)
		assert.Same(t, result, again)
	}
}
