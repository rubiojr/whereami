import QtQuick
import QtLocation
import QtPositioning

/*
 * SearchResultMarker
 *
 * Pulsing marker for a search/geocode result.
 *
 * Changes in this revision:
 *  - Ensure strictly boolean visibility bindings.
 *  - Guard anchorPoint access when source item not yet created.
 *  - Remove reliance on compound truthy object in visibility expression.
 */

MapQuickItem {
    id: root

    // Whether the marker should be displayed (strict boolean).
    property bool active: !!(targetCoordinate && targetCoordinate.latitude !== undefined)

    // Coordinate for the marker (QGeoCoordinate). Parent sets this when a search result is chosen.
    property var targetCoordinate: null

    // Optional theme object (provides searchResultColor, waypointBorderColor).
    property var theme

    // Pulsing animation toggle.
    property bool pulsing: true

    // Suggested waypoint name (if user wants to add this location as a waypoint).
    property string presetName: ""

    // Size customization.
    property int markerDiameter: 16
    property int borderWidth: 2

    // Fallback colors if theme not provided.
    readonly property color colorFill: (theme && theme.searchResultColor) ? theme.searchResultColor : "#ff5e37"
    readonly property color colorBorder: (theme && theme.waypointBorderColor) ? theme.waypointBorderColor : "#202020"

    // Visibility is pure boolean now.
    visible: active
    z: 4
    anchorPoint.x: markerRect ? markerRect.width / 2 : 0
    anchorPoint.y: markerRect ? markerRect.height / 2 : 0

    // Bind MapQuickItem.coordinate defensively.
    coordinate: (targetCoordinate && targetCoordinate.latitude !== undefined) ? targetCoordinate : QtPositioning.coordinate(0, 0)

    Accessible.role: Accessible.Button
    Accessible.name: qsTr("Search result")
    Accessible.description: qsTr("Right click to add waypoint at this location")

    // Root visual
    sourceItem: Rectangle {
        id: markerRect
        width: root.markerDiameter
        height: root.markerDiameter
        radius: width / 2
        color: root.colorFill
        border.color: root.colorBorder
        border.width: root.borderWidth
        antialiasing: true

        // Pulsing scale animation
        SequentialAnimation on scale {
            running: root.visible && root.pulsing
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
            hoverEnabled: true
            onPressed: function (mouse) {
                if (mouse.button === Qt.RightButton && root.targetCoordinate) {
                    root.addWaypointRequested(root.targetCoordinate.latitude, root.targetCoordinate.longitude, root.presetName);
                }
            }
        }
    }

    // Emitted when user requests adding this location as a waypoint.
    signal addWaypointRequested(real lat, real lon, string presetName)
}
