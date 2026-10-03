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
	assert.Equal(t, []int{24, 16, 8, 0}, []int{FullLayout.Bytes(), OffsetLayout.Bytes(), PositionLayout.Bytes(), LayoutCount.Bytes()})
	mesh := Mesh{ID: 1,
		Vertices:  []Vertex{{X: 1, Y: 2, OffsetX: 3, OffsetY: 4, U: 5, V: 6}},
		Offsets:   []OffsetVertex{{X: 7, Y: 8, OffsetX: 9, OffsetY: 10}, {X: 11}},
		Positions: []PositionVertex{{X: 12, Y: 13}, {X: 14}, {X: 15}},
		Indices:   []uint32{0, 0, 0}}
	assert.Equal(t, []int{1, 2, 3, 0}, []int{mesh.Len(FullLayout), mesh.Len(OffsetLayout), mesh.Len(PositionLayout), mesh.Len(LayoutCount)})
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

func TestMeshValidateRejectsEveryNonFiniteField(t *testing.T) {
	bad := []float32{float32(math.NaN()), -float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1)), math.Float32frombits(0x7f800001), math.Float32frombits(0xffffffff)}
	good := []float32{0, float32(math.Copysign(0, -1)), math.MaxFloat32, -math.MaxFloat32, math.SmallestNonzeroFloat32, math.Float32frombits(0x7f7fffff)}
	fields := []func(*Mesh) *float32{
		func(m *Mesh) *float32 { return &m.Vertices[1].X }, func(m *Mesh) *float32 { return &m.Vertices[1].Y },
		func(m *Mesh) *float32 { return &m.Vertices[1].OffsetX }, func(m *Mesh) *float32 { return &m.Vertices[1].OffsetY },
		func(m *Mesh) *float32 { return &m.Vertices[1].U }, func(m *Mesh) *float32 { return &m.Vertices[1].V },
		func(m *Mesh) *float32 { return &m.Offsets[2].X }, func(m *Mesh) *float32 { return &m.Offsets[2].Y },
		func(m *Mesh) *float32 { return &m.Offsets[2].OffsetX }, func(m *Mesh) *float32 { return &m.Offsets[2].OffsetY },
		func(m *Mesh) *float32 { return &m.Positions[0].X }, func(m *Mesh) *float32 { return &m.Positions[0].Y },
	}
	for i, field := range fields {
		for _, values := range [][]float32{bad, good} {
			for _, value := range values {
				mesh := Mesh{ID: 1, Vertices: make([]Vertex, 3), Offsets: make([]OffsetVertex, 4), Positions: make([]PositionVertex, 2)}
				*field(&mesh) = value
				if &values[0] == &bad[0] {
					assert.Error(t, mesh.Validate(), "field %d value %x", i, math.Float32bits(value))
				} else {
					assert.NoError(t, mesh.Validate(), "field %d value %x", i, math.Float32bits(value))
				}
			}
		}
	}
}

func BenchmarkMeshValidate(b *testing.B) {
	mesh := Mesh{ID: 1, Vertices: make([]Vertex, 65536), Offsets: make([]OffsetVertex, 65536), Positions: make([]PositionVertex, 65536), Indices: make([]uint32, 3*65536)}
	for i := range 65536 {
		f := float32(i)
		mesh.Vertices[i] = Vertex{X: f, Y: -f, OffsetX: 0.5, OffsetY: -0.5, U: f / 65536, V: 1}
		mesh.Offsets[i] = OffsetVertex{X: f, Y: -f, OffsetX: 0.25, OffsetY: 1}
		mesh.Positions[i] = PositionVertex{X: f, Y: f}
		mesh.Indices[3*i], mesh.Indices[3*i+1], mesh.Indices[3*i+2] = uint32(i), uint32((i+1)%65536), uint32((i+2)%65536)
	}
	b.SetBytes(int64(mesh.BufferBytes()))
	for b.Loop() {
		if err := mesh.Validate(); err != nil {
			b.Fatal(err)
		}
	}
}

