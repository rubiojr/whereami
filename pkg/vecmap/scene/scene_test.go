package scene

import (
	"encoding/json"
	"math"
	"slices"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSceneValidation(t *testing.T) {
	s := Scene{Meshes: []Mesh{{ID: 1, Vertices: make([]Vertex, 3)}}, Draws: []Draw{{Mesh: 1, Count: 3}}}
	require.NoError(t, s.Validate())
	assert.Equal(t, uintptr(24), unsafe.Sizeof(Vertex{}), "packed vertex ABI")
	s.Draws[0].First = math.MaxUint32
	assert.Error(t, s.Validate())
	s.Draws[0].First = 0
	s.Draws[0].Material.Texture = 9
	assert.Error(t, s.Validate())
	s.Draws[0].Material.Texture = 0
	s.Meshes[0].Vertices[0].X = float32(math.NaN())
	assert.Error(t, s.Validate())
	s.Meshes[0].Vertices[0].X = 0
	s.Textures = []Texture{{ID: 2, Width: 1 << 30, Height: 1 << 30}}
	assert.Error(t, s.Validate())
	s.Textures = nil
	s.Draws[0].Material.OffsetScale = 2.5
	require.NoError(t, s.Validate())
	for _, scale := range []float32{-1, float32(math.NaN()), float32(math.Inf(1))} {
		s.Draws[0].Material.OffsetScale = scale
		assert.Error(t, s.Validate())
	}
}

func TestOffsetScaleSplitsBatchesAndSerializes(t *testing.T) {
	narrow := Material{Color: [4]float32{1, 0, 0, 1}, MapAligned: true, OffsetScale: 1.5}
	wide := narrow
	wide.OffsetScale = 3
	draws := AppendDraw(nil, Draw{Mesh: 1, Count: 3, Material: narrow})
	draws = AppendDraw(draws, Draw{Mesh: 1, First: 3, Count: 3, Material: narrow})
	draws = AppendDraw(draws, Draw{Mesh: 1, First: 6, Count: 3, Material: wide})
	require.Len(t, draws, 2, "widths are per draw")
	encoded, err := json.Marshal(Material{})
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "OffsetScale", "existing scene files are unchanged")
	encoded, err = json.Marshal(wide)
	require.NoError(t, err)
	var decoded Material
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	assert.Equal(t, wide, decoded)
}

func TestAdjacentBatchingPreservesOrder(t *testing.T) {
	red := Material{Color: [4]float32{1, 0, 0, 0.5}}
	blue := Material{Color: [4]float32{0, 0, 1, 0.5}}
	var draws []Draw
	for index, material := range []Material{red, red, blue, red} {
		draws = AppendDraw(draws, Draw{Mesh: 1, First: uint32(index * 3), Count: 3, Material: material})
	}
	require.Len(t, draws, 3)
	assert.Equal(t, uint32(6), draws[0].Count)
	assert.Equal(t, blue, draws[1].Material)
	assert.Equal(t, red, draws[2].Material)
	draws = AppendDraw(draws, Draw{Mesh: 1, First: 12, Count: 3, Material: red, Clip: [4]float32{0, 0, 10, 10}})
	assert.Len(t, draws, 4, "clip changes must split a batch")
}

