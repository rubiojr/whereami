package retained

import (
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyCopyChargeExcludesBorrowedPayload(t *testing.T) {
	input := triangle()
	before := CopyBytes("a", input)
	vertices := make([]scene.Vertex, len(input.Meshes[0].Vertices), 1000)
	copy(vertices, input.Meshes[0].Vertices)
	input.Meshes[0].Vertices = vertices
	assert.Equal(t, before, CopyBytes("a", input))
	draws := make([]scene.Draw, len(input.Draws), 100)
	copy(draws, input.Draws)
	input.Draws = draws
	assert.Equal(t, before, CopyBytes("a", input), "unused input metadata capacity is not copied")
	s := newStore(t, Limits{})
	require.NoError(t, s.Apply([]Change{{"a", input}}))
	p := s.parts["a"]
	assert.Equal(t, len(p.scene.Draws), cap(p.scene.Draws))
	assert.Same(t, &vertices[0], &p.scene.Meshes[0].Vertices[0])
	assert.Zero(t, CopyBytes("a", nil))
}
