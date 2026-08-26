// qmllint disable unqualified
import QtQuick 2.15
import QtQuick.Controls 2.15
import QtQuick.Layouts 1.15

ApplicationWindow {
    id: window

    visible: true
    width: 1300
    height: 800
    minimumWidth: 360
    minimumHeight: 470
    title: qsTr("WhereAmI - GPX Waypoint Viewer")
    flags: Qt.FramelessWindowHint
    color: "transparent"

    property string timelineStartDate: ""
    property string timelineEndDate: ""
    property string apiToken: typeof whereamiApiToken !== "undefined" ? whereamiApiToken : ""
    property alias mapPage: mapView
    property alias timelinePage: timelinePageItem
    property alias currentPage: pages.currentIndex
    readonly property int currentUTCYear: new Date().getUTCFullYear()

    function openTimeline(startDate, endDate) {
        var alreadyOpen = pages.currentIndex === 1;
        timelineStartDate = startDate;
        timelineEndDate = endDate || startDate;
        if (alreadyOpen)
            timelinePageItem.beginTimeline();
        else
            pages.currentIndex = 1;
    }

    function openTimelineYear(year) {
        var selectedYear = Math.floor(Number(year));
        if (!isFinite(selectedYear) || selectedYear < 1 || selectedYear > currentUTCYear)
            selectedYear = currentUTCYear;
        openTimeline(selectedYear + "-01-01", selectedYear + "-12-31");
    }

    function openCurrentYearTimeline() {
        openTimelineYear(currentUTCYear);
    }

    function quitApplication() {
        Qt.quit();
    }

    StackLayout {
        id: pages
        anchors.fill: parent
        currentIndex: 0

        MapView {
            id: mapView
            hostWindow: window
            apiToken: window.apiToken
            active: pages.currentIndex === 0
            onTimelineRequested: window.openCurrentYearTimeline()
            onQuitRequested: window.quitApplication()
        }

        TimelinePage {
            id: timelinePageItem
            hostWindow: window
            api: mapView.apiService
            active: pages.currentIndex === 1
            startDate: window.timelineStartDate
            endDate: window.timelineEndDate
            onBackRequested: pages.currentIndex = 0
            onQuitRequested: window.quitApplication()
            onYearRequested: function (year) {
                window.openTimelineYear(year);
            }
        }
    }
}