func TestDashedMaterialValidatesAndSerializes(t *testing.T) {
	s := Scene{Meshes: []Mesh{{ID: 1, Vertices: make([]Vertex, 3)}}, Draws: []Draw{{Mesh: 1, Count: 3}}}
	dashed := Material{Kind: Dashed, Color: [4]float32{1, 0, 0, 1}, MapAligned: true, OffsetScale: 1.5, DashUnit: 0.75, Dashes: [4]float32{2, 1, 0, 3}}
	s.Draws[0].Material = dashed
	require.NoError(t, s.Validate())
	for name, change := range map[string]func(*Material){
		"kind":       func(m *Material) { m.Kind = Dashed + 1 },
		"zero unit":  func(m *Material) { m.DashUnit = 0 },
		"nan unit":   func(m *Material) { m.DashUnit = float32(math.NaN()) },
		"first dash": func(m *Material) { m.Dashes[0] = 0 },
		"gap":        func(m *Material) { m.Dashes[1] = -1 },
		"dash":       func(m *Material) { m.Dashes[2] = -1 },
		"last gap":   func(m *Material) { m.Dashes[3] = float32(math.Inf(1)) },
		"negative":   func(m *Material) { m.Dashes[3] = -1 },
	} {
		s.Draws[0].Material = dashed
		change(&s.Draws[0].Material)
		assert.Error(t, s.Validate(), name)
	}
	// Other kinds ignore the pattern but still require finite values.
	s.Draws[0].Material = Material{Dashes: [4]float32{0, -1}}
	require.NoError(t, s.Validate())
	s.Draws[0].Material.Dashes[0] = float32(math.NaN())
	assert.Error(t, s.Validate())

	other := dashed
	other.DashUnit = 1
	draws := AppendDraw(nil, Draw{Mesh: 1, Count: 3, Material: dashed})
	draws = AppendDraw(draws, Draw{Mesh: 1, First: 3, Count: 3, Material: dashed})
	draws = AppendDraw(draws, Draw{Mesh: 1, First: 6, Count: 3, Material: other})
	require.Len(t, draws, 2, "dash units are per draw")
	encoded, err := json.Marshal(Material{})
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "Dash", "existing scene files are unchanged")
	encoded, err = json.Marshal(dashed)
	require.NoError(t, err)
	var decoded Material
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	assert.Equal(t, dashed, decoded)
}

func TestVertexSectionsPackAndRead(t *testing.T) {
	assert.Equal(t, uintptr(16), unsafe.Sizeof(OffsetVertex{}), "packed vertex ABI")
	assert.Equal(t, uintptr(8), unsafe.Sizeof(PositionVertex{}), "packed vertex ABI")
	assert.Equal(t, []int{24, 16, 8, 0}, []int{FullLayout.Bytes(), OffsetLayout.Bytes(), PositionLayout.Bytes(), Layout(3).Bytes()})
	mesh := Mesh{ID: 1,
		Vertices:  []Vertex{{X: 1, Y: 2, OffsetX: 3, OffsetY: 4, U: 5, V: 6}},
		Offsets:   []OffsetVertex{{X: 7, Y: 8, OffsetX: 9, OffsetY: 10}, {X: 11}},
		Positions: []PositionVertex{{X: 12, Y: 13}, {X: 14}, {X: 15}},
		Indices:   []uint32{0, 0, 0}}
	assert.Equal(t, []int{1, 2, 3, 0}, []int{mesh.Len(FullLayout), mesh.Len(OffsetLayout), mesh.Len(PositionLayout), mesh.Len(3)})
	assert.Equal(t, []uint64{0, 24, 56}, []uint64{mesh.SectionOffset(FullLayout), mesh.SectionOffset(OffsetLayout), mesh.SectionOffset(PositionLayout)})
	assert.Equal(t, uint64(80), mesh.VertexBytes())
	assert.Equal(t, uint64(92), mesh.BufferBytes())
	assert.Equal(t, mesh.Vertices[0], mesh.At(FullLayout, 0))
	assert.Equal(t, Vertex{X: 7, Y: 8, OffsetX: 9, OffsetY: 10}, mesh.At(OffsetLayout, 0))
	assert.Equal(t, Vertex{X: 12, Y: 13}, mesh.At(PositionLayout, 0))
	assert.Panics(t, func() { mesh.At(OffsetLayout, 2) })
	require.NoError(t, mesh.Validate())

	for _, change := range []func(*Mesh){
		func(m *Mesh) { m.Offsets[1].OffsetY = float32(math.NaN()) },
		func(m *Mesh) { m.Positions[2].Y = float32(math.Inf(1)) },
		func(m *Mesh) { m.Indices[1] = 3 },
		func(m *Mesh) { m.Vertices, m.Offsets, m.Positions = nil, nil, nil },
	} {
		invalid := Mesh{ID: 1, Vertices: slices.Clone(mesh.Vertices), Offsets: slices.Clone(mesh.Offsets),
			Positions: slices.Clone(mesh.Positions), Indices: slices.Clone(mesh.Indices)}
		change(&invalid)
		assert.Error(t, invalid.Validate())
	}
	only := Mesh{ID: 1, Positions: make([]PositionVertex, 3)}
	require.NoError(t, only.Validate(), "a mesh needs vertices in any one section")
}

