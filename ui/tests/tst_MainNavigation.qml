import QtQuick 2.15
import QtTest 1.2
import "../components"

TestCase {
    name: "MainNavigation"

    Component {
        id: mainComponent
        Main {
            visible: false
            apiToken: "test-token"
        }
    }

    function test_reportNavigationPreservesMapState() {
        var application = createTemporaryObject(mainComponent, null);
        verify(application !== null);
        application.mapPage.apiService.apiPort = -1;
        wait(200);
        var expectedLatitude = application.mapPage.mapCenter.latitude;
        var expectedLongitude = application.mapPage.mapCenter.longitude;
        var expectedZoom = application.mapPage.mapZoomLevel;
        application.mapPage.dateFilterActive = true;
        application.mapPage.dateFilterStart = "2024-01-01";
        application.mapPage.dateFilterEnd = "2024-01-31";
        application.mapPage.showNonBookmarkWaypoints = false;
        application.mapPage.infoCardVisible = true;
        application.mapPage.waypointTableVisible = true;
        var selected = { name: "kept", lat: 41.3874, lon: 2.1686, bookmark: true, time: "2024-01-15T12:00:00Z" };
        application.mapPage.waypoints = [selected];
        application.mapPage.selectedWaypoint = selected;
        application.mapPage.selectedWaypointIndex = 0;

        application.openPlacesReport("2024-01-01", "2024-01-31");
        compare(application.currentPage, 1);
        application.currentPage = 0;

        compare(application.mapPage.mapCenter.latitude, expectedLatitude);
        compare(application.mapPage.mapCenter.longitude, expectedLongitude);
        compare(application.mapPage.mapZoomLevel, expectedZoom);
        verify(application.mapPage.dateFilterActive);
        compare(application.mapPage.dateFilterStart, "2024-01-01");
        compare(application.mapPage.dateFilterEnd, "2024-01-31");
        verify(!application.mapPage.showNonBookmarkWaypoints);
        verify(application.mapPage.infoCardVisible);
        verify(application.mapPage.waypointTableVisible);
        compare(application.mapPage.selectedWaypoint.name, "kept");
        compare(application.mapPage.selectedWaypointIndex, 0);
    }

    function test_placesNavigationUsesYearTimeline() {
        var application = createTemporaryObject(mainComponent, null);
        verify(application !== null);
        application.mapPage.apiService.apiPort = -1;
        wait(100);

        application.mapPage.mapToolbar.placesNavigationButton.clicked();
        compare(application.currentPage, 1);
        compare(application.reportStartDate, application.currentUTCYear + "-01-01");
        compare(application.reportEndDate, application.currentUTCYear + "-12-31");
        compare(application.placesPage.selectedYear, application.currentUTCYear);

        application.placesPage.yearRequested(application.currentUTCYear - 1);
        compare(application.reportStartDate, (application.currentUTCYear - 1) + "-01-01");
        compare(application.reportEndDate, (application.currentUTCYear - 1) + "-12-31");
        compare(application.placesPage.selectedYear, application.currentUTCYear - 1);

        application.placesPage.yearRequested(application.currentUTCYear);
        compare(application.reportStartDate, application.currentUTCYear + "-01-01");
        compare(application.reportEndDate, application.currentUTCYear + "-12-31");
    }

    function test_escapeClosesDetailsWithoutMapShortcutConflict() {
        var application = createTemporaryObject(mainComponent, null, { visible: true });
        verify(application !== null);
        application.mapPage.apiService.apiPort = -1;
        application.currentPage = 1;
        application.placesPage.result = {
            summary: {},
            places: [],
            timeline: []
        };
        application.placesPage.currentView = 1;
        verify(!application.mapPage.active);
        verify(application.placesPage.detailsPanel.visible);

        application.requestActivate();
        tryVerify(function() { return application.active; });
        application.placesPage.detailsPanel.forceActiveFocus();
        tryVerify(function() { return application.placesPage.detailsPanel.activeFocus; });
        keyClick(Qt.Key_Escape);

        compare(application.placesPage.currentView, 0);
        compare(application.currentPage, 1);
        verify(application.visible);
        tryVerify(function() { return application.placesPage.detailsButton.activeFocus; });
    }
}
