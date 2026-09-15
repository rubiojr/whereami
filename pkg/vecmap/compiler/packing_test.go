package compiler

import (
	"math"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/glyph"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScenePackingTopologyAndDrawOrder(t *testing.T) {
	var results []*scene.Scene
	for _, indexed := range []bool{false, true} {
		b := NewSceneBuilder(indexed, 0)
		mesh := BackgroundGeometry(indexed)
		material := scene.Material{Color: PackedColor(style.Color{Red: 255, Alpha: 255})}
		clip := [4]float32{0, 0, 256, 256}
		b.Geometry(mesh, material, clip)
		b.Geometry(mesh, material, clip)
		b.Geometry(geometry.Mesh{}, material, clip)
		quad := geometry.TextQuad(-2, -3, 4, 5, 0, 0, 1, 1)
		indices := []uint32{0, 1, 2, 0, 2, 3}
		image := b.Texture("icon", 1, 1, []byte{10, 20, 30, 255})
		imageMaterial := scene.Material{Kind: scene.Image, Texture: image, Color: [4]float32{1, 1, 1, 1}}
		b.IndexedText(geometry.Point{X: 10, Y: 20}, quad[:], indices, geometry.Point{X: 2, Y: 3}, math.Pi/2, imageMaterial)
		textMaterial := scene.Material{Kind: scene.SDFFill, Texture: image, FontScale: 1, Color: [4]float32{1, 1, 1, 1}}
		if indexed {
			b.IndexedText(geometry.Point{}, quad[:], indices, geometry.Point{}, 0, textMaterial)
		} else {
			var expanded []float32
			for _, index := range indices {
				v := quad[index]
				expanded = append(expanded, v.X, v.Y, v.U, v.V)
			}
			b.ExpandedText(geometry.Point{}, expanded, geometry.Point{}, 0, textMaterial)
		}
		result, err := b.Finish()
		require.NoError(t, err)
		require.Len(t, result.Draws, 3)
		assert.Equal(t, uint32(12), result.Draws[0].Count)
		assert.Equal(t, uint32(12), result.Draws[1].First)
		assert.Equal(t, clip, result.Draws[0].Clip)
		assert.Equal(t, [4]float32{}, result.Draws[1].Clip)
		assert.Equal(t, scene.Image, result.Draws[1].Material.Kind)
		assert.Equal(t, scene.SDFFill, result.Draws[2].Material.Kind)
		mesh.Vertices[0].X = 999
		quad[0].X = 999
		assert.Zero(t, result.Meshes[0].Vertices[0].X)
		repeated, err := b.Finish()
		require.NoError(t, err)
		assert.Same(t, result, repeated)
		results = append(results, result)
	}
	assert.Equal(t, results[0].Draws, results[1].Draws)
	assert.Equal(t, results[0].Textures, results[1].Textures)
	mesh := results[1].Meshes[0]
	require.Len(t, mesh.Indices, len(results[0].Meshes[0].Vertices))
	for i, index := range mesh.Indices {
		assert.Equal(t, results[0].Meshes[0].Vertices[i], mesh.Vertices[index])
	}
}

func TestSceneTextureIdentityAndGlyphConversion(t *testing.T) {
	b := NewSceneBuilder(false, 0)
	assert.Zero(t, b.GlyphAtlas(nil))
	atlas := &glyph.Atlas{Width: 2, Height: 1, Pixels: []byte{0, 123}}
	assert.Equal(t, uint64(1), b.GlyphAtlas(atlas))
	assert.Equal(t, uint64(1), b.GlyphAtlas(&glyph.Atlas{}), "first image wins by logical key")
	image := []byte{1, 2, 3, 4}
	assert.Equal(t, uint64(2), b.Texture("sprite", 1, 1, image))
	assert.Equal(t, uint64(2), b.Texture("sprite", -1, -1, nil))
	b.Geometry(BackgroundGeometry(false), scene.Material{Color: [4]float32{1, 1, 1, 1}}, [4]float32{})
	result, err := b.Finish()
	require.NoError(t, err)
	assert.Equal(t, []byte{0, 0, 0, 255, 123, 123, 123, 255}, result.Textures[0].RGBA)
	assert.Same(t, &image[0], &result.Textures[1].RGBA[0])
	clear(atlas.Pixels)
	assert.Equal(t, byte(123), result.Textures[0].RGBA[4], "converted atlas owns its bytes")
	assert.Zero(t, b.Texture("new", 1, 1, image))
	_, err = b.Finish()
	assert.ErrorIs(t, err, ErrPackingClosed)
	assert.Len(t, result.Textures, 2, "publication is sealed")
}

