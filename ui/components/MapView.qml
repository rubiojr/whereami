// qmllint disable unqualified
// NOTE: Pending addition of bottom-right floating logo overlay.
// I don’t have the full file with line numbers here to safely append the overlay.
// Please provide (a) the last ~15 lines of this file with their line numbers
// OR confirm that I should insert the overlay just before the final closing brace
// of the ApplicationWindow. Then I can supply an exact minimal patch.
//
// Proposed snippet (will be added near the end of ApplicationWindow):
//
//     Item {
//         id: logoOverlay
//         anchors.right: parent.right
//         anchors.bottom: parent.bottom
//         anchors.margins: 12
//         width: 48
//         height: 48
//         visible: true
//         opacity: 0.85
//         Image {
//             anchors.fill: parent
//             source: "qrc:/icons/io.github.rubiojr.whereami.svg"
//             fillMode: Image.PreserveAspectFit
//             smooth: true
//         }
//         MouseArea {
//             anchors.fill: parent
//             hoverEnabled: true
//             onEntered: logoOverlay.opacity = 1.0
//             onExited: logoOverlay.opacity = 0.85
//             acceptedButtons: Qt.NoButton   // purely decorative
//         }
//     }
//
// Let me know and I’ll produce the final exact edit block.
// (If the file already starts with a different first line, send that too so I can anchor properly.)
import QtQuick 2.15
import QtQuick.Controls 2.15

import QtQuick.Layouts 1.15
import QtLocation 6.5
import QtPositioning 6.5
import Qt.labs.platform 1.1
import "../services"
import "../themes"
import "../lib/MapViewLogic.js" as MapViewLogic

import "."

