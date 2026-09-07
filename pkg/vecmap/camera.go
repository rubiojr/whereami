package vecmap

import "math"

const (
	mercatorMaxLatitude = 85.0511287798066
	mercatorTileSize    = 256.0
)

// Coordinate is a WGS84 latitude and longitude in degrees.
type Coordinate struct {
	Latitude  float64
	Longitude float64
}

// ScreenPoint is a device-independent position in the camera viewport.
type ScreenPoint struct {
	X float64
	Y float64
}

// Camera is an immutable Web Mercator camera snapshot.
type Camera struct {
	Center      Coordinate
	Zoom        float64
	Bearing     float64
	Width       float64
	Height      float64
	MinimumZoom float64
	MaximumZoom float64
}

// NewCamera returns a normalized camera using the supported zoom range.
func NewCamera(center Coordinate, zoom, bearing, width, height float64) Camera {
	return Camera{
		Center:      center,
		Zoom:        zoom,
		Bearing:     bearing,
		Width:       width,
		Height:      height,
		MinimumZoom: 0,
		MaximumZoom: 20,
	}.normalized()
}

// WithViewport returns a snapshot with a new viewport size.
func (c Camera) WithViewport(width, height float64) Camera {
	c.Width = width
	c.Height = height
	return c.normalized()
}

// FromCoordinate projects a coordinate into the viewport. Longitudes use the
// wrapped world copy nearest the camera center.
func (c Camera) FromCoordinate(coordinate Coordinate) ScreenPoint {
	c = c.normalized()
	worldSize := c.worldSize()
	centerX, centerY := mercatorWorldPoint(c.Center, worldSize)
	pointX, pointY := mercatorWorldPoint(coordinate, worldSize)
	dx := pointX - centerX
	dx -= math.Round(dx/worldSize) * worldSize
	dy := pointY - centerY

	angle := -c.Bearing * math.Pi / 180
	cosAngle, sinAngle := math.Cos(angle), math.Sin(angle)
	return ScreenPoint{
		X: c.Width/2 + cosAngle*dx - sinAngle*dy,
		Y: c.Height/2 + sinAngle*dx + cosAngle*dy,
	}
}

// ToCoordinate unprojects a viewport position into a wrapped WGS84 coordinate.
func (c Camera) ToCoordinate(point ScreenPoint) Coordinate {
	c = c.normalized()
	angle := c.Bearing * math.Pi / 180
	cosAngle, sinAngle := math.Cos(angle), math.Sin(angle)
	screenX := point.X - c.Width/2
	screenY := point.Y - c.Height/2
	dx := cosAngle*screenX - sinAngle*screenY
	dy := sinAngle*screenX + cosAngle*screenY

	worldSize := c.worldSize()
	centerX, centerY := mercatorWorldPoint(c.Center, worldSize)
	return coordinateFromMercator(centerX+dx, centerY+dy, worldSize)
}

// Panned returns a camera whose center moved by viewport pixels.
func (c Camera) Panned(dx, dy float64) Camera {
	c = c.normalized()
	c.Center = c.ToCoordinate(ScreenPoint{X: c.Width/2 + dx, Y: c.Height/2 + dy})
	return c.normalized()
}

// ZoomedAt returns a camera at zoom that preserves the coordinate under point.
func (c Camera) ZoomedAt(zoom float64, point ScreenPoint) Camera {
	c = c.normalized()
	return c.ZoomedAround(c.ToCoordinate(point), zoom, point)
}

// ZoomedAround returns a camera at zoom with coordinate aligned to point.
func (c Camera) ZoomedAround(coordinate Coordinate, zoom float64, point ScreenPoint) Camera {
	c.Zoom = zoom
	c = c.normalized()
	return c.Aligned(coordinate, point)
}

// Aligned returns a camera that places coordinate at point.
func (c Camera) Aligned(coordinate Coordinate, point ScreenPoint) Camera {
	projected := c.FromCoordinate(coordinate)
	return c.Panned(projected.X-point.X, projected.Y-point.Y)
}

func (c Camera) normalized() Camera {
	if c.MinimumZoom == 0 && c.MaximumZoom == 0 {
		c.MaximumZoom = 20
	}
	if !isFinite(c.MinimumZoom) || c.MinimumZoom < 0 {
		c.MinimumZoom = 0
	}
	if !isFinite(c.MaximumZoom) || c.MaximumZoom > 20 {
		c.MaximumZoom = 20
	}
	if c.MaximumZoom < c.MinimumZoom {
		c.MaximumZoom = c.MinimumZoom
	}
	if !isFinite(c.Zoom) {
		c.Zoom = c.MinimumZoom
	}
	c.Zoom = math.Max(c.MinimumZoom, math.Min(c.MaximumZoom, c.Zoom))
	if !isFinite(c.Bearing) {
		c.Bearing = 0
	}
	c.Bearing = math.Mod(c.Bearing, 360)
	if c.Bearing < 0 {
		c.Bearing += 360
	}
	if !isFinite(c.Center.Latitude) {
		c.Center.Latitude = 0
	}
	if !isFinite(c.Center.Longitude) {
		c.Center.Longitude = 0
	}
	c.Center.Latitude = math.Max(-mercatorMaxLatitude, math.Min(mercatorMaxLatitude, c.Center.Latitude))
	c.Center.Longitude = wrapLongitude(c.Center.Longitude)
	if !isFinite(c.Width) || c.Width < 0 {
		c.Width = 0
	}
	if !isFinite(c.Height) || c.Height < 0 {
		c.Height = 0
	}
	if c.Height > 0 {
		worldSize := c.worldSize()
		angle := c.Bearing * math.Pi / 180
		halfVerticalExtent := (math.Abs(math.Sin(angle))*c.Width + math.Abs(math.Cos(angle))*c.Height) / 2
		centerX, centerY := mercatorWorldPoint(c.Center, worldSize)
		if 2*halfVerticalExtent >= worldSize {
			centerY = worldSize / 2
		} else {
			centerY = math.Max(halfVerticalExtent, math.Min(worldSize-halfVerticalExtent, centerY))
		}
		c.Center.Latitude = coordinateFromMercator(centerX, centerY, worldSize).Latitude
	}
	return c
}

func (c Camera) worldSize() float64 {
	return mercatorTileSize * math.Exp2(c.Zoom)
}

func mercatorWorldPoint(coordinate Coordinate, worldSize float64) (float64, float64) {
	latitude := math.Max(-mercatorMaxLatitude, math.Min(mercatorMaxLatitude, coordinate.Latitude))
	longitude := wrapLongitude(coordinate.Longitude)
	latitudeRadians := latitude * math.Pi / 180
	x := (longitude + 180) / 360 * worldSize
	y := (1 - math.Asinh(math.Tan(latitudeRadians))/math.Pi) / 2 * worldSize
	return x, y
}

func coordinateFromMercator(x, y, worldSize float64) Coordinate {
	x = math.Mod(x, worldSize)
	if x < 0 {
		x += worldSize
	}
	y = math.Max(0, math.Min(worldSize, y))
	return Coordinate{
		Latitude:  math.Atan(math.Sinh(math.Pi*(1-2*y/worldSize))) * 180 / math.Pi,
		Longitude: wrapLongitude(x/worldSize*360 - 180),
	}
}

func wrapLongitude(longitude float64) float64 {
	longitude = math.Mod(longitude+180, 360)
	if longitude < 0 {
		longitude += 360
	}
	return longitude - 180
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
