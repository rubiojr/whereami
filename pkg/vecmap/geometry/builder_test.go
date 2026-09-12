package geometry

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuilderTopology(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		b := NewBuilder[int](indexed, 18)
		require.NoError(t, b.Triangle(10, 11, 12))
		require.NoError(t, b.Quad(20, 21, 22, 23))
		require.NoError(t, b.Append([]int{30, 31, 32, 33}, []uint32{2, 0, 1}))
		require.NoError(t, b.Append([]int{99}, []uint32{}))
		assert.Equal(t, 12, b.Count())
		want := []int{10, 11, 12, 20, 21, 22, 20, 22, 23, 32, 30, 31}
		if indexed {
			var got []int
			for _, index := range b.Indices {
				got = append(got, b.Vertices[index])
			}
			assert.Equal(t, want, got)
			assert.Equal(t, []uint32{0, 1, 2, 3, 4, 5, 3, 5, 6, 9, 7, 8}, b.Indices)
		} else {
			assert.Nil(t, b.Indices)
			assert.Equal(t, want, b.Vertices)
		}
	}
}

func TestBuilderRejectsAtomically(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		b := NewBuilder[int](indexed, 6)
		require.NoError(t, b.Triangle(1, 2, 3))
		assert.Error(t, b.Append([]int{1}, nil))
		assert.Error(t, b.Append([]int{1}, []uint32{0, 1, 0}))
		assert.Error(t, b.Append([]int{1}, []uint32{0, math.MaxUint32, 0}))
		assert.ErrorIs(t, b.Quad(4, 5, 6, 7), ErrGeometryLimit)
		assert.Equal(t, 3, b.Count())
		assert.Equal(t, []int{1, 2, 3}, b.Vertices)
		require.NoError(t, b.Triangle(4, 5, 6))
		assert.Equal(t, 6, b.Count())
	}
	b := NewBuilder[int](true, -1)
	assert.ErrorIs(t, b.Triangle(1, 2, 3), ErrGeometryLimit)
}

func TestDirectDiskAndText(t *testing.T) {
	center := Point{X: math.Copysign(0, -1), Y: 5}
	disk := NewBuilder[Point](true, DiskSections*3)
	require.NoError(t, AppendDisk(&disk, center, 2))
	assert.Len(t, disk.Vertices, DiskSections+1)
	ring := DiskRing(center, 2)
	for i := range DiskSections {
		want := [3]Point{center, ring[i], ring[i+1]}
		for j := range 3 {
			got := disk.Vertices[disk.Indices[i*3+j]]
			assert.Equal(t, math.Float64bits(want[j].X), math.Float64bits(got.X))
			assert.Equal(t, math.Float64bits(want[j].Y), math.Float64bits(got.Y))
		}
	}
	quad := TextQuad(-1, -2, 3, 4, 0.1, 0.2, 0.3, 0.4)
	assert.Equal(t, TextVertex{X: 3, Y: 4, U: 0.3, V: 0.4}, quad[2])
	vertex := TransformTextVertex(Point{X: 10, Y: 20}, quad[2], Point{X: 1, Y: 2}, 1, 0)
	assert.Equal(t, float32(-6), vertex.OffsetX)
	assert.Equal(t, float32(4), vertex.OffsetY)
	assert.Equal(t, quad[2].U, vertex.U)
	assert.Equal(t, quad[2].V, vertex.V)
}

func TestLineSegmentTopology(t *testing.T) {
	for _, cap := range []string{"butt", "square", "round"} {
		mesh := NewBuilder[Point](true, 54)
		require.NoError(t, AppendLineSegment(&mesh, Point{}, Point{X: 10}, 2, cap, true))
		left, right := 0.0, 10.0
		if cap == "square" {
			left, right = -1, 11
		}
		assert.Equal(t, []Point{{X: left, Y: 1}, {X: left, Y: -1}, {X: right, Y: 1}, {X: right, Y: -1}}, mesh.Vertices[:4])
		assert.Equal(t, []uint32{0, 1, 2, 2, 1, 3}, mesh.Indices[:6])
		if cap == "round" {
			assert.Equal(t, 54, mesh.Count())
		} else {
			assert.Equal(t, 6, mesh.Count())
		}
	}
	empty := NewBuilder[Point](true, 0)
	require.NoError(t, AppendLineSegment(&empty, Point{}, Point{}, 2, "round", true))
	assert.Zero(t, empty.Count())
	assert.ErrorIs(t, AppendLineSegment(&empty, Point{}, Point{X: 10}, 2, "butt", false), ErrGeometryLimit)
	for _, limit := range []int{6, 30} {
		mesh := NewBuilder[Point](true, limit)
		assert.ErrorIs(t, AppendLineSegment(&mesh, Point{}, Point{X: 10}, 2, "round", true), ErrGeometryLimit)
	}
}
