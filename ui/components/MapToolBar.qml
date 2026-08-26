import QtQuick 2.15
import QtQuick.Controls 2.15
import QtQuick.Layouts 1.15
import QtQuick.Window 2.15
import "../themes"
import QtQuick.Shapes 1.15

ToolBar {
    id: toolbar
    // Rounded corners support; actual frameless flag must be set on the ApplicationWindow (`MapView.qml`).
    property real cornerRadius: 12
    // Host ApplicationWindow (set from MapView: rootWindow: window)
    property var rootWindow: null
    property bool active: true
    property alias timelineNavigationButton: timelineButton
    property alias quitButton: mapQuitButton
    ThemeLoader {
        id: theme
    }
    height: 50

    // Apply themed background (top-only rounded corners) & border
    background: Item {
        id: bg
        anchors.fill: parent
        // Custom path so only top corners are rounded (bottom remains square)
        Shape {
            anchors.fill: parent
            antialiasing: true
            ShapePath {
                id: path
                strokeWidth: 0
                // Use the theme-provided toolbar background; per-theme opacity/tint now handled inside each theme file
                fillColor: theme.toolbarBackground
                startX: 0
                startY: toolbar.height
                // Left edge up to start of top-left curve
                PathLine {
                    x: 0
                    y: toolbar.cornerRadius
                }
                // Top-left corner
                PathQuad {
                    x: toolbar.cornerRadius
                    y: 0
                    controlX: 0
                    controlY: 0
                }
                // Top edge to before top-right curve
                PathLine {
                    x: toolbar.width - toolbar.cornerRadius
                    y: 0
                }
                // Top-right corner
                PathQuad {
                    x: toolbar.width
                    y: toolbar.cornerRadius
                    controlX: toolbar.width
                    controlY: 0
                }
                // Right edge down
                PathLine {
                    x: toolbar.width
                    y: toolbar.height
                }
                // Bottom edge back to origin (square bottom)
                PathLine {
                    x: 0
                    y: toolbar.height
                }
            }
        }
        // Passive double-click handler on empty toolbar background (does not block buttons)
        TapHandler {
            id: backgroundDoubleTap
            acceptedButtons: Qt.LeftButton
            gesturePolicy: TapHandler.WithinBounds
            onDoubleTapped: toolbar.toggleFullScreen()
        }
    }

    // Drag handler enables window move when frameless (set flags on ApplicationWindow)
    DragHandler {
        id: dragHandler
        target: null
        onActiveChanged: {
            if (dragHandler.active) {
                var win = toolbar.rootWindow;
                if (win && win.startSystemMove)
                    win.startSystemMove();
            }
        }
    }

    // Corner resize handlers (top-left, top-right) for window resizing.
    // Small (16x16) transparent hotspots that initiate system resize when dragged.
    // They sit above the background but below interactive buttons due to creation order.
    // Top-left corner resize hotspot (uses MouseArea for proper cursor shape)
    MouseArea {
        id: topLeftResizeHotspot
        anchors.left: parent.left
        anchors.top: parent.top
        width: 18
        height: 18
        z: 3000
        acceptedButtons: Qt.LeftButton
        hoverEnabled: true
        cursorShape: Qt.SizeFDiagCursor
        onPressed: {
            var win = toolbar.rootWindow;
            if (win && win.startSystemResize)
                win.startSystemResize(Qt.TopEdge | Qt.LeftEdge);
        }
        // Let events fall through when not actively resizing
        onPositionChanged: function (ev) {
            if (!topLeftResizeHotspot.pressed)
                ev.accepted = false;
        }
    }

    // Top-right corner resize hotspot
    MouseArea {
        id: topRightResizeHotspot
        anchors.right: parent.right
        anchors.top: parent.top
        width: 18
        height: 18
        z: 3000
        acceptedButtons: Qt.LeftButton
        hoverEnabled: true
        cursorShape: Qt.SizeBDiagCursor
        onPressed: {
            var win = toolbar.rootWindow;
            if (win && win.startSystemResize)
                win.startSystemResize(Qt.TopEdge | Qt.RightEdge);
        }
        onPositionChanged: function (ev) {
            if (!topRightResizeHotspot.pressed)
                ev.accepted = false;
        }
    }

    // Toggle fullscreen on double-click convenience function
    function toggleFullScreen() {
        var win = toolbar.rootWindow;
        if (!win)
            return;
        if (win.visibility === Window.FullScreen) {
            // Return to normal windowed mode
            win.visibility = Window.Windowed;
        } else {
            win.visibility = Window.FullScreen;
        }
    }

    signal fitToWaypoints
    signal openFile
    signal searchLocation(string query)
    signal toggleWaypointsTable
    signal toggleInfoCard
    signal dateRangeSelected(string startDate, string endDate)
    signal dateRangeCleared
    signal timelineRequested
    signal helpRequested
    signal quitRequested

    property bool dateFilterActive: false
    property string dateRangeStart: ""
    property string dateRangeEnd: ""

    function closePopups() {
        dateRangePicker.close();
    }

    onActiveChanged: {
        if (!active)
            closePopups();
    }

    RowLayout {
        anchors.fill: parent
        anchors.leftMargin: toolbar.width < 600 ? 4 : 10
        anchors.rightMargin: toolbar.width < 600 ? 4 : 10
        spacing: 10

        // (App icon removed; logo will be displayed elsewhere)

        // File operations
        ToolButton {
            id: openFileButton
            icon.source: "qrc:/icons/folder.svg"
            icon.width: theme.toolbarButtonSize
            icon.height: theme.toolbarButtonSize
            icon.color: hovered ? theme.toolbarIconHover : theme.toolbarIcon
            // Styled tooltip consistent with MapControlButton
            CustomToolTip {
                tooltipText: "Import GPX directory"
                visible: openFileButton.hovered
                position: "bottom"
            }
            onClicked: toolbar.openFile()
        }

        // Map operations
        ToolButton {
            id: fitButton
            icon.source: "qrc:/icons/target.svg"
            icon.width: theme.toolbarButtonSize
            icon.height: theme.toolbarButtonSize
            icon.color: hovered ? theme.toolbarIconHover : theme.toolbarIcon
            CustomToolTip {
                tooltipText: "Fit to all waypoints"
                visible: fitButton.hovered
                position: "bottom"
            }
            onClicked: toolbar.fitToWaypoints()
        }

        ToolButton {
            id: tableToggleButton
            icon.source: "qrc:/icons/table.svg"
            icon.width: theme.toolbarButtonSize
            icon.height: theme.toolbarButtonSize
            icon.color: hovered ? theme.toolbarIconHover : theme.toolbarIcon
            CustomToolTip {
                tooltipText: "Show / Hide Waypoints Table"
                visible: tableToggleButton.hovered
                position: "bottom"
            }
            onClicked: {
                if (toolbar.toggleWaypointsTable)
                    toolbar.toggleWaypointsTable();
            }
        }

        ToolButton {
            id: dateRangeButton
            icon.source: "qrc:/icons/calendar.svg"
            icon.width: theme.toolbarButtonSize
            icon.height: theme.toolbarButtonSize
            icon.color: toolbar.dateFilterActive || hovered ? theme.toolbarIconHover : theme.toolbarIcon
            Accessible.name: "Open date picker"
            CustomToolTip {
                tooltipText: toolbar.dateFilterActive ? ("Date filter: " + toolbar.dateRangeStart + (toolbar.dateRangeEnd !== toolbar.dateRangeStart ? " to " + toolbar.dateRangeEnd : "") + ". Click to choose another period") : "Find where I was by date"
                visible: dateRangeButton.hovered
                position: "bottom"
            }
            onClicked: {
                dateRangePicker.setRange(toolbar.dateFilterActive ? toolbar.dateRangeStart : "",
                                         toolbar.dateFilterActive ? toolbar.dateRangeEnd : "");
                dateRangePicker.open();
            }
        }

        ToolButton {
            id: clearDateRangeButton
            visible: toolbar.dateFilterActive
            text: "×"
            Accessible.name: "Clear waypoint date filter"
            contentItem: Text {
                text: clearDateRangeButton.text
                color: clearDateRangeButton.hovered ? theme.toolbarIconHover : theme.toolbarIcon
                font.pixelSize: theme.scale(4)
                horizontalAlignment: Text.AlignHCenter
                verticalAlignment: Text.AlignVCenter
            }
            CustomToolTip {
                tooltipText: "Clear date filter"
                visible: clearDateRangeButton.hovered
                position: "bottom"
            }
            onClicked: {
                dateRangePicker.close();
                toolbar.dateRangeCleared();
            }
        }

        ToolButton {
            id: timelineButton
            icon.source: "qrc:/icons/timeline.svg"
            icon.width: theme.toolbarButtonSize
            icon.height: theme.toolbarButtonSize
            icon.color: hovered ? theme.toolbarIconHover : theme.toolbarIcon
            Accessible.name: "Open timeline by year"
            CustomToolTip {
                tooltipText: "Timeline by year"
                visible: timelineButton.hovered
                position: "bottom"
            }
            onClicked: toolbar.timelineRequested()
        }

        ToolButton {
            id: infoCardToggleButton
            icon.source: "qrc:/icons/info.svg"
            icon.width: theme.toolbarButtonSize
            icon.height: theme.toolbarButtonSize
            icon.color: hovered ? theme.toolbarIconHover : theme.toolbarIcon
            CustomToolTip {
                tooltipText: "Show / Hide Waypoint Info Card (Ctrl+I)"
                visible: infoCardToggleButton.hovered
                position: "bottom"
            }
            onClicked: {
                if (toolbar.toggleInfoCard)
                    toolbar.toggleInfoCard();
            }
        }

        ToolSeparator {
            // Avoid binding loop by using a fixed implicitHeight instead of referencing parent.height
            contentItem: Rectangle {
                implicitWidth: 1
                // Static height (30) rather than proportional binding to prevent binding loop warnings
                implicitHeight: 30
                color: theme.toolbarSeparator
            }
        }

        // Adaptive spacer that used to host the search box. Shrinks away on narrow widths
        Item {
            id: searchSpacer
            Layout.preferredWidth: Math.min(250, Math.max(0, toolbar.width - 700))
            Layout.minimumWidth: 0
            Layout.fillHeight: true
            visible: Layout.preferredWidth > 40
        }

        // Adaptive spacer for removed style selector. Gradually collapses to keep Help/Quit visible.
        Item {
            id: styleSpacer
            Layout.preferredWidth: (toolbar.width > 900 ? 160 : (toolbar.width > 780 ? 80 : 0))
            Layout.minimumWidth: 0
            Layout.fillHeight: true
            visible: Layout.preferredWidth > 0
        }

        // Spacer (push quit button to far right; help button sits before quit)
        Item {
            Layout.fillWidth: true
        }

        // Help (shortcuts) button
        ToolButton {
            id: helpButton
            icon.source: "qrc:/icons/help.svg"
            icon.width: theme.toolbarButtonSize
            icon.height: theme.toolbarButtonSize
            icon.color: hovered ? theme.toolbarIconHover : theme.toolbarIcon
            CustomToolTip {
                tooltipText: "Help / Shortcuts (Ctrl+? / Ctrl+/)"
                visible: helpButton.hovered
                position: "bottom"
            }
            onClicked: {
                if (toolbar.helpRequested)
                    toolbar.helpRequested();
            }
        }

        // Quit application
        ToolButton {
            id: mapQuitButton
            icon.source: "qrc:/icons/quit.svg"
            icon.width: theme.toolbarButtonSize
            icon.height: theme.toolbarButtonSize
            icon.color: hovered ? theme.toolbarIconHover : theme.toolbarIcon
            CustomToolTip {
                tooltipText: "Quit"
                visible: mapQuitButton.hovered
                position: "bottom"
            }
            onClicked: {
                if (toolbar.quitRequested)
                    toolbar.quitRequested();
            }
        }
    }

    DateRangePicker {
        id: dateRangePicker
        x: Math.max(4, Math.min(dateRangeButton.x, toolbar.width - width - 4))
        y: toolbar.height + 4
        onRangeApplied: function (startDate, endDate) {
            toolbar.dateRangeSelected(startDate, endDate);
        }
    }
}
