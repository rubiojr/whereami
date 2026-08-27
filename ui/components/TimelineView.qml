pragma ComponentBehavior: Bound
import QtQuick 2.15
import QtQuick.Controls 2.15
import QtQuick.Layouts 1.15
import QtLocation 6.5
import QtPositioning 6.5
import "../themes"

Item {
    id: timelineView

    property var result: null
    property int year: new Date().getUTCFullYear()
    property bool active: true
    property bool controlsVisible: true
    property bool cameraUserAdjusted: false
    property int currentIndex: -1
    property var selectedPlace: null
    property alias scrubber: timelineSlider
    readonly property var stops: result && result.timeline ? result.timeline : []
    readonly property int stopCount: stops.length
    readonly property var currentStop: currentIndex >= 0 && currentIndex < stopCount ? stops[currentIndex] : null
    readonly property var displayedPlace: selectedPlace || currentStop
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

    OpenFreeMapPlugin {
        id: mapPlugin
    }

    onResultChanged: {
        selectedPlace = null;
        currentIndex = -1;
        resetToLatest();
        Qt.callLater(function() { timelineView.focusCurrent(true); });
    }
    onCurrentIndexChanged: {
        selectedPlace = null;
        cameraUserAdjusted = false;
        Qt.callLater(focusCurrent);
    }
    onActiveChanged: {
        if (active)
            Qt.callLater(function() { timelineView.focusCurrent(true); });
    }
    onControlsVisibleChanged: Qt.callLater(timelineView.alignCurrent)
    onFocusTargetYChanged: {
        if (active)
            alignmentTimer.restart();
    }

    Timer {
        id: alignmentTimer
        interval: 60
        repeat: false
        onTriggered: timelineView.alignCurrent()
    }

    Shortcut {
        sequence: "Left"
        context: Qt.WindowShortcut
        enabled: timelineView.active && timelineView.visible && timelineView.controlsVisible && !timelineSlider.activeFocus
        onActivated: timelineView.previous()
    }

    Shortcut {
        sequence: "Right"
        context: Qt.WindowShortcut
        enabled: timelineView.active && timelineView.visible && timelineView.controlsVisible && !timelineSlider.activeFocus
        onActivated: timelineView.next()
    }

    function resetToLatest() {
        selectedPlace = null;
        currentIndex = result && result.timeline ? result.timeline.length - 1 : -1;
    }

    function selectStop(index) {
        selectedPlace = null;
        if (stopCount === 0) {
            currentIndex = -1;
            return;
        }
        currentIndex = Math.max(0, Math.min(stopCount - 1, Math.round(index)));
    }

    function showStop(index, place) {
        if (stopCount === 0) {
            selectedPlace = null;
            currentIndex = -1;
            return;
        }
        var targetIndex = Math.max(0, Math.min(stopCount - 1, Math.round(index)));
        if (currentIndex === targetIndex) {
            selectedPlace = place || null;
            cameraUserAdjusted = false;
            Qt.callLater(alignCurrent);
            return;
        }
        selectStop(targetIndex);
        selectedPlace = place || null;
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

    function stopIndexForPlace(place) {
        if (!place || place.timeline_index === undefined || place.timeline_index === null)
            return -1;
        if (typeof place.timeline_index !== "number")
            return -1;
        var index = place.timeline_index;
        if (!isFinite(index) || Math.floor(index) !== index || index < 0 || index >= stopCount)
            return -1;
        return index;
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
        active: timelineView.active && timelineView.stopCount > 0
        sourceComponent: mapComponent
        onLoaded: timelineView.focusCurrent(true)
    }

    Component {
        id: mapComponent

        Map {
            id: timelineMap

            center: QtPositioning.coordinate(0, 0)
            zoomLevel: 15.5
            property var deferredAlignmentCoordinate: QtPositioning.coordinate()

            Timer {
                id: deferredAlignmentTimer
                interval: 0
                onTriggered: timelineMap.alignCoordinateToPoint(timelineMap.deferredAlignmentCoordinate, timelineView.mapFocusPoint())
            }

            copyrightsVisible: false

            plugin: mapPlugin

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
                alignCoordinateToPoint(QtPositioning.coordinate(latitude, longitude), timelineView.mapFocusPoint());
            }

            Connections {
                target: timelineView

                function onFocusRequested(latitude, longitude, immediate) {
                    timelineMap.flyTo(latitude, longitude, immediate);
                }

                function onAlignmentRequested(latitude, longitude) {
                    timelineMap.alignStop(latitude, longitude);
                }
            }

            SequentialAnimation {
                id: cameraFlight
                property geoCoordinate destination
                property real cruiseZoom: 10

                NumberAnimation {
                    target: timelineMap
                    property: "zoomLevel"
                    to: cameraFlight.cruiseZoom
                    duration: 280
                    easing.type: Easing.OutCubic
                }
                CoordinateAnimation {
                    target: timelineMap
                    property: "center"
                    to: cameraFlight.destination
                    duration: 850
                    easing.type: Easing.InOutCubic
                }
                NumberAnimation {
                    target: timelineMap
                    property: "zoomLevel"
                    to: 15.5
                    duration: 480
                    easing.type: Easing.OutCubic
                }
                ScriptAction {
                    script: timelineMap.alignCoordinateToPoint(cameraFlight.destination, timelineView.mapFocusPoint())
                }
            }

            MapPolyline {
                line.width: 3
                line.color: theme.accent
                opacity: 0.55
                path: timelineView.contextPath()
            }

            MapQuickItem {
                visible: timelineView.currentIndex > 0 && timelineView.currentIndex < timelineView.stopCount
                coordinate: visible ? QtPositioning.coordinate(timelineView.stops[timelineView.currentIndex - 1].latitude, timelineView.stops[timelineView.currentIndex - 1].longitude) : QtPositioning.coordinate(0, 0)
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
                visible: timelineView.currentIndex >= 0 && timelineView.currentIndex < timelineView.stopCount - 1
                coordinate: visible ? QtPositioning.coordinate(timelineView.stops[timelineView.currentIndex + 1].latitude, timelineView.stops[timelineView.currentIndex + 1].longitude) : QtPositioning.coordinate(0, 0)
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
                coordinate: timelineView.currentStop ? QtPositioning.coordinate(timelineView.currentStop.latitude, timelineView.currentStop.longitude) : QtPositioning.coordinate(0, 0)
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
                            running: timelineView.active && timelineView.visible && timelineView.controlsVisible
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
                        timelineView.cameraUserAdjusted = true;
                        timelineMap.startCentroid = timelineMap.toCoordinate(pinch.centroid.position, false);
                        accumulatedScale = 1;
                    } else if (accumulatedScale !== 1) {
                        timelineMap.zoomLevel += Math.log2(accumulatedScale);
                        accumulatedScale = 1;
                    }
                }
                onScaleChanged: delta => {
                    accumulatedScale *= delta;
                    timelineMap.alignCoordinateToPoint(timelineMap.startCentroid, pinch.centroid.position);
                }
            }

            WheelHandler {
                rotationScale: 1 / 120
                property: "zoomLevel"
                onActiveChanged: {
                    if (active) {
                        cameraFlight.stop();
                        timelineView.cameraUserAdjusted = true;
                    }
                }
            }

            DragHandler {
                target: null
                onActiveChanged: {
                    if (active) {
                        cameraFlight.stop();
                        timelineView.cameraUserAdjusted = true;
                    }
                }
                onTranslationChanged: delta => timelineMap.pan(-delta.x, -delta.y)
            }
        }
    }

    Rectangle {
        anchors.centerIn: parent
        width: Math.max(0, Math.min(460, parent.width - 32))
        height: emptyContent.implicitHeight + 32
        radius: 16
        visible: timelineView.controlsVisible && timelineView.stopCount === 0
        color: Qt.rgba(theme.toolbarBackground.r, theme.toolbarBackground.g, theme.toolbarBackground.b, 0.95)

        Column {
            id: emptyContent
            anchors.centerIn: parent
            width: parent.width - 32
            spacing: 6

            Label {
                anchors.horizontalCenter: parent.horizontalCenter
                text: "No timeline for " + timelineView.year
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
        objectName: "timelinePanel"
        readonly property bool compact: timelineView.width < 620
        readonly property int contentMargin: compact ? 12 : 16
        anchors.horizontalCenter: parent.horizontalCenter
        anchors.bottom: parent.bottom
        anchors.bottomMargin: Math.max(30, mapOverlay.attributionHeight + 14)
        width: Math.max(0, Math.min(760, parent.width - 24))
        height: Math.max(compact ? 194 : 174, detailContent.implicitHeight + contentMargin * 2)
        radius: 18
        color: Qt.rgba(theme.toolbarBackground.r, theme.toolbarBackground.g, theme.toolbarBackground.b, 0.95)
        visible: timelineView.controlsVisible && timelineView.currentStop !== null

        ColumnLayout {
            id: detailContent
            anchors.fill: parent
            anchors.margins: detailPanel.contentMargin
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
                    enabled: !timelineView.atOldest
                    Accessible.name: "Previous older location"
                    onClicked: timelineView.previous()
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
                        text: timelineView.dateLabel(timelineView.currentStop)
                        color: theme.accent
                        font.bold: true
                        font.pixelSize: theme.scale(2)
                        elide: Text.ElideRight
                    }

                    Label {
                        Layout.fillWidth: true
                        text: timelineView.placeName(timelineView.displayedPlace)
                        color: theme.primaryText
                        font.bold: true
                        font.pixelSize: theme.scale(detailPanel.compact ? 4 : 5)
                        elide: Text.ElideRight
                    }

                    Label {
                        Layout.fillWidth: true
                        text: timelineView.hierarchy(timelineView.displayedPlace)
                        color: theme.secondaryText
                        font.pixelSize: theme.scale(2)
                        elide: Text.ElideRight
                    }

                    RowLayout {
                        Layout.fillWidth: true
                        spacing: detailPanel.compact ? 8 : 12

                        Label {
                            text: timelineView.timeRange(timelineView.currentStop)
                            color: theme.toolbarText
                            font.pixelSize: theme.scale(2)
                        }

                        Label {
                            text: {
                                var observations = timelineView.currentStop ? timelineView.currentStop.recorded_observations || 0 : 0;
                                return observations + (observations === 1 ? " observation" : " observations");
                            }
                            color: theme.toolbarTextSecondary
                            font.pixelSize: theme.scale(2)
                        }

                        Label {
                            Layout.fillWidth: true
                            visible: !detailPanel.compact
                            text: timelineView.coordinateLabel(timelineView.currentStop)
                            color: theme.toolbarTextSecondary
                            font.pixelSize: theme.scale(2)
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
                        text: (timelineView.currentIndex + 1) + " / " + timelineView.stopCount
                        color: theme.primaryText
                        font.bold: true
                        font.pixelSize: theme.scale(2)
                        horizontalAlignment: Text.AlignHCenter
                    }

                    Label {
                        id: timelineMetaLabel
                        objectName: "timelineMetaLabel"
                        Layout.fillWidth: true
                        text: {
                            var hasThreshold = timelineView.result && timelineView.result.timeline_stop_separation_meters !== undefined && timelineView.result.timeline_stop_separation_meters !== null;
                            var threshold = Math.round(hasThreshold ? timelineView.result.timeline_stop_separation_meters : 100);
                            return timelineView.result && timelineView.result.timeline_truncated ? "LIMITED\n" + threshold + " m stops" : threshold + " m stops";
                        }
                        color: timelineView.result && timelineView.result.timeline_truncated ? theme.accent : theme.secondaryText
                        font.bold: !!(timelineView.result && timelineView.result.timeline_truncated)
                        font.pixelSize: theme.scale(2)
                        horizontalAlignment: Text.AlignHCenter
                        lineHeight: 0.9
                        wrapMode: Text.WordWrap
                        Accessible.name: text
                    }

                    Item {
                        Layout.fillHeight: true
                    }

                }

                ToolButton {
                    id: newerButton
                    Layout.preferredWidth: detailPanel.compact ? 44 : 48
                    Layout.preferredHeight: detailPanel.compact ? 44 : 48
                    text: "›"
                    enabled: !timelineView.atLatest
                    Accessible.name: "Next newer location"
                    onClicked: timelineView.next()
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

            RowLayout {
                Layout.fillWidth: true
                Layout.preferredHeight: 28
                spacing: 6

                Slider {
                    id: timelineSlider
                    Layout.fillWidth: true
                    Layout.preferredHeight: 28
                    from: 0
                    to: Math.max(0, timelineView.stopCount - 1)
                    stepSize: 1
                    enabled: timelineView.stopCount > 1
                    Accessible.name: "Timeline position"
                    onMoved: timelineView.selectStop(value)
                    Keys.priority: Keys.BeforeItem
                    Keys.onLeftPressed: event => {
                        timelineView.previous();
                        event.accepted = true;
                    }
                    Keys.onRightPressed: event => {
                        timelineView.next();
                        event.accepted = true;
                    }
                    background: Rectangle {
                        x: timelineSlider.leftPadding
                        y: timelineSlider.topPadding + timelineSlider.availableHeight / 2 - height / 2
                        width: timelineSlider.availableWidth
                        height: 3
                        radius: 2
                        color: theme.toolbarSeparator

                        Rectangle {
                            width: timelineSlider.visualPosition * parent.width
                            height: parent.height
                            radius: parent.radius
                            color: theme.accent
                        }
                    }
                    handle: Rectangle {
                        x: timelineSlider.leftPadding + timelineSlider.visualPosition * (timelineSlider.availableWidth - width)
                        y: timelineSlider.topPadding + timelineSlider.availableHeight / 2 - height / 2
                        width: 18
                        height: 18
                        radius: 9
                        color: timelineSlider.pressed ? Qt.lighter(theme.accent, 1.2) : theme.accent
                        border.color: "white"
                        border.width: 2
                    }
                }

                ToolButton {
                    id: finishButton
                    objectName: "timelineFinishButton"
                    Layout.preferredWidth: 30
                    Layout.preferredHeight: 28
                    enabled: !timelineView.atLatest
                    opacity: enabled ? 1 : 0
                    icon.source: "qrc:/icons/finish.svg"
                    icon.width: 16
                    icon.height: 16
                    icon.color: theme.toolbarIcon
                    Accessible.name: "Go to latest recorded location"
                    Accessible.ignored: !enabled
                    onClicked: timelineView.resetToLatest()
                    CustomToolTip {
                        tooltipText: "Go to latest"
                        visible: finishButton.hovered
                        position: "top"
                    }
                    background: Rectangle {
                        radius: 7
                        color: finishButton.down ? theme.toolbarBackgroundHover : "transparent"
                    }
                    Behavior on opacity {
                        NumberAnimation { duration: 120 }
                    }
                }
            }

            Binding {
                target: timelineSlider
                property: "value"
                value: Math.max(timelineSlider.from, Math.min(timelineSlider.to, timelineView.currentIndex))
                restoreMode: Binding.RestoreBindingOrValue
            }
        }
    }

    // Declared last so provider attribution always paints above the timeline
    // panel and scrubber; the panel reserves room for it via attributionHeight.
    MapProviderOverlay {
        id: mapOverlay
        anchors.fill: parent
        z: 100
        mapLibreAvailable: mapPlugin.mapLibreAvailable
        theme: theme
    }
}
