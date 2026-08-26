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

    function test_timelineNavigationPreservesMapState() {
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

        application.openTimeline("2024-01-01", "2024-01-31");
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

    function test_timelineNavigationUsesYear() {
        var application = createTemporaryObject(mainComponent, null);
        verify(application !== null);
        application.mapPage.apiService.apiPort = -1;
        wait(100);

        application.mapPage.mapToolbar.timelineNavigationButton.clicked();
        compare(application.currentPage, 1);
        compare(application.timelineStartDate, application.currentUTCYear + "-01-01");
        compare(application.timelineEndDate, application.currentUTCYear + "-12-31");
        compare(application.timelinePage.selectedYear, application.currentUTCYear);

        application.timelinePage.yearRequested(application.currentUTCYear - 1);
        compare(application.timelineStartDate, (application.currentUTCYear - 1) + "-01-01");
        compare(application.timelineEndDate, (application.currentUTCYear - 1) + "-12-31");
        compare(application.timelinePage.selectedYear, application.currentUTCYear - 1);

        application.timelinePage.yearRequested(application.currentUTCYear);
        compare(application.timelineStartDate, application.currentUTCYear + "-01-01");
        compare(application.timelineEndDate, application.currentUTCYear + "-12-31");
    }

    function test_escapeClosesDetailsWithoutMapShortcutConflict() {
        var application = createTemporaryObject(mainComponent, null, { visible: true });
        verify(application !== null);
        application.mapPage.apiService.apiPort = -1;
        application.currentPage = 1;
        application.timelinePage.result = {
            summary: {},
            places: [],
            timeline: []
        };
        application.timelinePage.currentView = 1;
        verify(!application.mapPage.active);
        verify(application.timelinePage.detailsPanel.visible);

        application.requestActivate();
        tryVerify(function() { return application.active; });
        application.timelinePage.detailsPanel.forceActiveFocus();
        tryVerify(function() { return application.timelinePage.detailsPanel.activeFocus; });
        keyClick(Qt.Key_Escape);

        compare(application.timelinePage.currentView, 0);
        compare(application.currentPage, 1);
        verify(application.visible);
        tryVerify(function() { return application.timelinePage.detailsButton.activeFocus; });
    }
}
