pragma ComponentBehavior: Bound
import QtQuick 2.15
import QtQuick.Controls 2.15
import QtQuick.Layouts 1.15
import QtLocation 6.5
import QtPositioning 6.5
import "../themes"

Item {
    id: journey

    property var result: null
    property int year: new Date().getUTCFullYear()
    property bool active: true
    property bool controlsVisible: true
    property bool cameraUserAdjusted: false
    property int currentIndex: -1
    property alias scrubber: journeySlider
    readonly property var stops: result && result.timeline ? result.timeline : []
    readonly property int stopCount: stops.length
    readonly property var currentStop: currentIndex >= 0 && currentIndex < stopCount ? stops[currentIndex] : null
    readonly property bool atOldest: currentIndex <= 0
    readonly property bool atLatest: currentIndex < 0 || currentIndex >= stopCount - 1
    readonly property bool mapReady: mapLoader.status === Loader.Ready
    readonly property var mapItem: mapLoader.item
    readonly property bool panelVisible: detailPanel.visible
    readonly property real focusTargetY: controlsVisible && detailPanel.visible ? Math.max(72, detailPanel.y / 2) : height / 2

    signal focusRequested(real latitude, real longitude, bool immediate)
    signal alignmentRequested(real latitude, real longitude)

    clip: true
    focus: active && visible && controlsVisible

    ThemeLoader {
        id: theme
    }

    onResultChanged: {
        currentIndex = -1;
        resetToLatest();
        Qt.callLater(function() { journey.focusCurrent(true); });
    }
    onCurrentIndexChanged: {
        cameraUserAdjusted = false;
        Qt.callLater(focusCurrent);
    }
    onActiveChanged: {
        if (active)
            Qt.callLater(function() { journey.focusCurrent(true); });
    }
    onControlsVisibleChanged: Qt.callLater(journey.alignCurrent)
    onFocusTargetYChanged: {
        if (active)
            alignmentTimer.restart();
    }

    Timer {
        id: alignmentTimer
        interval: 60
        repeat: false
        onTriggered: journey.alignCurrent()
    }

    Shortcut {
        sequence: "Left"
        context: Qt.WindowShortcut
        enabled: journey.active && journey.visible && journey.controlsVisible && !journeySlider.activeFocus
        onActivated: journey.previous()
    }

    Shortcut {
        sequence: "Right"
        context: Qt.WindowShortcut
        enabled: journey.active && journey.visible && journey.controlsVisible && !journeySlider.activeFocus
        onActivated: journey.next()
    }

    function resetToLatest() {
        currentIndex = result && result.timeline ? result.timeline.length - 1 : -1;
    }

    function selectStop(index) {
        if (stopCount === 0) {
            currentIndex = -1;
            return;
        }
        currentIndex = Math.max(0, Math.min(stopCount - 1, Math.round(index)));
    }

    function previous() {
        if (!atOldest)
            selectStop(currentIndex - 1);
    }

    function next() {
        if (!atLatest)
            selectStop(currentIndex + 1);
    }

    function focusCurrent(immediate) {
        if (immediate === true)
            cameraUserAdjusted = false;
        if (!mapLoader.item || !currentStop)
            return;
        focusRequested(currentStop.latitude, currentStop.longitude, immediate === true);
    }

    function alignCurrent() {
        if (!mapLoader.item || !currentStop || cameraUserAdjusted)
            return;
        alignmentRequested(currentStop.latitude, currentStop.longitude);
    }

    function mapFocusPoint() {
        return Qt.point(width / 2, focusTargetY);
    }

    function placeName(stop) {
        if (!stop)
            return "No recorded location";
        return stop.locality || stop.local_admin || stop.county || stop.region || stop.country || "Unresolved location";
    }

    function hierarchy(stop) {
        if (!stop)
            return "";
        var parts = [];
        if (stop.local_admin && stop.local_admin !== stop.locality)
            parts.push(stop.local_admin);
        if (stop.county && stop.county !== stop.local_admin)
            parts.push(stop.county);
        if (stop.region)
            parts.push(stop.region);
        if (stop.country)
            parts.push(stop.country);
        return parts.join(" / ");
    }

    function dateLabel(stop) {
        if (!stop || !stop.date_utc || stop.date_utc.length < 10)
            return "Date unavailable";
        var month = Number(stop.date_utc.substring(5, 7)) - 1;
        if (!isFinite(month) || month < 0 || month > 11)
            return "Date unavailable";
        var day = stop.date_utc.substring(8, 10);
        return day + " " + Qt.locale().monthName(month, Locale.LongFormat) + " " + stop.date_utc.substring(0, 4);
    }

    function timeRange(stop) {
        if (!stop)
            return "Time unavailable";
        var first = stop.first_observation_utc ? stop.first_observation_utc.substring(11, 16) : "";
        var last = stop.last_observation_utc ? stop.last_observation_utc.substring(11, 16) : "";
        if (first === "")
            return "Time unavailable";
        return first === last ? first + " UTC" : first + " - " + last + " UTC";
    }

    function coordinateLabel(stop) {
        if (!stop || !isFinite(stop.latitude) || !isFinite(stop.longitude))
            return "";
        return Number(stop.latitude).toFixed(5) + ", " + Number(stop.longitude).toFixed(5);
    }

    function contextPath() {
        var path = [];
        if (stopCount === 0 || currentIndex < 0)
            return path;
        var first = Math.max(0, currentIndex - 1);
        var last = Math.min(stopCount - 1, currentIndex + 1);
        for (var index = first; index <= last; index++)
            path.push(QtPositioning.coordinate(stops[index].latitude, stops[index].longitude));
        return path;
    }

    Loader {
        id: mapLoader
        anchors.fill: parent
        active: journey.active && journey.stopCount > 0
        sourceComponent: mapComponent
        onLoaded: journey.focusCurrent(true)
    }

    Component {
        id: mapComponent

        Map {
            id: journeyMap

            center: QtPositioning.coordinate(0, 0)
            zoomLevel: 15.5
            property var deferredAlignmentCoordinate: QtPositioning.coordinate()

            Timer {
                id: deferredAlignmentTimer
                interval: 0
                onTriggered: journeyMap.alignCoordinateToPoint(journeyMap.deferredAlignmentCoordinate, journey.mapFocusPoint())
            }

            plugin: Plugin {
                name: "osm"
                PluginParameter { name: "osm.useragent"; value: "WhereAmI GPX Viewer" }
                PluginParameter { name: "osm.mapping.providersrepository.disabled"; value: "true" }
                PluginParameter { name: "osm.mapping.custom.host"; value: "http://127.0.0.1:43098/api/tiles/%z/%x/%y.png" }
                PluginParameter { name: "osm.mapping.cache.disk.size"; value: "0" }
                PluginParameter { name: "osm.mapping.highdpi_tiles"; value: "true" }
                PluginParameter { name: "osm.mapping.custom.mapcopyright"; value: "Carto" }
                PluginParameter { name: "osm.mapping.custom.datacopyright"; value: "OpenStreetMap contributors" }
            }

            Component.onCompleted: {
                if (supportedMapTypes.length > 0)
                    activeMapType = supportedMapTypes[supportedMapTypes.length - 1];
            }

            function flyTo(latitude, longitude, immediate) {
                var destination = QtPositioning.coordinate(latitude, longitude);
                cameraFlight.stop();
                if (immediate) {
                    center = destination;
                    zoomLevel = 15.5;
                    deferredAlignmentCoordinate = destination;
                    deferredAlignmentTimer.restart();
                    return;
                }
                var distance = center.distanceTo(destination);
                cameraFlight.destination = destination;
                cameraFlight.cruiseZoom = distance > 500000 ? 5.5 : (distance > 50000 ? 8 : (distance > 5000 ? 11 : 13.5));
                cameraFlight.start();
            }

            function alignStop(latitude, longitude) {
                alignCoordinateToPoint(QtPositioning.coordinate(latitude, longitude), journey.mapFocusPoint());
            }

            Connections {
                target: journey

                function onFocusRequested(latitude, longitude, immediate) {
                    journeyMap.flyTo(latitude, longitude, immediate);
                }

                function onAlignmentRequested(latitude, longitude) {
                    journeyMap.alignStop(latitude, longitude);
                }
            }

            SequentialAnimation {
                id: cameraFlight
                property geoCoordinate destination
                property real cruiseZoom: 10

                NumberAnimation {
                    target: journeyMap
                    property: "zoomLevel"
                    to: cameraFlight.cruiseZoom
                    duration: 280
                    easing.type: Easing.OutCubic
                }
                CoordinateAnimation {
                    target: journeyMap
                    property: "center"
                    to: cameraFlight.destination
                    duration: 850
                    easing.type: Easing.InOutCubic
                }
                NumberAnimation {
                    target: journeyMap
                    property: "zoomLevel"
                    to: 15.5
                    duration: 480
                    easing.type: Easing.OutCubic
                }
                ScriptAction {
                    script: journeyMap.alignCoordinateToPoint(cameraFlight.destination, journey.mapFocusPoint())
                }
            }

            MapPolyline {
                line.width: 3
                line.color: theme.accent
                opacity: 0.55
                path: journey.contextPath()
            }

            MapQuickItem {
                visible: journey.currentIndex > 0 && journey.currentIndex < journey.stopCount
                coordinate: visible ? QtPositioning.coordinate(journey.stops[journey.currentIndex - 1].latitude, journey.stops[journey.currentIndex - 1].longitude) : QtPositioning.coordinate(0, 0)
                anchorPoint.x: 7
                anchorPoint.y: 7
                sourceItem: Rectangle {
                    width: 14
                    height: 14
                    radius: 7
                    color: theme.toolbarBackground
                    border.color: theme.secondaryText
                    border.width: 2
                }
            }

            MapQuickItem {
                visible: journey.currentIndex >= 0 && journey.currentIndex < journey.stopCount - 1
                coordinate: visible ? QtPositioning.coordinate(journey.stops[journey.currentIndex + 1].latitude, journey.stops[journey.currentIndex + 1].longitude) : QtPositioning.coordinate(0, 0)
                anchorPoint.x: 7
                anchorPoint.y: 7
                sourceItem: Rectangle {
                    width: 14
                    height: 14
                    radius: 7
                    color: theme.toolbarBackground
                    border.color: theme.accent
                    border.width: 2
                }
            }

            MapQuickItem {
                coordinate: journey.currentStop ? QtPositioning.coordinate(journey.currentStop.latitude, journey.currentStop.longitude) : QtPositioning.coordinate(0, 0)
                anchorPoint.x: 25
                anchorPoint.y: 25
                z: 10

                sourceItem: Item {
                    width: 50
                    height: 50

                    Rectangle {
                        anchors.centerIn: parent
                        width: 42
                        height: 42
                        radius: 21
                        color: Qt.rgba(theme.accent.r, theme.accent.g, theme.accent.b, 0.22)

                        SequentialAnimation on scale {
                            running: journey.active && journey.visible && journey.controlsVisible
                            loops: Animation.Infinite
                            NumberAnimation { to: 1.22; duration: 900; easing.type: Easing.InOutQuad }
                            NumberAnimation { to: 1; duration: 900; easing.type: Easing.InOutQuad }
                        }
                    }

                    Rectangle {
                        anchors.centerIn: parent
                        width: 22
                        height: 22
                        radius: 11
                        color: theme.accent
                        border.color: "white"
                        border.width: 4
                    }
                }
            }

            property geoCoordinate startCentroid

            PinchHandler {
                id: pinch
                target: null
                property real accumulatedScale: 1
                onActiveChanged: {
                    if (active) {
                        cameraFlight.stop();
                        journey.cameraUserAdjusted = true;
                        journeyMap.startCentroid = journeyMap.toCoordinate(pinch.centroid.position, false);
                        accumulatedScale = 1;
                    } else if (accumulatedScale !== 1) {
                        journeyMap.zoomLevel += Math.log2(accumulatedScale);
                        accumulatedScale = 1;
                    }
                }
                onScaleChanged: delta => {
                    accumulatedScale *= delta;
                    journeyMap.alignCoordinateToPoint(journeyMap.startCentroid, pinch.centroid.position);
                }
            }

            WheelHandler {
                rotationScale: 1 / 120
                property: "zoomLevel"
                onActiveChanged: {
                    if (active) {
                        cameraFlight.stop();
                        journey.cameraUserAdjusted = true;
                    }
                }
            }

            DragHandler {
                target: null
                onActiveChanged: {
                    if (active) {
                        cameraFlight.stop();
                        journey.cameraUserAdjusted = true;
                    }
                }
                onTranslationChanged: delta => journeyMap.pan(-delta.x, -delta.y)
            }
        }
    }

    Rectangle {
        anchors.centerIn: parent
        width: Math.max(0, Math.min(460, parent.width - 32))
        height: emptyContent.implicitHeight + 32
        radius: 16
        visible: journey.controlsVisible && journey.stopCount === 0
        color: Qt.rgba(theme.toolbarBackground.r, theme.toolbarBackground.g, theme.toolbarBackground.b, 0.95)

        Column {
            id: emptyContent
            anchors.centerIn: parent
            width: parent.width - 32
            spacing: 6

            Label {
                anchors.horizontalCenter: parent.horizontalCenter
                text: "No journey for " + journey.year
                color: theme.primaryText
                font.bold: true
                font.pixelSize: theme.scale(4)
            }

            Label {
                width: parent.width
                text: "No valid recorded positions were found in this period."
                color: theme.secondaryText
                horizontalAlignment: Text.AlignHCenter
                wrapMode: Text.WordWrap
            }
        }
    }

    Rectangle {
        id: detailPanel
        objectName: "journeyPanel"
        readonly property bool compact: journey.width < 620
        anchors.horizontalCenter: parent.horizontalCenter
        anchors.bottom: parent.bottom
        anchors.bottomMargin: 30
        width: Math.max(0, Math.min(760, parent.width - 24))
        height: compact ? 184 : 164
        radius: 18
        color: Qt.rgba(theme.toolbarBackground.r, theme.toolbarBackground.g, theme.toolbarBackground.b, 0.95)
        visible: journey.controlsVisible && journey.currentStop !== null

        ColumnLayout {
            anchors.fill: parent
            anchors.margins: detailPanel.compact ? 12 : 16
            spacing: 6

            RowLayout {
                Layout.fillWidth: true
                Layout.fillHeight: true
                spacing: detailPanel.compact ? 8 : 12

                ToolButton {
                    id: olderButton
                    Layout.preferredWidth: detailPanel.compact ? 44 : 48
                    Layout.preferredHeight: detailPanel.compact ? 44 : 48
                    text: "‹"
                    enabled: !journey.atOldest
                    Accessible.name: "Previous older location"
                    onClicked: journey.previous()
                    contentItem: Text {
                        text: olderButton.text
                        color: olderButton.enabled ? theme.primaryText : theme.toolbarIconDisabled
                        font.bold: true
                        font.pixelSize: theme.scale(5)
                        horizontalAlignment: Text.AlignHCenter
                        verticalAlignment: Text.AlignVCenter
                    }
                    background: Rectangle {
                        radius: width / 2
                        color: olderButton.down ? theme.toolbarBackgroundHover : Qt.rgba(theme.background.r, theme.background.g, theme.background.b, 0.78)
                    }
                }

                ColumnLayout {
                    Layout.fillWidth: true
                    spacing: 1

                    Label {
                        Layout.fillWidth: true
                        text: journey.dateLabel(journey.currentStop)
                        color: theme.accent
                        font.bold: true
                        font.pixelSize: theme.scale(0)
                        elide: Text.ElideRight
                    }

                    Label {
                        Layout.fillWidth: true
                        text: journey.placeName(journey.currentStop)
                        color: theme.primaryText
                        font.bold: true
                        font.pixelSize: theme.scale(detailPanel.compact ? 4 : 5)
                        elide: Text.ElideRight
                    }

                    Label {
                        Layout.fillWidth: true
                        text: journey.hierarchy(journey.currentStop)
                        color: theme.secondaryText
                        font.pixelSize: theme.scale(0)
                        elide: Text.ElideRight
                    }

                    RowLayout {
                        Layout.fillWidth: true
                        spacing: detailPanel.compact ? 8 : 12

                        Label {
                            text: journey.timeRange(journey.currentStop)
                            color: theme.toolbarText
                            font.pixelSize: theme.scale(0)
                        }

                        Label {
                            text: {
                                var observations = journey.currentStop ? journey.currentStop.recorded_observations || 0 : 0;
                                return observations + (observations === 1 ? " observation" : " observations");
                            }
                            color: theme.toolbarTextSecondary
                            font.pixelSize: theme.scale(0)
                        }

                        Label {
                            Layout.fillWidth: true
                            visible: !detailPanel.compact
                            text: journey.coordinateLabel(journey.currentStop)
                            color: theme.toolbarTextSecondary
                            font.pixelSize: theme.scale(0)
                            horizontalAlignment: Text.AlignRight
                            elide: Text.ElideLeft
                        }
                    }
                }

                ColumnLayout {
                    Layout.preferredWidth: 104
                    Layout.fillHeight: true
                    spacing: 2

                    Label {
                        Layout.fillWidth: true
                        text: (journey.currentIndex + 1) + " / " + journey.stopCount
                        color: theme.primaryText
                        font.bold: true
                        font.pixelSize: theme.scale(1)
                        horizontalAlignment: Text.AlignHCenter
                    }

                    Label {
                        id: journeyMetaLabel
                        objectName: "journeyMetaLabel"
                        Layout.fillWidth: true
                        text: {
                            var threshold = Math.round(journey.result && journey.result.journey_separation_meters ? journey.result.journey_separation_meters : 100);
                            return journey.result && journey.result.journey_truncated ? "LIMITED\n" + threshold + " m stops" : threshold + " m stops";
                        }
                        color: journey.result && journey.result.journey_truncated ? theme.accent : theme.secondaryText
                        font.bold: !!(journey.result && journey.result.journey_truncated)
                        font.pixelSize: theme.scale(0)
                        horizontalAlignment: Text.AlignHCenter
                        lineHeight: 0.9
                        wrapMode: Text.WordWrap
                        Accessible.name: text
                    }

                    Item {
                        Layout.fillHeight: true
                    }

                    Button {
                        id: latestButton
                        Layout.fillWidth: true
                        Layout.preferredHeight: 28
                        text: "Latest"
                        enabled: !journey.atLatest
                        opacity: enabled ? 1 : 0
                        Accessible.name: "Go to latest recorded location"
                        Accessible.ignored: !enabled
                        onClicked: journey.resetToLatest()
                        contentItem: Text {
                            text: latestButton.text
                            color: "white"
                            font.bold: true
                            font.pixelSize: theme.scale(0)
                            horizontalAlignment: Text.AlignHCenter
                            verticalAlignment: Text.AlignVCenter
                        }
                        background: Rectangle {
                            radius: 7
                            color: theme.accent
                        }
                        Behavior on opacity {
                            NumberAnimation { duration: 120 }
                        }
                    }
                }

                ToolButton {
                    id: newerButton
                    Layout.preferredWidth: detailPanel.compact ? 44 : 48
                    Layout.preferredHeight: detailPanel.compact ? 44 : 48
                    text: "›"
                    enabled: !journey.atLatest
                    Accessible.name: "Next newer location"
                    onClicked: journey.next()
                    contentItem: Text {
                        text: newerButton.text
                        color: newerButton.enabled ? theme.primaryText : theme.toolbarIconDisabled
                        font.bold: true
                        font.pixelSize: theme.scale(5)
                        horizontalAlignment: Text.AlignHCenter
                        verticalAlignment: Text.AlignVCenter
                    }
                    background: Rectangle {
                        radius: width / 2
                        color: newerButton.down ? theme.toolbarBackgroundHover : Qt.rgba(theme.background.r, theme.background.g, theme.background.b, 0.78)
                    }
                }
            }

            Slider {
                id: journeySlider
                Layout.fillWidth: true
                Layout.preferredHeight: 28
                from: 0
                to: Math.max(0, journey.stopCount - 1)
                stepSize: 1
                enabled: journey.stopCount > 1
                Accessible.name: "Journey position"
                onMoved: journey.selectStop(value)
                Keys.priority: Keys.BeforeItem
                Keys.onLeftPressed: event => {
                    journey.previous();
                    event.accepted = true;
                }
                Keys.onRightPressed: event => {
                    journey.next();
                    event.accepted = true;
                }
                background: Rectangle {
                    x: journeySlider.leftPadding
                    y: journeySlider.topPadding + journeySlider.availableHeight / 2 - height / 2
                    width: journeySlider.availableWidth
                    height: 3
                    radius: 2
                    color: theme.toolbarSeparator

                    Rectangle {
                        width: journeySlider.visualPosition * parent.width
                        height: parent.height
                        radius: parent.radius
                        color: theme.accent
                    }
                }
                handle: Rectangle {
                    x: journeySlider.leftPadding + journeySlider.visualPosition * (journeySlider.availableWidth - width)
                    y: journeySlider.topPadding + journeySlider.availableHeight / 2 - height / 2
                    width: 18
                    height: 18
                    radius: 9
                    color: journeySlider.pressed ? Qt.lighter(theme.accent, 1.2) : theme.accent
                    border.color: "white"
                    border.width: 2
                }
            }

            Binding {
                target: journeySlider
                property: "value"
                value: journey.currentIndex
                restoreMode: Binding.RestoreBindingOrValue
            }
        }
    }
}
