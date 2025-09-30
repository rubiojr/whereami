import QtQuick 2.15
import QtPositioning 5.15

/*
  ShortcutsController.qml
  Centralized keyboard shortcuts controller that manages all application shortcuts.

  This service extracts shortcut handling from MapView.qml to create a cleaner separation
  of concerns and make shortcuts easier to manage and test.

  Design notes:
    - All shortcuts are defined declaratively as children of this service
    - The service needs references to key application state (map, selectedWaypoint, etc)
    - Emits signals for actions that require UI updates
    - Follows the service pattern established by API.qml

  Usage:
    ShortcutsController {
        id: shortcutsController
        map: mapView.map
        selectedWaypoint: root.selectedWaypoint
        // ... other property bindings
    }
*/

QtObject {
    id: root

    // Required references for shortcuts to work
    property var map: null
    property var knobs: null
    property var api: null
    property var addWaypointDialog: null
    property var logoOverlay: null
    property var aboutOverlay: null
    property var locationSnack: null

    // State that shortcuts need to access/modify
    property var selectedWaypoint: null
    property int selectedWaypointIndex: -1
    property var waypoints: []
    property bool searchOverlayVisible: false
    property bool waypointTableVisible: false
    property bool infoCardVisible: false
    property bool showNonBookmarkWaypoints: true
    property bool clusteringEnabled: false
    property bool tagFilterActive: false
    property var searchResultLocation: null
    property var lastSearchSuggestion: null
    property real pendingLat: 0
    property real pendingLon: 0
    property string pendingWaypointName: ""

    // Signals emitted by shortcuts
    signal zoomIn
    signal zoomOut
    signal resetView
    signal toggleSearch
    signal toggleBookmarkMode
    signal toggleWaypointTable
    signal toggleInfoCard
    signal clearSelection
    signal deleteSelectedBookmark
    signal openAddWaypointDialog(real lat, real lon, string presetName)
    signal clusterFetchRequested
    signal escapePressed
    signal toggleHelp
    signal zoomToSelectedWaypoint
    signal goToCurrentLocation

    // Helper function to request cluster fetch
    function requestClusterFetch() {
        clusterFetchRequested();
    }

    // Shortcuts container
    property list<Shortcut> shortcuts: [
        // Zoom in with Ctrl++ or Ctrl+=
        Shortcut {
            sequence: "Ctrl++"
            onActivated: {
                if (root.selectedWaypoint && root.map) {
                    root.map.center = QtPositioning.coordinate(root.selectedWaypoint.lat, root.selectedWaypoint.lon);
                }
                if (root.map) {
                    root.map.zoomLevel += 1;
                }
                root.zoomIn();
            }
        },
        Shortcut {
            sequence: "Ctrl+="
            onActivated: {
                if (root.selectedWaypoint && root.map) {
                    root.map.center = QtPositioning.coordinate(root.selectedWaypoint.lat, root.selectedWaypoint.lon);
                }
                if (root.map) {
                    root.map.zoomLevel += 1;
                }
                root.zoomIn();
            }
        },

        // Zoom out with Ctrl+-
        Shortcut {
            sequence: "Ctrl+-"
            onActivated: {
                if (root.selectedWaypoint && root.map) {
                    root.map.center = QtPositioning.coordinate(root.selectedWaypoint.lat, root.selectedWaypoint.lon);
                }
                if (root.map) {
                    root.map.zoomLevel -= 1;
                }
                root.zoomOut();
            }
        },

        // Reset view to initial position + zoom (Ctrl+0)
        Shortcut {
            sequence: "Ctrl+0"
            onActivated: {
                if (root.map && root.knobs) {
                    root.map.center = root.knobs.initialPosition;
                    root.map.zoomLevel = 2; // initial zoom level
                }
                root.resetView();
            }
        },

        // Quit application
        Shortcut {
            sequence: "Ctrl+Q"
            onActivated: Qt.quit()
        },

        // Toggle the search overlay with Ctrl+F
        Shortcut {
            sequence: "Ctrl+F"
            onActivated: {
                root.toggleSearch();
            }
        },

        // Toggle bookmark-only mode with Ctrl+B
        Shortcut {
            sequence: "Ctrl+B"
            onActivated: {
                root.toggleBookmarkMode();
            }
        },

        // Toggle waypoint table with Ctrl+T
        Shortcut {
            sequence: "Ctrl+T"
            onActivated: {
                root.toggleWaypointTable();
            }
        },

        // Toggle waypoint info card with Ctrl+I
        Shortcut {
            sequence: "Ctrl+I"
            onActivated: {
                root.toggleInfoCard();
            }
        },

        // Escape: close Add Waypoint dialog if open; otherwise clear selection, hide search, etc.
        Shortcut {
            sequence: "Escape"
            context: Qt.ApplicationShortcut
            onActivated: {
                root.escapePressed();
            }
        },

        // Toggle help overlay with Ctrl+? or Ctrl+/
        Shortcut {
            sequence: "Ctrl+?"
            onActivated: {
                root.toggleHelp();
            }
        },
        Shortcut {
            sequence: "Ctrl+/"
            onActivated: {
                root.toggleHelp();
            }
        },

        // Zoom to selected waypoint at level 18 with Ctrl+G
        Shortcut {
            sequence: "Ctrl+G"
            onActivated: {
                root.zoomToSelectedWaypoint();
            }
        },

        // Delete selected bookmark waypoint with Delete key
        Shortcut {
            sequence: "Delete"
            onActivated: {
                root.deleteSelectedBookmark();
            }
        },

        // Go to current location with Ctrl+L
        Shortcut {
            sequence: "Ctrl+L"
            onActivated: {
                root.goToCurrentLocation();
            }
        },

        // Ctrl+Enter / Ctrl+Return: open AddWaypointDialog at current search result or selected waypoint
        Shortcut {
            sequence: "Ctrl+Enter"
            context: Qt.ApplicationShortcut
            onActivated: handleAddWaypointShortcut()
        },
        Shortcut {
            sequence: "Ctrl+Return"
            context: Qt.ApplicationShortcut
            onActivated: handleAddWaypointShortcut()
        }
    ]

    // Helper function for add waypoint shortcuts
    function handleAddWaypointShortcut() {
        var targetCoord = null;
        var preset = "";
        if (root.selectedWaypoint) {
            targetCoord = {
                lat: root.selectedWaypoint.lat,
                lon: root.selectedWaypoint.lon
            };
            preset = root.selectedWaypoint.name || "";
        } else if (root.searchResultLocation) {
            targetCoord = {
                lat: root.searchResultLocation.latitude,
                lon: root.searchResultLocation.longitude
            };
            if (root.lastSearchSuggestion && root.lastSearchSuggestion.name) {
                preset = root.lastSearchSuggestion.name;
            }
        }
        if (!targetCoord) {
            return;
        }

        root.pendingLat = targetCoord.lat;
        root.pendingLon = targetCoord.lon;
        root.pendingWaypointName = preset;
        root.openAddWaypointDialog(root.pendingLat, root.pendingLon, root.pendingWaypointName);

        if (root.addWaypointDialog) {
            root.addWaypointDialog.lat = root.pendingLat;
            root.addWaypointDialog.lon = root.pendingLon;
            root.addWaypointDialog.presetName = root.pendingWaypointName;

            var window = root.addWaypointDialog.parent;
            while (window && !window.width) {
                window = window.parent;
            }

            if (window) {
                var px = root.map ? root.map.width / 2 : (window.width / 2);
                var py = root.map ? Math.min(140, root.map.height * 0.25) : 120;
                root.addWaypointDialog.x = Math.min(window.width - root.addWaypointDialog.width - 20, Math.max(20, px - root.addWaypointDialog.width / 2));
                root.addWaypointDialog.y = Math.min(window.height - root.addWaypointDialog.height - 20, Math.max(20, py));
                root.addWaypointDialog.open();
            }
        }
    }
}
