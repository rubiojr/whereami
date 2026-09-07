import QtQuick 2.15

Canvas {
    id: root

    required property var map
    property var path: []
    property real lineWidth: 1
    property color lineColor: "black"
    readonly property int mapRevision: map && map.cameraRevision !== undefined ? Number(map.cameraRevision) : 0

    antialiasing: true

    onPathChanged: requestPaint()
    onLineWidthChanged: requestPaint()
    onLineColorChanged: requestPaint()
    onWidthChanged: requestPaint()
    onHeightChanged: requestPaint()
    onMapChanged: requestPaint()
    onMapRevisionChanged: requestPaint()

    onPaint: {
        var context = getContext("2d");
        context.reset();
        context.clearRect(0, 0, width, height);
        if (!map || !path || path.length < 2)
            return;
        context.beginPath();
        var first = map.fromCoordinate(path[0], false);
        context.moveTo(first.x, first.y);
        for (var index = 1; index < path.length; index++) {
            var point = map.fromCoordinate(path[index], false);
            context.lineTo(point.x, point.y);
        }
        context.lineWidth = lineWidth;
        context.strokeStyle = lineColor;
        context.stroke();
    }
}
