package vecmap

import "github.com/rubiojr/whereami/pkg/vecmap/view"

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
	return affineTransform(view.TileTransform(camera, tile, 0))
}

func wrappedRoadCameraTransform(camera Camera, tile vectorTileID, wrap int) affineTransform {
	return affineTransform(view.TileTransform(camera, tile, wrap))
}

func (t affineTransform) mapPoint(point roadPoint) roadPoint {
	return roadPoint(view.Affine(t).MapPoint(view.ScreenPoint(point)))
}

func tileLocalCoordinate(tile vectorTileID, point roadPoint) Coordinate {
	return view.TileCoordinate(tile, view.ScreenPoint(point))
}

func libertyPatternPhase(tile vectorTileID, wrap int, patternWidth, patternHeight float64) (float64, float64) {
	return view.PatternPhase(tile, wrap, patternWidth, patternHeight)
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
	return view.WorldWraps(camera)
}
