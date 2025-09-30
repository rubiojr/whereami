import QtQuick 2.15
import "../themes"

import QtQuick.Layouts 1.15
import QtQuick.Shapes 1.15

Rectangle {
    id: statusBar
    height: 30

    ThemeLoader {
        id: theme
    }

    // Replaced rectangular background with custom Shape for bottom-only rounding
    border.width: 0
    color: "transparent"
    // Custom background path (square top, rounded bottom corners)
    Item {
        id: statusBarBg
        anchors.fill: parent
        z: -1
        Shape {
            anchors.fill: parent
            antialiasing: true
            ShapePath {
                strokeWidth: 0
                fillColor: theme.background
                startX: 0
                startY: 0
                // Top edge
                PathLine {
                    x: statusBar.width
                    y: 0
                }
                // Right side down to start of bottom-right curve
                PathLine {
                    x: statusBar.width
                    y: statusBar.height - statusBar.cornerRadius
                }
                // Bottom-right corner
                PathQuad {
                    x: statusBar.width - statusBar.cornerRadius
                    y: statusBar.height
                    controlX: statusBar.width
                    controlY: statusBar.height
                }
                // Bottom edge to before bottom-left curve
                PathLine {
                    x: statusBar.cornerRadius
                    y: statusBar.height
                }
                // Bottom-left corner
                PathQuad {
                    x: 0
                    y: statusBar.height - statusBar.cornerRadius
                    controlX: 0
                    controlY: statusBar.height
                }
                // Left side back to top
                PathLine {
                    x: 0
                    y: 0
                }
            }
        }
    }

    property real cornerRadius: 12
    property string mouseCoordinates: ""
    property real zoomLevel: 10
    property int waypointCount: 0
    // Root ApplicationWindow reference (optional, mirrors MapToolBar)
    property var rootWindow: null

    // Corner resize hotspots (bottom-left & bottom-right) for frameless window resizing.
    // Transparent areas that trigger platform-native resize via startSystemResize.
    MouseArea {
        id: bottomLeftResizeHotspot
        anchors.left: parent.left
        anchors.bottom: parent.bottom
        width: 18
        height: 18
        opacity: 0
        z: 3000
        acceptedButtons: Qt.LeftButton
        hoverEnabled: true
        cursorShape: Qt.SizeBDiagCursor
        onPressed: {
            var win = statusBar.rootWindow;
            if (win && win.startSystemResize)
                win.startSystemResize(Qt.BottomEdge | Qt.LeftEdge);
        }
        onPositionChanged: function (ev) {
            if (!bottomLeftResizeHotspot.pressed)
                ev.accepted = false;
        }
    }

    MouseArea {
        id: bottomRightResizeHotspot
        anchors.right: parent.right
        anchors.bottom: parent.bottom
        width: 18
        height: 18
        opacity: 0
        z: 3000
        acceptedButtons: Qt.LeftButton
        hoverEnabled: true
        cursorShape: Qt.SizeFDiagCursor
        onPressed: {
            var win2 = statusBar.rootWindow;
            if (win2 && win2.startSystemResize)
                win2.startSystemResize(Qt.BottomEdge | Qt.RightEdge);
        }
        onPositionChanged: function (ev) {
            if (!bottomRightResizeHotspot.pressed)
                ev.accepted = false;
        }
    }

    RowLayout {
        anchors.fill: parent
        anchors.leftMargin: 10
        anchors.rightMargin: 10
        spacing: 20

        // Mouse coordinates
        Rectangle {
            Layout.preferredWidth: 200
            Layout.fillHeight: true
            color: theme.mapStatusBar.sectionBackground

            RowLayout {
                anchors.verticalCenter: parent.verticalCenter
                spacing: 5

                Text {
                    text: "📍"
                    font.pixelSize: theme.statusBarTextSize
                    color: theme.accent
                }

                Text {
                    text: statusBar.mouseCoordinates !== "" ? statusBar.mouseCoordinates : "Move mouse over map"
                    font.pixelSize: theme.statusBarTextSize
                    font.family: "monospace"
                    color: statusBar.mouseCoordinates !== "" ? theme.primaryText : theme.secondaryText
                }
            }
        }

        Rectangle {
            implicitWidth: 1
            Layout.fillHeight: true
            Layout.topMargin: 5
            Layout.bottomMargin: 5
            color: theme.secondaryText
        }

        // Zoom level
        Rectangle {
            Layout.preferredWidth: 120
            Layout.fillHeight: true
            color: theme.mapStatusBar.sectionBackground

            RowLayout {
                anchors.verticalCenter: parent.verticalCenter
                spacing: 5

                Text {
                    text: "🔍"
                    font.pixelSize: theme.statusBarTextSize
                    color: theme.accent
                }

                Text {
                    text: "Zoom: " + statusBar.zoomLevel.toFixed(1)
                    font.pixelSize: theme.statusBarTextSize
                    color: theme.primaryText
                }
            }
        }

        Rectangle {
            implicitWidth: 1
            Layout.fillHeight: true
            Layout.topMargin: 5
            Layout.bottomMargin: 5
            color: theme.secondaryText
        }

        // Waypoint count
        Rectangle {
            Layout.preferredWidth: 150
            Layout.fillHeight: true
            color: theme.mapStatusBar.sectionBackground

            RowLayout {
                anchors.verticalCenter: parent.verticalCenter
                spacing: 5

                Text {
                    text: "📌"
                    font.pixelSize: theme.statusBarTextSize
                    color: theme.accent
                }

                Text {
                    text: statusBar.waypointCount + " waypoints loaded"
                    font.pixelSize: theme.statusBarTextSize
                    color: theme.primaryText
                }
            }
        }

        // Spacer
        Item {
            Layout.fillWidth: true
        }

        // App version/status
        Text {
            text: "Ready"
            font.pixelSize: theme.statusBarTextSize
            Layout.alignment: Qt.AlignVCenter
            color: theme.secondaryText
        }
    }
}
