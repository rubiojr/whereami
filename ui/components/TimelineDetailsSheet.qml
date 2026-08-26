pragma ComponentBehavior: Bound
import QtQuick 2.15
import QtQuick.Controls 2.15
import QtQuick.Layouts 1.15
import "../themes"

Item {
    id: details

    property var result: null
    property string period: ""
    property bool open: false
    property real topInset: 68
    property real bottomInset: 30
    property string sortColumn: "observations"
    property bool sortAscending: false
    property alias filterText: placeFilter.text
    property alias placeList: placesList
    property alias placeScrollBar: placesScrollBar
    readonly property bool compact: width < 640
    readonly property bool shortLayout: height < 520
    readonly property int distinctPlaceCount: result && result.places ? result.places.length : 0
    readonly property var displayedPlaces: sortedPlaces()

    signal closeRequested
    signal placeRequested(var place)
    signal refreshRequested

    visible: open && result !== null
    focus: visible
    Accessible.role: Accessible.Dialog
    Accessible.name: "Timeline details"
    onVisibleChanged: {
        if (visible)
            Qt.callLater(forceActiveFocus);
    }

    ThemeLoader {
        id: theme
    }

    function hierarchy(place) {
        var parts = [];
        if (place.locality)
            parts.push(place.locality);
        if (place.local_admin && place.local_admin !== place.locality)
            parts.push(place.local_admin);
        if (place.county)
            parts.push(place.county);
        if (place.region)
            parts.push(place.region);
        if (place.country)
            parts.push(place.country);
        return parts.join(" · ");
    }

    function displayName(place) {
        if (!place)
            return "Unnamed administrative area";
        return place.locality || place.local_admin || place.county || place.region || place.country || "Unnamed administrative area";
    }

    function placeIsNavigable(place) {
        if (!place || place.timeline_index === undefined || place.timeline_index === null || !result || !result.timeline)
            return false;
        if (typeof place.timeline_index !== "number")
            return false;
        var index = place.timeline_index;
        return isFinite(index) && Math.floor(index) === index && index >= 0 && index < result.timeline.length;
    }

    function compareNames(left, right) {
        var leftName = hierarchy(left).toLocaleLowerCase();
        var rightName = hierarchy(right).toLocaleLowerCase();
        return leftName.localeCompare(rightName);
    }

    function sortedPlaces() {
        var source = result && result.places ? result.places : [];
        var query = filterText.trim().toLocaleLowerCase();
        var filtered = [];
        for (var index = 0; index < source.length; index++) {
            var searchText = displayName(source[index]) + " " + hierarchy(source[index]);
            if (query === "" || searchText.toLocaleLowerCase().indexOf(query) >= 0)
                filtered.push(source[index]);
        }
        filtered.sort(function (left, right) {
            var comparison = 0;
            if (sortColumn === "name") {
                comparison = compareNames(left, right);
            } else if (sortColumn === "days") {
                comparison = Number(left.recorded_days || 0) - Number(right.recorded_days || 0);
            } else {
                comparison = Number(left.recorded_observations || 0) - Number(right.recorded_observations || 0);
            }
            if (comparison === 0)
                return compareNames(left, right);
            return sortAscending ? comparison : -comparison;
        });
        return filtered;
    }

    function setSort(column) {
        if (sortColumn === column) {
            sortAscending = !sortAscending;
            return;
        }
        sortColumn = column;
        sortAscending = column === "name";
    }

    function sortLabel(column, label) {
        if (sortColumn !== column)
            return label;
        return label + (sortAscending ? " ↑" : " ↓");
    }

    function sortAccessibleName(column, label) {
        if (sortColumn !== column)
            return "Sort by " + label;
        return "Sort by " + label + ", currently " + (sortAscending ? "ascending" : "descending");
    }

    function movementSummary() {
        var hasThreshold = result && result.timeline_stop_separation_meters !== undefined && result.timeline_stop_separation_meters !== null;
        var threshold = Math.round(hasThreshold ? result.timeline_stop_separation_meters : 100);
        var text = threshold + " m significant-stop threshold";
        if (result && result.timeline_truncated)
            text += " · timeline limited";
        return text;
    }

    function observationSummary() {
        var summary = result && result.summary ? result.summary : {};
        return "Recorded " + (summary.recorded_observations || 0)
                + " · Resolved " + (summary.resolved_observations || 0)
                + " · Outside boundaries " + (summary.unresolved_observations || 0)
                + " · Invalid coordinates " + (summary.invalid_coordinates || 0)
                + " · Distinct places " + distinctPlaceCount;
    }

    Shortcut {
        sequence: "Escape"
        context: Qt.WindowShortcut
        enabled: details.visible
        onActivated: details.closeRequested()
    }

    Rectangle {
        anchors.fill: parent
        color: Qt.rgba(0, 0, 0, 0.28)

        MouseArea {
            anchors.fill: parent
            onClicked: details.closeRequested()
        }
    }

    Rectangle {
        id: sheet
        objectName: "timelineDetailsSheet"
        anchors.top: parent.top
        anchors.bottom: parent.bottom
        anchors.horizontalCenter: parent.horizontalCenter
        anchors.topMargin: details.topInset
        anchors.bottomMargin: details.bottomInset
        width: Math.max(0, Math.min(980, parent.width - 32))
        radius: 18
        clip: true
        color: Qt.rgba(theme.toolbarBackground.r, theme.toolbarBackground.g, theme.toolbarBackground.b, 0.97)

        MouseArea {
            anchors.fill: parent
        }

        ColumnLayout {
            id: sheetContent
            objectName: "timelineDetailsContent"
            anchors.fill: parent
            anchors.margins: details.shortLayout ? 10 : (details.compact ? 14 : 20)
            spacing: details.shortLayout ? 6 : (details.compact ? 10 : 14)

            RowLayout {
                Layout.fillWidth: true
                spacing: 12

                Label {
                    Layout.fillWidth: true
                    text: "Recorded places"
                    color: theme.primaryText
                    font.bold: true
                    font.pixelSize: theme.scale(4)
                    elide: Text.ElideRight
                }

                ToolButton {
                    id: refreshButton
                    text: "Refresh"
                    Accessible.name: "Refresh timeline"
                    onClicked: details.refreshRequested()
                    contentItem: Text {
                        text: refreshButton.text
                        color: theme.accent
                        font.bold: true
                        horizontalAlignment: Text.AlignHCenter
                        verticalAlignment: Text.AlignVCenter
                    }
                    background: Rectangle {
                        radius: 8
                        color: refreshButton.down ? theme.toolbarBackgroundHover : "transparent"
                    }
                }
            }

            Label {
                id: timelineMetadata
                objectName: "timelineDetailsMetadata"
                Layout.fillWidth: true
                text: details.period + " · UTC · " + details.movementSummary()
                color: theme.secondaryText
                font.pixelSize: theme.scale(1)
                wrapMode: Text.WordWrap
            }

            GridLayout {
                Layout.fillWidth: true
                visible: !details.shortLayout
                columns: details.compact ? 2 : 4
                columnSpacing: 8
                rowSpacing: 8

                Repeater {
                    model: details.result ? [
                        { key: "recorded", label: "Recorded", value: details.result.summary ? details.result.summary.recorded_observations || 0 : 0 },
                        { key: "resolved", label: "Resolved", value: details.result.summary ? details.result.summary.resolved_observations || 0 : 0 },
                        { key: "outside", label: "Outside boundaries", value: details.result.summary ? details.result.summary.unresolved_observations || 0 : 0 },
                        { key: "places", label: "Distinct places", value: details.distinctPlaceCount }
                    ] : []

                    delegate: Rectangle {
                        id: summaryCard
                        required property var modelData
                        Layout.fillWidth: true
                        Layout.preferredHeight: details.compact ? 58 : 66
                        radius: 10
                        color: Qt.rgba(theme.toolbarBackgroundHover.r, theme.toolbarBackgroundHover.g, theme.toolbarBackgroundHover.b, 0.72)

                        Column {
                            anchors.centerIn: parent
                            spacing: 1

                            Label {
                                objectName: "timelineSummaryValue-" + summaryCard.modelData.key
                                anchors.horizontalCenter: parent.horizontalCenter
                                text: summaryCard.modelData.value
                                color: theme.primaryText
                                font.bold: true
                                font.pixelSize: theme.scale(4)
                            }

                            Label {
                                anchors.horizontalCenter: parent.horizontalCenter
                                text: summaryCard.modelData.label
                                color: theme.secondaryText
                                font.pixelSize: theme.scale(0)
                            }
                        }
                    }
                }
            }

            Label {
                id: shortSummary
                objectName: "timelineDetailsShortSummary"
                Layout.fillWidth: true
                visible: details.shortLayout
                text: details.observationSummary()
                color: theme.secondaryText
                font.pixelSize: theme.scale(0)
                wrapMode: Text.WordWrap
            }

            RowLayout {
                Layout.fillWidth: true
                spacing: 8

                TextField {
                    id: placeFilter
                    objectName: "timelinePlaceFilter"
                    Layout.fillWidth: true
                    Layout.preferredHeight: 36
                    placeholderText: "Filter places by name"
                    Accessible.name: "Filter timeline places by name"
                    selectByMouse: true
                    leftPadding: 12
                    rightPadding: 12
                    color: theme.primaryText
                    placeholderTextColor: theme.secondaryText
                    background: Rectangle {
                        radius: 9
                        color: Qt.rgba(theme.background.r, theme.background.g, theme.background.b, 0.72)
                        border.color: placeFilter.activeFocus ? theme.accent : theme.toolbarBorder
                        border.width: placeFilter.activeFocus ? 2 : 1
                    }
                }

                Label {
                    text: details.displayedPlaces.length + " of " + details.distinctPlaceCount
                    color: theme.secondaryText
                    font.pixelSize: theme.scale(1)
                    Accessible.name: text + " places shown"
                }

                ToolButton {
                    id: clearFilterButton
                    visible: placeFilter.text !== ""
                    text: "×"
                    Accessible.name: "Clear place filter"
                    onClicked: placeFilter.clear()
                    contentItem: Text {
                        text: clearFilterButton.text
                        color: clearFilterButton.hovered ? theme.toolbarIconHover : theme.toolbarIcon
                        font.pixelSize: theme.scale(3)
                        horizontalAlignment: Text.AlignHCenter
                        verticalAlignment: Text.AlignVCenter
                    }
                    background: Rectangle {
                        radius: 8
                        color: clearFilterButton.down ? theme.toolbarBackgroundHover : "transparent"
                    }
                }
            }

            Rectangle {
                Layout.fillWidth: true
                Layout.fillHeight: true
                Layout.minimumHeight: details.shortLayout ? 72 : 0
                radius: 12
                color: Qt.rgba(theme.background.r, theme.background.g, theme.background.b, 0.5)

                ListView {
                    id: placesList
                    objectName: "timelineDetailsList"
                    anchors.fill: parent
                    clip: true
                    model: details.displayedPlaces
                    spacing: 1
                    boundsBehavior: Flickable.StopAtBounds
                    headerPositioning: ListView.OverlayHeader

                    ScrollBar.vertical: ScrollBar {
                        id: placesScrollBar
                        policy: ScrollBar.AsNeeded
                        Accessible.name: "Timeline places scrollbar"
                    }

                    header: Rectangle {
                        objectName: "timelinePlacesHeader"
                        width: placesList.width
                        height: details.compact ? 48 : 40
                        z: 2
                        color: Qt.rgba(theme.toolbarBackground.r, theme.toolbarBackground.g, theme.toolbarBackground.b, 1)

                        RowLayout {
                            anchors.fill: parent
                            anchors.leftMargin: 14
                            anchors.rightMargin: 14 + (placesScrollBar.visible ? placesScrollBar.width : 0)
                            spacing: 10

                            ToolButton {
                                id: nameSortButton
                                objectName: "timelineNameSortButton"
                                Layout.fillWidth: true
                                Layout.fillHeight: true
                                padding: 0
                                text: details.sortLabel("name", "Administrative place")
                                Accessible.name: details.sortAccessibleName("name", "administrative place name")
                                onClicked: details.setSort("name")
                                contentItem: Text {
                                    text: nameSortButton.text
                                    color: theme.toolbarText
                                    font.bold: true
                                    horizontalAlignment: Text.AlignLeft
                                    verticalAlignment: Text.AlignVCenter
                                    elide: Text.ElideRight
                                }
                                background: Rectangle {
                                    radius: 6
                                    color: nameSortButton.down ? theme.toolbarBackground : "transparent"
                                }
                            }

                            ColumnLayout {
                                visible: details.compact
                                Layout.preferredWidth: 86
                                Layout.fillHeight: true
                                spacing: 0

                                ToolButton {
                                    id: compactObservationsSortButton
                                    objectName: "timelineCompactObservationsSortButton"
                                    Layout.fillWidth: true
                                    Layout.fillHeight: true
                                    padding: 0
                                    text: details.sortLabel("observations", "Obs")
                                    Accessible.name: details.sortAccessibleName("observations", "recorded observations")
                                    onClicked: details.setSort("observations")
                                    contentItem: Text {
                                        text: compactObservationsSortButton.text
                                        color: theme.toolbarTextSecondary
                                        font.pixelSize: theme.scale(0)
                                        horizontalAlignment: Text.AlignRight
                                        verticalAlignment: Text.AlignVCenter
                                    }
                                    background: Rectangle {
                                        radius: 5
                                        color: compactObservationsSortButton.down ? theme.toolbarBackgroundHover : "transparent"
                                    }
                                }

                                ToolButton {
                                    id: compactDaysSortButton
                                    objectName: "timelineCompactDaysSortButton"
                                    Layout.fillWidth: true
                                    Layout.fillHeight: true
                                    padding: 0
                                    text: details.sortLabel("days", "Days")
                                    Accessible.name: details.sortAccessibleName("days", "recorded days")
                                    onClicked: details.setSort("days")
                                    contentItem: Text {
                                        text: compactDaysSortButton.text
                                        color: theme.toolbarTextSecondary
                                        font.pixelSize: theme.scale(0)
                                        horizontalAlignment: Text.AlignRight
                                        verticalAlignment: Text.AlignVCenter
                                    }
                                    background: Rectangle {
                                        radius: 5
                                        color: compactDaysSortButton.down ? theme.toolbarBackgroundHover : "transparent"
                                    }
                                }
                            }

                            ToolButton {
                                id: observationsSortButton
                                objectName: "timelineObservationsSortButton"
                                Layout.preferredWidth: 110
                                Layout.fillHeight: true
                                visible: !details.compact
                                padding: 0
                                text: details.sortLabel("observations", "Observations")
                                Accessible.name: details.sortAccessibleName("observations", "recorded observations")
                                onClicked: details.setSort("observations")
                                contentItem: Text {
                                    text: observationsSortButton.text
                                    color: theme.toolbarTextSecondary
                                    horizontalAlignment: Text.AlignRight
                                    verticalAlignment: Text.AlignVCenter
                                }
                                background: Rectangle {
                                    radius: 6
                                    color: observationsSortButton.down ? theme.toolbarBackground : "transparent"
                                }
                            }

                            ToolButton {
                                id: daysSortButton
                                objectName: "timelineDaysSortButton"
                                Layout.preferredWidth: 70
                                Layout.fillHeight: true
                                visible: !details.compact
                                padding: 0
                                text: details.sortLabel("days", "Days")
                                Accessible.name: details.sortAccessibleName("days", "recorded days")
                                onClicked: details.setSort("days")
                                contentItem: Text {
                                    text: daysSortButton.text
                                    color: theme.toolbarTextSecondary
                                    horizontalAlignment: Text.AlignRight
                                    verticalAlignment: Text.AlignVCenter
                                }
                                background: Rectangle {
                                    radius: 6
                                    color: daysSortButton.down ? theme.toolbarBackground : "transparent"
                                }
                            }
                        }
                    }

                    delegate: Rectangle {
                        id: placeRow
                        objectName: "timelinePlaceRow-" + placeRow.index
                        required property var modelData
                        required property int index
                        readonly property bool navigable: details.placeIsNavigable(modelData)
                        width: placesList.width
                        height: details.compact ? 58 : 62
                        opacity: navigable ? 1 : 0.62
                        color: placeTap.pressed || placeHover.hovered ? Qt.rgba(theme.toolbarBackgroundHover.r, theme.toolbarBackgroundHover.g, theme.toolbarBackgroundHover.b, 0.82) : (placeRow.index % 2 === 0 ? "transparent" : Qt.rgba(theme.toolbarBackgroundHover.r, theme.toolbarBackgroundHover.g, theme.toolbarBackgroundHover.b, 0.42))
                        activeFocusOnTab: navigable
                        border.color: activeFocus ? theme.accent : "transparent"
                        border.width: activeFocus ? 2 : 0
                        Accessible.role: navigable ? Accessible.Button : Accessible.StaticText
                        Accessible.name: navigable ? "Show " + details.displayName(placeRow.modelData) + " on timeline" : details.displayName(placeRow.modelData)
                        Accessible.description: (placeRow.modelData.recorded_observations || 0) + " recorded observations, " + (placeRow.modelData.recorded_days || 0) + " recorded days" + (navigable ? "" : ", timeline navigation unavailable")
                        Accessible.onPressAction: placeRow.requestNavigation(true)

                        function requestNavigation(ignoreMotion) {
                            if (!navigable || (!ignoreMotion && (placesList.moving || placesList.flicking)))
                                return;
                            details.placeRequested(modelData);
                        }

                        TapHandler {
                            id: placeTap
                            enabled: placeRow.navigable
                            acceptedButtons: Qt.LeftButton
                            onTapped: placeRow.requestNavigation(false)
                        }

                        HoverHandler {
                            id: placeHover
                            enabled: placeRow.navigable
                            cursorShape: placeRow.navigable ? Qt.PointingHandCursor : Qt.ArrowCursor
                        }

                        Keys.onReturnPressed: event => {
                            placeRow.requestNavigation(true);
                            event.accepted = true;
                        }
                        Keys.onEnterPressed: event => {
                            placeRow.requestNavigation(true);
                            event.accepted = true;
                        }
                        Keys.onSpacePressed: event => {
                            placeRow.requestNavigation(true);
                            event.accepted = true;
                        }
                        onActiveFocusChanged: {
                            if (activeFocus)
                                placesList.positionViewAtIndex(index, ListView.Contain);
                        }

                        RowLayout {
                            anchors.fill: parent
                            anchors.leftMargin: 14
                            anchors.rightMargin: 14 + (placesScrollBar.visible ? placesScrollBar.width : 0)
                            spacing: 10

                            ColumnLayout {
                                Layout.fillWidth: true
                                spacing: 1

                                Label {
                                    Layout.fillWidth: true
                                    text: details.displayName(placeRow.modelData)
                                    color: theme.primaryText
                                    font.bold: true
                                    font.pixelSize: theme.scale(2)
                                    elide: Text.ElideRight
                                }

                                Label {
                                    Layout.fillWidth: true
                                    text: details.hierarchy(placeRow.modelData)
                                    color: theme.secondaryText
                                    font.pixelSize: theme.scale(1)
                                    elide: Text.ElideRight
                                }
                            }

                            ColumnLayout {
                                id: compactMetrics
                                visible: details.compact
                                Layout.preferredWidth: 86
                                spacing: 0

                                Label {
                                    objectName: "compactObservations-" + placeRow.index
                                    Layout.fillWidth: true
                                    text: (placeRow.modelData.recorded_observations || 0) + " obs"
                                    color: theme.toolbarText
                                    font.bold: true
                                    horizontalAlignment: Text.AlignRight
                                }

                                Label {
                                    objectName: "compactDays-" + placeRow.index
                                    Layout.fillWidth: true
                                    text: (placeRow.modelData.recorded_days || 0) + " days"
                                    color: theme.toolbarTextSecondary
                                    font.pixelSize: theme.scale(0)
                                    horizontalAlignment: Text.AlignRight
                                }
                            }

                            Label {
                                Layout.preferredWidth: 110
                                visible: !details.compact
                                text: placeRow.modelData.recorded_observations || 0
                                color: theme.toolbarText
                                font.bold: true
                                horizontalAlignment: Text.AlignRight
                            }

                            Label {
                                Layout.preferredWidth: 70
                                visible: !details.compact
                                text: placeRow.modelData.recorded_days || 0
                                color: theme.toolbarTextSecondary
                                horizontalAlignment: Text.AlignRight
                            }
                        }
                    }

                }

                Label {
                    objectName: "timelinePlacesEmptyState"
                    anchors.left: parent.left
                    anchors.right: parent.right
                    anchors.top: parent.top
                    anchors.bottom: parent.bottom
                    anchors.leftMargin: 16
                    anchors.rightMargin: 16
                    anchors.topMargin: details.compact ? 48 : 40
                    visible: placesList.count === 0
                    text: details.filterText === "" ? "No administrative places were resolved for this period." : "No places match this filter."
                    color: theme.secondaryText
                    horizontalAlignment: Text.AlignHCenter
                    verticalAlignment: Text.AlignVCenter
                    wrapMode: Text.WordWrap
                }
            }

            Label {
                objectName: "timelineDetailsAttribution"
                Layout.fillWidth: true
                text: details.result && details.result.dataset ? "Recorded observations only · " + details.result.dataset.attribution + " · " + details.result.dataset.license : ""
                color: theme.secondaryText
                font.pixelSize: theme.scale(0)
                wrapMode: Text.WordWrap
            }
        }
    }
}
