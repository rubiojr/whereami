pragma ComponentBehavior: Bound

import QtQuick 2.15
import QtQuick.Window 2.15
import QtPositioning 6.5
import QtTest 1.2
import "../components"

TestCase {
    id: testCase
    name: "MapProvider"

    readonly property QtObject testTheme: QtObject {
        property color toolbarBackground: "#20242b"
        property color toolbarBorder: "#58606c"
        property color toolbarText: "white"
        property color accent: "orange"

        function scale() {
            return 12;
        }
    }

    Component {
        id: pluginComponent
        OpenFreeMapPlugin {}
    }

    Component {
        id: overlayWindowComponent
        Window {
            width: 800
            height: 600
            visible: true
            property alias overlay: providerOverlay

            MapProviderOverlay {
                id: providerOverlay
                anchors.fill: parent
                basemapAvailable: false
                theme: testCase.testTheme
            }
        }
    }

    Component {
        id: adapterWindowComponent

        Window {
            width: 320
            height: 200
            visible: true
            property alias adapter: mapAdapter
            property alias vectorCamera: cameraState

            Item {
                id: fakeVectorItem
            }

            QtObject {
                id: cameraState
                property real centerLatitude: 0
                property real centerLongitude: 0
                property real centerTargetLatitude: 0
                property real centerTargetLongitude: 0
                property int centerSerial: 0
                property real zoomLevel: 9
                property real bearing: 0
                property real minimumZoomLevel: 0
                property real maximumZoomLevel: 20
                property real viewportWidth: 0
                property real viewportHeight: 0
                property int cameraRevision: 0
                property int statusSerial: 0
                property int tilesRequested: 0
                property int tilesLoaded: 0
                property int tilesLoading: 0
                property int tileErrors: 0
                property string tileError: ""

                onZoomLevelChanged: cameraRevision++
            }

            OpenFreeMapPlugin {
                id: legacyPlugin
            }

            MapAdapter {
                id: mapAdapter
                anchors.fill: parent
                vectorRequested: true
                vectorItem: fakeVectorItem
                vectorCamera: cameraState
                legacyPlugin: legacyPlugin
                center: QtPositioning.coordinate(0, 0)
                zoomLevel: 9

                Behavior on zoomLevel {
                    enabled: !mapAdapter.syncingBackend
                    NumberAnimation { duration: 300 }
                }
            }
        }
    }

    function test_providerConfiguration() {
        var plugin = createTemporaryObject(pluginComponent, testCase);
        verify(plugin !== null);
        compare(plugin.styleUrl, "https://tiles.openfreemap.org/styles/liberty");

        var expectedProvider = plugin.availableServiceProviders.indexOf("maplibre") !== -1
                               ? "maplibre" : "itemsoverlay";
        compare(plugin.name, expectedProvider);

        // A provider the installation does not ship leaves every map blank.
        verify(plugin.availableServiceProviders.indexOf(plugin.name) !== -1);
    }

    function test_pluginConfiguresPersistentCache() {
        var plugin = createTemporaryObject(pluginComponent, testCase);
        verify(plugin !== null);
        compare(plugin.cacheSizeBytes, 268435456);

        var byName = {};
        for (var i = 0; i < plugin.parameters.length; i++) {
            byName[plugin.parameters[i].name] = plugin.parameters[i].value;
        }
        compare(byName["maplibre.map.styles"], plugin.styleUrl);
        compare(byName["maplibre.cache.size"], plugin.cacheSizeBytes);
        verify("maplibre.cache.directory" in byName);
        compare(byName["maplibre.cache.directory"], plugin.cacheDirectory);
    }

    function test_overlayShowsAttributionOnlyWithMapLibre() {
        var win = createTemporaryObject(overlayWindowComponent, testCase);
        verify(win !== null);
        var overlay = win.overlay;

        var unavailableNotice = findChild(overlay, "mapProviderUnavailableNotice");
        var attribution = findChild(overlay, "mapAttribution");
        var attributionText = findChild(overlay, "mapAttributionText");
        var providerStatus = findChild(overlay, "mapProviderStatus");
        var providerStatusText = findChild(overlay, "mapProviderStatusText");
        verify(unavailableNotice !== null);
        verify(attribution !== null);
        verify(attributionText !== null);
        verify(providerStatus !== null);
        verify(providerStatusText !== null);

        tryCompare(unavailableNotice, "visible", true);
        compare(attribution.visible, false);

        overlay.basemapAvailable = true;
        tryCompare(attribution, "visible", true);
        compare(unavailableNotice.visible, false);

        verify(attributionText.text.indexOf("https://openfreemap.org") !== -1);
        verify(attributionText.text.indexOf("https://www.openmaptiles.org/") !== -1);
        verify(attributionText.text.indexOf("https://www.openstreetmap.org/copyright") !== -1);
        compare(overlay.attributionHeight, attribution.height);
        verify(overlay.attributionHeight > 0);

        overlay.mapLoading = true;
        tryCompare(providerStatus, "visible", true);
        overlay.mapLoading = false;
        overlay.mapError = "network unavailable";
        tryCompare(providerStatus, "visible", true);
        compare(providerStatusText.text, "Map error: network unavailable");
        overlay.mapFallbackActive = true;
        compare(providerStatusText.text, "Map fallback active: network unavailable");
    }

    function test_adapterFallsBackOnlyAfterTotalVectorFailure() {
        var win = createTemporaryObject(adapterWindowComponent, testCase);
        verify(win !== null);
        var adapter = win.adapter;
        var camera = win.vectorCamera;
        tryCompare(adapter, "vectorActive", true);

        camera.tilesRequested = 2;
        camera.tilesLoaded = 1;
        camera.tileErrors = 1;
        camera.tileError = "one tile unavailable";
        wait(300);
        compare(adapter.vectorFailed, false);

        camera.tilesLoaded = 0;
        camera.tileErrors = 2;
        camera.tileError = "all tiles unavailable";
        tryCompare(adapter, "vectorFailed", true, 1000);
        compare(adapter.vectorActive, false);
        compare(adapter.backendFailure, "all tiles unavailable");
    }

    function test_adapterDoesNotInterruptAnimatedZoomOnCameraRevision() {
        failOnWarning(/Binding loop detected/);
        var win = createTemporaryObject(adapterWindowComponent, testCase);
        verify(win !== null);
        var adapter = win.adapter;
        var camera = win.vectorCamera;
        tryCompare(adapter, "vectorActive", true);
        tryCompare(adapter, "zoomLevel", 9, 500);
        wait(200);

        adapter.zoomLevel = 11;
        wait(80);
        verify(adapter.zoomLevel > 9 && adapter.zoomLevel < 11);
        tryCompare(adapter, "zoomLevel", 11, 700);
        tryCompare(camera, "zoomLevel", 11, 700);
    }

    function test_adapterOnlyReportsLoadingUntilFirstRenderableCover() {
        var win = createTemporaryObject(adapterWindowComponent, testCase);
        verify(win !== null);
        var adapter = win.adapter;
        var camera = win.vectorCamera;
        tryCompare(adapter, "vectorActive", true);
        compare(adapter.ready, false);
        compare(adapter.loading, true);

        camera.tilesRequested = 4;
        camera.tilesLoading = 4;
        compare(adapter.loading, true);

        camera.tilesLoaded = 1;
        camera.tilesLoading = 3;
        tryCompare(adapter, "ready", true, 1000);
        compare(adapter.loading, false);

        camera.tilesLoaded = 0;
        camera.tilesLoading = 4;
        wait(300);
        compare(adapter.ready, true);
        compare(adapter.loading, false);
    }
}
