package scene

import (
	"math"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIndexMeshPreservesEveryVertexAndDrawRange(t *testing.T) {
	vertices := []Vertex{{X: 0, Y: 0}, {X: 1, Y: 0, U: 1}, {X: 1, Y: 1, U: 1, V: 1}, {X: 0, Y: 0}, {X: 1, Y: 1, U: 1, V: 1}, {X: 0, Y: 1, V: 1}}
	vertices = append(vertices, vertices...) // a second material pass over the same geometry
	original := Mesh{ID: 7, Revision: 3, Vertices: vertices}
	before := slices.Clone(vertices)
	indexed, err := IndexMesh(original)
	require.NoError(t, err)
	require.Len(t, indexed.Vertices, 4)
	require.Len(t, indexed.Indices, len(vertices))
	assert.Equal(t, original.ID, indexed.ID)
	assert.Equal(t, original.Revision, indexed.Revision)
	for i, index := range indexed.Indices {
		assert.Equal(t, vertexBits(vertices[i]), vertexBits(indexed.Vertices[index]))
	}
	assert.Equal(t, before, original.Vertices)
	assert.Less(t, indexed.BufferBytes(), original.BufferBytes())
	s := Scene{Meshes: []Mesh{indexed}, Draws: []Draw{{Mesh: 7, First: 0, Count: 6}, {Mesh: 7, First: 6, Count: 6}}}
	require.NoError(t, s.Validate())
	s.Draws[1].First = 9
	assert.Error(t, s.Validate())
	indexed.Indices[0] = math.MaxUint32
	assert.ErrorContains(t, indexed.Validate(), "index out of bounds")
}

func TestIndexMeshPreservesAttributesAndSignedZero(t *testing.T) {
	unique := []Vertex{{X: 1}, {X: 1, U: 1}, {X: 1, V: 1}, {X: 1, OffsetX: 1}, {X: 1, OffsetY: 1}, {X: 1, Y: math.Float32frombits(1 << 31)}}
	vertices := append(slices.Clone(unique), unique...)
	mesh, err := IndexMesh(Mesh{ID: 1, Vertices: vertices})
	require.NoError(t, err)
	require.Len(t, mesh.Vertices, len(unique))
	for i, index := range mesh.Indices {
		assert.Equal(t, vertexBits(vertices[i]), vertexBits(mesh.Vertices[index]))
	}
	assert.Equal(t, uint32(1<<31), math.Float32bits(mesh.Vertices[5].Y))
}

func TestIndexMeshSkipsLargerAndExistingRepresentations(t *testing.T) {
	vertices := []Vertex{{X: 1}, {X: 2}, {X: 3}, {X: 4}, {X: 5}, {X: 6}}
	mesh := Mesh{ID: 1, Vertices: vertices}
	indexed, err := IndexMesh(mesh)
	require.NoError(t, err)
	assert.Empty(t, indexed.Indices)
	assert.Same(t, &mesh.Vertices[0], &indexed.Vertices[0])
	mesh.Indices = []uint32{0, 1, 2}
	indexed, err = IndexMesh(mesh)
	require.NoError(t, err)
	assert.Same(t, &mesh.Indices[0], &indexed.Indices[0])
	mesh.Vertices[0].U = float32(math.Inf(1))
	_, err = IndexMesh(mesh)
	assert.Error(t, err)
}

func BenchmarkIndexMesh(b *testing.B) {
	vertices := make([]Vertex, 0, 256*256*6)
	for y := range 256 {
		for x := range 256 {
			a, c := float32(x), float32(y)
			vertices = append(vertices, Vertex{X: a, Y: c}, Vertex{X: a + 1, Y: c}, Vertex{X: a + 1, Y: c + 1}, Vertex{X: a, Y: c}, Vertex{X: a + 1, Y: c + 1}, Vertex{X: a, Y: c + 1})
		}
	}
	mesh := Mesh{ID: 1, Vertices: vertices}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := IndexMesh(mesh); err != nil {
			b.Fatal(err)
		}
	}
}
