import QtQuick

/*
 * LogoOverlay
 * Floating bottom-right logo / about trigger extracted from MapView.
 *
 * Responsibilities:
 *  - Display application logo.
 *  - Adjust visibility / opacity based on `waypointTableVisible` & `helpVisible`.
 *  - Emit `aboutRequested()` when clicked so parent can open About overlay.
 *
 * Public API:
 *  - property bool waypointTableVisible
 *  - property bool helpVisible
 *  - property url  logoSource (override icon if desired)
 *  - signal aboutRequested()
 *
 * Parent Integration Example:
 *  LogoOverlay {
 *      id: logoOverlay
 *      waypointTableVisible: waypointTableVisible
 *      helpVisible: helpOverlay.visible   // or track explicitly
 *      onAboutRequested: aboutOverlay.open()
 *  }
 *
 * Notes:
 *  - Opacity logic mirrors the original inline implementation:
 *      help open -> 1.0
 *      table visible -> (logo hidden) opacity 0.0
 *      idle -> 0.9 (hover raises to 1.0)
 *  - `helpVisible` is intentionally independent from `visible` property so a parent
 *    can keep the logo hidden while help is open or explicitly show it.
 */

Item {
    id: root
    width: implicitWidth
    height: implicitHeight
    implicitWidth: 60
    implicitHeight: 60

    // When the waypoint table is visible we fade (and hide) the logo unless help overlay is showing.
    property bool waypointTableVisible: false
    // When true (e.g. help/about overlay displayed) we force full opacity & visibility.
    property bool helpVisible: false
    // Allow overriding the icon if theming / branding changes.
    property url logoSource: "qrc:/icons/io.github.rubiojr.whereami.svg"

    // Emitted when the user clicks the logo requesting About overlay.
    signal aboutRequested

    // Derived presentation.
    opacity: helpVisible ? 1.0 : (waypointTableVisible ? 0.0 : 0.9)
    // Remain visible while help overlay is up, or when table hidden.
    visible: helpVisible || !waypointTableVisible
    z: 5
    scale: clickArea.containsMouse ? 1.1 : 1.0

    // Smooth transitions for any opacity change.
    Behavior on opacity {
        NumberAnimation {
            duration: 180
            easing.type: Easing.InOutQuad
        }
    }

    // Smooth scale animation on hover
    Behavior on scale {
        NumberAnimation {
            duration: 150
            easing.type: Easing.OutQuad
        }
    }

    Image {
        id: logoImage
        anchors.fill: parent
        source: root.logoSource
        fillMode: Image.PreserveAspectFit
        smooth: true
        antialiasing: true
    }

    // Click target / hover behavior
    MouseArea {
        id: clickArea
        anchors.fill: parent
        hoverEnabled: true
        acceptedButtons: Qt.LeftButton

        onEntered: {
            if (!root.helpVisible && !root.waypointTableVisible)
                root.opacity = 1.0;
        }
        onExited: {
            if (!root.helpVisible)
                root.opacity = (root.waypointTableVisible ? 0.0 : 0.9);
        }
        onClicked: {
            // Mark help visible locally (parent may also bind helpVisible externally).
            root.helpVisible = true;
            root.opacity = 1.0;
            root.aboutRequested();
        }
    }

    // Optional accessible name (if using accessibility tooling)
    Accessible.role: Accessible.Button
    Accessible.name: qsTr("About")
}
