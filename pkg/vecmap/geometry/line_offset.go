package geometry

import "math"

// OffsetLine applies the existing averaged-normal offset with a 4x miter bound.
// Zero offset borrows the input unchanged; nonzero offset returns an owned copy.
func OffsetLine(path []Point, offset float64) []Point {
	if offset == 0 {
		return path
	}
	offsetPath := make([]Point, len(path))
	for index, point := range path {
		var normalX, normalY float64
		var referenceX, referenceY float64
		normalCount := 0
		if index > 0 {
			deltaX := point.X - path[index-1].X
			deltaY := point.Y - path[index-1].Y
			length := math.Hypot(deltaX, deltaY)
			if length > Epsilon {
				referenceX, referenceY = -deltaY/length, deltaX/length
				normalX += referenceX
				normalY += referenceY
				normalCount++
			}
		}
		if index+1 < len(path) {
			deltaX := path[index+1].X - point.X
			deltaY := path[index+1].Y - point.Y
			length := math.Hypot(deltaX, deltaY)
			if length > Epsilon {
				referenceX, referenceY = -deltaY/length, deltaX/length
				normalX += referenceX
				normalY += referenceY
				normalCount++
			}
		}
		length := math.Hypot(normalX, normalY)
		if length > Epsilon {
			normalX /= length
			normalY /= length
		}
		distance := offset
		if normalCount == 2 {
			denominator := normalX*referenceX + normalY*referenceY
			if math.Abs(denominator) > Epsilon {
				distance = offset / denominator
				distance = max(-4*math.Abs(offset), min(4*math.Abs(offset), distance))
			}
		}
		offsetPath[index] = Point{X: point.X + normalX*distance, Y: point.Y + normalY*distance}
	}
	return offsetPath
}