func TestValidateExceptSkipsOnlyContentOfCheckedMeshes(t *testing.T) {
	scene := func() *Scene {
		return &Scene{Meshes: []Mesh{{ID: 1, Vertices: []Vertex{{X: float32(math.NaN())}, {}, {}}, Indices: []uint32{0, 1, 5}},
			{ID: 2, Vertices: make([]Vertex, 3)}}, Draws: []Draw{{Mesh: 1, Count: 3}, {Mesh: 2, Count: 3}}}
	}
	first := func(m *Mesh) bool { return m.ID == 1 }
	assert.Error(t, scene().Validate())
	assert.Error(t, scene().ValidateExcept(func(m *Mesh) bool { return m.ID == 2 }))
	require.NoError(t, scene().ValidateExcept(first), "non-finite vertices and out-of-range indices of a checked mesh")
	for name, change := range map[string]func(*Scene){
		"size":         func(s *Scene) { s.Meshes[0].Vertices = nil },
		"zero ID":      func(s *Scene) { s.Meshes[0].ID = 0 },
		"duplicate ID": func(s *Scene) { s.Meshes[1].ID = 1 },
		"draw range":   func(s *Scene) { s.Draws[0].First = 3 },
		"other mesh":   func(s *Scene) { s.Meshes[1].Vertices[2].V = float32(math.Inf(-1)) },
	} {
		invalid := scene()
		change(invalid)
		assert.Error(t, invalid.ValidateExcept(first), name)
	}
}

func TestPackedSectionsPackAndRead(t *testing.T) {
	assert.Equal(t, []uintptr{4, 8, 12}, []uintptr{unsafe.Sizeof(PackedPositionVertex{}), unsafe.Sizeof(PackedOffsetVertex{}), unsafe.Sizeof(PackedDashedVertex{})}, "packed vertex ABI")
	assert.Equal(t, []int{4, 8, 12}, []int{PackedPositionLayout.Bytes(), PackedOffsetLayout.Bytes(), PackedDashedLayout.Bytes()})
	assert.Equal(t, []bool{false, false, false, true, true, true, false},
		[]bool{FullLayout.Packed(), OffsetLayout.Packed(), PositionLayout.Packed(), PackedPositionLayout.Packed(), PackedOffsetLayout.Packed(), PackedDashedLayout.Packed(), LayoutCount.Packed()})
	assert.True(t, PackedDashedLayout.Valid())
	assert.False(t, LayoutCount.Valid())

	x, y, ok := PackPosition(16.875, -10.0625)
	require.True(t, ok)
	assert.Equal(t, [2]int16{540, -322}, [2]int16{x, y}, "MVT coordinates are exact")
	dx, dy, ok := PackOffset(0.70710678, -3.96)
	require.True(t, ok)
	assert.Equal(t, [2]int16{2896, -16220}, [2]int16{dx, dy})
	for _, v := range [][2]float64{{1024, 0}, {0, -1024.1}, {math.NaN(), 0}, {math.Inf(1), 0}} {
		_, _, ok := PackPosition(v[0], v[1])
		assert.False(t, ok, "%v", v)
	}
	_, _, ok = PackPosition(1023.96875, -1024)
	assert.True(t, ok, "the int16 range itself")
	_, _, ok = PackOffset(8, 0)
	assert.False(t, ok)

	mesh := Mesh{ID: 1, Vertices: []Vertex{{X: 1}},
		PackedPositions: []PackedPositionVertex{{X: 32, Y: -64}, {X: 1}},
		PackedOffsets:   []PackedOffsetVertex{{X: 96, Y: 16, OffsetX: 4096, OffsetY: -2048}},
		PackedDashed:    []PackedDashedVertex{{X: 8, OffsetY: 1, U: 12.5}, {}, {}},
		Indices:         []uint32{0, 1, 2}}
	assert.Equal(t, []int{1, 0, 0, 2, 1, 3}, []int{mesh.Len(FullLayout), mesh.Len(OffsetLayout), mesh.Len(PositionLayout),
		mesh.Len(PackedPositionLayout), mesh.Len(PackedOffsetLayout), mesh.Len(PackedDashedLayout)})
	assert.Equal(t, []uint64{0, 24, 24, 24, 32, 40}, []uint64{mesh.SectionOffset(FullLayout), mesh.SectionOffset(OffsetLayout), mesh.SectionOffset(PositionLayout),
		mesh.SectionOffset(PackedPositionLayout), mesh.SectionOffset(PackedOffsetLayout), mesh.SectionOffset(PackedDashedLayout)})
	assert.Equal(t, uint64(76), mesh.VertexBytes())
	assert.Equal(t, Vertex{X: 1, Y: -2}, mesh.At(PackedPositionLayout, 0))
	assert.Equal(t, Vertex{X: 3, Y: 0.5, OffsetX: 1, OffsetY: -0.5}, mesh.At(PackedOffsetLayout, 0))
	assert.Equal(t, Vertex{X: 0.25, OffsetY: 1.0 / 4096, U: 12.5}, mesh.At(PackedDashedLayout, 0))
	require.NoError(t, mesh.Validate())

	scene := Scene{Meshes: []Mesh{mesh}, Textures: []Texture{}, Draws: []Draw{{Mesh: 1, Count: 3, Layout: PackedDashedLayout}}}
	require.NoError(t, scene.Validate())
	scene.Draws[0].Layout = PackedPositionLayout
	assert.Error(t, scene.Validate(), "index 2 is past the packed positions")
	scene.Draws[0].Layout = LayoutCount
	assert.Error(t, scene.Validate())

	invalid := mesh
	invalid.PackedDashed = []PackedDashedVertex{{U: float32(math.NaN())}, {}, {}}
	assert.Error(t, invalid.Validate(), "the distance must be finite")
	invalid = Mesh{ID: 1, PackedOffsets: []PackedOffsetVertex{{}}}
	require.NoError(t, invalid.Validate(), "a packed section alone is a mesh")

	encoded, err := json.Marshal(mesh)
	require.NoError(t, err)
	var decoded Mesh
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	assert.Equal(t, mesh, decoded)
	indexed, err := IndexMesh(mesh)
	require.NoError(t, err)
	assert.Equal(t, mesh, indexed, "IndexMesh leaves meshes with other sections alone")
}

