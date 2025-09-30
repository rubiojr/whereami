import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtPositioning

/*
 * MapControls
 * Floating column of map control buttons extracted from MapView.
 *
 * Responsibilities:
 *  - User actions: zoom in/out, reset orientation, toggle bookmark filter,
 *    go to current location, center on selected waypoint, delete selected bookmark.
 *  - Emits explicit signals instead of directly mutating parent state (where practical),
 *    to keep coupling low.
 *
 * Usage:
 *  MapControls {
 *      map: map
 *      knobs: knobs
 *      waypoints: waypoints
 *      selectedWaypoint: selectedWaypoint
 *      selectedWaypointIndex: selectedWaypointIndex
 *      showNonBookmarkWaypoints: showNonBookmarkWaypoints
 *      clusteringEnabled: clusteringEnabled
 *      tagFilterActive: tagFilterActive
 *      clusterFetchDebounce: clusterFetchDebounce
 *      currentLocationValid: currentLocationValid
 *      currentLocationLat: currentLocationLat
 *      currentLocationLon: currentLocationLon
 *      locationSnack: locationSnack
 *
 *      onShowNonBookmarkWaypointsChanged: showNonBookmarkWaypoints = newValue
 *      onRequestDeleteSelected: api.deleteWaypoint(waypoint)
 *      onRequestLocate: fetchCurrentLocation(forceCenter)
 *  }
 */

Column {
    id: root
    spacing: 5

    // --- Inputs (bind from MapView) ---
    property var map
    property var knobs
    property var api
    property var waypoints: []
    property var selectedWaypoint
    property int selectedWaypointIndex: -1
    property bool showNonBookmarkWaypoints: true
    property bool clusteringEnabled: true
    property bool tagFilterActive: false
    property var clusterFetchDebounce
    property bool currentLocationValid: false
    property double currentLocationLat: 0
    property double currentLocationLon: 0
    property var locationSnack

    // --- Signals (parent hooks into these) ---

    signal requestDeleteSelected(var waypoint)
    signal requestLocate(bool forceCenter)
    signal requestCenterOnSelected
    // Parent can connect requestCenterOnSelected() if it wants special handling;
    // we still center directly for responsiveness.

    // Utility: safely center map on selected waypoint
    function centerOnSelectedIfAny() {
        if (map && selectedWaypoint) {
            map.center = QtPositioning.coordinate(selectedWaypoint.lat, selectedWaypoint.lon);
        }
    }

    // Utility: restart clustering debounce if appropriate
    function refreshClustersIfNeeded() {
        if (clusteringEnabled && clusterFetchDebounce && !tagFilterActive) {
            clusterFetchDebounce.restart();
        }
    }

    MapControlButton {
        text: "+"
        tooltipText: "Zoom In"
        onClicked: {
            root.centerOnSelectedIfAny();
            if (root.map)
                root.map.zoomLevel++;
        }
    }

    MapControlButton {
        text: "-"
        tooltipText: "Zoom Out"
        onClicked: {
            root.centerOnSelectedIfAny();
            if (root.map)
                root.map.zoomLevel--;
        }
    }

    MapControlButton {
        text: root.showNonBookmarkWaypoints ? "All" : "★"
        tooltipText: root.showNonBookmarkWaypoints ? "Show Only Bookmarks" : "Show All Waypoints"
        onClicked: {
            root.showNonBookmarkWaypoints = !root.showNonBookmarkWaypoints;
            // property assignment triggers showNonBookmarkWaypointsChanged (auto signal from property)
            // If selection becomes hidden (non-bookmark while switching to bookmark-only),
            // parent is expected to clear selection—keeping logic centralized.
            root.refreshClustersIfNeeded();
            if (root.locationSnack && root.locationSnack.show) {
                var msg = root.showNonBookmarkWaypoints ? "Showing all waypoints" : "Showing only bookmarks";
                root.locationSnack.autoHide = true;
                root.locationSnack.durationMs = 3000;
                root.locationSnack.show(msg);
            }
        }
    }

    MapControlButton {
        text: "N"
        tooltipText: "Reset Bearing"
        onClicked: {
            if (root.map)
                root.map.bearing = 0;
        }
    }

    MapControlButton {
        text: "◎"
        tooltipText: "Go to Current Location"
        onClicked: {
            var hadFix = root.currentLocationValid;
            // Request a location update (force center when new fix arrives)
            root.requestLocate(true);
            // If we already have a fix, center immediately for responsiveness
            if (hadFix && root.map) {
                root.map.center = QtPositioning.coordinate(root.currentLocationLat, root.currentLocationLon);
                if (root.knobs && root.map.zoomLevel < root.knobs.searchZoomLevel)
                    root.map.zoomLevel = root.knobs.searchZoomLevel;
            }
            if (!hadFix && root.locationSnack && root.locationSnack.show) {
                root.locationSnack.show("Locating...");
            }
        }
    }

    MapControlButton {
        id: deleteButton
        visible: !!(root.selectedWaypoint && root.selectedWaypoint.bookmark)
        text: "✖"
        tooltipText: "Delete Selected Bookmark"
        onClicked: {
            if (root.selectedWaypoint) {
                root.requestDeleteSelected(root.selectedWaypoint);
            }
        }
    }
}
