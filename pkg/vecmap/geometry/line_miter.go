package geometry

import "math"

func appendMiterJoin(previous, point, next Point, halfWidth float64, mesh *Builder[Point]) error {
	firstX, firstY := point.X-previous.X, point.Y-previous.Y
	secondX, secondY := next.X-point.X, next.Y-point.Y
	firstLength := math.Hypot(firstX, firstY)
	secondLength := math.Hypot(secondX, secondY)
	if firstLength <= Epsilon || secondLength <= Epsilon || halfWidth <= Epsilon {
		return nil
	}
	firstX, firstY = firstX/firstLength, firstY/firstLength
	secondX, secondY = secondX/secondLength, secondY/secondLength
	cross := firstX*secondY - firstY*secondX
	if math.Abs(cross) <= Epsilon {
		return nil
	}
	for _, side := range []float64{-1, 1} {
		firstCorner := Point{X: point.X - firstY*halfWidth*side, Y: point.Y + firstX*halfWidth*side}
		secondCorner := Point{X: point.X - secondY*halfWidth*side, Y: point.Y + secondX*halfWidth*side}
		deltaX := secondCorner.X - firstCorner.X
		deltaY := secondCorner.Y - firstCorner.Y
		factor := (deltaX*secondY - deltaY*secondX) / cross
		miter := Point{X: firstCorner.X + firstX*factor, Y: firstCorner.Y + firstY*factor}
		if math.Hypot(miter.X-point.X, miter.Y-point.Y) > halfWidth*4 {
			miter = point
		}
		if err := mesh.Triangle(firstCorner, miter, secondCorner); err != nil {
			return err
		}
	}
	return nil
}
