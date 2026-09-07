package vecmap

import (
	"sync/atomic"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/qml"
)

type cameraSnapshot struct {
	Camera   Camera
	Revision uint64
}

type qmlCamera struct {
	item       *Item
	properties *qml.QQmlPropertyMap
	snapshot   atomic.Pointer[cameraSnapshot]

	// Command operands are read and written only on Qt's GUI thread. QML writes
	// the operands first, then bumps the matching serial to apply them together.
	panDX                 float64
	panDY                 float64
	centerTarget          Coordinate
	zoomAnchor            ScreenPoint
	zoomCenter            Coordinate
	zoomTarget            float64
	projectCoordinate     Coordinate
	projectedPoint        ScreenPoint
	unprojectPoint        ScreenPoint
	unprojectedCoordinate Coordinate
	alignCoordinate       Coordinate
	alignPoint            ScreenPoint
}

func newQMLCamera(item *Item, initial Camera) *qmlCamera {
	properties := qml.NewQQmlPropertyMap()
	if properties == nil {
		return nil
	}
	initial = initial.normalized()
	camera := &qmlCamera{
		item:         item,
		properties:   properties,
		centerTarget: initial.Center,
		zoomTarget:   initial.Zoom,
	}
	camera.snapshot.Store(&cameraSnapshot{Camera: initial})
	camera.insertReal("centerTargetLatitude", initial.Center.Latitude)
	camera.insertReal("centerTargetLongitude", initial.Center.Longitude)
	camera.insertInteger("centerSerial", 0)
	camera.insertReal("panDX", 0)
	camera.insertReal("panDY", 0)
	camera.insertInteger("panSerial", 0)
	camera.insertReal("zoomAnchorX", 0)
	camera.insertReal("zoomAnchorY", 0)
	camera.insertInteger("zoomCaptureSerial", 0)
	camera.insertReal("zoomTarget", camera.snapshot.Load().Camera.Zoom)
	camera.insertInteger("zoomSerial", 0)
	camera.insertReal("projectLatitude", initial.Center.Latitude)
	camera.insertReal("projectLongitude", initial.Center.Longitude)
	camera.insertInteger("projectSerial", 0)
	camera.insertReal("projectedX", 0)
	camera.insertReal("projectedY", 0)
	camera.insertReal("unprojectX", 0)
	camera.insertReal("unprojectY", 0)
	camera.insertInteger("unprojectSerial", 0)
	camera.insertReal("unprojectedLatitude", initial.Center.Latitude)
	camera.insertReal("unprojectedLongitude", initial.Center.Longitude)
	camera.insertReal("alignLatitude", initial.Center.Latitude)
	camera.insertReal("alignLongitude", initial.Center.Longitude)
	camera.insertReal("alignX", 0)
	camera.insertReal("alignY", 0)
	camera.insertInteger("alignSerial", 0)
	camera.insertInteger("statusSerial", 0)
	camera.insertInteger("tilesRequested", 0)
	camera.insertInteger("tilesLoaded", 0)
	camera.insertInteger("tilesLoading", 0)
	camera.insertInteger("tileErrors", 0)
	camera.insertString("tileError", "")
	camera.publish(camera.snapshot.Load())
	camera.properties.Freeze()
	connectQMLCamera(camera.properties, camera)
	return camera
}

func (c *qmlCamera) close() {
	if c == nil || c.properties == nil {
		return
	}
	c.properties.Delete()
	c.properties = nil
}

func (c *qmlCamera) qObject() *qt.QObject {
	if c == nil || c.properties == nil {
		return nil
	}
	return c.properties.QObject
}

func (c *qmlCamera) current() *cameraSnapshot {
	return c.snapshot.Load()
}