func TestDrawsAreBoundedByTheirSection(t *testing.T) {
	indexed := Scene{Meshes: []Mesh{{ID: 1, Vertices: make([]Vertex, 3), Offsets: make([]OffsetVertex, 4), Positions: make([]PositionVertex, 6),
		Indices: []uint32{0, 1, 2, 0, 1, 3, 3, 4, 5}}},
		Draws: []Draw{{Mesh: 1, Count: 3}, {Mesh: 1, First: 3, Count: 3, Layout: OffsetLayout}, {Mesh: 1, First: 6, Count: 3, Layout: PositionLayout}}}
	require.NoError(t, indexed.Validate())
	// Index three is in range of the mesh, but not of the full section.
	indexed.Draws[0].First = 3
	assert.ErrorContains(t, indexed.Validate(), "vertex section")
	indexed.Draws[0].First = 0
	indexed.Draws[1].First = 6
	assert.ErrorContains(t, indexed.Validate(), "vertex section")
	indexed.Draws[1].First = 3
	indexed.Draws[2].Layout = 3
	assert.Error(t, indexed.Validate())
	indexed.Draws[2].Layout = PositionLayout
	indexed.Draws[2].First = 9
	assert.Error(t, indexed.Validate())

	expanded := Scene{Meshes: []Mesh{{ID: 1, Vertices: make([]Vertex, 3), Positions: make([]PositionVertex, 6)}},
		Draws: []Draw{{Mesh: 1, Count: 3}, {Mesh: 1, First: 3, Count: 3, Layout: PositionLayout}}}
	require.NoError(t, expanded.Validate())
	expanded.Draws[0].First = 3
	assert.Error(t, expanded.Validate(), "an unindexed range counts vertices of its own section")
	expanded.Draws[0].First = 0
	expanded.Draws[1].Layout = OffsetLayout
	assert.Error(t, expanded.Validate())
}

func TestLayoutSplitsBatchesAndSerializes(t *testing.T) {
	material := Material{Color: [4]float32{1, 0, 0, 1}}
	draws := AppendDraw(nil, Draw{Mesh: 1, Count: 3, Material: material, Layout: PositionLayout})
	draws = AppendDraw(draws, Draw{Mesh: 1, First: 3, Count: 3, Material: material, Layout: PositionLayout})
	require.Len(t, draws, 1)
	assert.Equal(t, uint32(6), draws[0].Count)
	draws = AppendDraw(draws, Draw{Mesh: 1, First: 6, Count: 3, Material: material, Layout: OffsetLayout})
	require.Len(t, draws, 2, "a range of another section is another draw")

	data, err := json.Marshal(Scene{Meshes: []Mesh{{ID: 1, Vertices: make([]Vertex, 3)}}, Draws: []Draw{{Mesh: 1, Count: 3}}})
	require.NoError(t, err)
	assert.NotContains(t, string(data), "Layout", "existing scene files are unchanged")
	assert.NotContains(t, string(data), "Positions")
	assert.NotContains(t, string(data), "Offsets")
	sectioned := Scene{Meshes: []Mesh{{ID: 1, Positions: make([]PositionVertex, 3)}}, Draws: []Draw{{Mesh: 1, Count: 3, Layout: PositionLayout}}}
	data, err = json.Marshal(sectioned)
	require.NoError(t, err)
	var decoded Scene
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, sectioned.Draws, decoded.Draws)
	assert.Equal(t, sectioned.Meshes[0].Positions, decoded.Meshes[0].Positions)
	require.NoError(t, decoded.Validate())
}
