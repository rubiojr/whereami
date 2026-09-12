package vecmap

import "github.com/rubiojr/whereami/pkg/vecmap/view"

// Coordinate is a WGS84 latitude and longitude in degrees.
type Coordinate = view.Coordinate

// ScreenPoint is a device-independent position in the camera viewport.
type ScreenPoint = view.ScreenPoint

// Camera is the toolkit-neutral immutable Web Mercator camera.
type Camera = view.Camera

const (
	mercatorMaxLatitude = view.MaxLatitude
	mercatorTileSize    = view.TileSize
)

// NewCamera returns a normalized camera using the supported zoom range.
func NewCamera(center Coordinate, zoom, bearing, width, height float64) Camera {
	return view.NewCamera(center, zoom, bearing, width, height)
}

func mercatorWorldPoint(coordinate Coordinate, worldSize float64) (float64, float64) {
	return view.ProjectMercator(coordinate, worldSize)
}
