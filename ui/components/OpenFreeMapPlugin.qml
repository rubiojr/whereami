import QtQml 2.15
import QtLocation 6.5

/*
    OpenFreeMapPlugin

    Shared map provider configuration for every map in the application.

    Renders the OpenFreeMap vector basemap through the MapLibre QtLocation
    geoservice. That provider is a native plugin built against the exact Qt
    version it runs on, so it is not always present; when it is missing the
    plugin falls back to QtLocation's overlay-only provider so waypoints,
    navigation and tests keep working without a basemap.
*/

Plugin {
    id: root

    readonly property bool mapLibreAvailable: availableServiceProviders.indexOf("maplibre") !== -1
    readonly property string styleUrl: "https://tiles.openfreemap.org/styles/liberty"

    // Persistent cache for vector tiles, styles, glyphs and sprites. The
    // application injects its own cache directory so --cache-dir governs the
    // map cache too. Without it MapLibre keeps the cache in memory only.
    // qmllint disable unqualified
    readonly property string cacheDirectory: typeof whereamiMapCacheDir !== "undefined" ? whereamiMapCacheDir : ""
    // qmllint enable unqualified
    readonly property int cacheSizeBytes: 268435456

    // Keep maps and their overlays usable when the native runtime is missing.
    name: mapLibreAvailable ? "maplibre" : "itemsoverlay"

    PluginParameter {
        name: "maplibre.map.styles"
        value: root.styleUrl
    }

    PluginParameter {
        name: "maplibre.cache.directory"
        value: root.cacheDirectory
    }

    PluginParameter {
        name: "maplibre.cache.size"
        value: root.cacheSizeBytes
    }

    Component.onCompleted: {
        if (root.mapLibreAvailable && root.cacheDirectory === "") {
            console.warn("Map cache directory unavailable; caching map data in memory for this session only.");
        }
    }
}
