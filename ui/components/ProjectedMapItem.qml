import QtQuick 2.15
import QtPositioning 6.5

Item {
    id: root

    required property var map
    property geoCoordinate coordinate: QtPositioning.coordinate(0, 0)
    property point anchorPoint: Qt.point(0, 0)
    property Component sourceItem
    property point projectedPoint: Qt.point(0, 0)
    readonly property int mapRevision: map && map.cameraRevision !== undefined ? Number(map.cameraRevision) : 0

    function updateProjectedPoint() {
        projectedPoint = map ? map.fromCoordinate(coordinate, false) : Qt.point(0, 0);
    }

    onCoordinateChanged: updateProjectedPoint()
    onMapChanged: updateProjectedPoint()
    onMapRevisionChanged: updateProjectedPoint()
    Component.onCompleted: updateProjectedPoint()

    x: projectedPoint.x - anchorPoint.x
    y: projectedPoint.y - anchorPoint.y
    width: contentLoader.width
    height: contentLoader.height

    Loader {
        id: contentLoader
        sourceComponent: root.sourceItem
    }

}
