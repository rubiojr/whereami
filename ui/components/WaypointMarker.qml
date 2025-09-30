import QtQuick
import QtQuick.Controls
import QtPositioning

/*
 * WaypointMarker
 *
 * Visual + interaction component for a single waypoint or cluster marker.
 * Extracted from the inline delegate logic previously inside MapView's MapItemView.
 *
 * Responsibilities:
 *  - Render a circular marker (cluster or waypoint) with proper colors & sizing.
 *  - Show a pulsing "halo" animation when selected (non-cluster only).
 *  - Emit click semantics distinguishing cluster expansion vs waypoint selection.
 *
 * Public API:
 *  property var waypoint            // Object containing at least: { lat, lon, type?("cluster"), name, bookmark, count? }
 *  property bool selected           // True when this marker represents the currently selected waypoint
 *  property var theme               // Theme object providing colors & sizes (clusterMarkerColor, waypointSelectedColor, etc.)
 *
 *  signal waypointClicked(var waypoint)
 *  signal clusterActivated(var clusterObject)
 *
 * Usage (inside a MapQuickItem `sourceItem` or directly inside a delegate):
 *  WaypointMarker {
 *      waypoint: modelData
 *      selected: (modelData.type !== "cluster" && false)   // selection condition example
 *      theme: theme
 *      onWaypointClicked: // update selection
 *      onClusterActivated: // center map & zoom
 *  }
 *
 * NOTE: This component is purely visual + interaction. The parent (delegate / controller) is
 * responsible for:
 *  - Re-associating a marker's waypoint object with the canonical object in the master waypoint array.
 *  - Centering / zooming the map after click events (except cluster auto-zoom logic you may add).
 */

Rectangle {
    id: root
    property var waypoint
    property bool selected: false
    property var theme

    // Derived flags
    readonly property bool isCluster: waypoint && waypoint.type === "cluster"
    readonly property bool isBookmark: waypoint && waypoint.bookmark === true

    // Colors (fall back to hard-coded defaults if theme not provided)
    readonly property color colorCluster: theme && theme.clusterMarkerColor ? theme.clusterMarkerColor : "#5460d0"
    readonly property color colorSelected: theme && theme.waypointSelectedColor ? theme.waypointSelectedColor : "#ffcc33"
    readonly property color colorBookmark: theme && theme.bookmarkColor ? theme.bookmarkColor : "#ffd24d"
    readonly property color colorDefault: theme && theme.waypointDefaultColor ? theme.waypointDefaultColor : "#3388ff"
    readonly property color colorBorder: theme && theme.waypointBorderColor ? theme.waypointBorderColor : "#202020"
    readonly property color colorHalo: theme && theme.waypointSelectedHaloColor ? theme.waypointSelectedHaloColor : "#40ffc107"

    // Sizes
    readonly property int waypointRadius: theme && theme.waypointMarkerRadius ? theme.waypointMarkerRadius : 8
    readonly property int clusterRadius: theme && theme.clusterMarkerRadius ? theme.clusterMarkerRadius : 11

    // Computed visual size
    width: (isCluster ? clusterRadius : waypointRadius) * 2
    height: width
    radius: width / 2

    color: isCluster ? colorCluster : (selected ? colorSelected : (isBookmark ? colorBookmark : colorDefault))

    border.width: 1
    border.color: colorBorder

    Accessible.role: Accessible.Button
    Accessible.name: (isCluster ? qsTr("Cluster (%1 points)").arg(waypoint && waypoint.count ? waypoint.count : 0) : (waypoint && waypoint.name ? qsTr("Waypoint %1").arg(waypoint.name) : qsTr("Waypoint")))
    Accessible.description: isCluster ? qsTr("Zoom in to expand cluster") : qsTr("Select waypoint")

    // Selection halo (animated) – only for non-cluster + selected
    Rectangle {
        id: halo
        anchors.centerIn: parent
        width: parent.width * 1.9
        height: width
        radius: width / 2
        visible: root.selected && !root.isCluster
        color: root.colorHalo
        border.width: 0
        z: -1
        scale: 1.0

        // Opacity breathing
        SequentialAnimation on opacity {
            running: halo.visible
            loops: Animation.Infinite
            NumberAnimation {
                from: 0.9
                to: 0.25
                duration: 650
                easing.type: Easing.InOutQuad
            }
            NumberAnimation {
                from: 0.25
                to: 0.9
                duration: 650
                easing.type: Easing.InOutQuad
            }
        }
        // Scale pulse
        SequentialAnimation {
            running: halo.visible
            loops: Animation.Infinite
            ParallelAnimation {
                NumberAnimation {
                    target: halo
                    property: "scale"
                    from: 1.0
                    to: 1.28
                    duration: 650
                    easing.type: Easing.InOutQuad
                }
            }
            ParallelAnimation {
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
    }

    // Cluster count text
    Text {
        anchors.centerIn: parent
        visible: root.isCluster
        text: (root.isCluster && root.waypoint && root.waypoint.count !== undefined) ? (root.waypoint.count + "") : ""
        color: root.colorBorder
        font.bold: true
        font.pixelSize: (root.theme && root.theme.scale) ? root.theme.scale(1) : 12
    }

    // Interaction surface (slightly larger hit area)
    MouseArea {
        anchors.fill: parent
        anchors.margins: -4
        hoverEnabled: true
        cursorShape: Qt.PointingHandCursor
        onClicked: {
            if (!root.waypoint)
                return;
            if (root.isCluster) {
                root.clusterActivated(root.waypoint);
            } else {
                root.waypointClicked(root.waypoint);
            }
        }
    }

    // Animated transitions on size & color changes
    Behavior on width {
        NumberAnimation {
            duration: 150
            easing.type: Easing.OutQuad
        }
    }
    Behavior on color {
        ColorAnimation {
            duration: 150
        }
    }

    signal waypointClicked(var waypoint)
    signal clusterActivated(var clusterObject)
}
