pragma ComponentBehavior: Bound
import QtQuick 2.15
import QtQuick.Controls 6.5
import QtQuick.Layouts 1.15
import "../themes"
import "../lib/MapViewLogic.js" as MapViewLogic

Popup {
    id: picker

    signal rangeApplied(string startDate, string endDate)

    property string startDateKey: ""
    property string endDateKey: ""
    property bool rangeEndPending: false
    property int currentTab: 0
    property int visibleMonth: new Date().getMonth()
    property int visibleYear: new Date().getFullYear()

    width: 344
    height: 480
    padding: 16
    modal: false
    focus: true
    closePolicy: Popup.CloseOnEscape | Popup.CloseOnPressOutside

    ThemeLoader {
        id: theme
    }

    function pad(value) {
        return value < 10 ? "0" + value : "" + value;
    }

    function dateKey(date) {
        if (!date || isNaN(date.getTime()))
            return "";
        return date.getFullYear() + "-" + pad(date.getMonth() + 1) + "-" + pad(date.getDate());
    }

    function dateFromKey(key) {
        if (!key || key.length !== 10)
            return null;
        var parts = key.split("-");
        if (parts.length !== 3)
            return null;
        var year = Number(parts[0]);
        var month = Number(parts[1]) - 1;
        var day = Number(parts[2]);
        var date = new Date(Date.UTC(year, month, day));
        if (isNaN(date.getTime()) || date.getUTCFullYear() !== year || date.getUTCMonth() !== month || date.getUTCDate() !== day)
            return null;
        return date;
    }

    function setRange(startDate, endDate) {
        startDateKey = startDate || "";
        endDateKey = endDate || startDateKey;
        rangeEndPending = false;

        var selected = dateFromKey(startDateKey);
        if (selected) {
            visibleMonth = selected.getUTCMonth();
            visibleYear = selected.getUTCFullYear();
        } else {
            var today = new Date();
            visibleMonth = today.getMonth();
            visibleYear = today.getFullYear();
        }
    }

    function setSelection(startDate, endDate) {
        startDateKey = startDate;
        endDateKey = endDate || startDate;
        rangeEndPending = false;
        var selected = dateFromKey(startDateKey);
        if (selected) {
            visibleMonth = selected.getUTCMonth();
            visibleYear = selected.getUTCFullYear();
        }
    }

    function applyPreset(preset) {
        var range = MapViewLogic.datePresetRange(preset);
        if (!range)
            return;
        setSelection(range.start, range.end);
        rangeApplied(range.start, range.end);
        close();
    }

    function selectDate(date) {
        var key = dateKey(date);
        if (key === "")
            return;

        if (!rangeEndPending) {
            startDateKey = key;
            endDateKey = key;
            rangeEndPending = true;
            visibleMonth = date.getMonth();
            visibleYear = date.getFullYear();
            return;
        }

        if (key < startDateKey) {
            endDateKey = startDateKey;
            startDateKey = key;
        } else {
            endDateKey = key;
        }
        rangeEndPending = false;
        visibleMonth = date.getMonth();
        visibleYear = date.getFullYear();
    }

    function changeMonth(offset) {
        var next = new Date(visibleYear, visibleMonth + offset, 1);
        visibleMonth = next.getMonth();
        visibleYear = next.getFullYear();
    }

    function selectionSummary() {
        if (startDateKey === "")
            return "Choose a day";
        if (endDateKey !== "" && endDateKey !== startDateKey)
            return startDateKey + " to " + endDateKey;
        return rangeEndPending ? startDateKey + "  ·  click another day for a range" : startDateKey;
    }

    function isInSelection(key) {
        if (startDateKey === "" || key === "")
            return false;
        var end = endDateKey !== "" ? endDateKey : startDateKey;
        return key >= startDateKey && key <= end;
    }

    function isSelectionEdge(key) {
        return key !== "" && (key === startDateKey || key === endDateKey);
    }

    component PresetButton: Button {
        id: presetButton
        property string preset

        Layout.fillWidth: true
        Layout.preferredHeight: 36
        onClicked: picker.applyPreset(preset)

        contentItem: Text {
            text: presetButton.text
            color: theme.toolbarText
            font.pixelSize: theme.scale(1)
            horizontalAlignment: Text.AlignHCenter
            verticalAlignment: Text.AlignVCenter
        }

        background: Rectangle {
            radius: 5
            color: presetButton.down ? theme.addWaypointDialog.button.backgroundPressed : (presetButton.hovered ? theme.addWaypointDialog.button.background : theme.toolbarBackgroundHover)
            border.color: theme.toolbarSeparator
        }
    }

    background: Rectangle {
        radius: 12
        color: theme.toolbarBackground
        border.color: theme.toolbarBorder
        border.width: 1
    }

    contentItem: ColumnLayout {
        spacing: 8

        Label {
            text: "Where was I?"
            color: theme.toolbarText
            font.bold: true
            font.pixelSize: theme.scale(3)
        }

        TabBar {
            id: pickerTabs
            Layout.fillWidth: true
            Layout.preferredHeight: 34
            currentIndex: picker.currentTab
            onCurrentIndexChanged: picker.currentTab = currentIndex
            background: Rectangle {
                color: "transparent"
            }

            TabButton {
                id: calendarTab
                text: "Calendar"
                contentItem: Text {
                    text: calendarTab.text
                    color: theme.toolbarText
                    font.bold: calendarTab.checked
                    horizontalAlignment: Text.AlignHCenter
                    verticalAlignment: Text.AlignVCenter
                }
                background: Rectangle {
                    radius: 5
                    color: calendarTab.checked ? theme.toolbarBackgroundHover : "transparent"
                    border.color: calendarTab.checked ? theme.toolbarSeparator : "transparent"
                }
            }

            TabButton {
                id: quickPicksTab
                text: "Quick picks"
                contentItem: Text {
                    text: quickPicksTab.text
                    color: theme.toolbarText
                    font.bold: quickPicksTab.checked
                    horizontalAlignment: Text.AlignHCenter
                    verticalAlignment: Text.AlignVCenter
                }
                background: Rectangle {
                    radius: 5
                    color: quickPicksTab.checked ? theme.toolbarBackgroundHover : "transparent"
                    border.color: quickPicksTab.checked ? theme.toolbarSeparator : "transparent"
                }
            }
        }

        Label {
            Layout.fillWidth: true
            Layout.topMargin: 8
            Layout.bottomMargin: 4
            text: picker.selectionSummary()
            color: picker.startDateKey !== "" ? theme.toolbarText : theme.toolbarTextSecondary
            font.pixelSize: theme.scale(2)
            horizontalAlignment: Text.AlignHCenter
            elide: Text.ElideRight
        }

        StackLayout {
            Layout.fillWidth: true
            Layout.fillHeight: true
            currentIndex: picker.currentTab

            Item {
                ColumnLayout {
                    anchors.fill: parent
                    spacing: 4

                    RowLayout {
                        Layout.fillWidth: true

                        ToolButton {
                            id: previousMonthButton
                            text: "‹"
                            Accessible.name: "Previous month"
                            onClicked: picker.changeMonth(-1)
                            contentItem: Text {
                                text: previousMonthButton.text
                                color: theme.toolbarText
                                font.pixelSize: theme.scale(4)
                                horizontalAlignment: Text.AlignHCenter
                                verticalAlignment: Text.AlignVCenter
                            }
                        }

                        Label {
                            Layout.fillWidth: true
                            text: Qt.locale().monthName(picker.visibleMonth, Locale.LongFormat) + " " + picker.visibleYear
                            color: theme.toolbarText
                            font.bold: true
                            horizontalAlignment: Text.AlignHCenter
                        }

                        ToolButton {
                            id: nextMonthButton
                            text: "›"
                            Accessible.name: "Next month"
                            onClicked: picker.changeMonth(1)
                            contentItem: Text {
                                text: nextMonthButton.text
                                color: theme.toolbarText
                                font.pixelSize: theme.scale(4)
                                horizontalAlignment: Text.AlignHCenter
                                verticalAlignment: Text.AlignVCenter
                            }
                        }
                    }

                    DayOfWeekRow {
                        id: weekDays
                        Layout.fillWidth: true
                        locale: Qt.locale()
                        delegate: Text {
                            required property string shortName
                            width: weekDays.width / 7
                            height: 24
                            text: shortName
                            color: theme.toolbarTextSecondary
                            font.bold: true
                            font.pixelSize: theme.scale(1)
                            horizontalAlignment: Text.AlignHCenter
                            verticalAlignment: Text.AlignVCenter
                        }
                    }

                    MonthGrid {
                        id: monthGrid
                        Layout.fillWidth: true
                        Layout.fillHeight: true
                        month: picker.visibleMonth
                        year: picker.visibleYear
                        locale: Qt.locale()
                        spacing: 2

                        delegate: Rectangle {
                            id: dayCell
                            required property var model
                            readonly property string key: picker.dateKey(model.date)
                            readonly property bool currentMonth: model.month === monthGrid.month

                            implicitWidth: (monthGrid.width - monthGrid.spacing * 6) / 7
                            implicitHeight: (monthGrid.height - monthGrid.spacing * 5) / 6
                            radius: 5
                            color: picker.isSelectionEdge(key) ? theme.addWaypointDialog.button.background : (picker.isInSelection(key) ? theme.toolbarBackgroundHover : "transparent")
                            opacity: currentMonth ? 1 : 0.25

                            Text {
                                anchors.centerIn: parent
                                text: dayCell.model.day
                                color: picker.isInSelection(dayCell.key) ? theme.addWaypointDialog.button.text : theme.toolbarText
                                font.bold: picker.isSelectionEdge(dayCell.key)
                                font.pixelSize: theme.scale(1)
                            }

                            MouseArea {
                                anchors.fill: parent
                                cursorShape: Qt.PointingHandCursor
                                onClicked: picker.selectDate(dayCell.model.date)
                            }
                        }
                    }

                    Label {
                        Layout.fillWidth: true
                        text: "Waypoint dates are matched in UTC."
                        color: theme.toolbarTextSecondary
                        font.pixelSize: theme.scale(1)
                        horizontalAlignment: Text.AlignHCenter
                    }
                }
            }

            Item {
                ColumnLayout {
                    anchors.fill: parent
                    spacing: 8

                    Label {
                        Layout.fillWidth: true
                        text: "Single days"
                        color: theme.toolbarTextSecondary
                        font.bold: true
                    }

                    GridLayout {
                        Layout.fillWidth: true
                        columns: 2
                        columnSpacing: 8
                        rowSpacing: 8

                        PresetButton {
                            text: "Today"
                            preset: "today"
                        }
                        PresetButton {
                            text: "Yesterday"
                            preset: "yesterday"
                        }
                        PresetButton {
                            text: "This day, last year"
                            preset: "last-year"
                            Layout.columnSpan: 2
                        }
                    }

                    Item {
                        Layout.preferredHeight: 4
                    }

                    Label {
                        Layout.fillWidth: true
                        text: "Ranges"
                        color: theme.toolbarTextSecondary
                        font.bold: true
                    }

                    GridLayout {
                        Layout.fillWidth: true
                        columns: 2
                        columnSpacing: 8
                        rowSpacing: 8

                        PresetButton {
                            text: "Last 7 days"
                            preset: "last-7"
                        }
                        PresetButton {
                            text: "Last 30 days"
                            preset: "last-30"
                        }
                        PresetButton {
                            text: "This month"
                            preset: "this-month"
                        }
                        PresetButton {
                            text: "Last month"
                            preset: "last-month"
                        }
                    }

                    Item {
                        Layout.fillHeight: true
                    }

                    Label {
                        Layout.fillWidth: true
                        text: "Presets use UTC dates and remain editable in Calendar."
                        wrapMode: Text.WordWrap
                        color: theme.toolbarTextSecondary
                        font.pixelSize: theme.scale(1)
                        horizontalAlignment: Text.AlignHCenter
                    }
                }
            }
        }

        RowLayout {
            Layout.fillWidth: true

            Item {
                Layout.fillWidth: true
            }

            Button {
                id: applyButton
                text: "Show waypoints"
                visible: picker.currentTab === 0
                enabled: picker.startDateKey !== ""
                Layout.preferredWidth: 150
                Layout.preferredHeight: 34
                contentItem: Text {
                    text: applyButton.text
                    color: theme.addWaypointDialog.button.text
                    font.bold: true
                    horizontalAlignment: Text.AlignHCenter
                    verticalAlignment: Text.AlignVCenter
                }
                background: Rectangle {
                    radius: 5
                    color: applyButton.down ? theme.addWaypointDialog.button.backgroundPressed : (applyButton.hovered ? theme.toolbarBackgroundHover : theme.addWaypointDialog.button.background)
                    border.color: theme.addWaypointDialog.button.border
                    opacity: applyButton.enabled ? 1 : 0.45
                }
                onClicked: {
                    var end = picker.endDateKey !== "" ? picker.endDateKey : picker.startDateKey;
                    picker.rangeApplied(picker.startDateKey, end);
                    picker.close();
                }
            }
        }
    }
}
