package vecmap

import (
	"errors"
	"runtime"
	"sync/atomic"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/qml"
	quick "github.com/rubiojr/whereami/internal/miqtquick"
)

var errCreateItem = errors.New("create Qt Quick vector map item")

// Options configures a vector map item.
type Options struct {
	// CacheDir stores downloaded tiles, rasters, and glyph ranges. An empty
	// path disables persistent caching.
	CacheDir string
	// InitialCamera sets the initial center, zoom, bearing, and viewport. A nil
	// camera uses zoom 9 at latitude 0, longitude 0 with an empty viewport.
	InitialCamera *Camera
}

// Item is a Go-managed QQuickItem that retains one scene-graph node per road tile.
type Item struct {
	quickItem *quick.QQuickItem
	camera    *qmlCamera

	paintNodeUpdates atomic.Uint64
	geometryBuilds   atomic.Uint64
	geometryUpdates  atomic.Uint64
	geometryRemovals atomic.Uint64
	transformUpdates atomic.Uint64
	sdfLabels        atomic.Int64
	sdfAtlasGlyphs   atomic.Int64
	tiles            atomic.Pointer[roadTileSnapshot]
	styledTiles      atomic.Pointer[libertySceneSnapshot]
	finalStats       atomic.Pointer[Stats]
	scheduler        *tileScheduler
	styleCompiler    *libertySceneCompiler
	glyphs           *glyphManager
	closed           atomic.Bool

	// The retained-node cache is accessed only from Qt's render thread.
	lastWidth                 float64
	lastHeight                float64
	lastCameraRevision        uint64
	lastCameraZoom            float64
	lastCameraBearing         float64
	lastTileRevision          uint64
	lastStyleZoom             float64
	lastStyleRequestRevision  uint64
	lastStyleRequestZoom      float64
	lastGlyphRevision         uint64
	hasStyleRequest           bool
	renderedScene             *libertySceneSnapshot
	lastWorldWraps            []int
	tileNodes                 map[vectorTileID][]retainedTileTransform
	sceneNodes                []*quick.QSGNode
	retainedTiles             map[vectorTileID]struct{}
	symbolTransforms          []retainedLibertySymbolTransform
	symbolLayers              []retainedLibertySymbolLayer
	acceptedSymbols           map[libertySymbolKey]libertyAcceptedSymbol
	renderedSDFScene          *sdfScene
	lastSymbolPlacementZoom   float64
	lastSymbolPlacementAngle  float64
	lastSymbolPlacementCenter Coordinate
	hasSymbolPlacement        bool
	sdfAtlas                  *quick.QSGSDFAtlas
	sdfAtlasGeneration        uint64
	sdfGlyphAtlas             *sdfGlyphAtlas
	sdfGlyphAtlasGeneration   uint64
	sdfGlyphAtlasKeys         map[sdfGlyphKey]struct{}
	sdfGlyphAtlasGlyphs       map[sdfGlyphKey]sdfGlyph
	sdfLayoutScene            *libertySceneSnapshot
	sdfLayoutGlyphRevision    uint64
	sdfLayouts                map[libertySDFLayoutKey]*sdfTextLayout
	sdfLayoutGlyphs           map[sdfGlyphKey]sdfGlyph
	sdfLayoutGlyphKeys        map[sdfGlyphKey]struct{}
	sdfLayoutHasCandidates    bool
	sdfAtlasRetry             bool
	sdfAtlasRetryAttempts     uint8
}

// New creates a parentless C++-owned item on Qt's GUI thread. A QGuiApplication
// must already exist. The caller must keep the item alive until Close.
func New(cacheDir string) (*Item, error) {
	return NewWithOptions(Options{CacheDir: cacheDir})
}