ApplicationWindow {
    id: window
    visible: true
    width: 1300
    height: 800
    title: qsTr("WhereAmI - GPX Waypoint Viewer")
    flags: Qt.FramelessWindowHint
    color: "transparent"

    // Local theme instance
    ThemeLoader {
        id: theme
    }

    Knobs {
        id: knobs
    }

    // Central API service (stage 1 migration: waypoint load, clusters, location, import)
    API {
        id: api
        apiPort: 43098

        // Populate waypoints when loaded
        onWaypointsLoaded: function (arr) {
            window.waypoints = arr;
            if (clusteringEnabled)
                clusterFetchDebounce.restart();
        }

        // Update clusters
        onClustersFetched: function (clusters, params) {
            clusterModel = clusters;
        }

        // Handle location fetch (mirrors previous XHR success logic)
        onLocationFetched: function (loc) {
            if (!loc)
                return;
            var wasValid = currentLocationValid;
            currentLocationValid = true;
            currentLocationLat = loc.lat;
            currentLocationLon = loc.lon;
            currentLocationAccuracy = loc.accuracy_m || 0;
            if ((forceCenterOnNextFix || !wasValid) && map) {
                map.center = QtPositioning.coordinate(currentLocationLat, currentLocationLon);
                if ((map.zoomLevel < knobs.searchZoomLevel || !wasValid)) {
                    locationZoomTimer.restart();
                }
            }
            forceCenterOnNextFix = false;
        }

        // Import lifecycle
        onImportCompleted: function (summary, params) {
            // Refresh waypoints after import
            api.getWaypoints();
            if (clusteringEnabled)
                clusterFetchDebounce.restart();

            var msg = "";
            if (summary && typeof summary === "object") {
                var importedCount = (summary.count !== undefined ? summary.count : waypoints.length);
                var skippedCount = (summary.skipped !== undefined ? summary.skipped : 0);
                if (skippedCount > 0) {
                    msg = "Imported " + importedCount + " waypoint(s); skipped " + skippedCount + " duplicate file(s)";
                } else {
                    msg = "Imported " + importedCount + " waypoint(s)";
                }
            } else {
                msg = "GPX import complete";
            }
            if (undoBar && undoBar.show)
                undoBar.show(msg);
            if (locationSnack && locationSnack.showing)
                locationSnack.hide();
        }
        onImportFailed: function (error, params) {
            console.error("Import failed:", error);
            if (locationSnack && locationSnack.showWithAction)
                locationSnack.showWithAction("Import failed", "");
        }

        // --- Migrated waypoint add/delete handlers (formerly WaypointService) ---
        onWaypointAddStarted: function (wp) {
            var arr = window.waypoints.slice();
            arr.push(wp);
            window.waypoints = arr;
        }
        onWaypointAdded: function (savedWp, originalWp) {
            var arr = window.waypoints.slice();
            var foundIndex = -1;
            for (var i = 0; i < arr.length; i++) {
                var it = arr[i];
                if (it === originalWp || (it.name === originalWp.name && Math.abs(it.lat - originalWp.lat) < 1e-9 && Math.abs(it.lon - originalWp.lon) < 1e-9)) {
                    arr[i] = savedWp;
                    foundIndex = i;
                    break;
                }
            }
            window.waypoints = arr;
            // Select the newly saved waypoint and show its info card
            if (foundIndex >= 0) {
                window.selectedWaypoint = savedWp;
                window.selectedWaypointIndex = foundIndex;
                // Center map (non-intrusive: only if not already centered on a different selection)
                if (map && map.center && (Math.abs(map.center.latitude - savedWp.lat) > 1e-6 || Math.abs(map.center.longitude - savedWp.lon) > 1e-6)) {
                    map.center = QtPositioning.coordinate(savedWp.lat, savedWp.lon);
                }
            }
            if (clusteringEnabled)
                clusterFetchDebounce.restart();
        }
        onWaypointAddFailed: function (originalWp, errorMessage) {
            console.error("Add waypoint failed:", errorMessage);
            var arr = window.waypoints.slice();
            for (var i = 0; i < arr.length; i++) {
                var it = arr[i];
                if (it === originalWp || (it.name === originalWp.name && Math.abs(it.lat - originalWp.lat) < 1e-9 && Math.abs(it.lon - originalWp.lon) < 1e-9)) {
                    arr.splice(i, 1);
                    break;
                }
            }
            window.waypoints = arr;
        }
        onWaypointDeleteStarted: function (wp) {
            var arr = window.waypoints.slice();
            for (var i = 0; i < arr.length; i++) {
                var it = arr[i];
                if (it === wp || (it.name === wp.name && Math.abs(it.lat - wp.lat) < 1e-9 && Math.abs(it.lon - wp.lon) < 1e-9)) {
                    arr.splice(i, 1);
                    break;
                }
            }
            window.waypoints = arr;
            if (window.selectedWaypoint && window.selectedWaypoint.name === wp.name && Math.abs(window.selectedWaypoint.lat - wp.lat) < 1e-9 && Math.abs(window.selectedWaypoint.lon - wp.lon) < 1e-9) {
                window.selectedWaypoint = null;
                window.selectedWaypointIndex = -1;
            }
        }
        onWaypointDeleted: function (wp) {
            lastDeletedWaypoint = wp;
            undoBar.show(wp && wp.name ? ("Deleted \"" + wp.name + "\"") : "Waypoint deleted");
            if (clusteringEnabled)
                clusterFetchDebounce.restart();
        }
        onWaypointDeleteFailed: function (wp, errorMessage) {
            console.error("Delete waypoint failed:", errorMessage);
            var arr = window.waypoints.slice();
            var exists = false;
            for (var i = 0; i < arr.length; i++) {
                if (arr[i].name === wp.name && Math.abs(arr[i].lat - wp.lat) < 1e-9 && Math.abs(arr[i].lon - wp.lon) < 1e-9) {
                    exists = true;
                    break;
                }
            }
            if (!exists) {
                arr.push(wp);
                window.waypoints = arr;
            }
            if (clusteringEnabled)
                clusterFetchDebounce.restart();
        }
        // --- Tag synchronization (ensure WaypointTable updates immediately) ---
        // When tags are fetched / added / deleted in the info card, update the authoritative
        // waypoint list here so any open views (e.g. WaypointTable) re-render with new tags.
        onTagsFetched: function (name, lat, lon, tags) {
            if (!tags || !Array.isArray(tags))
                return;
            var arr = window.waypoints.slice();
            for (var i = 0; i < arr.length; i++) {
                var w = arr[i];
                if (!w)
                    continue;
                if (w.name === name && Math.abs(w.lat - lat) < 1e-9 && Math.abs(w.lon - lon) < 1e-9) {
                    // Shallow copy to trigger change notification
                    var copy = {};
                    for (var k in w)
                        copy[k] = w[k];
                    copy.tags = tags.slice();
                    arr[i] = copy;
                    break;
                }
            }
            window.waypoints = arr;
        }
        onTagAdded: function (name, lat, lon, tags, tag) {
            if (!tags || !Array.isArray(tags))
                return;
            var arr = window.waypoints.slice();
            for (var i = 0; i < arr.length; i++) {
                var w = arr[i];
                if (!w)
                    continue;
                if (w.name === name && Math.abs(w.lat - lat) < 1e-9 && Math.abs(w.lon - lon) < 1e-9) {
                    var copy = {};
                    for (var k in w)
                        copy[k] = w[k];
                    copy.tags = tags.slice();
                    arr[i] = copy;
                    break;
                }
            }
            window.waypoints = arr;
        }
        onTagDeleted: function (name, lat, lon, tags, tag) {
            if (!tags || !Array.isArray(tags))
                return;
            var arr = window.waypoints.slice();
            for (var i = 0; i < arr.length; i++) {
                var w = arr[i];
                if (!w)
                    continue;
                if (w.name === name && Math.abs(w.lat - lat) < 1e-9 && Math.abs(w.lon - lon) < 1e-9) {
                    var copy = {};
                    for (var k in w)
                        copy[k] = w[k];
                    copy.tags = tags.slice();
                    arr[i] = copy;
                    break;
                }
            }
            window.waypoints = arr;
        }
    }

    // Shortcuts controller
    ShortcutsController {
        id: shortcutsController
        map: map
        knobs: knobs
        api: api
        addWaypointDialog: typeof addWaypointDialog !== 'undefined' ? addWaypointDialog : null
        logoOverlay: logoOverlay
        aboutOverlay: aboutOverlay
        locationSnack: locationSnack

        // Bind state properties
        selectedWaypoint: window.selectedWaypoint
        selectedWaypointIndex: window.selectedWaypointIndex
        waypoints: window.waypoints
        searchOverlayVisible: window.searchOverlayVisible
        waypointTableVisible: window.waypointTableVisible
        infoCardVisible: window.infoCardVisible
        showNonBookmarkWaypoints: window.showNonBookmarkWaypoints
        clusteringEnabled: window.clusteringEnabled
        tagFilterActive: window.tagFilterActive
        searchResultLocation: window.searchResultLocation
        lastSearchSuggestion: window.lastSearchSuggestion
        pendingLat: window.pendingLat
        pendingLon: window.pendingLon
        pendingWaypointName: window.pendingWaypointName

        // Handle signals
        onToggleSearch: window.searchOverlayVisible = !window.searchOverlayVisible
        onToggleBookmarkMode: {
            window.showNonBookmarkWaypoints = !window.showNonBookmarkWaypoints;
            // If we switch to bookmark-only and the selected waypoint is not a bookmark, clear selection
            if (!window.showNonBookmarkWaypoints && window.selectedWaypoint && !window.selectedWaypoint.bookmark) {
                window.selectedWaypoint = null;
                window.selectedWaypointIndex = -1;
            }
            if (window.clusteringEnabled && !window.tagFilterActive) {
                clusterFetchDebounce.restart();
            }

            // Snackbar feedback
            if (locationSnack && locationSnack.show) {
                var msg = window.showNonBookmarkWaypoints ? "Showing all waypoints" : "Showing only bookmarks";
                // Ensure it auto-hides quickly for mode toggles
                locationSnack.autoHide = true;
                locationSnack.durationMs = 3000;
                locationSnack.show(msg);
            }
        }
        onToggleWaypointTable: {
            window.waypointTableVisible = !window.waypointTableVisible;
            if (window.waypointTableVisible) {
                window.searchOverlayVisible = false;
            }
        }
        onToggleInfoCard: {
            if (window.selectedWaypoint) {
                window.infoCardVisible = !window.infoCardVisible;
            }
        }
        onClearSelection: {
            window.selectedWaypoint = null;
            window.selectedWaypointIndex = -1;
        }
        onClusterFetchRequested: {
            if (clusteringEnabled) {
                clusterFetchDebounce.restart();
            }
        }
        onOpenAddWaypointDialog: function (lat, lon, presetName) {
            window.pendingLat = lat;
            window.pendingLon = lon;
            window.pendingWaypointName = presetName;
        }
        onEscapePressed: {
            if (typeof addWaypointDialog !== 'undefined' && addWaypointDialog.visible) {
                // Explicit reject so any rejection handlers run; return to avoid falling through.
                addWaypointDialog.reject();
                return;
            }
            if (window.selectedWaypoint) {
                window.selectedWaypoint = null;
                window.selectedWaypointIndex = -1;
            } else if (window.searchOverlayVisible) {
                window.searchOverlayVisible = false;
            } else if (window.waypointTableVisible) {
                window.waypointTableVisible = false;
            }
        }
        onDeleteSelectedBookmark: {
            if (window.selectedWaypoint && window.selectedWaypoint.bookmark) {
                var idx = MapViewLogic.findWaypointIndex(window.waypoints, window.selectedWaypoint);
                if (idx !== -1) {
                    api.deleteWaypoint(window.waypoints[idx]);
                }
            }
        }
        onToggleHelp: {
            if (helpOverlay) {
                helpOverlay.toggle();
            }
        }
        onZoomToSelectedWaypoint: {
            if (window.selectedWaypoint && map) {
                centerThenZoomAnimation.stop();
                centerThenZoomAnimation.targetCenter = QtPositioning.coordinate(window.selectedWaypoint.lat, window.selectedWaypoint.lon);
                centerThenZoomAnimation.targetZoom = knobs.searchZoomLevel;
                centerThenZoomAnimation.start();
            }
        }
        onGoToCurrentLocation: {
            var hadFix = currentLocationValid;
            // Request a location update (force center when new fix arrives)
            fetchCurrentLocation(true);
            if (!locationPollTimer.running)
                locationPollTimer.start();
            // If we already have a fix, center immediately for responsiveness
            if (hadFix && map) {
                map.center = QtPositioning.coordinate(currentLocationLat, currentLocationLon);
                if (map.zoomLevel < knobs.searchZoomLevel)
                    map.zoomLevel = knobs.searchZoomLevel;
            }
            if (!hadFix && locationSnack && locationSnack.show) {
                locationSnack.autoHide = false;
                locationSnack.show("Locating...");
            }
        }
    }

    // Initial waypoints now loaded via HTTP at startup (no Go injection)
    property var waypoints: []
    property var selectedWaypoint: null
    property bool infoCardVisible: true
    property int selectedWaypointIndex: -1
    property string mouseCoordinates: ""
    property var searchResultLocation: null
    property var lastSearchSuggestion: null
    property bool waypointTableVisible: false
    // Tag filter (propagated from SearchBox)
    property bool tagFilterActive: false
    property var tagFilteredWaypoints: []
    // Aggregated unique tag vocabulary (kept in sync with waypoints)
    property var allTags: []
    // Local cluster model for tag-filtered waypoints (constructed client-side)
    property var tagClusterModel: []

    // Filtering: toggle visibility of non-bookmark waypoints.
    // When false, only bookmark (`bookmark: true`) waypoints are shown.
    property bool showNonBookmarkWaypoints: true
    property var bookmarkOnlyWaypoints: []

    onWaypointsChanged: {
        // Recompute bookmarkOnlyWaypoints (avoid mutating original objects)
        bookmarkOnlyWaypoints = MapViewLogic.extractBookmarkWaypoints(waypoints);

        // Recompute global tag vocabulary
        allTags = MapViewLogic.buildTagVocabulary(waypoints);

        if (clusteringEnabled)
            clusterFetchDebounce.restart();

        // Clear selection if hidden by filter
        if (MapViewLogic.shouldClearSelection(showNonBookmarkWaypoints, selectedWaypoint)) {
            window.selectedWaypoint = null;
            window.selectedWaypointIndex = -1;
        }
    }

    // Current location (GeoClue) state
    property bool currentLocationValid: false
    property double currentLocationLat: 0
    property double currentLocationLon: 0
    property real currentLocationAccuracy: 0
    // Track whether next successful fix should force recenter (API migration helper)
    property bool forceCenterOnNextFix: false

    // Hide the locating snackbar once we have a fix
    onCurrentLocationValidChanged: {
        if (currentLocationValid && locationSnack.showing) {
            locationSnack.hide();
        }
    }

    // Poll GeoClue-backed HTTP location endpoint
    Timer {
        id: locationPollTimer
        interval: 10000
        repeat: true
        running: false
        onTriggered: fetchCurrentLocation(false)
    }

    // Delayed zoom so we animate center first, then zoom in.
    Timer {
        id: locationZoomTimer
        interval: 850    // Slightly longer than center animation (800ms) to avoid concurrent jump
        repeat: false
        onTriggered: {
            if (map && (map.zoomLevel < knobs.searchZoomLevel)) {
                map.zoomLevel = knobs.searchZoomLevel;
            }
        }
    }

    // Sequential animation for zoom-out-then-center-then-zoom-in behavior
    SequentialAnimation {
        id: centerThenZoomAnimation
        property var targetCenter: null
        property real targetZoom: 17
        property real originalZoom: 0

        // First, store original zoom and zoom out if we're already at target zoom
        ScriptAction {
            script: {
                if (map) {
                    centerThenZoomAnimation.originalZoom = map.zoomLevel;
                    // If already at target zoom, zoom out first to make movement visible
                    if (Math.abs(centerThenZoomAnimation.originalZoom - centerThenZoomAnimation.targetZoom) < 0.1) {
                        map.zoomLevel = 6;
                    }
                }
            }
        }
        // Brief pause for zoom out animation
        PauseAnimation {
            duration: map && Math.abs(centerThenZoomAnimation.originalZoom - centerThenZoomAnimation.targetZoom) < 0.1 ? 400 : 0
        }
        // Then center on the waypoint
        ScriptAction {
            script: {
                if (map && centerThenZoomAnimation.targetCenter) {
                    map.center = centerThenZoomAnimation.targetCenter;
                }
            }
        }
        // Wait for center animation to complete
        PauseAnimation {
            duration: 800
        }
        // Finally zoom to target
        ScriptAction {
            script: {
                if (map) {
                    map.zoomLevel = centerThenZoomAnimation.targetZoom;
                }
            }
        }
    }
    // (Removed highlightFadeTimer – searchResultLocation now persists until a new search result is chosen or a waypoint is selected)
    property var newWaypoints: []
    // Directory chooser for importing GPX files (FolderDialog from Qt.labs.platform)
    FolderDialog {
        id: gpxDirDialog
        title: "Select GPX Directory"
        currentFolder: StandardPaths.writableLocation(StandardPaths.HomeLocation)
        onAccepted: {
            if (gpxDirDialog.currentFolder && map) {
                map.importGpxDirectory(gpxDirDialog.currentFolder);
            }
        }
    }
    property bool addWaypointDialogOpen: false
    property double pendingLat: 0
    property double pendingLon: 0
    property string pendingWaypointName: ""
    property int pendingClickX: 0
    property int pendingClickY: 0

    // Keyboard shortcuts: Ctrl++ / Ctrl+= / Ctrl+- to zoom in/out the map.
    // Search overlay visibility toggled with Ctrl+F.
    property bool searchOverlayVisible: false
    // Clustering (backend-provided)
    property bool clusteringEnabled: true
    property var clusterModel: []
    property int clusterGridSize: 30

    // WaypointService removed (Stage 2). Logic migrated to API signal handlers.

    // --- Undo Snackbar (component) ---
    property var lastDeletedWaypoint: null

    SnackBar {
        id: undoBar
        actionText: "Undo"
        anchors.horizontalCenter: parent.horizontalCenter
        anchors.bottom: parent.bottom
        anchors.bottomMargin: 20
        z: 1000
        durationMs: 5000
        onUndoRequested: {
            if (lastDeletedWaypoint) {
                api.addWaypoint(lastDeletedWaypoint);
                lastDeletedWaypoint = null;
                if (clusteringEnabled)
                    clusterFetchDebounce.restart();
            }
        }
        onDismissed: {
            lastDeletedWaypoint = null;
        }
    }

    // Snackbar used to indicate that location is being acquired
    SnackBar {
        id: locationSnack
        anchors.horizontalCenter: parent.horizontalCenter
        anchors.bottom: parent.bottom
        anchors.bottomMargin: undoBar.visible ? 70 : 20
        z: 1001
        actionText: ""          // no action button
        durationMs: 10000
        autoHide: false          // we hide manually when fix arrives
    }

    // Debounce + fetch logic for backend clusters
    Timer {
        id: clusterFetchDebounce
        interval: 200
        repeat: false
        onTriggered: fetchClusters()
    }

    // Migrated to central API service
    function fetchClusters() {
        if (!clusteringEnabled)
            return;
        if (!map)
            return;

        // If a tag filter is active, build clusters locally over the filtered subset.
        if (tagFilterActive) {
            tagClusterModel = MapViewLogic.buildLocalClusters(tagFilteredWaypoints || [], map.zoomLevel, clusterGridSize);
            return;
        }

        // Normal (non tag-filter) clustering via backend (with bookmark-only subset if requested)
        api.getClusters(Math.round(map.zoomLevel), clusterGridSize, !showNonBookmarkWaypoints);
    }

    // Focus a waypoint by (case-insensitive) name. Returns true if found (bookmark preferred).
    function focusWaypointByName(name) {
        var chosen = MapViewLogic.findWaypointByName(waypoints, name);
        if (!chosen)
            return false;

        window.selectedWaypoint = chosen;
        window.selectedWaypointIndex = MapViewLogic.findWaypointIndex(waypoints, chosen);

        // Highlight & animate
        searchResultLocation = QtPositioning.coordinate(chosen.lat, chosen.lon);

        // Use sequential zoom-out-center-zoom-in animation
        centerThenZoomAnimation.stop();
        centerThenZoomAnimation.targetCenter = QtPositioning.coordinate(chosen.lat, chosen.lon);
        centerThenZoomAnimation.targetZoom = knobs.searchZoomLevel;
        centerThenZoomAnimation.start();
        return true;
    }

    // Migrated to central API service
    function fetchCurrentLocation(forceCenter) {
        if (forceCenter)
            forceCenterOnNextFix = true;
        api.getLocation();
    }

    // loadInitialWaypoints removed (migrated to api.getWaypoints())

    Component.onCompleted: {
        // Stage 1 migration: use central API for initial load
        api.getWaypoints();
        if (clusteringEnabled)
            clusterFetchDebounce.restart();
    }

    header: MapToolBar {
        id: toolbar
        cornerRadius: 12
        rootWindow: window
        // onSearchLocation removed (legacy GeocodeModel path)
        onOpenFile: {
            // Open directory picker
            gpxDirDialog.open();
        }
        onFitToWaypoints: {
            var bounds = MapViewLogic.calculateWaypointBounds(waypoints);
            if (bounds && map) {
                map.center = QtPositioning.coordinate(bounds.centerLat, bounds.centerLon);
                map.zoomLevel = bounds.zoom;
            }
        }
        onToggleWaypointsTable: {
            waypointTableVisible = !waypointTableVisible;
            if (waypointTableVisible)
                searchOverlayVisible = false;
        }
        onToggleInfoCard: {
            if (selectedWaypoint) {
                infoCardVisible = !infoCardVisible;
            }
        }
        onHelpRequested: {
            // Toggle (or open) the shortcuts help overlay
            if (helpOverlay)
                helpOverlay.toggle();
        }
        onQuitRequested: {
            Qt.quit();
        }
    }

    footer: MapStatusBar {
        id: statusBar
        cornerRadius: 12
        rootWindow: window
        mouseCoordinates: window.mouseCoordinates
        zoomLevel: map.zoomLevel
        waypointCount: window.waypoints.length
    }

    RowLayout {
        anchors.fill: parent
        spacing: 0

        // Left column removed — map takes full remaining width
        Item {
            Layout.fillWidth: true
            Layout.fillHeight: true

            Map {
                id: map
                anchors.fill: parent
                center: knobs.initialPosition
                zoomLevel: knobs.initialZoomLevel
                Behavior on center {
                    CoordinateAnimation {
                        duration: 800
                        easing.type: Easing.InOutQuad
                    }
                }
                Behavior on zoomLevel {
                    NumberAnimation {
                        duration: 600
                        easing.type: Easing.InOutQuad
                    }
                }
                onZoomLevelChanged: {
                    if (clusteringEnabled)
                        clusterFetchDebounce.restart();
                }

                // Import a directory of GPX files by calling backend /api/import.
                // dirUrl may be a QML url / object; we coerce to string and normalize.
                function importGpxDirectory(dirUrl) {
                    var path = MapViewLogic.normalizeDirectoryPath(dirUrl);
                    if (path === "") {
                        return;
                    }
                    if (locationSnack)
                        locationSnack.showWithAction("Importing GPX…", "");
                    // Delegate to central API service (handles success/failure + waypoint refresh)
                    api.importGpxDirectory({
                        dir: path,
                        recursive: true
                    });
                }

                Component.onCompleted: {
                    if (supportedMapTypes.length > 0) {
                        activeMapType = supportedMapTypes[supportedMapTypes.length - 1];
                    }
                }

                plugin: Plugin {
                    name: "osm"
                    // Identify client
                    PluginParameter {
                        name: "osm.useragent"
                        value: "WhereAmI GPX Viewer"
                    }
                    // Disable remote provider repository (removes redirect parsing noise)
                    PluginParameter {
                        name: "osm.mapping.providersrepository.disabled"
                        value: "true"
                    }
                    // Custom tile host now proxied locally to reduce upstream load & enable caching
                    PluginParameter {
                        name: "osm.mapping.custom.host"
                        value: "http://127.0.0.1:43098/api/tiles/%z/%x/%y.png"
                    }
                    // Caching (reduce repeated tile fetches / HTTP2 resets)
                    PluginParameter {
                        name: "osm.mapping.cache.disk.size"
                        value: "0"            // 128 MB
                    }
                    // High DPI tiles (set to false if hitting provider limits)
                    PluginParameter {
                        name: "osm.mapping.highdpi_tiles"
                        value: "true"
                    }
                    // Copyright / attribution
                    PluginParameter {
                        name: "osm.mapping.custom.mapcopyright"
                        value: "Carto"
                    }
                    PluginParameter {
                        name: "osm.mapping.custom.datacopyright"
                        value: "OpenStreetMap contributors"
                    }
                }

                // Gesture handlers
                property geoCoordinate startCentroid

                // Optimized pinch: accumulate scale; apply zoom once at gesture end to cut tile churn
                PinchHandler {
                    id: pinch
                    target: null
                    property real accumulatedScale: 1.0
                    onActiveChanged: {
                        if (active) {
                            map.startCentroid = map.toCoordinate(pinch.centroid.position, false);
                            accumulatedScale = 1.0;
                        } else {
                            if (accumulatedScale !== 1.0) {
                                var newZoom = map.zoomLevel + Math.log2(accumulatedScale);
                                // Clamp if minimum/maximum properties exist
                                if (map.minimumZoomLevel !== undefined)
                                    newZoom = Math.max(map.minimumZoomLevel, newZoom);
                                if (map.maximumZoomLevel !== undefined)
                                    newZoom = Math.min(map.maximumZoomLevel, newZoom);
                                map.zoomLevel = newZoom;
                                accumulatedScale = 1.0;
                                if (clusteringEnabled)
                                    clusterFetchDebounce.restart();
                            }
                        }
                    }
                    onScaleChanged: delta => {
                        accumulatedScale *= delta;
                        map.alignCoordinateToPoint(map.startCentroid, pinch.centroid.position);
                    }
                    onRotationChanged: delta => {
                        map.bearing -= delta;
                        map.alignCoordinateToPoint(map.startCentroid, pinch.centroid.position);
                    }
                    grabPermissions: PointerHandler.TakeOverForbidden
                }

                WheelHandler {
                    id: wheel
                    rotationScale: 1 / 120
                    property: "zoomLevel"
                }

                DragHandler {
                    id: drag
                    target: null
                    onTranslationChanged: delta => map.pan(-delta.x, -delta.y)
                }

                MouseArea {
                    anchors.fill: parent
                    hoverEnabled: true
                    acceptedButtons: Qt.AllButtons
                    onPressed: function (mouse) {
                        if (mouse.button === Qt.LeftButton) {
                            // Clear highlight when clicking on the map background
                            searchResultLocation = null;
                        }
                    }

                    onPositionChanged: {
                        var coord = map.toCoordinate(Qt.point(mouseX, mouseY));
                        window.mouseCoordinates = coord.latitude.toFixed(6) + ", " + coord.longitude.toFixed(6);
                    }
                    onExited: window.mouseCoordinates = ""
                    onDoubleClicked: function (mouse) {
                        // Center map on double-click position and zoom in one level (clamped)
                        var coord = map.toCoordinate(Qt.point(mouse.x, mouse.y));
                        map.center = coord;
                        var targetZoom = map.zoomLevel + 1;
                        if (map.maximumZoomLevel !== undefined)
                            targetZoom = Math.min(map.maximumZoomLevel, targetZoom);
                        map.zoomLevel = targetZoom;
                        if (clusteringEnabled)
                            clusterFetchDebounce.restart();
                    }

                    // Global add waypoint dialog (used on right-click)
                    AddWaypointDialog {
                        id: addWaypointDialog
                        lat: pendingLat
                        lon: pendingLon
                        titleText: "Add Waypoint"

                        // The component emits `addRequested(wp)` when the user accepts.
                        // Delegate persistence to central service.
                        onAddRequested: function (wp) {
                            api.addWaypoint(wp);
                            // Preselect immediately so the info card opens; final object replaced on onWaypointAdded.
                            window.selectedWaypoint = wp;
                            window.selectedWaypointIndex = -1;
                        }
                    }
                }

                // Right-click to add a new waypoint
                MouseArea {
                    anchors.fill: parent
                    acceptedButtons: Qt.RightButton
                    onPressed: function (mouse) {
                        if (mouse.button === Qt.RightButton) {
                            var c = map.toCoordinate(Qt.point(mouse.x, mouse.y));
                            pendingLat = c.latitude;
                            pendingLon = c.longitude;
                            pendingWaypointName = "";
                            pendingClickX = mouse.x;
                            pendingClickY = mouse.y;
                            if (typeof addWaypointDialog !== 'undefined') {
                                addWaypointDialog.lat = pendingLat;
                                addWaypointDialog.lon = pendingLon;
                                addWaypointDialog.presetName = ""; // Clear any previous preset (e.g. from search result)
                                addWaypointDialog.x = Math.min(window.width - addWaypointDialog.width - 20, mouse.x + 10);
                                addWaypointDialog.y = Math.min(window.height - addWaypointDialog.height - 20, mouse.y + 10);
                                addWaypointDialog.open();
                            }
                        }
                    }
                }

                TapHandler {
                    onDoubleTapped: {
                        var clickedCoordinate = map.toCoordinate(point.position);
                        map.center = clickedCoordinate;
                        map.zoomLevel += 1;
                    }
                }

                // Waypoint / Cluster markers (restored inline implementation)
                MapItemView {
                    id: markersView
                    model: window.tagFilterActive ? (window.clusteringEnabled ? window.tagClusterModel : window.tagFilteredWaypoints) : (window.clusteringEnabled ? window.clusterModel : (!window.showNonBookmarkWaypoints ? window.bookmarkOnlyWaypoints : window.waypoints))

                    // Keep selection valid when models change (clusters / filters toggled)
                    onModelChanged: {
                        if (!window.selectedWaypoint)
                            return;
                        var visible = false;
                        var activeList = model;
                        for (var i = 0; i < activeList.length; i++) {
                            var it = activeList[i];
                            if (!it)
                                continue;
                            if (it.type === "cluster")
                                continue;
                            if (it.name === window.selectedWaypoint.name && Math.abs(it.lat - window.selectedWaypoint.lat) < 1e-9 && Math.abs(it.lon - window.selectedWaypoint.lon) < 1e-9) {
                                visible = true;
                                break;
                            }
                        }
                        if (!visible && !(window.selectedWaypoint && window.selectedWaypoint.transient)) {
                            window.selectedWaypoint = null;
                            window.selectedWaypointIndex = -1;
                        }
                    }

                    delegate: MapQuickItem {
                        id: marker
                        coordinate: (modelData && modelData.lat !== undefined && modelData.lon !== undefined) ? QtPositioning.coordinate(modelData.lat, modelData.lon) : QtPositioning.coordinate(0, 0)
                        anchorPoint.x: circle.width / 2
                        anchorPoint.y: circle.height / 2

                        property bool isCluster: !!(modelData && (modelData.type === "cluster" || (modelData.type === undefined && modelData.count !== undefined && modelData.count > 1)))
                        property bool isSelected: !!(!isCluster && window.selectedWaypoint && window.selectedWaypoint.name === modelData.name && Math.abs(window.selectedWaypoint.lat - modelData.lat) < 1e-9 && Math.abs(window.selectedWaypoint.lon - modelData.lon) < 1e-9)

                        sourceItem: Rectangle {
                            id: circle
                            width: (isCluster ? theme.clusterMarkerRadius : theme.waypointMarkerRadius) * 2
                            height: width
                            radius: width / 2
                            color: isCluster ? theme.clusterMarkerColor : (isSelected ? theme.waypointSelectedColor : (modelData && modelData.bookmark ? theme.bookmarkColor : theme.waypointDefaultColor))
                            border.color: theme.waypointBorderColor
                            border.width: 1
                            antialiasing: true

                            // Selection halo
                            Rectangle {
                                id: halo
                                anchors.centerIn: parent
                                width: parent.width * 1.9
                                height: width
                                radius: width / 2
                                visible: isSelected && !isCluster
                                color: theme.waypointSelectedHaloColor
                                z: -1
                                SequentialAnimation on opacity {
                                    running: halo.visible
                                    loops: Animation.Infinite
                                    NumberAnimation {
                                        from: 0.85
                                        to: 0.25
                                        duration: 650
                                        easing.type: Easing.InOutQuad
                                    }
                                    NumberAnimation {
                                        from: 0.25
                                        to: 0.85
                                        duration: 650
                                        easing.type: Easing.InOutQuad
                                    }
                                }
                                SequentialAnimation {
                                    running: halo.visible
                                    loops: Animation.Infinite
                                    NumberAnimation {
                                        target: halo
                                        property: "scale"
                                        from: 1.0
                                        to: 1.28
                                        duration: 650
                                        easing.type: Easing.InOutQuad
                                    }
                                    NumberAnimation {
                                        target: halo
                                        property: "scale"
                                        from: 1.28
                                        to: 1.0
                                        duration: 650
                                        easing.type: Easing.InOutQuad
                                    }
                                }
                            }

                            // Cluster count
                            Text {
                                anchors.centerIn: parent
                                visible: !!(isCluster && modelData && modelData.count !== undefined)
                                text: (modelData && modelData.count !== undefined) ? (modelData.count + "") : ""
                                color: theme.waypointBorderColor
                                font.bold: true
                                font.pixelSize: theme && theme.scale ? theme.scale(1) : 12
                            }

                            MouseArea {
                                anchors.fill: parent
                                anchors.margins: -4
                                hoverEnabled: true
                                cursorShape: Qt.PointingHandCursor
                                onClicked: {
                                    if (isCluster) {
                                        if (map) {
                                            map.center = QtPositioning.coordinate(modelData.lat, modelData.lon);
                                            map.zoomLevel += 1;
                                            if (clusteringEnabled && clusterFetchDebounce)
                                                clusterFetchDebounce.restart();
                                        }
                                    } else {
                                        // Resolve canonical waypoint
                                        var found = -1;
                                        for (var i = 0; i < window.waypoints.length; i++) {
                                            var w = window.waypoints[i];
                                            if (w.name === modelData.name && Math.abs(w.lat - modelData.lat) < 1e-9 && Math.abs(w.lon - modelData.lon) < 1e-9) {
                                                found = i;
                                                break;
                                            }
                                        }
                                        if (found >= 0) {
                                            window.selectedWaypoint = window.waypoints[found];
                                            window.selectedWaypointIndex = found;
                                        } else {
                                            window.selectedWaypoint = modelData;
                                            window.selectedWaypointIndex = -1;
                                        }
                                        searchResultLocation = null;

                                        // Smart zoom: only use zoom-out animation if moving far from current position
                                        if (map) {
                                            var targetCoord = QtPositioning.coordinate(modelData.lat, modelData.lon);
                                            var currentCenter = map.center;
                                            var distance = currentCenter.distanceTo(targetCoord);

                                            // If close to current center (< 1km), just center and zoom smoothly
                                            if (distance < 1000 && map.zoomLevel >= knobs.searchZoomLevel - 2) {
                                                map.center = targetCoord;
                                                if (map.zoomLevel < knobs.searchZoomLevel) {
                                                    map.zoomLevel = knobs.searchZoomLevel;
                                                }
                                            } else {
                                                // Far away or zoomed out: use the dramatic zoom-out-zoom-in animation
                                                centerThenZoomAnimation.stop();
                                                centerThenZoomAnimation.targetCenter = targetCoord;
                                                centerThenZoomAnimation.targetZoom = knobs.searchZoomLevel;
                                                centerThenZoomAnimation.start();
                                            }
                                        }
                                    }
                                }
                            }

                            Behavior on color {
                                ColorAnimation {
                                    duration: 150
                                }
                            }
                            Behavior on width {
                                NumberAnimation {
                                    duration: 150
                                    easing.type: Easing.OutQuad
                                }
                            }
                        }
                    }
                }

                // Current location marker (inline)
                MapQuickItem {
                    id: currentLocationMarker
                    visible: currentLocationValid
                    coordinate: QtPositioning.coordinate(currentLocationLat, currentLocationLon)
                    anchorPoint.x: locOuter.width / 2
                    anchorPoint.y: locOuter.height / 2
                    sourceItem: Rectangle {
                        id: locOuter
                        width: 18
                        height: 18
                        radius: 9
                        color: theme.currentLocationColor
                        border.color: theme.waypointBorderColor
                        border.width: 2
                        antialiasing: true
                        Rectangle {
                            anchors.centerIn: parent
                            width: 6
                            height: 6
                            radius: 3
                            color: theme.currentLocationInnerColor
                            antialiasing: true
                        }
                    }
                }

                // Search result marker (inline)
                MapQuickItem {
                    id: searchMarker
                    visible: searchResultLocation !== null
                    coordinate: searchResultLocation || QtPositioning.coordinate(0, 0)
                    anchorPoint.x: searchCircle.width / 2
                    anchorPoint.y: searchCircle.height / 2
                    sourceItem: Rectangle {
                        id: searchCircle
                        width: 16
                        height: 16
                        radius: 8
                        color: theme.searchResultColor
                        border.color: theme.waypointBorderColor
                        border.width: 2
                        antialiasing: true
                        SequentialAnimation on scale {
                            running: searchMarker.visible
                            loops: Animation.Infinite
                            NumberAnimation {
                                from: 1.0
                                to: 1.3
                                duration: 1000
                                easing.type: Easing.InOutQuad
                            }
                            NumberAnimation {
                                from: 1.3
                                to: 1.0
                                duration: 1000
                                easing.type: Easing.InOutQuad
                            }
                        }
                        MouseArea {
                            anchors.fill: parent
                            acceptedButtons: Qt.RightButton
                            onPressed: function (mouse) {
                                if (mouse.button === Qt.RightButton && searchResultLocation) {
                                    var coord = searchResultLocation;
                                    pendingLat = coord.latitude;
                                    pendingLon = coord.longitude;
                                    pendingWaypointName = (lastSearchSuggestion && lastSearchSuggestion.name) ? lastSearchSuggestion.name : "";
                                    if (typeof addWaypointDialog !== 'undefined') {
                                        addWaypointDialog.lat = pendingLat;
                                        addWaypointDialog.lon = pendingLon;
                                        addWaypointDialog.presetName = pendingWaypointName;
                                        var pt = map.fromCoordinate(coord);
                                        var dx = pt.x;
                                        var dy = pt.y;
                                        addWaypointDialog.x = Math.min(window.width - addWaypointDialog.width - 20, dx + 10);
                                        addWaypointDialog.y = Math.min(window.height - addWaypointDialog.height - 20, dy + 10);
                                        addWaypointDialog.open();
                                    }
                                }
                            }
                        }
                    }
                }

                // Top-center search control (toggled with Ctrl+F) — now with live suggestions.
                SearchBox {
                    id: overlaySearchBox
                    visible: searchOverlayVisible
                    width: Math.min(520, map.width * 0.6)
                    anchors.horizontalCenter: parent.horizontalCenter
                    anchors.top: parent.top
                    anchors.topMargin: 12

                    maxSuggestions: 8
                    showSuggestions: true
                    suggestions: []
                    api: api
                    waypoints: window.waypoints
                    // tagVocabulary removed (backend distinct tags now provide suggestions)
                    onSuggestionChosen: function (s) {
                        if (!s)
                            return;
                        // Use sequential zoom-out-center-zoom-in animation
                        centerThenZoomAnimation.stop();
                        centerThenZoomAnimation.targetCenter = QtPositioning.coordinate(s.lat, s.lon);
                        centerThenZoomAnimation.targetZoom = knobs.searchZoomLevel;
                        centerThenZoomAnimation.start();
                        // Always highlight the chosen location (bookmark, waypoint, or geocode)
                        searchResultLocation = QtPositioning.coordinate(s.lat, s.lon);
                        lastSearchSuggestion = s;
                        // Local waypoint/bookmark selection (marker still shown)
                        if (s.source === "bookmark" || s.source === "waypoint") {
                            var found = -1;
                            for (var i = 0; i < window.waypoints.length; i++) {
                                var w = window.waypoints[i];
                                if (w && w.name === s.name && Math.abs(w.lat - s.lat) < 1e-9 && Math.abs(w.lon - s.lon) < 1e-9) {
                                    window.selectedWaypoint = w;
                                    found = i;
                                    break;
                                }
                            }
                            window.selectedWaypointIndex = found;
                        } else {
                            // Geocoded remote result: create a transient (non-persisted) waypoint object
                            // so the info card can display details immediately.
                            window.selectedWaypoint = {
                                name: s.name || "Location",
                                lat: s.lat,
                                lon: s.lon,
                                bookmark: false,
                                tags: [],
                                desc: s.desc ? s.desc : "",
                                time: ""   // no timestamp for geocode result
                                ,
                                transient: true   // flag so UI can distinguish GEOCODE and avoid model-clearing removal
                            };
                            // Force the info card visible for this transient selection
                            infoCardVisible = true;
                            window.selectedWaypointIndex = -1;
                            // (transient flag set in object literal above)
                        }
                        focusZoomTimer.restart();
                        searchOverlayVisible = false;
                    }
                    // When shown ensure the inner input receives focus
                    onVisibleChanged: {
                        if (visible && overlaySearchBox.input) {
                            overlaySearchBox.input.forceActiveFocus();
                            overlaySearchBox.input.selectAll();
                        }
                    }
                    // Hide search overlay when requested
                    onHideRequested: {
                        searchOverlayVisible = false;
                    }
                    // Propagate tag filter changes to map
                    onTagFilterChanged: function (active, matches) {
                        window.tagFilterActive = active;
                        window.tagFilteredWaypoints = matches;
                        if (window.selectedWaypoint && active) {
                            var keep = false;
                            for (var i = 0; i < matches.length; i++) {
                                var mw = matches[i];
                                if (mw.name === window.selectedWaypoint.name && Math.abs(mw.lat - window.selectedWaypoint.lat) < 1e-9 && Math.abs(mw.lon - window.selectedWaypoint.lon) < 1e-9) {
                                    keep = true;
                                    break;
                                }
                            }
                            if (!keep) {
                                window.selectedWaypoint = null;
                                window.selectedWaypointIndex = -1;
                            }
                        }
                        if (window.clusteringEnabled) {
                            clusterFetchDebounce.restart();
                        }

                        // When tag filter becomes active and has matches, zoom to fit all tagged waypoints
                        if (active && matches && matches.length > 0 && map) {
                            var bounds = MapViewLogic.calculateWaypointBounds(matches);
                            if (bounds) {
                                map.center = QtPositioning.coordinate(bounds.centerLat, bounds.centerLon);
                                map.zoomLevel = bounds.zoom;
                            }
                        }
                    }
                }

                WaypointInfoCard {
                    id: overlayInfoCard
                    waypoint: selectedWaypoint
                    api: api
                    width: Math.min(360, map.width * 0.32)
                    anchors.left: parent.left
                    anchors.top: parent.top
                    anchors.margins: 12
                    visible: selectedWaypoint !== null && infoCardVisible

                    // Handle adding a transient (geocoded) waypoint as a real persisted waypoint
                    onAddGeocodeRequested: function (wp) {
                        if (!wp)
                            return;
                        if (typeof addWaypointDialog !== "undefined") {
                            pendingLat = wp.lat;
                            pendingLon = wp.lon;
                            pendingWaypointName = wp.name || "";
                            addWaypointDialog.lat = pendingLat;
                            addWaypointDialog.lon = pendingLon;
                            addWaypointDialog.presetName = pendingWaypointName;
                            // Position similar to Ctrl+Enter logic (near top center)
                            var px = map ? map.width / 2 : (window.width / 2);
                            var py = map ? Math.min(140, map.height * 0.25) : 120;
                            addWaypointDialog.x = Math.min(window.width - addWaypointDialog.width - 20, Math.max(20, px - addWaypointDialog.width / 2));
                            addWaypointDialog.y = Math.min(window.height - addWaypointDialog.height - 20, Math.max(20, py));
                            addWaypointDialog.open();
                        }
                    }

                    // When the name is edited inside the info card:
                    onNameEdited: function (updated) {
                        // Refresh selected waypoint reference (new object identity)
                        window.selectedWaypoint = updated;

                        // Update authoritative waypoints array (by coordinates as stable key)
                        var idx = -1;
                        for (var i = 0; i < window.waypoints.length; i++) {
                            var w = window.waypoints[i];
                            if (Math.abs(w.lat - updated.lat) < 1e-9 && Math.abs(w.lon - updated.lon) < 1e-9) {
                                idx = i;
                                break;
                            }
                        }
                        if (idx >= 0) {
                            var arr = waypoints.slice();
                            arr[idx] = updated;
                            waypoints = arr;
                            selectedWaypointIndex = idx;
                        }

                        // Refresh clusters if enabled
                        if (clusteringEnabled)
                            clusterFetchDebounce.restart();
                    }
                }
                WaypointTable {
                    id: waypointTable
                    map: map
                    waypoints: window.waypoints
                    bookmarkOnlyWaypoints: window.bookmarkOnlyWaypoints
                    clusteringEnabled: window.clusteringEnabled
                    clusterModel: window.clusterModel
                    showNonBookmarkWaypoints: window.showNonBookmarkWaypoints
                    tagFilterActive: window.tagFilterActive
                    tagFilteredWaypoints: window.tagFilteredWaypoints
                    externalSelectedWaypoint: window.selectedWaypoint
                    externalSelectedWaypointIndex: window.selectedWaypointIndex
                    z: 10
                    open: waypointTableVisible
                    onCloseRequested: {
                        waypointTableVisible = false;
                        Qt.callLater(function () {
                            waypointTableVisible = false;
                        });
                    }

                    // (logoOverlay moved outside WaypointTable so it remains visible even when table is hidden)
                    onSelectionRequested: function (wp) {
                        if (!wp)
                            return;
                        if (window.selectedWaypoint === wp)
                            window.selectedWaypoint = null;
                        window.selectedWaypoint = wp;
                        window.selectedWaypointIndex = MapViewLogic.findWaypointIndex(window.waypoints, wp);

                        // Single-click: just center, no zoom
                        map.center = QtPositioning.coordinate(wp.lat, wp.lon);
                    }
                    onWaypointActivated: function (wp) {
                        if (!wp)
                            return;
                        if (window.selectedWaypoint === wp)
                            window.selectedWaypoint = null;
                        window.selectedWaypoint = wp;
                        window.selectedWaypointIndex = MapViewLogic.findWaypointIndex(window.waypoints, wp);

                        // Use sequential zoom-out-center-zoom-in animation
                        centerThenZoomAnimation.stop();
                        centerThenZoomAnimation.targetCenter = QtPositioning.coordinate(wp.lat, wp.lon);
                        centerThenZoomAnimation.targetZoom = knobs.searchZoomLevel;
                        centerThenZoomAnimation.start();
                    }
                }
            } // Map

            // Map Controls (zoom, reset) anchored over the map
            MapControls {
                id: mapControls
                z: 5
                anchors {
                    right: parent.right
                    top: parent.top
                    margins: 10
                }

                map: map
                knobs: knobs
                api: api
                waypoints: window.waypoints
                selectedWaypoint: window.selectedWaypoint
                selectedWaypointIndex: window.selectedWaypointIndex
                showNonBookmarkWaypoints: window.showNonBookmarkWaypoints
                clusteringEnabled: window.clusteringEnabled
                tagFilterActive: window.tagFilterActive
                clusterFetchDebounce: clusterFetchDebounce
                currentLocationValid: window.currentLocationValid
                currentLocationLat: window.currentLocationLat
                currentLocationLon: window.currentLocationLon
                locationSnack: locationSnack

                onShowNonBookmarkWaypointsChanged: function (newValue) {
                    window.showNonBookmarkWaypoints = newValue;
                    if (!newValue && window.selectedWaypoint && !window.selectedWaypoint.bookmark) {
                        window.selectedWaypoint = null;
                        window.selectedWaypointIndex = -1;
                    }
                }
                onRequestDeleteSelected: function (wp) {
                    if (!wp)
                        return;
                    var idx = -1;
                    for (var i = 0; i < window.waypoints.length; i++) {
                        var w = window.waypoints[i];
                        if (w.name === wp.name && Math.abs(w.lat - wp.lat) < 1e-9 && Math.abs(w.lon - wp.lon) < 1e-9) {
                            idx = i;
                            break;
                        }
                    }
                    if (idx !== -1) {
                        api.deleteWaypoint(window.waypoints[idx]);
                    } else {
                        console.warn("Selected waypoint not found; delete aborted");
                    }
                }
                onRequestLocate: function (forceCenter) {
                    fetchCurrentLocation(forceCenter);
                    if (!locationPollTimer.running)
                        locationPollTimer.start();
                }
                onRequestCenterOnSelected: {
                    if (selectedWaypoint && map) {
                        map.center = QtPositioning.coordinate(selectedWaypoint.lat, selectedWaypoint.lon);
                    }
                }
            }
        }
    }

    // Floating bottom-right logo overlay replaced by LogoOverlay component
    LogoOverlay {
        id: logoOverlay
        anchors.right: parent.right
        anchors.bottom: parent.bottom
        anchors.margins: 12
        waypointTableVisible: waypointTableVisible
        helpVisible: aboutOverlay.visible
        onAboutRequested: {
            aboutOverlay.open();
        }
    }

    // (logoOverlay moved outside WaypointTable; help overlay logic unchanged)

    // About overlay component (extracted from inline implementation)
    AboutOverlay {
        id: aboutOverlay
        anchors.fill: parent
        theme: theme
        api: api
        onRequestClose:
        // helpVisible is now bound to aboutOverlay.visible, no need to set manually
        {}
    }

    // Help (shortcuts) overlay component (dedicated file)
    HelpOverlay {
        id: helpOverlay
        anchors.fill: parent
        theme: theme
    }
}
