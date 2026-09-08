package quick

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransformNodeAffineMatrix(t *testing.T) {
	node := NewQSGTransformNode()
	require.NotNil(t, node)
	defer node.Delete()

	node.SetAffine(2, 3, 4, 5, 6, 7)
	x, y := node.MapPoint(11, 13)
	assert.InDelta(t, 67, x, 1e-6)
	assert.InDelta(t, 116, y, 1e-6)
}

func TestSceneGraphNodeChildLifecycle(t *testing.T) {
	root := NewQSGNode()
	require.NotNil(t, root)
	child := NewQSGTransformNode()
	require.NotNil(t, child)

	root.AppendChildNode(child.QSGNode)
	assert.Equal(t, 1, root.ChildCount())
	root.RemoveChildNode(child.QSGNode)
	assert.Zero(t, root.ChildCount())

	child.Delete()
	root.Delete()
}

func TestClipNodeRect(t *testing.T) {
	node := NewQSGClipNode()
	require.NotNil(t, node)
	defer node.Delete()

	node.SetRect(1, 2, 256, 255)
}

func BenchmarkTransformNodeSetAffineUnchanged(b *testing.B) {
	node := NewQSGTransformNode()
	require.NotNil(b, node)
	defer node.Delete()

	node.SetAffine(2, 3, 4, 5, 6, 7)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		node.SetAffine(2, 3, 4, 5, 6, 7)
	}
}
