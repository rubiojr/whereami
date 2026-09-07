package vecmap

import "math"

const tileSize = mercatorTileSize

type roadPoint struct {
	X float64
	Y float64
}

type roadSegment struct {
	Start roadPoint
	End   roadPoint
}

type affineTransform struct {
	M11 float64
	M12 float64
	M21 float64
	M22 float64
	DX  float64
	DY  float64
}

func roadTileVertices(segments []roadSegment) []float32 {
	vertices := make([]float32, 0, len(segments)*4)
	for _, segment := range segments {
		vertices = append(vertices,
			float32(segment.Start.X), float32(segment.Start.Y),
			float32(segment.End.X), float32(segment.End.Y),
		)
	}

	return vertices
}

func roadCameraTransform(camera Camera, tile vectorTileID) affineTransform {
	worldSize := tileSize * math.Exp2(float64(tile.Z))
	centerX, centerY := mercatorWorldPoint(camera.Center, worldSize)
	deltaX := float64(tile.X)*tileSize - centerX
	if worldSize > 0 {
		deltaX -= math.Round(deltaX/worldSize) * worldSize
	}
	deltaY := float64(tile.Y)*tileSize - centerY

	scale := math.Exp2(camera.Zoom - float64(tile.Z))
	angle := -camera.Bearing * math.Pi / 180
	cosAngle := math.Cos(angle)
	sinAngle := math.Sin(angle)
	m11 := scale * cosAngle
	m12 := -scale * sinAngle
	m21 := scale * sinAngle
	m22 := scale * cosAngle

	return affineTransform{
		M11: m11,
		M12: m12,
		M21: m21,
		M22: m22,
		DX:  camera.Width/2 + m11*deltaX + m12*deltaY,
		DY:  camera.Height/2 + m21*deltaX + m22*deltaY,
	}
}

func wrappedRoadCameraTransform(camera Camera, tile vectorTileID, wrap int) affineTransform {
	transform := roadCameraTransform(camera, tile)
	worldSize := tileSize * math.Exp2(float64(tile.Z))
	transform.DX += transform.M11 * float64(wrap) * worldSize
	transform.DY += transform.M21 * float64(wrap) * worldSize
	return transform
}

func (t affineTransform) mapPoint(point roadPoint) roadPoint {
	return roadPoint{
		X: t.M11*point.X + t.M12*point.Y + t.DX,
		Y: t.M21*point.X + t.M22*point.Y + t.DY,
	}
}

func tileLocalCoordinate(tile vectorTileID, point roadPoint) Coordinate {
	worldSize := tileSize * math.Exp2(float64(tile.Z))
	return coordinateFromMercator(
		float64(tile.X)*tileSize+point.X,
		float64(tile.Y)*tileSize+point.Y,
		worldSize,
	)
}

func libertyPatternPhase(tile vectorTileID, wrap int, patternWidth, patternHeight float64) (float64, float64) {
	worldTiles := int64(1) << tile.Z
	phaseX := float64(int64(tile.X)+int64(wrap)*worldTiles) * tileSize
	phaseY := float64(tile.Y) * tileSize
	if patternWidth > 0 {
		phaseX = math.Mod(phaseX, patternWidth)
	}
	if patternHeight > 0 {
		phaseY = math.Mod(phaseY, patternHeight)
	}
	return phaseX, phaseY
}

func pointVertices(points []roadPoint) []float32 {
	vertices := make([]float32, 0, len(points)*2)
	for _, point := range points {
		vertices = append(vertices, float32(point.X), float32(point.Y))
	}
	return vertices
}

func segmentPoints(segments []roadSegment) []roadPoint {
	points := make([]roadPoint, 0, len(segments)*2)
	for _, segment := range segments {
		points = append(points, segment.Start, segment.End)
	}
	return points
}

func backgroundTriangles() []roadPoint {
	return []roadPoint{
		{X: 0, Y: 0}, {X: tileSize, Y: 0}, {X: tileSize, Y: tileSize},
		{X: 0, Y: 0}, {X: tileSize, Y: tileSize}, {X: 0, Y: tileSize},
	}
}

func libertyWorldWraps(camera Camera) []int {
	worldPixels := tileSize * math.Exp2(camera.Zoom)
	viewportDiagonal := math.Hypot(camera.Width, camera.Height)
	if worldPixels <= 0 || worldPixels > viewportDiagonal*2 {
		return []int{0}
	}
	maximumWrap := int(math.Ceil(viewportDiagonal/worldPixels)) + 1
	maximumWrap = min(maximumWrap, 8)
	wraps := make([]int, 0, maximumWrap*2+1)
	for wrap := -maximumWrap; wrap <= maximumWrap; wrap++ {
		wraps = append(wraps, wrap)
	}
	return wraps
}
