package quick

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSDFTextPasses(t *testing.T) {
	fill := [4]int{-10, 128, 300, 255}
	halo := [4]int{255, -1, 64, 128}
	passes, count := sdfTextPasses(fill, halo, 2, 3, -1)
	require.Equal(t, 2, count)
	assert.Equal(t, sdfMaterialData{4: 1, 6: 64.0 / 255, 7: 128.0 / 255, 8: 2, 9: 3}, passes[0])
	assert.Equal(t, sdfMaterialData{1: 128.0 / 255, 2: 1, 3: 1, 8: 2}, passes[1])
	for _, width := range []float32{0, -1} {
		single, n := sdfTextPasses(fill, halo, 2, width, 5)
		assert.Equal(t, 1, n)
		assert.Equal(t, passes[1], single[0])
		assert.Zero(t, single[1])
	}
	halo[3] = 0
	single, n := sdfTextPasses(fill, halo, 2, 3, 5)
	assert.Equal(t, 1, n)
	assert.Equal(t, passes[1], single[0])
}

func TestPatternVertices(t *testing.T) {
	// Negative coordinates and non-square patterns exercise world-wrap phase.
	points := []float32{-8, -4, 24, 0, 0, 12}
	vertices := make([]float32, 12)
	writePatternVertices(vertices, points, 16, 8, 4, 2)
	assert.Equal(t, []float32{-8, -4, -0.25, -0.25, 24, 0, 1.75, 0.25, 0, 12, 0.25, 1.75}, vertices)
	assert.Equal(t, []float32{-8, -4, 24, 0, 0, 12}, points)
}

func TestRenderImageSize(t *testing.T) {
	assert.True(t, validImageSize(24, 2, 3, 4))
	assert.True(t, validImageSize(6, 2, 3, 1))
	for _, size := range [][3]int{{0, 0, 1}, {4, -1, -1}, {4, 2, 2}, {0, 1 << 30, 4}, {0, 4, 1 << 31}} {
		assert.False(t, validImageSize(size[0], size[1], size[2], 4))
	}
}

func BenchmarkPatternVertices(b *testing.B) {
	for _, count := range []int{6, 6144, 98304} {
		points := make([]float32, count*2)
		for index := range points {
			points[index] = float32(index%513) - 128
		}
		destination := make([]float32, count*4)
		b.Run(fmt.Sprintf("%d/direct", count), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				writePatternVertices(destination, points, 37, 19, 3, 7)
			}
		})
		b.Run(fmt.Sprintf("%d/legacy-copy", count), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				vertices := make([]float32, len(points)*2)
				writePatternVertices(vertices, points, 37, 19, 3, 7)
				copy(destination, vertices)
			}
		})
	}
}
