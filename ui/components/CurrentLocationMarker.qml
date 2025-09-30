import QtQuick
import QtLocation
import QtPositioning

/*
 * CurrentLocationMarker
 *
 * Encapsulates the current (device) location marker that was previously inline in `MapView`.
 *
 * Features:
 *  - Shows a two-ring marker (outer ring + inner dot).
 *  - Optional accuracy circle (disabled by default; set showAccuracy = true).
 *  - Visibility driven by `valid` (mirrors previous `currentLocationValid`).
 *
 * Public API:
 *  property bool valid                  // When false the marker is hidden.
 *  property double lat                  // Latitude of current fix.
 *  property double lon                  // Longitude of current fix.
 *  property real accuracy               // Horizontal accuracy in meters (for accuracy circle).
 *  property bool showAccuracy           // Toggle rendering of accuracy radius circle (performance cost at high zoom).
 *  property var theme                   // Theme object (colors). Falls back to sane defaults if absent.
 *
 *  color roles expected (if provided by theme):
 *    - currentLocationColor
 *    - currentLocationInnerColor
 *    - waypointBorderColor
 *
 * Usage (inside a Map):
 *  CurrentLocationMarker {
 *      valid: currentLocationValid
 *      lat: currentLocationLat
 *      lon: currentLocationLon
 *      accuracy: currentLocationAccuracy
 *      showAccuracy: false
 *      theme: theme
 *  }
 */

MapQuickItem {
    id: root

    // ---- Public properties ----
    property bool valid: false
    property double lat: 0
    property double lon: 0
    property real accuracy: 0
    property bool showAccuracy: false
    property var theme

    // Derived
    visible: valid
    coordinate: QtPositioning.coordinate(lat, lon)

    // Accuracy circle size is handled using a child MapCircle if showAccuracy enabled.
    // NOTE: MapCircle would require a plugin-backed map & can be expensive; we keep it off by default.
    // (If you want to enable later, you could swap to a MapCircle outside this component.)

    // The marker visuals
    anchorPoint.x: outer.width / 2
    anchorPoint.y: outer.height / 2

    sourceItem: Item {
        id: container
        width: 24
        height: 24

        // Optional accuracy ring (approximation): drawn as a faint circle sized by accuracy (clamped).
        // Because converting meters to pixels depends on latitude & zoom level, we keep a simple visual
        // (scaled relative to marker size) unless you implement a dynamic projection-based size externally.
        Rectangle {
            id: accuracyRing
            visible: root.showAccuracy && root.accuracy > 0
            anchors.centerIn: parent
            width: Math.min(220, Math.max(40, root.accuracy * 0.6)) // heuristic scaling
            height: width
            radius: width / 2
            color: "transparent"
            border.color: (theme && theme.waypointBorderColor) ? theme.waypointBorderColor : "#1a1a1a"
            border.width: 1
            opacity: 0.20
            antialiasing: true
        }

        // Outer circle (primary location indicator)
        Rectangle {
            id: outer
            anchors.centerIn: parent
            width: 18
            height: 18
            radius: 9
            color: (theme && theme.currentLocationColor) ? theme.currentLocationColor : "#3da9f5"
            border.color: (theme && theme.waypointBorderColor) ? theme.waypointBorderColor : "#202020"
            border.width: 2
            antialiasing: true

            // Inner dot
            Rectangle {
                anchors.centerIn: parent
                width: 6
                height: 6
                radius: 3
                color: (theme && theme.currentLocationInnerColor) ? theme.currentLocationInnerColor : "#ffffff"
                antialiasing: true
            }
        }

        // Subtle pulse animation (optional, disabled by default to reduce distraction)
        property bool pulse: false
        SequentialAnimation on scale {
            running: container.pulse && root.visible
            loops: Animation.Infinite
            NumberAnimation {
                from: 1.0
                to: 1.18
                duration: 900
                easing.type: Easing.InOutQuad
            }
            NumberAnimation {
                from: 1.18
                to: 1.0
                duration: 900
                easing.type: Easing.InOutQuad
            }
        }
    }
}