func TestScenePackingRejections(t *testing.T) {
	material := scene.Material{Color: [4]float32{1, 1, 1, 1}}
	for _, test := range []struct {
		name    string
		indexed bool
		apply   func(*SceneBuilder)
	}{
		{"bad triangle", false, func(b *SceneBuilder) {
			b.Geometry(geometry.Mesh{Vertices: make([]geometry.Point, 2)}, material, [4]float32{})
		}},
		{"bad index", true, func(b *SceneBuilder) {
			b.Geometry(geometry.Mesh{Vertices: make([]geometry.Point, 3), Indices: []uint32{0, 1, 3}}, material, [4]float32{})
		}},
		{"bad xyuv", false, func(b *SceneBuilder) { b.ExpandedText(geometry.Point{}, []float32{1}, geometry.Point{}, 0, material) }},
		{"incomplete text triangle", false, func(b *SceneBuilder) {
			b.ExpandedText(geometry.Point{}, make([]float32, 8), geometry.Point{}, 0, material)
		}},
		{"wrong mode", true, func(b *SceneBuilder) { b.ExpandedText(geometry.Point{}, nil, geometry.Point{}, 0, material) }},
		{"bad text index", false, func(b *SceneBuilder) {
			b.IndexedText(geometry.Point{}, nil, []uint32{0, 0, 0}, geometry.Point{}, 0, material)
		}},
		{"texture dimensions", false, func(b *SceneBuilder) { b.Texture("bad", math.MaxInt, 1, nil) }},
		{"texture height", false, func(b *SceneBuilder) { b.Texture("bad", 1, 16385, nil) }},
		{"texture storage", false, func(b *SceneBuilder) { b.Texture("bad", 1, 1, nil) }},
		{"texture zero", false, func(b *SceneBuilder) { b.Texture("bad", 0, 1, nil) }},
		{"glyph dimensions", false, func(b *SceneBuilder) { b.GlyphAtlas(&glyph.Atlas{Width: math.MaxInt, Height: 1}) }},
		{"glyph storage", false, func(b *SceneBuilder) { b.GlyphAtlas(&glyph.Atlas{Width: 1, Height: 1}) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			b := NewSceneBuilder(test.indexed, 0)
			test.apply(b)
			b.Geometry(BackgroundGeometry(false), material, [4]float32{})
			b.ExpandedText(geometry.Point{}, nil, geometry.Point{}, 0, material)
			assert.Zero(t, b.GlyphAtlas(&glyph.Atlas{Width: 1, Height: 1, Pixels: []byte{0}}))
			assert.Zero(t, b.Texture("later", 1, 1, []byte{0, 0, 0, 0}))
			result, err := b.Finish()
			assert.ErrorIs(t, err, ErrPackingInput)
			assert.Nil(t, result)
		})
	}
	zero := &SceneBuilder{}
	result, err := zero.Finish()
	assert.ErrorIs(t, err, ErrPackingInput)
	assert.Nil(t, result)
}

func FuzzScenePacking(f *testing.F) {
	f.Add(1.0, 2.0, 0.0, []byte{0, 1, 2}, false)
	f.Add(math.MaxFloat64, math.Inf(1), math.NaN(), []byte{255}, true)
	f.Fuzz(func(t *testing.T, x, y, angle float64, topology []byte, indexed bool) {
		if len(topology) > 64 {
			return
		}
		indices := make([]uint32, len(topology))
		for i, value := range topology {
			indices[i] = uint32(value)
		}
		b := NewSceneBuilder(indexed, 128)
		material := scene.Material{Color: [4]float32{1, 1, 1, 1}}
		b.Geometry(geometry.Mesh{Vertices: []geometry.Point{{X: x, Y: y}, {X: 1}, {Y: 1}}, Indices: indices}, material, [4]float32{})
		b.IndexedText(geometry.Point{X: x, Y: y}, []geometry.TextVertex{{X: 1}, {Y: 1}, {}}, nil, geometry.Point{}, angle, material)
		result, err := b.Finish()
		if err != nil {
			require.Nil(t, result)
			return
		}
		require.NoError(t, result.Validate())
		require.LessOrEqual(t, len(result.Meshes[0].Vertices), 128)
		require.LessOrEqual(t, len(result.Meshes[0].Indices), 128)
	})
}

func TestScenePackingLimitsAndFinalValidation(t *testing.T) {
	material := scene.Material{Color: [4]float32{1, 1, 1, 1}}
	for _, limit := range []int{-1, MaxSceneElements + 1} {
		b := NewSceneBuilder(false, limit)
		_, err := b.Finish()
		assert.ErrorIs(t, err, geometry.ErrGeometryLimit)
	}
	for _, indexed := range []bool{false, true} {
		b := NewSceneBuilder(indexed, 6)
		b.Geometry(BackgroundGeometry(indexed), material, [4]float32{})
		b.Geometry(BackgroundGeometry(indexed), material, [4]float32{})
		result, err := b.Finish()
		assert.ErrorIs(t, err, geometry.ErrGeometryLimit)
		assert.Nil(t, result)
	}
	b := NewSceneBuilder(false, 3)
	b.IndexedText(geometry.Point{}, make([]geometry.TextVertex, 4), []uint32{0, 1, 2}, geometry.Point{}, 0, material)
	_, err := b.Finish()
	assert.ErrorIs(t, err, geometry.ErrGeometryLimit, "scratch vertex count is bounded before allocation")
	b = NewSceneBuilder(false, 0)
	for i := range MaxSceneTextures {
		b.textures[string(rune(i))] = uint64(i + 1)
	}
	b.Texture("over limit", 1, 1, make([]byte, 4))
	_, err = b.Finish()
	assert.ErrorIs(t, err, geometry.ErrGeometryLimit)
	b = NewSceneBuilder(false, 0)
	mesh := BackgroundGeometry(false)
	mesh.Vertices[0].X = math.MaxFloat64
	b.Geometry(mesh, material, [4]float32{})
	result, err := b.Finish()
	assert.Error(t, err, "nonfinite packed output fails before publication")
	assert.Nil(t, result)
	b = NewSceneBuilder(false, 0)
	b.Geometry(BackgroundGeometry(true), material, [4]float32{})
	result, err = b.Finish()
	require.NoError(t, err)
	assert.Len(t, result.Meshes[0].Vertices, 6, "indexed input can expand without a conversion buffer")
}
