package view

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCameraProjectionRoundTrip(t *testing.T) {
	camera := NewCamera(Coordinate{Latitude: 40.4168, Longitude: -3.7038}, 7.25, 37, 900, 600)
	coordinates := []Coordinate{
		{Latitude: 40.4168, Longitude: -3.7038},
		{Latitude: 41.3874, Longitude: 2.1686},
		{Latitude: 37.3891, Longitude: -5.9845},
	}

	for _, coordinate := range coordinates {
		got := camera.ToCoordinate(camera.FromCoordinate(coordinate))
		require.InDelta(t, coordinate.Latitude, got.Latitude, 1e-9)
		require.InDelta(t, coordinate.Longitude, got.Longitude, 1e-9)
	}
}

func TestCameraCenterAndFractionalZoom(t *testing.T) {
	center := Coordinate{Latitude: 48.8566, Longitude: 2.3522}
	camera := NewCamera(center, 4.5, 0, 800, 500)
	assert.Equal(t, ScreenPoint{X: 400, Y: 250}, camera.FromCoordinate(center))

	coordinate := Coordinate{Latitude: 48.8566, Longitude: 3.3522}
	atZoom := camera.FromCoordinate(coordinate)
	atNextZoom := NewCamera(center, 5.5, 0, 800, 500).FromCoordinate(coordinate)
	require.InDelta(t, 2*(atZoom.X-400), atNextZoom.X-400, 1e-9)
}

func TestCameraBearingRotatesWorldAroundCenter(t *testing.T) {
	center := Coordinate{}
	east := Coordinate{Longitude: 1}
	camera := NewCamera(center, 8, 90, 400, 300)
	point := camera.FromCoordinate(east)

	require.InDelta(t, 200, point.X, 1e-9)
	assert.Less(t, point.Y, 150.0)
}

func TestCameraWrapsAcrossAntimeridian(t *testing.T) {
	camera := NewCamera(Coordinate{Longitude: 179}, 4, 0, 800, 500)
	point := camera.FromCoordinate(Coordinate{Longitude: -179})

	assert.Greater(t, point.X, 400.0)
	assert.Less(t, point.X, 450.0)
	got := camera.ToCoordinate(point)
	require.InDelta(t, -179, got.Longitude, 1e-9)
}

func TestCameraPanAndAlign(t *testing.T) {
	camera := NewCamera(Coordinate{Latitude: 40, Longitude: -3}, 9.5, 23, 640, 480)
	panned := camera.Panned(75, -40)
	restored := panned.Panned(-75, 40)
	require.InDelta(t, camera.Center.Latitude, restored.Center.Latitude, 1e-9)
	require.InDelta(t, camera.Center.Longitude, restored.Center.Longitude, 1e-9)

	coordinate := Coordinate{Latitude: 40.2, Longitude: -2.8}
	target := ScreenPoint{X: 150, Y: 330}
	aligned := camera.Aligned(coordinate, target)
	projected := aligned.FromCoordinate(coordinate)
	require.InDelta(t, target.X, projected.X, 1e-9)
	require.InDelta(t, target.Y, projected.Y, 1e-9)
}

func TestCameraZoomPreservesAnchor(t *testing.T) {
	camera := NewCamera(Coordinate{Latitude: 40, Longitude: -3}, 9.5, 23, 640, 480)
	anchor := ScreenPoint{X: 117, Y: 362}
	coordinate := camera.ToCoordinate(anchor)

	zoomed := camera.ZoomedAt(12.25, anchor)
	projected := zoomed.FromCoordinate(coordinate)

	assert.Equal(t, 12.25, zoomed.Zoom)
	require.InDelta(t, anchor.X, projected.X, 1e-9)
	require.InDelta(t, anchor.Y, projected.Y, 1e-9)
}

