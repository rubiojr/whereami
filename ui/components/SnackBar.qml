import QtQuick 2.15
import QtQuick.Controls 2.15
import QtQuick.Layouts 1.15
import "../themes"

/*
  SnackBar.qml
  ------------
  Reusable transient snackbar with a customizable action button (e.g. Undo, Retry, Close).

  Features:
    - Call `show(message)` to display and auto-hide after `durationMs`.
    - Emits `undoRequested()` when the action button is clicked.
    - Emits `dismissed()` when it auto-hides (timeout) or is hidden programmatically.
    - Supports custom action text, colors and manual dismissal.
    - Can optionally freeze auto-hide while hovered (`pauseOnHover`).

  Example usage in a parent:
    SnackBar {
        id: undoBar
        anchors.horizontalCenter: parent.horizontalCenter
        anchors.bottom: parent.bottom
        anchors.bottomMargin: 20
        onUndoRequested: {
            // Re-add waypoint or perform restore
        }
    }

    // After deleting something:
    undoBar.show("Deleted \"Home Marker\"")

  Notes:
    - The parent (e.g., MapView) manages what "undo" means; this component just signals.
    - `z` should be set by parent if overlap issues occur (e.g., z: 1000).
*/

Rectangle {
    id: root
    ThemeLoader {
        id: theme
    }

    // Public API
    property string message: ""
    property string actionText: "Action"
    property int durationMs: 5000
    property bool autoHide: true
    property bool pauseOnHover: true

    // Themed colors (bindings to theme; callers normally shouldn't override)
    property color backgroundColor: theme.snackBar.background
    property color textColor: theme.snackBar.text
    property color actionBackground: theme.snackBar.actionBackground
    property color actionTextColor: theme.snackBar.actionText

    // Layout metrics
    property int horizontalPadding: 16
    property int verticalPadding: 10
    property int spacing: 16
    radius: 6

    // Slide animation configuration (use separate property so Behavior is valid)
    property int slideDistance: 40
    property real slideOffset: root.slideDistance
    transform: Translate {
        y: root.slideOffset
    }

    // Exposed state (read-only outside by convention)
    property bool showing: root.visible

    signal undoRequested
    signal dismissed

    // Public method to display the snackbar (with slide-in)
    function show(msg) {
        root.message = (msg === undefined || msg === null) ? "" : msg;
        root.slideOffset = root.slideDistance;       // start below
        root.visible = true;
        root.opacity = 1;
        slideInKick.start();               // animate into place
        root._restartTimer();
    }

    // Show with a custom action label (set empty label to hide action button)
    function showWithAction(msg, label) {
        root.actionText = (label === undefined || label === null) ? "" : label;
        root.show(msg);
    }

    // Public method to hide early (slide out)
    function hide() {
        if (root.visible) {
            root.slideOffset = root.slideDistance;
            slideOutFinalize.start();
        }
    }

    function _restartTimer() {
        if (!root.autoHide)
            return;
        hideTimer.stop();
        hideTimer.interval = root.durationMs;
        hideTimer.start();
    }

    width: Math.min(420, parent ? parent.width - 40 : 420)
    // Height = text line height + vertical padding top/bottom
    implicitHeight: msgText.implicitHeight + root.verticalPadding * 2
    color: root.backgroundColor
    visible: false
    opacity: 0.0

    Behavior on opacity {
        NumberAnimation {
            duration: 180
            easing.type: Easing.InOutQuad
        }
    }
    Behavior on slideOffset {
        NumberAnimation {
            duration: 180
            easing.type: Easing.OutQuad
        }
    }

    onVisibleChanged: if (!root.visible)
        hideTimer.stop()

    Timer {
        id: hideTimer
        repeat: false
        onTriggered: {
            if (!root.pauseOnHover || !hoverArea.containsMouse) {
                root.hide();
            } else {
                root._restartTimer();
            }
        }
    }

    Timer {
        id: slideInKick
        interval: 0
        repeat: false
        onTriggered: root.slideOffset = 0
    }

    Timer {
        id: slideOutFinalize
        interval: 180
        repeat: false
        onTriggered: {
            if (root.visible && root.slideOffset === root.slideDistance) {
                root.visible = false;
                root.opacity = 0;
                root.dismissed();
            }
        }
    }

    Item {
        id: contentRow
        anchors {
            left: parent.left
            right: parent.right
            verticalCenter: parent.verticalCenter
            leftMargin: root.horizontalPadding
            rightMargin: root.horizontalPadding
        }
        // Content row height tracks text height (button matches it)
        height: msgText.implicitHeight

        Text {
            id: msgText
            anchors {
                left: parent.left
                right: actionButton.left
                verticalCenter: parent.verticalCenter
                rightMargin: root.spacing
            }
            text: root.message
            color: root.textColor
            font.pixelSize: theme && theme.scale ? theme.scale(2) : 14
            verticalAlignment: Text.AlignVCenter
            elide: Text.ElideRight
            wrapMode: Text.NoWrap
        }

        Rectangle {
            id: actionButton
            visible: root.actionText.length > 0
            // Match text height + internal padding; ensure min size for tap
            implicitHeight: Math.max(28, msgText.implicitHeight)
            implicitWidth: actionLabel.implicitWidth + 20
            anchors.right: parent.right
            anchors.verticalCenter: parent.verticalCenter
            radius: 4
            color: root.actionBackground
            border.color: Qt.darker(root.actionBackground, 1.2)
            border.width: 1

            Text {
                id: actionLabel
                anchors.centerIn: parent
                text: root.actionText
                color: root.actionTextColor
                font.bold: true
                font.pixelSize: theme && theme.scale ? theme.scale(1) : 13
            }

            MouseArea {
                anchors.fill: parent
                cursorShape: Qt.PointingHandCursor
                onClicked: {
                    root.undoRequested();
                    root.hide();
                }
            }
        }
    }

    MouseArea {
        id: hoverArea
        anchors.fill: parent
        hoverEnabled: true
        acceptedButtons: Qt.NoButton
        onEntered: if (root.pauseOnHover && hideTimer.running)
            hideTimer.stop()
        onExited: if (root.pauseOnHover && root.autoHide && root.visible)
            root._restartTimer()
    }
}
