pragma ComponentBehavior: Bound

import QtQuick 2.15
import QtQuick.Window 2.15
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
                mapLibreAvailable: false
                theme: testCase.testTheme
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
        verify(unavailableNotice !== null);
        verify(attribution !== null);
        verify(attributionText !== null);

        tryCompare(unavailableNotice, "visible", true);
        compare(attribution.visible, false);

        overlay.mapLibreAvailable = true;
        tryCompare(attribution, "visible", true);
        compare(unavailableNotice.visible, false);

        verify(attributionText.text.indexOf("https://openfreemap.org") !== -1);
        verify(attributionText.text.indexOf("https://www.openmaptiles.org/") !== -1);
        verify(attributionText.text.indexOf("https://www.openstreetmap.org/copyright") !== -1);
        compare(overlay.attributionHeight, attribution.height);
        verify(overlay.attributionHeight > 0);
    }
}
