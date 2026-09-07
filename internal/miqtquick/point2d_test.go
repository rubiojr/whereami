package quick

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewQSGPointGeometry(t *testing.T) {
	geometry := NewQSGPointGeometry([]float32{1, 2, 3, 4})
	require.NotNil(t, geometry)
	defer geometry.Delete()

	vertices := geometry.VertexDataAsPoint2D()
	require.NotNil(t, vertices)
	assert.Equal(t, float32(1), vertices.X())
	assert.Equal(t, float32(2), vertices.Y())
	assert.Nil(t, NewQSGPointGeometry([]float32{1, 2, 3}))
	assert.Nil(t, NewQSGPointGeometry(nil))
}
