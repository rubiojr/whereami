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

    property string reportStartDate: ""
    property string reportEndDate: ""
    property string apiToken: typeof whereamiApiToken !== "undefined" ? whereamiApiToken : ""
    property alias mapPage: mapView
    property alias placesPage: placesReport
    property alias currentPage: pages.currentIndex
    readonly property int currentUTCYear: new Date().getUTCFullYear()

    function openPlacesReport(startDate, endDate) {
        var alreadyOpen = pages.currentIndex === 1;
        reportStartDate = startDate;
        reportEndDate = endDate || startDate;
        if (alreadyOpen)
            placesReport.beginReport();
        else
            pages.currentIndex = 1;
    }

    function openPlacesYear(year) {
        var selectedYear = Math.floor(Number(year));
        if (!isFinite(selectedYear) || selectedYear < 1 || selectedYear > currentUTCYear)
            selectedYear = currentUTCYear;
        openPlacesReport(selectedYear + "-01-01", selectedYear + "-12-31");
    }

    function openCurrentYearPlaces() {
        openPlacesYear(currentUTCYear);
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
            onPlacesRequested: window.openCurrentYearPlaces()
        }

        PlacesReport {
            id: placesReport
            hostWindow: window
            api: mapView.apiService
            active: pages.currentIndex === 1
            startDate: window.reportStartDate
            endDate: window.reportEndDate
            onBackRequested: pages.currentIndex = 0
            onYearRequested: function (year) {
                window.openPlacesYear(year);
            }
        }
    }
}
