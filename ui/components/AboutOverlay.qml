import QtQuick 2.15
import QtQuick.Controls 2.15
import "../themes"

/*
  AboutOverlay.qml
  ----------------
  Full-screen opaque black overlay centered content (single vertical column).

  API:
    - open(): show overlay (resets to About view)
    - close(): hide overlay (emits requestClose)
    - toggleShortcuts(): switch between About info and shortcuts list
    - Properties for metadata you may override:
        appName, tagLine, version, author, sourceUrl, licenseText, showShortcuts
        theme: pass an existing ThemeLoader instance (optional)
    - Signal: requestClose (emitted after closing animation completes or immediate close)

  Intended usage (MapView.qml):
      AboutOverlay {
          id: aboutOverlay
          anchors.fill: parent
          theme: theme        // reuse existing ThemeLoader if available
          onRequestClose: {
              // reset any external flags (e.g. logoOverlay.helpVisible = false)
          }
      }
      // Show with: aboutOverlay.open()
*/

Item {
    id: root
    anchors.fill: parent
    visible: false
    z: 9999
    focus: visible

    // ----------------------- Public State & Metadata ----------------------------
    property bool showShortcuts: false
    property string appName: "whereami"
    property string tagLine: "Lightweight desktop waypoint & GPX viewer"
    property string version: "loading..."
    property string author: "Sergio Rubio"
    property string sourceUrl: "https://github.com/rubiojr/whereami"
    property string licenseText: "MIT"
    // Optional injected theme (ThemeLoader item). If null, internal loader supplies one.
    property var theme: null
    // Optional API service for fetching version info
    property var api: null

    // Runtime version info from Go
    property var versionInfo: null

    signal requestClose

    // ----------------------- Internal Helpers -----------------------------------
    // Root-scoped helper; all call sites now use root.scale(...) to avoid
    // shadowing by inner Text items that may expose a 'scale' property.
    function scale(step) {
        if (root.theme && typeof root.theme.scale === "function")
            return root.theme.scale(step);
        // Fallback discrete sizes (approximate original modular scale)
        var table = {
            1: 12,
            2: 14,
            3: 18,
            4: 20,
            5: 24,
            6: 30
        };
        return table[step] || table[1];
    }

    function open() {
        if (root.visible)
            return;
        root.showShortcuts = false;
        root.visible = true;
        root.opacity = 0.0;
        appear.start();
        root.forceActiveFocus();
        // Fetch version info when opening
        if (root.api && typeof root.api.getVersion === "function") {
            root.api.getVersion();
        }
    }

    function close() {
        if (!root.visible)
            return;
        disappear.start();
    }

    function toggleShortcuts() {
        root.showShortcuts = !root.showShortcuts;
    }

    Loader {
        id: themeLoader
        source: root.theme ? "" : "qrc:/themes/ThemeLoader.qml"
        visible: false
        onLoaded: {
            if (!root.theme && themeLoader.item)
                root.theme = themeLoader.item;
        }
    }

    // Dim backdrop (keeps input from hitting map)
    Rectangle {
        id: backdrop
        anchors.fill: parent
        color: Qt.rgba(0, 0, 0, 0.55)
        opacity: 1.0
    }

    // Center floating panel (fixed size)
    Rectangle {
        id: panel
        width: 400
        height: 500
        radius: 12
        color: Qt.rgba(0, 0, 0, 0.92)
        border.color: "#444"
        border.width: 1
        anchors.horizontalCenter: parent.horizontalCenter
        anchors.verticalCenter: parent.verticalCenter

        Column {
            id: contentCol
            spacing: 16
            anchors {
                fill: parent
                margins: 18
            }
            width: parent.width - 36

            // App Icon
            Image {
                id: appIcon
                source: "qrc:/icons/io.github.rubiojr.whereami.svg"
                width: 120
                height: 120
                fillMode: Image.PreserveAspectFit
                smooth: true
                anchors.horizontalCenter: parent.horizontalCenter
            }

            // App Name
            Text {
                text: root.appName
                anchors.horizontalCenter: parent.horizontalCenter
                font.bold: true
                font.pixelSize: root.scale(6)
                color: "#FFFFFF"
                wrapMode: Text.NoWrap
                horizontalAlignment: Text.AlignHCenter
            }

            // Tagline
            Text {
                text: root.tagLine
                width: Math.min(contentCol.width * 0.85, 560)
                anchors.horizontalCenter: parent.horizontalCenter
                font.pixelSize: root.scale(3)
                color: "#CCCCCC"
                wrapMode: Text.Wrap
                horizontalAlignment: Text.AlignHCenter
            }

            // Divider
            Rectangle {
                width: Math.min(contentCol.width * 0.85, 560)
                height: 1
                color: "#333333"
                anchors.horizontalCenter: parent.horizontalCenter
            }

            // About section (shown when not in shortcuts view)
            Column {
                id: aboutSection
                visible: !root.showShortcuts
                spacing: 10
                anchors.horizontalCenter: parent.horizontalCenter
                width: Math.min(contentCol.width * 0.85, 560)

                Text {
                    text: "Version: " + root.version
                    color: "#DDDDDD"
                    font.pixelSize: root.scale(2)
                }
                Text {
                    text: "Go: " + (root.versionInfo ? root.versionInfo.go_version : "unknown")
                    color: "#DDDDDD"
                    font.pixelSize: root.scale(2)
                }
                Text {
                    text: "Platform: " + (root.versionInfo ? (root.versionInfo.go_os + "/" + root.versionInfo.go_arch) : "unknown")
                    color: "#DDDDDD"
                    font.pixelSize: root.scale(2)
                }
                Text {
                    text: "Author: " + root.author
                    color: "#DDDDDD"
                    font.pixelSize: root.scale(2)
                }
                // Source clickable
                Item {
                    width: parent.width
                    implicitHeight: sourceLink.implicitHeight
                    Text {
                        id: sourceLink
                        text: "Source: " + root.sourceUrl
                        color: linkMouse.containsMouse ? "#99CCFF" : "#66B2FF"
                        font.pixelSize: root.scale(2)
                        wrapMode: Text.NoWrap
                        elide: Text.ElideRight
                    }
                    MouseArea {
                        id: linkMouse
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: Qt.openUrlExternally(root.sourceUrl)
                    }
                }
                Text {
                    text: "License: " + root.licenseText
                    color: "#DDDDDD"
                    font.pixelSize: root.scale(2)
                }
            }

            // Buttons
            Row {
                spacing: 18
                anchors.horizontalCenter: parent.horizontalCenter

                Button {
                    text: "Close"
                    onClicked: root.close()
                }
            }

            // Bottom spacer
            Rectangle {
                width: 1
                height: 8
                color: "transparent"
            }
        }
    }

    // ----------------------- Animations -----------------------------------------
    ParallelAnimation {
        id: appear
        PropertyAnimation {
            target: root
            property: "opacity"
            from: 0.0
            to: 1.0
            duration: 160
            easing.type: Easing.OutQuad
        }
        PropertyAnimation {
            target: panel
            property: "scale"
            from: 0.96
            to: 1.0
            duration: 180
            easing.type: Easing.OutCubic
        }
    }

    // Handle version fetch results
    Connections {
        target: root.api
        function onVersionFetched(versionInfo) {
            root.versionInfo = versionInfo;
            // Build a comprehensive version string
            var versionStr = "unknown";
            if (versionInfo) {
                if (versionInfo.app_version) {
                    versionStr = versionInfo.app_version;
                } else if (versionInfo.build_info && versionInfo.build_info.commit_short) {
                    versionStr = "dev-" + versionInfo.build_info.commit_short;
                    if (versionInfo.build_info.dirty === "true") {
                        versionStr += "-dirty";
                    }
                } else {
                    versionStr = "development";
                }
            }
            root.version = versionStr;
        }
        function onVersionFetchFailed(error) {
            console.warn("Failed to fetch version info:", error);
            root.version = "unknown";
        }
    }
    ParallelAnimation {
        id: disappear
        onFinished: {
            root.visible = false;
            root.requestClose();
        }
        PropertyAnimation {
            target: root
            property: "opacity"
            to: 0.0
            duration: 120
            easing.type: Easing.InQuad
        }
        PropertyAnimation {
            target: panel
            property: "scale"
            to: 0.96
            duration: 120
            easing.type: Easing.InQuad
        }
    }

    // ----------------------- Input Handling -------------------------------------
    Keys.onEscapePressed: {
        if (root.showShortcuts) {
            root.showShortcuts = false;
        } else {
            root.close();
        }
    }
    // Eat clicks so underlying map doesn't receive them.
    MouseArea {
        anchors.fill: parent
        acceptedButtons: Qt.NoButton
    }
}
