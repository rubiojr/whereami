package scene

import (
	"encoding/json"
	"math"
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
