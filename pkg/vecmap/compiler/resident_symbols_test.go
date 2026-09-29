package compiler

import (
	"math"
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

// symbolLayout returns one glyph quad in em units, baked at scale or as a unit
// mesh carrying scale.
func symbolLayout(indexed, unit bool, scale float64) *glyph.PreparedLayout {
	factor := scale
	if unit {
		factor = 1
	}
	quad := geometry.TextQuad(-7*factor, -19*factor, 9*factor, 5*factor, 0.25, 0.5, 0.75, 1)
	layout := &glyph.PreparedLayout{TextLayout: glyph.TextLayout{Scale: scale}, LayoutMesh: glyph.LayoutMesh{Unit: unit}}
	indices := []uint32{0, 1, 2, 0, 2, 3}
	if indexed {
		layout.Vertices, layout.Indices = quad[:], indices
		return layout
	}
	for _, index := range indices {
		v := quad[index]
		layout.Expanded = append(layout.Expanded, v.X, v.Y, v.U, v.V)
	}
	return layout
}

func residentSymbol(indexed, unit bool, textSize, iconSize float64) RenderSymbol {
	return RenderSymbol{Symbol: placement.Symbol{Order: 4, Anchor: geometry.Point{X: 10, Y: 20},
		IconName: "dot", IconSize: iconSize, IconOpacity: 1, IconAnchor: "top-left", IconOffset: geometry.Point{X: 3, Y: -2}, IconRotate: 0.3,
		TextSize: textSize, TextColor: style.Color{Red: 255, Alpha: 255}, HaloColor: style.Color{Alpha: 255}, HaloWidth: 1,
		TextOffset: geometry.Point{X: 0.5, Y: 1.25}, LineAngle: 0.7},
		Accepted: placement.Accepted{Text: true, Icon: true}, Layout: symbolLayout(indexed, unit, textSize/glyph.EmSize)}
}

func symbolSprite(string, style.Color, float64) (sprite.Image, bool) {
	return sprite.Image{Pixels: make([]byte, 6*4*4), Width: 6, Height: 4, PixelRatio: 2}, true
}

func packSymbols(t *testing.T, indexed, resident bool, items ...RenderSymbol) (*scene.Scene, []DrawSource) {
	t.Helper()
	b := NewFragmentBuilder(indexed, 0, 0)
	if resident {
		b.ResidentSymbols()
	}
	atlas := b.GlyphAtlas(&glyph.Atlas{Width: 1, Height: 1, Pixels: []byte{90}})
	b.SymbolLayer(0, len(items), func(i int) RenderSymbol { return items[i] }, atlas, symbolSprite)
	result, sources, err := b.Finish()
	require.NoError(t, err)
	return result, sources
}

func meshElements(mesh scene.Mesh) []scene.Vertex {
	if mesh.Indices == nil {
		return mesh.Vertices
	}
	elements := make([]scene.Vertex, len(mesh.Indices))
	for i, index := range mesh.Indices {
		elements[i] = mesh.Vertices[index]
	}
	return elements
}

func TestResidentSymbolsMatchBakedOutput(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		var meshes []scene.Mesh
		for _, size := range [][2]float64{{12, 1}, {13.5, 0.75}, {31, 2.5}} {
			textSize, iconSize := size[0], size[1]
			baked, bakedSources := packSymbols(t, indexed, false, residentSymbol(indexed, false, textSize, iconSize))
			resident, sources := packSymbols(t, indexed, true, residentSymbol(indexed, true, textSize, iconSize))
			assert.Equal(t, bakedSources, sources, "provenance and draw order are unchanged")
			require.Len(t, baked.Meshes, 1)
			require.Len(t, resident.Meshes, 1)
			assert.Equal(t, uint64(StableMesh), baked.Meshes[0].ID)
			assert.Equal(t, uint64(SymbolMesh), resident.Meshes[0].ID)
			assert.Equal(t, baked.Meshes[0].Indices, resident.Meshes[0].Indices)
			assert.Equal(t, baked.Textures, resident.Textures)
			require.Len(t, resident.Draws, 3)
			scales := []float32{float32(iconSize), float32(textSize / glyph.EmSize), float32(textSize / glyph.EmSize)}
			want, got := meshElements(baked.Meshes[0]), meshElements(resident.Meshes[0])
			for i, draw := range resident.Draws {
				assert.Equal(t, uint64(SymbolMesh), draw.Mesh)
				assert.Zero(t, baked.Draws[i].Material.OffsetScale)
				assert.Equal(t, scales[i], draw.Material.OffsetScale)
				material := draw.Material
				material.OffsetScale = 0
				assert.Equal(t, baked.Draws[i].Material, material, "only the offset scale differs")
				assert.Equal(t, baked.Draws[i].First, draw.First)
				assert.Equal(t, baked.Draws[i].Count, draw.Count)
				for k := draw.First; k < draw.First+draw.Count; k++ {
					assert.Equal(t, want[k].X, got[k].X)
					assert.Equal(t, want[k].Y, got[k].Y)
					assert.Equal(t, want[k].U, got[k].U)
					assert.Equal(t, want[k].V, got[k].V)
					// The vertex shader multiplies in float32.
					assert.InDelta(t, want[k].OffsetX, got[k].OffsetX*scales[i], 1e-4)
					assert.InDelta(t, want[k].OffsetY, got[k].OffsetY*scales[i], 1e-4)
				}
			}
			meshes = append(meshes, resident.Meshes[0])
		}
		assert.Equal(t, meshes[0], meshes[1], "sizes change draws, never the mesh")
		assert.Equal(t, meshes[0], meshes[2])
		// At icon size one and 24 pixel text both forms hold the same vertices.
		baked, _ := packSymbols(t, indexed, false, residentSymbol(indexed, false, glyph.EmSize, 1))
		assert.Equal(t, baked.Meshes[0].Vertices, meshes[0].Vertices)
	}
}

