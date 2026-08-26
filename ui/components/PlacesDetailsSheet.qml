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
    readonly property bool compact: width < 640
    readonly property bool shortLayout: height < 520

    signal closeRequested
    signal refreshRequested

    visible: open && result !== null
    focus: visible
    Accessible.role: Accessible.Dialog
    Accessible.name: "Places report details"
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

    function movementSummary() {
        var threshold = Math.round(result && result.journey_separation_meters ? result.journey_separation_meters : 100);
        var text = threshold + " m significant-stop threshold";
        if (result && result.journey_truncated)
            text += " · journey limited";
        return text;
    }

    function observationSummary() {
        var summary = result && result.summary ? result.summary : {};
        return "Recorded " + (summary.recorded_observations || 0)
                + " · Resolved " + (summary.resolved_observations || 0)
                + " · Outside boundaries " + (summary.unresolved_observations || 0)
                + " · Invalid coordinates " + (summary.invalid_coordinates || 0);
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
        objectName: "placesDetailsSheet"
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
            objectName: "placesDetailsContent"
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
                    Accessible.name: "Refresh places report"
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
                id: reportMetadata
                objectName: "placesDetailsMetadata"
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
                        { label: "Recorded", value: details.result.summary.recorded_observations || 0 },
                        { label: "Resolved", value: details.result.summary.resolved_observations || 0 },
                        { label: "Outside boundaries", value: details.result.summary.unresolved_observations || 0 },
                        { label: "Invalid coordinates", value: details.result.summary.invalid_coordinates || 0 }
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
                objectName: "placesDetailsShortSummary"
                Layout.fillWidth: true
                visible: details.shortLayout
                text: details.observationSummary()
                color: theme.secondaryText
                font.pixelSize: theme.scale(0)
                wrapMode: Text.WordWrap
            }

            Rectangle {
                Layout.fillWidth: true
                Layout.fillHeight: true
                Layout.minimumHeight: details.shortLayout ? 72 : 0
                radius: 12
                color: Qt.rgba(theme.background.r, theme.background.g, theme.background.b, 0.5)

                ListView {
                    id: placesList
                    objectName: "placesDetailsList"
                    anchors.fill: parent
                    clip: true
                    model: details.result ? details.result.places : []
                    spacing: 1

                    header: Rectangle {
                        width: placesList.width
                        height: 40
                        color: theme.toolbarBackgroundHover

                        RowLayout {
                            anchors.fill: parent
                            anchors.leftMargin: 14
                            anchors.rightMargin: 14

                            Label {
                                Layout.fillWidth: true
                                text: "Administrative place"
                                color: theme.toolbarText
                                font.bold: true
                            }

                            Label {
                                Layout.preferredWidth: details.compact ? 86 : 110
                                text: details.compact ? "Recorded" : "Observations"
                                color: theme.toolbarTextSecondary
                                horizontalAlignment: Text.AlignRight
                            }

                            Label {
                                Layout.preferredWidth: 70
                                visible: !details.compact
                                text: "Days"
                                color: theme.toolbarTextSecondary
                                horizontalAlignment: Text.AlignRight
                            }
                        }
                    }

                    delegate: Rectangle {
                        id: placeRow
                        required property var modelData
                        required property int index
                        width: placesList.width
                        height: details.compact ? 58 : 62
                        color: placeRow.index % 2 === 0 ? "transparent" : Qt.rgba(theme.toolbarBackgroundHover.r, theme.toolbarBackgroundHover.g, theme.toolbarBackgroundHover.b, 0.42)

                        RowLayout {
                            anchors.fill: parent
                            anchors.leftMargin: 14
                            anchors.rightMargin: 14
                            spacing: 10

                            ColumnLayout {
                                Layout.fillWidth: true
                                spacing: 1

                                Label {
                                    Layout.fillWidth: true
                                    text: placeRow.modelData.locality || placeRow.modelData.local_admin || placeRow.modelData.county || placeRow.modelData.region || placeRow.modelData.country || "Unnamed administrative area"
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
                                    text: placeRow.modelData.recorded_observations + " obs"
                                    color: theme.toolbarText
                                    font.bold: true
                                    horizontalAlignment: Text.AlignRight
                                }

                                Label {
                                    objectName: "compactDays-" + placeRow.index
                                    Layout.fillWidth: true
                                    text: placeRow.modelData.recorded_days + " days"
                                    color: theme.toolbarTextSecondary
                                    font.pixelSize: theme.scale(0)
                                    horizontalAlignment: Text.AlignRight
                                }
                            }

                            Label {
                                Layout.preferredWidth: 110
                                visible: !details.compact
                                text: placeRow.modelData.recorded_observations
                                color: theme.toolbarText
                                font.bold: true
                                horizontalAlignment: Text.AlignRight
                            }

                            Label {
                                Layout.preferredWidth: 70
                                visible: !details.compact
                                text: placeRow.modelData.recorded_days
                                color: theme.toolbarTextSecondary
                                horizontalAlignment: Text.AlignRight
                            }
                        }
                    }

                    Label {
                        anchors.centerIn: parent
                        visible: placesList.count === 0
                        width: Math.min(420, parent.width - 32)
                        text: "No administrative places were resolved for this period."
                        color: theme.secondaryText
                        horizontalAlignment: Text.AlignHCenter
                        wrapMode: Text.WordWrap
                    }
                }
            }

            Label {
                objectName: "placesDetailsAttribution"
                Layout.fillWidth: true
                text: details.result && details.result.dataset ? "Recorded observations only · " + details.result.dataset.attribution + " · " + details.result.dataset.license : ""
                color: theme.secondaryText
                font.pixelSize: theme.scale(0)
                wrapMode: Text.WordWrap
            }
        }
    }
}