func (c *qmlCamera) handleValueChanged(key string, value float64) {
	if c == nil || c.item == nil || c.item.closed.Load() {
		return
	}
	current := c.snapshot.Load()
	next := current.Camera

	switch key {
	case "centerLatitude":
		next.Center.Latitude = value
	case "centerLongitude":
		next.Center.Longitude = value
	case "centerTargetLatitude":
		c.centerTarget.Latitude = value
		return
	case "centerTargetLongitude":
		c.centerTarget.Longitude = value
		return
	case "centerSerial":
		next.Center = c.centerTarget
	case "zoomLevel":
		next.Zoom = value
	case "bearing":
		next.Bearing = value
	case "minimumZoomLevel":
		next.MinimumZoom = value
	case "maximumZoomLevel":
		next.MaximumZoom = value
	case "viewportWidth":
		next.Width = value
	case "viewportHeight":
		next.Height = value
	case "panDX":
		c.panDX = value
		return
	case "panDY":
		c.panDY = value
		return
	case "panSerial":
		next = next.Panned(c.panDX, c.panDY)
	case "zoomAnchorX":
		c.zoomAnchor.X = value
		return
	case "zoomAnchorY":
		c.zoomAnchor.Y = value
		return
	case "zoomCaptureSerial":
		c.zoomCenter = next.ToCoordinate(c.zoomAnchor)
		return
	case "zoomTarget":
		c.zoomTarget = value
		return
	case "zoomSerial":
		next = next.ZoomedAround(c.zoomCenter, c.zoomTarget, c.zoomAnchor)
	case "projectLatitude":
		c.projectCoordinate.Latitude = value
		return
	case "projectLongitude":
		c.projectCoordinate.Longitude = value
		return
	case "projectSerial":
		c.projectedPoint = next.FromCoordinate(c.projectCoordinate)
		c.insertReal("projectedX", c.projectedPoint.X)
		c.insertReal("projectedY", c.projectedPoint.Y)
		return
	case "unprojectX":
		c.unprojectPoint.X = value
		return
	case "unprojectY":
		c.unprojectPoint.Y = value
		return
	case "unprojectSerial":
		c.unprojectedCoordinate = next.ToCoordinate(c.unprojectPoint)
		c.insertReal("unprojectedLatitude", c.unprojectedCoordinate.Latitude)
		c.insertReal("unprojectedLongitude", c.unprojectedCoordinate.Longitude)
		return
	case "alignLatitude":
		c.alignCoordinate.Latitude = value
		return
	case "alignLongitude":
		c.alignCoordinate.Longitude = value
		return
	case "alignX":
		c.alignPoint.X = value
		return
	case "alignY":
		c.alignPoint.Y = value
		return
	case "alignSerial":
		next = next.Aligned(c.alignCoordinate, c.alignPoint)
	case "statusSerial":
		c.publishStatus()
		return
	default:
		return
	}

	next = next.normalized()
	snapshot := &cameraSnapshot{Camera: next, Revision: current.Revision + 1}
	c.snapshot.Store(snapshot)
	c.publish(snapshot)
	c.item.quickItem.Update()
}

func (c *qmlCamera) publish(snapshot *cameraSnapshot) {
	camera := snapshot.Camera
	c.insertReal("centerLatitude", camera.Center.Latitude)
	c.insertReal("centerLongitude", camera.Center.Longitude)
	c.insertReal("zoomLevel", camera.Zoom)
	c.insertReal("bearing", camera.Bearing)
	c.insertReal("minimumZoomLevel", camera.MinimumZoom)
	c.insertReal("maximumZoomLevel", camera.MaximumZoom)
	c.insertReal("viewportWidth", camera.Width)
	c.insertReal("viewportHeight", camera.Height)
	c.insertInteger("cameraRevision", int64(snapshot.Revision))
}

func (c *qmlCamera) publishStatus() {
	stats := c.item.Stats()
	c.insertInteger("tilesRequested", int64(stats.TilesRequested))
	c.insertInteger("tilesLoaded", int64(stats.TilesLoaded))
	c.insertInteger("tilesLoading", int64(stats.TilesLoading))
	c.insertInteger("tileErrors", int64(stats.TileErrors))
	c.insertString("tileError", stats.TileError)
}

func (c *qmlCamera) insertReal(key string, value float64) {
	variant := qt.NewQVariant9(value)
	c.properties.Insert(key, variant)
	variant.Delete()
}

func (c *qmlCamera) insertInteger(key string, value int64) {
	variant := qt.NewQVariant6(value)
	c.properties.Insert(key, variant)
	variant.Delete()
}

func (c *qmlCamera) insertString(key, value string) {
	variant := qt.NewQVariant14(value)
	c.properties.Insert(key, variant)
	variant.Delete()
}
