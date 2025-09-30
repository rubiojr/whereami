import QtQuick 2.15
import QtQuick.Controls 2.15
import "../themes"

/*
  HelpOverlay.qml
  ----------------
  Floating (centered) modal-style panel listing keyboard shortcuts (separate from AboutOverlay).

  Features:
    - Dimmed backdrop (blocks interaction with map beneath).
    - Centered panel (default 420x540) with scroll (Flickable + ScrollBar).
    - Public API: open(), close(), toggle(), addShortcut(line).
    - Accepts externally injected theme (ThemeLoader item); falls back to its own ThemeLoader.
    - Esc key, backdrop click, or Close button dismisses the overlay.
    - Smooth fade + scale animations.
    - Safe if created once and reused (model only initialized on first open).

  Typical usage in MapView.qml:
      HelpOverlay {
          id: helpOverlay
          anchors.fill: parent
          theme: theme      // optional: pass existing ThemeLoader
      }
      // Show: helpOverlay.open()
      // Toggle: helpOverlay.toggle()

  NOTE:
    The global Escape shortcut in MapView should close this first; however this
    component is self‑sufficient (handles Esc internally) so it also works standalone.
*/

Item {
    id: root
    anchors.fill: parent
    visible: false
    z: 9998
    focus: visible

    // Optional externally injected theme (ThemeLoader item)
    property var theme: null

    // Default shortcut lines (append only – do not modify order in code paths relying on index)
    readonly property var defaultShortcuts: ["+ / Ctrl++ : Zoom In", "- / Ctrl+- : Zoom Out", "Ctrl+0 : Reset View", "Ctrl+F : Toggle Search Overlay", "Ctrl+T : Toggle Waypoints Table", "Ctrl+I : Toggle Waypoint Info Card", "Ctrl+? / Ctrl+/ : Toggle Help Overlay", "Ctrl+B : Toggle Bookmark-Only Mode", "Ctrl+Enter / Ctrl+Return : Add Waypoint", "Delete : Delete Selected Bookmark", "Ctrl+L : Go to Current Location", "Escape : Close Add Waypoint Dialog / Clear Selection / Hide Search / Close Table / Close Help", "Ctrl+Q : Quit Application", "Double Click Map : Center & Zoom In"]

    // Internal shortcut items (mirrors + dynamic user additions)
    ListModel {
        id: shortcutModel
    }

    signal closed

    // ---------------------------------------------------------------------------
    // API
    // ---------------------------------------------------------------------------
    function open() {
        if (visible)
            return;
        ensureModel();
        root.opacity = 0.0;
        visible = true;
        openAnim.start();
        root.forceActiveFocus();
    }

    function close() {
        if (!visible)
            return;
        closeAnim.start();
    }

    function toggle() {
        if (visible)
            close();
        else
            open();
    }

    function addShortcut(line) {
        if (!line || typeof line !== "string")
            return;
        shortcutModel.append({
            text: line
        });
    }

    // Adaptive scaling helper (delegates to active theme if available)
    function scale(step) {
        if (root.theme && typeof root.theme.scale === "function")
            return root.theme.scale(step);
        var map = {
            1: 12,
            2: 14,
            3: 18,
            4: 20,
            5: 24,
            6: 30
        };
        return map[step] || map[1];
    }

    function ensureModel() {
        if (shortcutModel.count === 0) {
            for (var i = 0; i < defaultShortcuts.length; ++i)
                shortcutModel.append({
                    text: defaultShortcuts[i]
                });
        }
    }

    // Theme fallback loader (only loads if not injected)
    Loader {
        id: themeLoader
        source: root.theme ? "" : "qrc:/themes/ThemeLoader.qml"
        visible: false
        onLoaded: {
            if (!root.theme && themeLoader.item)
                root.theme = themeLoader.item;
        }
    }

    // ---------------------------------------------------------------------------
    // Backdrop
    // ---------------------------------------------------------------------------
    Rectangle {
        id: backdrop
        anchors.fill: parent
        color: Qt.rgba(0, 0, 0, 0.55)
        opacity: 1.0
        MouseArea {
            anchors.fill: parent
            onClicked: root.close()
        }
    }

    // ---------------------------------------------------------------------------
    // Panel
    // ---------------------------------------------------------------------------
    Rectangle {
        id: panel
        width: 420
        height: 540
        radius: 14
        color: Qt.rgba(0, 0, 0, 0.92)
        border.color: "#444"
        border.width: 1
        anchors.horizontalCenter: parent.horizontalCenter
        anchors.verticalCenter: parent.verticalCenter
        clip: true

        // Header
        Column {
            id: headerCol
            spacing: 12
            anchors {
                left: parent.left
                right: parent.right
                top: parent.top
                leftMargin: 20
                rightMargin: 20
                topMargin: 20
            }

            Text {
                text: "Keyboard Shortcuts"
                font.bold: true
                font.pixelSize: root.scale(4)
                color: "#FFFFFF"
                wrapMode: Text.NoWrap
            }

            Rectangle {
                width: parent.width
                height: 1
                color: "#333333"
            }
        }

        // Shortcut list (scrollable)
        Flickable {
            id: listFlick
            anchors {
                left: parent.left
                right: parent.right
                top: headerCol.bottom
                bottom: buttonRow.top
                leftMargin: 20
                rightMargin: 20
                topMargin: 12
                bottomMargin: 12
            }
            clip: true
            contentWidth: width
            contentHeight: listColumn.height
            flickableDirection: Flickable.VerticalFlick

            Column {
                id: listColumn
                width: parent.width
                spacing: 6

                Repeater {
                    model: shortcutModel
                    delegate: Text {
                        text: model.text
                        color: "#DDDDDD"
                        font.pixelSize: root.scale(2)
                        wrapMode: Text.Wrap
                        width: listColumn.width
                    }
                }

                Rectangle {
                    width: 1
                    height: 4
                    color: "transparent"
                }
            }

            ScrollBar.vertical: ScrollBar {
                policy: ScrollBar.AsNeeded
            }
        }

        // Footer buttons
        Row {
            id: buttonRow
            spacing: 14
            anchors.horizontalCenter: parent.horizontalCenter
            anchors.bottom: parent.bottom
            anchors.bottomMargin: 16

            Button {
                text: "Close"
                onClicked: root.close()
                focus: true
            }
        }
    }

    // ---------------------------------------------------------------------------
    // Animations
    // ---------------------------------------------------------------------------
    ParallelAnimation {
        id: openAnim
        PropertyAnimation {
            target: root
            property: "opacity"
            from: 0
            to: 1
            duration: 160
            easing.type: Easing.OutQuad
        }
        PropertyAnimation {
            target: panel
            property: "scale"
            from: 0.95
            to: 1.0
            duration: 190
            easing.type: Easing.OutCubic
        }
    }

    ParallelAnimation {
        id: closeAnim
        onFinished: {
            root.visible = false;
            root.closed();
        }
        PropertyAnimation {
            target: root
            property: "opacity"
            to: 0
            duration: 120
            easing.type: Easing.InQuad
        }
        PropertyAnimation {
            target: panel
            property: "scale"
            to: 0.95
            duration: 120
            easing.type: Easing.InQuad
        }
    }

    // Swallow unhandled mouse input (panel + backdrop already handle clicks)
    MouseArea {
        anchors.fill: parent
        acceptedButtons: Qt.NoButton
    }
}