func TestCameraZoomAroundMovesAnchor(t *testing.T) {
	camera := NewCamera(Coordinate{Latitude: 40, Longitude: -3}, 9.5, 23, 640, 480)
	start := ScreenPoint{X: 117, Y: 362}
	target := ScreenPoint{X: 205, Y: 298}
	coordinate := camera.ToCoordinate(start)

	zoomed := camera.ZoomedAround(coordinate, 8.75, target)
	projected := zoomed.FromCoordinate(coordinate)

	assert.Equal(t, 8.75, zoomed.Zoom)
	require.InDelta(t, target.X, projected.X, 1e-9)
	require.InDelta(t, target.Y, projected.Y, 1e-9)
}

func TestCameraZoomAtClampsBeforeAligning(t *testing.T) {
	camera := NewCamera(Coordinate{Latitude: 40, Longitude: 179.5}, 9.5, 23, 640, 480)
	anchor := ScreenPoint{X: 117, Y: 362}
	coordinate := camera.ToCoordinate(anchor)

	zoomed := camera.ZoomedAt(100, anchor)
	projected := zoomed.FromCoordinate(coordinate)

	assert.Equal(t, camera.MaximumZoom, zoomed.Zoom)
	require.InDelta(t, anchor.X, projected.X, 1e-6)
	require.InDelta(t, anchor.Y, projected.Y, 1e-6)
}

func TestCameraClampsLatitudeAndZoom(t *testing.T) {
	camera := NewCamera(Coordinate{Latitude: 90, Longitude: 540}, 30, -15, -1, -2)
	require.InDelta(t, mercatorMaxLatitude, camera.Center.Latitude, 1e-12)
	assert.Equal(t, -180.0, camera.Center.Longitude)
	assert.Equal(t, 20.0, camera.Zoom)
	assert.Equal(t, 345.0, camera.Bearing)
	assert.Zero(t, camera.Width)
	assert.Zero(t, camera.Height)
}

func TestCameraNormalizesSupportedZoomRange(t *testing.T) {
	camera := Camera{Zoom: -100, MinimumZoom: -100, MaximumZoom: 100}.normalized()
	assert.Equal(t, float64(0), camera.MinimumZoom)
	assert.Equal(t, float64(20), camera.MaximumZoom)
	assert.Equal(t, float64(0), camera.Zoom)
}

func TestCameraMinimumZoomCannotRaiseMaximumBeyondSupportedRange(t *testing.T) {
	camera := (Camera{Zoom: 100, MinimumZoom: 100, MaximumZoom: 100}).Normalized()
	assert.Equal(t, float64(20), camera.MinimumZoom)
	assert.Equal(t, float64(20), camera.MaximumZoom)
	assert.Equal(t, float64(20), camera.Zoom)
}

func TestCameraLiteralUsesDefaultZoomRange(t *testing.T) {
	camera := (Camera{Zoom: 12}).normalized()

	assert.Equal(t, float64(12), camera.Zoom)
	assert.Equal(t, float64(20), camera.MaximumZoom)
}

func TestCameraKeepsViewportInsideMercatorWorld(t *testing.T) {
	for _, bearing := range []float64{0, 37, 90, 225} {
		for _, latitude := range []float64{-mercatorMaxLatitude, mercatorMaxLatitude} {
			camera := NewCamera(Coordinate{Latitude: latitude}, 2, bearing, 800, 600)
			worldSize := camera.worldSize()
			_, centerY := mercatorWorldPoint(camera.Center, worldSize)
			angle := bearing * math.Pi / 180
			halfVerticalExtent := (math.Abs(math.Sin(angle))*camera.Width + math.Abs(math.Cos(angle))*camera.Height) / 2

			assert.GreaterOrEqual(t, centerY, halfVerticalExtent-1e-9)
			assert.LessOrEqual(t, centerY, worldSize-halfVerticalExtent+1e-9)
		}
	}
}

func TestCameraCentersWorldWhenViewportIsTaller(t *testing.T) {
	camera := NewCamera(Coordinate{Latitude: mercatorMaxLatitude}, 1, 0, 800, 600)
	require.InDelta(t, 0, camera.Center.Latitude, 1e-12)
}
