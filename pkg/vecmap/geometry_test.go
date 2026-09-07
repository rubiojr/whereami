package vecmap

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRoadCameraTransformMatchesCameraProjection(t *testing.T) {
	cameras := []Camera{
		NewCamera(Coordinate{Latitude: 40.4168, Longitude: -3.7038}, 9, 0, 260, 190),
		NewCamera(Coordinate{Latitude: 40.405, Longitude: -3.72}, 10.25, 47, 360, 240),
		NewCamera(Coordinate{Latitude: 40.44, Longitude: -3.66}, 7.5, 291, 180, 320),
	}
	tiles := []vectorTileID{
		pinnedTile,
		{X: 251, Y: 193, Z: 9},
		{X: 0, Y: 256, Z: 9},
	}
	points := []roadPoint{{X: 0, Y: 0}, {X: 128, Y: 96}, {X: 256, Y: 256}}

	for _, camera := range cameras {
		for _, tile := range tiles {
			transform := roadCameraTransform(camera, tile)
			for _, point := range points {
				actual := transform.mapPoint(point)
				expected := camera.FromCoordinate(tileLocalCoordinate(tile, point))
				assert.InDelta(t, expected.X, actual.X, 1e-8)
				assert.InDelta(t, expected.Y, actual.Y, 1e-8)
			}
		}
	}
}

func TestRoadCameraTransformUsesNearestWrappedCopy(t *testing.T) {
	camera := NewCamera(Coordinate{Latitude: 0, Longitude: 179.9}, 2, 0, 300, 200)
	tile := vectorTileID{X: 0, Y: 2, Z: 2}

	actual := roadCameraTransform(camera, tile).mapPoint(roadPoint{})

	assert.InDelta(t, camera.Width/2, actual.X, 1, "wrapped tile should be adjacent to the antimeridian center")
}

func TestRoadTileVerticesContainLinePairs(t *testing.T) {
	segments := []roadSegment{
		{Start: roadPoint{X: 10, Y: 20}, End: roadPoint{X: 30, Y: 40}},
		{Start: roadPoint{X: 50, Y: 60}, End: roadPoint{X: 70, Y: 80}},
	}

	vertices := roadTileVertices(segments)

	assert.Equal(t, []float32{10, 20, 30, 40, 50, 60, 70, 80}, vertices)
}
