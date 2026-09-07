// qmllint disable unqualified
import QtQuick
import QtQuick.Window

Window {
    id: window
    visible: true
    width: 900
    height: 600
    title: "vecmap example"
    color: "#141b1e"

    function serial(name) {
        return Number(mapCamera[name]) + 1
    }

    Item {
        id: mapHost
        anchors.fill: parent
    }

    Binding { target: mapItem; property: "parent"; value: mapHost }
    Binding { target: mapItem; property: "width"; value: mapHost.width }
    Binding { target: mapItem; property: "height"; value: mapHost.height }

    DragHandler {
        id: drag
        target: null
        property point previousTranslation: Qt.point(0, 0)

        onActiveChanged: previousTranslation = Qt.point(0, 0)
        onTranslationChanged: {
            mapCamera.panDX = previousTranslation.x - translation.x
            mapCamera.panDY = previousTranslation.y - translation.y
            mapCamera.panSerial = window.serial("panSerial")
            previousTranslation = translation
        }
    }

    WheelHandler {
        target: null
        onWheel: function(event) {
            mapCamera.zoomAnchorX = event.x
            mapCamera.zoomAnchorY = event.y
            mapCamera.zoomCaptureSerial = window.serial("zoomCaptureSerial")
            mapCamera.zoomTarget = Number(mapCamera.zoomLevel) + event.angleDelta.y / 1200
            mapCamera.zoomSerial = window.serial("zoomSerial")
            event.accepted = true
        }
    }

    Rectangle {
        anchors {
            left: parent.left
            bottom: parent.bottom
            margins: 12
        }
        color: "#cc141b1e"
        radius: 4
        width: label.width + 16
        height: label.height + 10

        Text {
            id: label
            anchors.centerIn: parent
            color: "white"
            text: "zoom " + Number(mapCamera.zoomLevel).toFixed(1)
        }
    }

    Text {
        anchors {
            right: parent.right
            bottom: parent.bottom
            margins: 12
        }
        color: "white"
        text: "© OpenMapTiles © OpenStreetMap contributors"
    }
}