func TestResidentSymbolMeshRouting(t *testing.T) {
	fill, line, dashed := splitPrimitives(true)
	item := residentSymbol(true, true, 12, 1)
	for _, test := range []struct {
		split, resident bool
		meshes, draws   []uint64
	}{
		{false, false, []uint64{StableMesh}, []uint64{StableMesh, StableMesh, StableMesh, StableMesh, StableMesh, StableMesh}},
		{true, false, []uint64{StableMesh, DynamicMesh}, []uint64{StableMesh, StableMesh, DynamicMesh, DynamicMesh, DynamicMesh, DynamicMesh}},
		{false, true, []uint64{StableMesh, SymbolMesh}, []uint64{StableMesh, StableMesh, StableMesh, SymbolMesh, SymbolMesh, SymbolMesh}},
		{true, true, []uint64{StableMesh, DynamicMesh, SymbolMesh}, []uint64{StableMesh, StableMesh, DynamicMesh, SymbolMesh, SymbolMesh, SymbolMesh}},
	} {
		b := NewFragmentBuilder(true, 0, 0)
		if test.split {
			b.Split()
		}
		if test.resident {
			b.ResidentSymbols()
		}
		for _, p := range []Primitive{fill, line, dashed} {
			b.Primitive(view.TileID{}, p, nil)
		}
		atlas := b.GlyphAtlas(&glyph.Atlas{Width: 1, Height: 1, Pixels: []byte{90}})
		b.SymbolLayer(0, 1, func(int) RenderSymbol { return item }, atlas, symbolSprite)
		result, _, err := b.Finish()
		require.NoError(t, err)
		var meshes, draws []uint64
		for _, mesh := range result.Meshes {
			meshes = append(meshes, mesh.ID)
		}
		for _, draw := range result.Draws {
			draws = append(draws, draw.Mesh)
		}
		assert.Equal(t, test.meshes, meshes)
		assert.Equal(t, test.draws, draws)
		// A unit layout is scaled per draw in whichever mesh holds it.
		assert.Equal(t, float32(0.5), result.Draws[4].Material.OffsetScale)
		if test.resident {
			assert.Equal(t, float32(1), result.Draws[3].Material.OffsetScale)
		} else {
			assert.Zero(t, result.Draws[3].Material.OffsetScale)
		}
	}
	// A symbol-only or symbol-free fragment keeps the fixed IDs.
	b := NewFragmentBuilder(true, 0, 0)
	b.Split()
	b.ResidentSymbols()
	b.Primitive(view.TileID{}, fill, nil)
	result, _, err := b.Finish()
	require.NoError(t, err)
	require.Len(t, result.Meshes, 1)
	assert.Equal(t, uint64(StableMesh), result.Meshes[0].ID)
	b = NewFragmentBuilder(true, 0, 0)
	b.ResidentSymbols()
	result, _, err = b.Finish()
	require.NoError(t, err)
	assert.Empty(t, result.Meshes)
}