func TestShortIndicesCountFromTheirDrawsBase(t *testing.T) {
	positions := make([]PackedPositionVertex, 70_000)
	mesh := Mesh{ID: 1, Vertices: []Vertex{{X: 1}, {X: 2}, {X: 3}}, PackedPositions: positions, ShortIndices: []uint16{0, 1, 2, 2, 1, 0}}
	assert.True(t, mesh.Indexed())
	assert.Equal(t, 6, mesh.IndexCount())
	assert.Equal(t, uint32(2), mesh.Index(3))
	assert.Equal(t, uint64(12), mesh.IndexBytes())
	assert.Equal(t, mesh.VertexBytes()+12, mesh.BufferBytes())
	require.NoError(t, mesh.Validate())

	scene := Scene{Meshes: []Mesh{mesh}, Draws: []Draw{
		{Mesh: 1, Count: 3},
		{Mesh: 1, First: 3, Count: 3, Layout: PackedPositionLayout, Base: 69_997},
	}}
	require.NoError(t, scene.Validate())
	for _, draw := range []Draw{
		{Mesh: 1, Count: 3, Base: 1},                                    // index 2 is past the full section
		{Mesh: 1, Count: 3, Layout: PackedPositionLayout, Base: 69_998}, // and past the packed one
		{Mesh: 1, Count: 3, Layout: PackedPositionLayout, Base: 70_000},
		{Mesh: 1, Count: 3, Layout: OffsetLayout},
	} {
		scene.Draws = []Draw{draw}
		assert.Error(t, scene.Validate(), "%+v", draw)
	}

	both := mesh
	both.Indices = []uint32{0, 1, 2}
	assert.Error(t, both.Validate(), "a mesh has one index width")
	unindexed := Scene{Meshes: []Mesh{{ID: 1, Vertices: []Vertex{{X: 1}, {X: 2}, {X: 3}, {X: 4}}}}, Draws: []Draw{{Mesh: 1, Count: 3, Base: 1}}}
	assert.Error(t, unindexed.Validate(), "only indices count from a base")
	long := Scene{Meshes: []Mesh{{ID: 1, PackedPositions: positions, Indices: []uint32{0, 1, 2}}}, Draws: []Draw{{Mesh: 1, Count: 3, Layout: PackedPositionLayout, Base: 69_997}}}
	require.NoError(t, long.Validate(), "uint32 indices may count from a base too")
	long.Draws[0].Base = 69_998
	assert.Error(t, long.Validate())

	draws := AppendDraw(nil, Draw{Mesh: 1, Count: 3})
	draws = AppendDraw(draws, Draw{Mesh: 1, First: 3, Count: 3, Base: 3})
	assert.Len(t, draws, 2, "draws counting from other vertices don't merge")
	draws = AppendDraw(draws, Draw{Mesh: 1, First: 6, Count: 3, Base: 3})
	assert.Equal(t, []Draw{{Mesh: 1, Count: 3}, {Mesh: 1, First: 3, Count: 6, Base: 3}}, draws)

	encoded, err := json.Marshal(scene.Meshes[0])
	require.NoError(t, err)
	var decoded Mesh
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	assert.Equal(t, mesh, decoded)
	short := Mesh{ID: 1, Vertices: []Vertex{{X: 1}, {X: 2}, {X: 3}, {X: 1}, {X: 2}, {X: 3}}, ShortIndices: []uint16{0, 1, 2}}
	indexed, err := IndexMesh(short)
	require.NoError(t, err)
	assert.Equal(t, short, indexed, "IndexMesh leaves indexed meshes alone")
}
