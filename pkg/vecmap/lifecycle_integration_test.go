//go:build integration

package vecmap

import (
	"context"
	"runtime"
	"testing"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/qml"
	quick "github.com/rubiojr/whereami/internal/miqtquick"
	"github.com/stretchr/testify/require"
)

const lifecycleQML = `
import QtQuick
import QtQuick.Window

Window {
    visible: true
    width: 200
    height: 150

    Item {
        id: host
        anchors.fill: parent

        Binding { target: vectorItem; property: "parent"; value: host }
        Binding { target: vectorItem; property: "width"; value: host.width }
        Binding { target: vectorItem; property: "height"; value: host.height }
        Component.onCompleted: {
            vectorCamera.panDX = 1
            vectorCamera.panDY = 0
            vectorCamera.panSerial += 1
        }
    }
}
`

func TestItemLifecycle(t *testing.T) {
	t.Setenv("QT_QUICK_BACKEND", "software")
	runtime.LockOSThread()
	require.ErrorContains(t, RegisterFonts(), "QGuiApplication is not initialized")

	application := qt.NewQApplication([]string{
		"vecmap-lifecycle-test",
		"-platform",
		"offscreen",
	})
	require.NotNil(t, application)
	t.Cleanup(application.Delete)
	registrationResult := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		registrationResult <- RegisterFonts()
	}()
	require.ErrorContains(t, <-registrationResult, "must run on the GUI thread")
	require.NoError(t, RegisterFonts())
	require.NoError(t, RegisterFonts())
	require.True(t, qt.QFontDatabase_HasFamily(libertyDesktopFontFamily))
	styles := qt.QFontDatabase_Styles(libertyDesktopFontFamily)
	require.Contains(t, styles, "Regular")
	require.Contains(t, styles, "Bold")
	require.Contains(t, styles, "Italic")
	writingSystems := qt.QFontDatabase_WritingSystemsWithFamily(libertyDesktopFontFamily)
	for _, writingSystem := range []qt.QFontDatabase__WritingSystem{
		qt.QFontDatabase__Gurmukhi,
		qt.QFontDatabase__Gujarati,
		qt.QFontDatabase__Kannada,
		qt.QFontDatabase__Lao,
	} {
		require.Contains(t, writingSystems, writingSystem)
	}

	testDualItemLifecycle(t)
	for range 10 {
		testRenderedItemLifecycle(t)
	}
}

