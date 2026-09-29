package view

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStyleZoomAt(t *testing.T) {
	assert.Equal(t, StyleZoom(12.53), StyleZoomAt(12.53, 0))
	assert.Equal(t, 12.5, StyleZoomAt(12.53, 0))
	assert.Equal(t, 11.5, StyleZoomAt(12.53, 1))
	assert.Equal(t, 10.5, StyleZoomAt(12.53, 2))
	assert.Zero(t, StyleZoomAt(0.5, 1))
	assert.Equal(t, 12.5, StyleZoomAt(12.53, -1))
	assert.Equal(t, StyleZoomAt(12.53, MaxCoarser), StyleZoomAt(12.53, MaxCoarser+1))
}
