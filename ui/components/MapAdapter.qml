pragma ComponentBehavior: Bound

import QtQuick 2.15
import QtLocation 6.5
import QtPositioning 6.5

Item {
    id: root

    property var vectorItem: null
    property var vectorCamera: null
    property bool vectorRequested: false
    property var legacyPlugin: null
    property geoCoordinate center: QtPositioning.coordinate(0, 0)
    property real zoomLevel: 0
    property real bearing: 0
    property real minimumZoomLevel: 0
    property real maximumZoomLevel: 20
    property bool copyrightsVisible: false
    property var activeMapType: null
    property bool vectorFailed: false
    property string backendFailure: ""
    // Loading status is only useful before the first renderable vector cover.
    // Retained content remains usable while later covers refresh.
    property bool vectorContentReady: false
    readonly property var supportedMapTypes: legacyLoader.item ? legacyLoader.item.supportedMapTypes : []
    readonly property bool vectorActive: vectorRequested && !vectorFailed && vectorItem !== null && vectorCamera !== null
    readonly property bool basemapAvailable: vectorActive || !!(legacyPlugin && legacyPlugin.mapLibreAvailable)
    readonly property bool ready: vectorActive ? vectorContentReady : legacyLoader.status === Loader.Ready
    readonly property bool loading: vectorActive ? !vectorContentReady && (Number(vectorCamera.tilesLoading) > 0 || Number(vectorCamera.tilesRequested) === 0) : legacyLoader.status === Loader.Loading
    readonly property string errorString: backendFailure !== "" ? backendFailure : (vectorActive ? String(vectorCamera.tileError || "") : (legacyLoader.status === Loader.Error ? qsTr("Legacy map backend failed to load") : ""))
    readonly property double cameraRevision: vectorActive ? backendRevision : legacyRevision

    property double backendRevision: vectorActive ? Number(vectorCamera.cameraRevision) : 0
    property int legacyRevision: 0
    property bool syncingBackend: false

    clip: true

    function serialValue(name) {
        var value = Number(vectorCamera[name]);
        return isFinite(value) ? value + 1 : 1;
    }

    function applyCenterToVector() {
        if (!vectorActive || syncingBackend || !center)
            return;
        vectorCamera.centerTargetLatitude = center.latitude;
        vectorCamera.centerTargetLongitude = center.longitude;
        vectorCamera.centerSerial = serialValue("centerSerial");
        syncNormalizedVectorState();
    }

    function syncToVector() {
        if (!vectorActive)
            return;
        vectorCamera.minimumZoomLevel = minimumZoomLevel;
        vectorCamera.maximumZoomLevel = maximumZoomLevel;
        vectorCamera.bearing = bearing;
        vectorCamera.centerTargetLatitude = center.latitude;
        vectorCamera.centerTargetLongitude = center.longitude;
        vectorCamera.centerSerial = serialValue("centerSerial");
        vectorCamera.zoomLevel = zoomLevel;
        syncFromVector();
    }

    function syncNormalizedVectorState() {
        if (!vectorActive)
            return;
        var epsilon = 0.000001;
        if (Math.abs(Number(vectorCamera.centerLatitude) - center.latitude) > epsilon
                || Math.abs(Number(vectorCamera.centerLongitude) - center.longitude) > epsilon
                || Math.abs(Number(vectorCamera.zoomLevel) - zoomLevel) > epsilon
                || Math.abs(Number(vectorCamera.bearing) - bearing) > epsilon
                || Math.abs(Number(vectorCamera.minimumZoomLevel) - minimumZoomLevel) > epsilon
                || Math.abs(Number(vectorCamera.maximumZoomLevel) - maximumZoomLevel) > epsilon)
            syncFromVector();
    }

    function syncFromVector() {
        if (!vectorActive)
            return;
        syncingBackend = true;
        center = QtPositioning.coordinate(vectorCamera.centerLatitude, vectorCamera.centerLongitude);
        zoomLevel = vectorCamera.zoomLevel;
        bearing = vectorCamera.bearing;
        minimumZoomLevel = vectorCamera.minimumZoomLevel;
        maximumZoomLevel = vectorCamera.maximumZoomLevel;
        syncingBackend = false;
    }

    function pan(dx, dy) {
        if (vectorActive) {
            vectorCamera.panDX = dx;
            vectorCamera.panDY = dy;
            vectorCamera.panSerial = serialValue("panSerial");
            syncFromVector();
            return;
        }
        if (legacyLoader.item) {
            legacyLoader.item.pan(dx, dy);
            syncingBackend = true;
            center = legacyLoader.item.center;
            syncingBackend = false;
            legacyRevision++;
        }
    }

    function fromCoordinate(coordinate, clipToViewport) {
        if (vectorActive) {
            vectorCamera.projectLatitude = coordinate.latitude;
            vectorCamera.projectLongitude = coordinate.longitude;
            vectorCamera.projectSerial = serialValue("projectSerial");
            return Qt.point(vectorCamera.projectedX, vectorCamera.projectedY);
        }
        return legacyLoader.item ? legacyLoader.item.fromCoordinate(coordinate, clipToViewport) : Qt.point(width / 2, height / 2);
    }

    function toCoordinate(point, clipToViewport) {
        if (vectorActive) {
            vectorCamera.unprojectX = point.x;
            vectorCamera.unprojectY = point.y;
            vectorCamera.unprojectSerial = serialValue("unprojectSerial");
            return QtPositioning.coordinate(vectorCamera.unprojectedLatitude, vectorCamera.unprojectedLongitude);
        }
        return legacyLoader.item ? legacyLoader.item.toCoordinate(point, clipToViewport) : center;
    }

    function alignCoordinateToPoint(coordinate, point) {
        if (vectorActive) {
            vectorCamera.alignLatitude = coordinate.latitude;
            vectorCamera.alignLongitude = coordinate.longitude;
            vectorCamera.alignX = point.x;
            vectorCamera.alignY = point.y;
            vectorCamera.alignSerial = serialValue("alignSerial");
            syncFromVector();
            return;
        }
        if (legacyLoader.item) {
            legacyLoader.item.alignCoordinateToPoint(coordinate, point);
            syncingBackend = true;
            center = legacyLoader.item.center;
            syncingBackend = false;
            legacyRevision++;
        }
    }

    onCenterChanged: {
        if (!syncingBackend) {
            legacyRevision++;
            applyCenterToVector();
        }
    }
    onZoomLevelChanged: {
        if (!syncingBackend) {
            legacyRevision++;
            if (vectorActive) {
                vectorCamera.zoomLevel = zoomLevel;
                syncNormalizedVectorState();
            }
        }
    }
    onBearingChanged: {
        if (!syncingBackend) {
            legacyRevision++;
            if (vectorActive) {
                vectorCamera.bearing = bearing;
                syncNormalizedVectorState();
            }
        }
    }
    onMinimumZoomLevelChanged: {
        if (vectorActive && !syncingBackend) {
            vectorCamera.minimumZoomLevel = minimumZoomLevel;
            syncNormalizedVectorState();
        }
    }
    onMaximumZoomLevelChanged: {
        if (vectorActive && !syncingBackend) {
            vectorCamera.maximumZoomLevel = maximumZoomLevel;
            syncNormalizedVectorState();
        }
    }
    onBackendRevisionChanged: Qt.callLater(root.syncNormalizedVectorState)
    onVectorActiveChanged: {
        if (!vectorActive)
            vectorContentReady = false;
        Qt.callLater(syncToVector);
    }
    onWidthChanged: legacyRevision++
    onHeightChanged: legacyRevision++
    Component.onCompleted: Qt.callLater(syncToVector)

    Timer {
        interval: 250
        repeat: true
        running: root.vectorActive
        triggeredOnStart: true
        onTriggered: {
            root.vectorCamera.statusSerial = root.serialValue("statusSerial");
            var requested = Number(root.vectorCamera.tilesRequested);
            var loaded = Number(root.vectorCamera.tilesLoaded);
            var loading = Number(root.vectorCamera.tilesLoading);
            var failures = Number(root.vectorCamera.tileErrors);
            if (loaded > 0)
                root.vectorContentReady = true;
            if (requested > 0 && loaded === 0 && loading === 0 && failures >= requested) {
                root.backendFailure = String(root.vectorCamera.tileError || qsTr("Go vector map failed to load"));
                root.vectorFailed = true;
            }
        }
    }

    Rectangle {
        anchors.fill: parent
        color: "#f8f4f0"
        visible: root.vectorActive
        z: -101
    }

    Item {
        id: vectorHost
        anchors.fill: parent
        visible: root.vectorActive
        z: -100
    }

    Binding {
        target: root.vectorItem
        property: "parent"
        value: vectorHost
        when: root.vectorActive
        restoreMode: Binding.RestoreBindingOrValue
    }
    Binding {
        target: root.vectorItem
        property: "width"
        value: vectorHost.width
        when: root.vectorActive
        restoreMode: Binding.RestoreBindingOrValue
    }
    Binding {
        target: root.vectorItem
        property: "height"
        value: vectorHost.height
        when: root.vectorActive
        restoreMode: Binding.RestoreBindingOrValue
    }
    Binding {
        target: root.vectorCamera
        property: "viewportWidth"
        value: root.width
        when: root.vectorActive
        restoreMode: Binding.RestoreBindingOrValue
    }
    Binding {
        target: root.vectorCamera
        property: "viewportHeight"
        value: root.height
        when: root.vectorActive
        restoreMode: Binding.RestoreBindingOrValue
    }

    Loader {
        id: legacyLoader
        anchors.fill: parent
        active: !root.vectorActive
        z: -100
        sourceComponent: Component {
            Map {
                id: legacyMap
                plugin: root.legacyPlugin
                center: root.center
                zoomLevel: root.zoomLevel
                bearing: root.bearing
                copyrightsVisible: root.copyrightsVisible

                Component.onCompleted: {
                    if (supportedMapTypes.length > 0) {
                        activeMapType = supportedMapTypes[supportedMapTypes.length - 1];
                        root.activeMapType = activeMapType;
                    }
                }
            }
        }
    }
}
