// Package vecmap provides a Qt Quick vector map item backed by OpenFreeMap
// vector tiles and the Liberty style.
//
// The package requires cgo and Qt 6 QML and Quick development libraries. A
// QGuiApplication must exist before constructing an item. Construction, use,
// and Item.Close must occur on Qt's GUI thread; applications should keep that
// goroutine locked to its OS thread for the complete Qt lifecycle.
//
// New creates a parentless QQuickItem and a QML-facing camera property map.
// Applications own the Qt object integration and must call Item.Close after the
// Qt event loop has stopped.
package vecmap
