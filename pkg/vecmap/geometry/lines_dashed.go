package geometry

import (
	"fmt"
	"math"
)

// MaxShaderDashes is the longest dash array a DashPattern carries.
const MaxShaderDashes = 4

// DashedVertex is an ExtrudedVertex with its distance along the path centerline
// in tile units. Both vertices across the line share one distance, so the value
// interpolated over a segment quad is the centerline distance of each fragment.
type DashedVertex struct {
	Anchor, Direction Point
	Distance          float64
}

// Position applies a half width, as ExtrudedVertex.Position.
func (v DashedVertex) Position(halfWidth float64) Point {
	return Point{X: v.Anchor.X + v.Direction.X*halfWidth, Y: v.Anchor.Y + v.Direction.Y*halfWidth}
}

// DashedMesh has one quad per nondegenerate path segment.
type DashedMesh struct {
	Vertices []DashedVertex
	Indices  []uint32
}

// DashPattern is an even dash array of alternating dash and gap lengths in
// multiples of the line width, padded with zeros. It is the part of a dashed
// line's paint that does not depend on the evaluated width.
type DashPattern [MaxShaderDashes]float64

// NewDashPattern converts an evaluated dash array for a butt-capped line of the
// given tile-unit width. It reports false where the consumer cannot reproduce
// TessellateLines: more than four entries after an odd array is repeated, an
// invalid or unscaled entry, an entry too short for the baked walk to keep, a
// value that float32 cannot carry as a positive finite number, or a first dash
// of zero, which can leave a path without any geometry. Those lines must stay
// baked.
func NewDashPattern(dashes []float64, width float64) (DashPattern, bool) {
	var pattern DashPattern
	count := len(dashes)
	if count%2 != 0 {
		count *= 2
	}
	if count == 0 || count > MaxShaderDashes || !(width > Epsilon) || !packable(width) {
		return DashPattern{}, false
	}
	for index := range count {
		dash := dashes[index%len(dashes)]
		scaled := dash * width
		if math.IsNaN(scaled) || math.IsInf(scaled, 0) || dash < 0 || (dash != 0 && (scaled <= Epsilon || !packable(dash))) {
			return DashPattern{}, false
		}
		pattern[index] = dash
	}
	if pattern[0] == 0 {
		return DashPattern{}, false
	}
	return pattern, true
}

// packable reports whether a positive value stays positive and finite in float32.
func packable(value float64) bool {
	packed := float32(value)
	return packed > 0 && !math.IsInf(float64(packed), 0)
}

// Length is the pattern period in multiples of the line width.
func (p DashPattern) Length() float64 { return p[0] + p[1] + p[2] + p[3] }

// Covers reports whether a centerline distance lies on a dash for a line width
// in the distance's unit. It is the reference for the consumer's per-fragment
// test: dashes are half-open intervals starting at the path's first point.
func (p DashPattern) Covers(distance, width float64) bool {
	period := p.Length()
	if !(period > 0) || !(width > 0) {
		return false
	}
	position := math.Mod(distance/width, period)
	return position < p[0] || (position >= p[0]+p[1] && position < p[0]+p[1]+p[2])
}

// TessellateDashedLines emits one butt-capped quad per path segment with
// width-independent vertices and centerline distances. Dashes are left to the
// consumer, which keeps the fragments a DashPattern covers. The covered area is
// that of TessellateLines with the pattern's dashes, a butt cap and no offset,
// within floating-point rounding, for every line width. Like the baked form it
// emits no joins. Distances restart at each path and skip segments no longer than
// Epsilon. Inputs are neither modified nor retained; the result owns its slices.
func TessellateDashedLines(paths [][]Point, maximumTriangles int, indexed bool) (DashedMesh, error) {
	if maximumTriangles < 0 || maximumTriangles > MaxLineTriangles {
		return DashedMesh{}, fmt.Errorf("%w: invalid line triangle limit", ErrGeometryLimit)
	}
	mesh := NewBuilder[DashedVertex](indexed, maximumTriangles*3)
	for _, rawPath := range paths {
		path := cleanLine(rawPath)
		distance := 0.0
		for index := 1; index < len(path); index++ {
			start, end := path[index-1], path[index]
			deltaX, deltaY := end.X-start.X, end.Y-start.Y
			length := math.Hypot(deltaX, deltaY)
			if length <= Epsilon {
				continue
			}
			normal := Point{X: -deltaY / length, Y: deltaX / length}
			opposite := Point{X: -normal.X, Y: -normal.Y}
			if err := mesh.Append([]DashedVertex{
				{Anchor: start, Direction: normal, Distance: distance},
				{Anchor: start, Direction: opposite, Distance: distance},
				{Anchor: end, Direction: normal, Distance: distance + length},
				{Anchor: end, Direction: opposite, Distance: distance + length},
			}, []uint32{0, 1, 2, 2, 1, 3}); err != nil {
				return DashedMesh{}, err
			}
			distance += length
		}
	}
	return DashedMesh{Vertices: mesh.Vertices, Indices: mesh.Indices}, nil
}