func TestResidentSymbolsKeepUnscalableSizesBaked(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		for _, size := range []float64{0, -2, 1e-50} {
			item := residentSymbol(indexed, false, 12, size)
			item.Accepted.Text = false
			baked, _ := packSymbols(t, indexed, false, item)
			resident, _ := packSymbols(t, indexed, true, item)
			require.Len(t, resident.Draws, 1)
			assert.Zero(t, resident.Draws[0].Material.OffsetScale, "icon size %v", size)
			assert.Equal(t, baked.Meshes[0].Vertices, resident.Meshes[0].Vertices)
			assert.Equal(t, uint64(SymbolMesh), resident.Meshes[0].ID)
		}
		// Baked text in a resident builder is ordinary geometry in the symbol mesh.
		item := residentSymbol(indexed, false, 12, 1)
		item.Accepted.Icon = false
		baked, _ := packSymbols(t, indexed, false, item)
		resident, _ := packSymbols(t, indexed, true, item)
		assert.Equal(t, baked.Meshes[0].Vertices, resident.Meshes[0].Vertices)
		for _, draw := range resident.Draws {
			assert.Zero(t, draw.Material.OffsetScale)
		}
	}
}

func TestResidentSymbolRejections(t *testing.T) {
	fill, _, dashed := splitPrimitives(false)
	b := NewFragmentBuilder(false, 0, 0)
	b.Primitive(view.TileID{}, fill, nil)
	b.ResidentSymbols()
	_, _, err := b.Finish()
	assert.ErrorIs(t, err, ErrPackingInput, "ResidentSymbols must precede packing")
	var zero FragmentBuilder
	zero.ResidentSymbols()
	_, _, err = zero.Finish()
	assert.ErrorIs(t, err, ErrPackingInput)
	for _, scale := range []float64{0, -1, 1e-50, math.MaxFloat64, math.NaN(), math.Inf(1)} {
		item := residentSymbol(false, true, 12, 1)
		item.Layout.Scale = scale
		b := NewFragmentBuilder(false, 0, 0)
		b.ResidentSymbols()
		labels := b.SymbolLayer(0, 1, func(int) RenderSymbol { return item }, 0, nil)
		assert.Zero(t, labels)
		_, _, err := b.Finish()
		assert.ErrorIs(t, err, ErrPackingInput, "unit layout scale %v", scale)
	}
	// The element limit bounds the three meshes together.
	item := residentSymbol(false, true, 12, 1)
	item.Accepted.Text = false
	count := len(fill.Mesh.Vertices) + len(dashed.Mesh.Vertices) + 6
	for _, limit := range []int{count, count - 1} {
		b := NewFragmentBuilder(false, limit, 0)
		b.Split()
		b.ResidentSymbols()
		b.Primitive(view.TileID{}, fill, nil)
		b.Primitive(view.TileID{}, dashed, nil)
		b.SymbolLayer(0, 1, func(int) RenderSymbol { return item }, 0, symbolSprite)
		_, _, err := b.Finish()
		if limit == count {
			assert.NoError(t, err)
		} else {
			assert.ErrorIs(t, err, geometry.ErrGeometryLimit)
		}
	}
}