// NewWithOptions creates a parentless C++-owned item with explicit options on
// Qt's GUI thread. A QGuiApplication must already exist. The caller must keep
// the item alive until Close.
func NewWithOptions(options Options) (*Item, error) {
	if err := RegisterFonts(); err != nil {
		return nil, err
	}
	initialCamera := Camera{}
	if options.InitialCamera == nil {
		initialCamera = NewCamera(Coordinate{}, 9, 0, 0, 0)
	} else {
		initialCamera = options.InitialCamera.normalized()
	}
	item, err := newItem(openFreeMapRoadLoader(options.CacheDir), initialCamera)
	if err != nil {
		return nil, err
	}
	item.glyphs = newGlyphManager(openFreeMapGlyphRangeLoader(options.CacheDir), func() {
		if !item.closed.Load() {
			queueQuickItemUpdate(item.quickItem)
		}
	})
	return item, nil
}

func newItem(loader roadTileLoader, initialCamera Camera) (*Item, error) {
	item := &Item{quickItem: quick.NewQQuickItem()}
	if item.quickItem == nil {
		return nil, errCreateItem
	}

	item.quickItem.SetFlag2(quick.QQuickItem__ItemHasContents, true)
	qml.QJSEngine_SetObjectOwnership(item.quickItem.QObject, qml.QJSEngine__CppOwnership)
	item.camera = newQMLCamera(item, initialCamera)
	if item.camera == nil {
		item.quickItem.Delete()
		return nil, errCreateItem
	}
	qml.QJSEngine_SetObjectOwnership(item.camera.qObject(), qml.QJSEngine__CppOwnership)
	item.quickItem.OnUpdatePaintNode(item.updatePaintNode)
	if loader != nil {
		item.styleCompiler = newLibertySceneCompiler(item.publishStyledTiles)
		item.scheduler = newTileScheduler(loader, item.publishTiles)
	}
	return item, nil
}

func (i *Item) publishTiles(snapshot *roadTileSnapshot) {
	if i.closed.Load() {
		return
	}
	previous := i.tiles.Swap(snapshot)
	if previous == nil || previous.contentRevision != snapshot.contentRevision {
		queueQuickItemUpdate(i.quickItem)
	}
}

func (i *Item) publishStyledTiles(snapshot *libertySceneSnapshot) {
	if i.closed.Load() {
		return
	}
	i.styledTiles.Store(snapshot)
	queueQuickItemUpdate(i.quickItem)
}

func (i *Item) requestTiles(camera Camera) {
	if i.scheduler != nil {
		i.scheduler.request(camera)
	}
}

// QObject returns the base pointer used to expose this item as a QML context
// property. The returned wrapper does not own the C++ object.
func (i *Item) QObject() *qt.QObject {
	if i == nil || i.quickItem == nil {
		return nil
	}
	return i.quickItem.QObject
}

// CameraQObject returns the QML property map for camera and viewport state.
func (i *Item) CameraQObject() *qt.QObject {
	if i == nil || i.camera == nil {
		return nil
	}
	return i.camera.qObject()
}

// SetZoom sets the initial or current camera zoom on the Qt GUI
// thread.
func (i *Item) SetZoom(zoom float64) {
	if i == nil || i.camera == nil || i.closed.Load() {
		return
	}
	i.camera.handleValueChanged("zoomLevel", zoom)
}

// Close destroys the QQuickItem. It must run on the Qt GUI thread after the
// event loop has stopped.
func (i *Item) Close() {
	if i == nil || !i.closed.CompareAndSwap(false, true) {
		return
	}
	if i.scheduler != nil {
		i.scheduler.stop()
	}
	if i.styleCompiler != nil {
		i.styleCompiler.stop()
	}
	if i.glyphs != nil {
		i.glyphs.close()
	}
	finalStats := i.Stats()
	i.finalStats.Store(&finalStats)
	i.camera.close()
	i.quickItem.Delete()
	i.clearRetainedSceneGraph()
	i.renderedScene = nil
	i.tiles.Store(nil)
	i.styledTiles.Store(nil)
	i.camera = nil
	i.scheduler = nil
	i.styleCompiler = nil
	i.glyphs = nil
	i.quickItem = nil
	runtime.KeepAlive(i)
}
