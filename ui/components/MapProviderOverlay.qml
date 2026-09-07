import QtQuick 2.15

Item {
    id: root

    required property bool basemapAvailable
    required property var theme
    property bool mapLoading: false
    property string mapError: ""
    property bool mapFallbackActive: false
    readonly property bool showAttribution: basemapAvailable
    readonly property bool showUnavailableNotice: !basemapAvailable
    // Published so views can keep their own bottom furniture clear of the
    // attribution, which must stay legible at every window size.
    readonly property real attributionHeight: attribution.height

    z: 1000

    Rectangle {
        id: mapStatus

        objectName: "mapProviderStatus"
        visible: root.mapLoading || root.mapError !== ""
        anchors.top: parent.top
        anchors.horizontalCenter: parent.horizontalCenter
        anchors.topMargin: 8
        width: Math.min(implicitWidth, Math.max(0, parent.width - 24))
        height: statusText.implicitHeight + 12
        implicitWidth: statusText.implicitWidth + 20
        radius: 6
        color: root.theme.toolbarBackground
        border.color: root.mapError !== "" ? root.theme.accent : root.theme.toolbarBorder

        Text {
            id: statusText

            objectName: "mapProviderStatusText"
            anchors.fill: parent
            anchors.margins: 6
            text: root.mapError !== ""
                  ? (root.mapFallbackActive ? qsTr("Map fallback active: %1").arg(root.mapError) : qsTr("Map error: %1").arg(root.mapError))
                  : qsTr("Loading map…")
            color: root.theme.toolbarText
            font.pixelSize: root.theme.scale(1)
            horizontalAlignment: Text.AlignHCenter
            verticalAlignment: Text.AlignVCenter
            elide: Text.ElideRight
        }
    }

    Rectangle {
        id: unavailableNotice

        objectName: "mapProviderUnavailableNotice"
        visible: root.showUnavailableNotice
        anchors.centerIn: parent
        width: Math.min(implicitWidth, Math.max(0, parent.width - 32))
        height: unavailableText.implicitHeight + 24
        implicitWidth: unavailableText.implicitWidth + 32
        radius: 8
        color: root.theme.toolbarBackground
        border.color: root.theme.toolbarBorder

        Text {
            id: unavailableText

            anchors.fill: parent
            anchors.margins: 12
            text: qsTr("Basemap support is unavailable. Check renderer logs or install the optional MapLibre Native Qt fallback.")
            color: root.theme.toolbarText
            font.pixelSize: root.theme.scale(1)
            horizontalAlignment: Text.AlignHCenter
            verticalAlignment: Text.AlignVCenter
            wrapMode: Text.Wrap
        }
    }

    Rectangle {
        id: attribution

        objectName: "mapAttribution"
        visible: root.showAttribution
        anchors.left: parent.left
        anchors.bottom: parent.bottom
        anchors.margins: 6
        width: Math.min(implicitWidth, Math.max(0, parent.width - 12))
        height: attributionText.implicitHeight + 8
        implicitWidth: attributionText.implicitWidth + 12
        radius: 4
        color: Qt.rgba(root.theme.toolbarBackground.r,
                       root.theme.toolbarBackground.g,
                       root.theme.toolbarBackground.b, 0.92)
        border.color: root.theme.toolbarBorder

        Text {
            id: attributionText

            objectName: "mapAttributionText"
            anchors.fill: parent
            anchors.margins: 4
            text: qsTr("<a href=\"https://openfreemap.org\">OpenFreeMap</a> <a href=\"https://www.openmaptiles.org/\">&copy; OpenMapTiles</a> Data from <a href=\"https://www.openstreetmap.org/copyright\">OpenStreetMap</a>")
            textFormat: Text.RichText
            color: root.theme.toolbarText
            linkColor: root.theme.accent
            font.pixelSize: root.theme.scale(1)
            wrapMode: Text.Wrap
            onLinkActivated: function(link) {
                Qt.openUrlExternally(link);
            }
        }
    }
}
