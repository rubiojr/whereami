package scene

import (
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