func testRenderedItemLifecycle(t *testing.T) {
	t.Helper()

	item, err := newItem(func(_ context.Context, tile vectorTileID) (*tileBucket, error) {
		background := backgroundTriangles()
		return &tileBucket{
			tile:         tile,
			featureCount: 1,
			segments: []roadSegment{{
				Start: roadPoint{X: 32, Y: 96},
				End:   roadPoint{X: 224, Y: 160},
			}},
			land: fillBucket{
				featureCount: 1,
				triangles: []roadPoint{
					{X: -32, Y: -32}, {X: 288, Y: -32}, {X: 288, Y: 288},
					{X: -32, Y: -32}, {X: 288, Y: 288}, {X: -32, Y: 288},
				},
			},
			water: fillBucket{
				featureCount: 1,
				triangles: []roadPoint{
					{X: 48, Y: 48}, {X: 208, Y: 48}, {X: 128, Y: 208},
				},
				cutouts: []roadPoint{
					{X: 112, Y: 96}, {X: 144, Y: 96}, {X: 128, Y: 128},
				},
			},
			liberty: []libertyRenderPrimitive{{
				order: 0, layerID: "background", triangles: background,
				color: mapColor{Red: 248, Green: 244, Blue: 240, Alpha: 255},
			}},
			symbols: []libertySymbolCandidate{{
				order: 110, layerID: "label_country_1", anchor: roadPoint{X: 128, Y: 128},
				text: "Madrid", fontFamily: "Noto Sans Bold", textSize: 14,
				textColor: mapColor{Red: 40, Green: 48, Blue: 54, Alpha: 255},
				haloColor: mapColor{Red: 255, Green: 255, Blue: 255, Alpha: 255}, haloWidth: 1,
				lineHeight: 1.2, maximumWidth: 10, iconName: "airport", iconSize: 0.6,
				iconColor: mapColor{Red: 80, Green: 90, Blue: 100, Alpha: 255}, iconOpacity: 1,
				viewportAligned: true, iconViewportAligned: true,
			}},
		}, nil
	}, NewCamera(Coordinate{}, 9, 0, 0, 0))
	require.NoError(t, err)
	item.glyphs = newGlyphManager(nil, nil)
	defer func() {
		if item != nil {
			item.Close()
		}
	}()
	require.NotNil(t, item.QObject())
	require.NotNil(t, item.CameraQObject())
	require.Nil(t, item.QObject().Parent())

	engine := qml.NewQQmlApplicationEngine()
	require.NotNil(t, engine)
	defer func() {
		if engine != nil {
			engine.Delete()
		}
	}()
	engine.RootContext().SetContextProperty("vectorItem", item.QObject())
	engine.RootContext().SetContextProperty("vectorCamera", item.CameraQObject())
	engine.LoadData([]byte(lifecycleQML))
	require.Len(t, engine.RootObjects(), 1)
	require.GreaterOrEqual(t, item.camera.current().Revision, uint64(1))
	item.camera.handleValueChanged("viewportWidth", 200)
	item.camera.handleValueChanged("viewportHeight", 150)
	item.camera.handleValueChanged("centerTargetLatitude", 48.8566)
	item.camera.handleValueChanged("centerTargetLongitude", 2.3522)
	item.camera.handleValueChanged("centerSerial", 1)
	item.camera.handleValueChanged("projectLatitude", 48.8566)
	item.camera.handleValueChanged("projectLongitude", 2.3522)
	item.camera.handleValueChanged("projectSerial", 1)
	require.InDelta(t, 100, item.camera.projectedPoint.X, 1e-6)
	require.InDelta(t, 75, item.camera.projectedPoint.Y, 1e-6)
	item.camera.handleValueChanged("unprojectX", 100)
	item.camera.handleValueChanged("unprojectY", 75)
	item.camera.handleValueChanged("unprojectSerial", 1)
	require.InDelta(t, 48.8566, item.camera.unprojectedCoordinate.Latitude, 1e-6)
	require.InDelta(t, 2.3522, item.camera.unprojectedCoordinate.Longitude, 1e-6)
	item.camera.handleValueChanged("alignLatitude", 48.8566)
	item.camera.handleValueChanged("alignLongitude", 2.3522)
	item.camera.handleValueChanged("alignX", 80)
	item.camera.handleValueChanged("alignY", 60)
	item.camera.handleValueChanged("alignSerial", 1)
	aligned := item.camera.current().Camera.FromCoordinate(Coordinate{Latitude: 48.8566, Longitude: 2.3522})
	require.InDelta(t, 80, aligned.X, 1e-6)
	require.InDelta(t, 60, aligned.Y, 1e-6)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		qt.QCoreApplication_ProcessEvents()
		stats := item.Stats()
		if stats.TilesLoaded > 1 && stats.TilesLoading == 0 && stats.FallbackTiles == 0 &&
			stats.GeometryBuilds-stats.GeometryRemovals == uint64(stats.TilesLoaded) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	stats := item.Stats()
	require.Greater(t, stats.TilesLoaded, 1, "scheduler did not load multiple tiles before timeout")
	require.Equal(t, uint64(stats.TilesLoaded), stats.GeometryBuilds-stats.GeometryRemovals,
		"render thread did not retain every selected tile before timeout")
	runtime.GC()
	runtime.GC()
	qt.QCoreApplication_ProcessEvents()
	item.camera.handleValueChanged("statusSerial", 1)
	require.Equal(t, int64(stats.TilesLoaded), item.camera.properties.Value("tilesLoaded").ToLongLong())
	require.Equal(t, int64(stats.TilesLoading), item.camera.properties.Value("tilesLoading").ToLongLong())
	require.Zero(t, stats.GeometryUpdates)
	builds := stats.GeometryBuilds
	removals := stats.GeometryRemovals
	transforms := stats.TransformUpdates
	retainedSceneNodes := append([]*quick.QSGNode(nil), item.sceneNodes...)
	item.glyphs.mu.Lock()
	item.glyphs.revision++
	item.glyphs.mu.Unlock()

	item.camera.handleValueChanged("bearing", 1)
	deadline = time.Now().Add(time.Second)
	for item.Stats().TransformUpdates == transforms && time.Now().Before(deadline) {
		qt.QCoreApplication_ProcessEvents()
		time.Sleep(time.Millisecond)
	}
	stats = item.Stats()
	require.Greater(t, stats.TransformUpdates, transforms, "render thread did not update transforms before timeout")
	require.Zero(t, stats.GeometryUpdates)
	require.Equal(t, builds, stats.GeometryBuilds)
	require.Equal(t, removals, stats.GeometryRemovals)
	require.Len(t, item.sceneNodes, len(retainedSceneNodes))
	for index := range retainedSceneNodes {
		require.Same(t, retainedSceneNodes[index], item.sceneNodes[index], "glyph update replaced basemap scene node %d", index)
	}

	item.camera.handleValueChanged("panDX", 1024)
	item.camera.handleValueChanged("panDY", 0)
	item.camera.handleValueChanged("panSerial", 2)
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		qt.QCoreApplication_ProcessEvents()
		stats = item.Stats()
		if stats.GeometryRemovals > removals && stats.TilesLoading == 0 &&
			stats.GeometryBuilds-stats.GeometryRemovals == uint64(stats.TilesLoaded) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	stats = item.Stats()
	require.Greater(t, stats.GeometryRemovals, removals, "render thread did not remove stale tiles before timeout")
	require.Equal(t, uint64(stats.TilesLoaded), stats.GeometryBuilds-stats.GeometryRemovals)
	require.Zero(t, stats.GeometryUpdates)

	engine.Delete()
	engine = nil
	require.Nil(t, item.QObject().Parent())
	beforeClose := item.Stats()
	item.Close()
	afterClose := item.Stats()
	require.Equal(t, beforeClose.TilesLoaded, afterClose.TilesLoaded)
	require.Nil(t, item.tiles.Load())
	require.Nil(t, item.styledTiles.Load())
	require.Nil(t, item.renderedScene)
	item.Close()
	item = nil
}

