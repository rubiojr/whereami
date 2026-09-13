package geometry

import (
	"errors"
	"fmt"
	"math"
)

var ErrLinePattern = errors.New("invalid dashed line pattern")

const maxDashedSegmentsPerPath = 100_000

type lineSegment struct{ Start, End Point }

// Keep pattern allocation and growth in the consuming function so the compiler
// can retain small, non-escaping dash arrays on its stack.
func scaleDashPattern(pattern, dashes []float64, width float64) (bool, error) {
	patternLength := 0.0
	for index, dash := range dashes {
		pattern[index] = dash * width
		if pattern[index] < 0 || math.IsNaN(pattern[index]) || math.IsInf(pattern[index], 0) {
			return false, ErrLinePattern
		}
		patternLength += pattern[index]
	}
	return patternLength <= Epsilon, nil
}

func dashedSegments(path []Point, dashes []float64, width float64, maximumTriangles int) ([]lineSegment, error) {
	if len(dashes) == 0 {
		return solidSegments(path), nil
	}
	pattern := make([]float64, len(dashes))
	empty, err := scaleDashPattern(pattern, dashes, width)
	if err != nil {
		return nil, err
	}
	if empty {
		return solidSegments(path), nil
	}
	if len(pattern)%2 != 0 {
		pattern = append(pattern, pattern...)
	}
	segments := make([]lineSegment, 0, len(path))
	patternIndex := 0
	remaining := pattern[0]
	drawing := true
	advancePattern := func() bool {
		for range len(pattern) {
			if remaining > Epsilon {
				return true
			}
			patternIndex = (patternIndex + 1) % len(pattern)
			remaining = pattern[patternIndex]
			drawing = patternIndex%2 == 0
		}
		return remaining > Epsilon
	}
	if !advancePattern() {
		return solidSegments(path), nil
	}
	iterations := 0
	maximumSegments := min(maxDashedSegmentsPerPath, maximumTriangles/2)
	maximumIterations := max(1024, maximumSegments*4)
	for index := 1; index < len(path); index++ {
		start := path[index-1]
		end := path[index]
		deltaX := end.X - start.X
		deltaY := end.Y - start.Y
		length := math.Hypot(deltaX, deltaY)
		position := 0.0
		for position < length-Epsilon {
			iterations++
			if iterations > maximumIterations {
				return nil, fmt.Errorf("%w: dashed line exceeds %d-iteration limit", ErrGeometryLimit, maximumIterations)
			}
			step := min(remaining, length-position)
			if drawing {
				if len(segments) >= maximumSegments {
					return nil, fmt.Errorf("%w: dashed line exceeds %d-segment limit", ErrGeometryLimit, maximumSegments)
				}
				firstFactor := position / length
				secondFactor := (position + step) / length
				segments = append(segments, lineSegment{
					Start: Point{X: start.X + deltaX*firstFactor, Y: start.Y + deltaY*firstFactor},
					End:   Point{X: start.X + deltaX*secondFactor, Y: start.Y + deltaY*secondFactor},
				})
			}
			position += step
			remaining -= step
			if !advancePattern() {
				return solidSegments(path), nil
			}
		}
	}
	return segments, nil
}

func solidSegments(path []Point) []lineSegment {
	segments := make([]lineSegment, 0, max(0, len(path)-1))
	for index := 1; index < len(path); index++ {
		segments = append(segments, lineSegment{Start: path[index-1], End: path[index]})
	}
	return segments
}
