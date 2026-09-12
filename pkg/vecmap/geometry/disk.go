package geometry

import "math"

const DiskSections = 8

var diskDirections = [...]Point{
	{X: 1, Y: 0},
	{X: math.Sqrt2 / 2, Y: math.Sqrt2 / 2},
	{X: 0, Y: 1},
	{X: -math.Sqrt2 / 2, Y: math.Sqrt2 / 2},
	{X: -1, Y: 0},
	{X: -math.Sqrt2 / 2, Y: -math.Sqrt2 / 2},
	{X: 0, Y: -1},
	{X: math.Sqrt2 / 2, Y: -math.Sqrt2 / 2},
	{X: 1, Y: 0},
}

// DiskRing is the existing eight-section line cap/join approximation. Keep the
// original arithmetic and closing point for bit-identical triangle expansion.
func DiskRing(center Point, radius float64) [DiskSections + 1]Point {
	var ring [DiskSections + 1]Point
	for i, direction := range diskDirections {
		ring[i] = Point{X: center.X + direction.X*radius, Y: center.Y + direction.Y*radius}
	}
	return ring
}

func AppendDisk(b *Builder[Point], center Point, radius float64) error {
	ring := DiskRing(center, radius)
	var vertices [DiskSections + 1]Point
	vertices[0] = center
	copy(vertices[1:], ring[:DiskSections])
	var indices [DiskSections * 3]uint32
	for i := range DiskSections {
		indices[i*3+1], indices[i*3+2] = uint32(i+1), uint32((i+1)%DiskSections+1)
	}
	return b.Append(vertices[:], indices[:])
}
