package quick

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSDFTextNodeRejectsInvalidBuffers(t *testing.T) {
	assert.Nil(t, NewQSGSDFAtlas(nil, []byte{0, 0, 0, 0}, 1, 1))
	assert.Nil(t, NewQSGSDFAtlas(&QQuickItem{}, []byte{0, 0, 0, 0}, 1, 1))
	assert.Nil(t, NewQSGSDFAtlas(&QQuickItem{}, []byte{0}, 1, 1))
	var atlas *QSGSDFAtlas
	assert.Nil(t, atlas.NewTextNode(make([]float32, 24), [4]int{}, [4]int{}, 1, 0, 0))
	assert.Nil(t, (&QSGSDFAtlas{}).NewTextNode(make([]float32, 23), [4]int{}, [4]int{}, 1, 0, 0))
}