func testDualItemLifecycle(t *testing.T) {
	t.Helper()
	first, err := newItem(nil, NewCamera(Coordinate{}, 9, 0, 0, 0))
	require.NoError(t, err)
	second, err := newItem(nil, NewCamera(Coordinate{}, 9, 0, 0, 0))
	require.NoError(t, err)
	defer first.Close()
	defer second.Close()

	engine := qml.NewQQmlApplicationEngine()
	require.NotNil(t, engine)
	engine.RootContext().SetContextProperty("firstItem", first.QObject())
	engine.RootContext().SetContextProperty("secondItem", second.QObject())
	engine.LoadData([]byte(`
import QtQuick
import QtQuick.Window
Window {
    visible: true
    width: 240
    height: 120
    Item {
        id: firstHost
        width: 120
        height: parent.height
		Binding { target: firstItem; property: "parent"; value: firstHost }
		Binding { target: firstItem; property: "width"; value: firstHost.width }
		Binding { target: firstItem; property: "height"; value: firstHost.height }
    }
    Item {
        id: secondHost
        x: 120
        width: 120
        height: parent.height
		Binding { target: secondItem; property: "parent"; value: secondHost }
		Binding { target: secondItem; property: "width"; value: secondHost.width }
		Binding { target: secondItem; property: "height"; value: secondHost.height }
    }
}`))
	require.Len(t, engine.RootObjects(), 1)
	require.NotEqual(t, first.QObject().UnsafePointer(), second.QObject().UnsafePointer())

	first.camera.handleValueChanged("centerTargetLatitude", 48.8566)
	first.camera.handleValueChanged("centerTargetLongitude", 2.3522)
	first.camera.handleValueChanged("centerSerial", 1)
	second.camera.handleValueChanged("centerTargetLatitude", 35.6762)
	second.camera.handleValueChanged("centerTargetLongitude", 139.6503)
	second.camera.handleValueChanged("centerSerial", 1)
	require.NotEqual(t, first.camera.current().Camera.Center, second.camera.current().Camera.Center)

	engine.Delete()
	require.Nil(t, first.QObject().Parent())
	require.Nil(t, second.QObject().Parent())
}
