//go:build vecmap_rhi

package qtrhi

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestGeneratedLifecycle(t *testing.T) {
	for range 100 {
		node := NewQSGRenderNode()
		called, destroyed := false, false
		OnDestroyed(node.UnsafePointer(), func() { destroyed = true })
		node.OnPrepare(func(func()) { called = true })
		assert.Panics(t, func() { node.OnPrepare(func(func()) { t.Error("replaced existing callback") }) })
		node.Prepare()
		assert.True(t, called)
		node.Delete()
		assert.True(t, destroyed)
	}
	destructionCallbacks.Range(func(_, _ any) bool { t.Error("native destruction callback leaked"); return true })
}

func TestPairArrayBounds(t *testing.T) {
	var cb QRhiCommandBuffer
	assert.Panics(t, func() {
		cb.SetVertexInput(0, 2, struct {
			First  *QRhiBuffer
			Second uint32
		}{})
	})
	assert.Panics(t, func() {
		cb.SetShaderResources3(nil, 2, struct {
			First  int
			Second uint32
		}{})
	})
	assert.Panics(t, func() { cb.SetShaderResources2(nil, 1) })
}
