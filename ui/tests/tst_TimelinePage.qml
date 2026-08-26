pragma ComponentBehavior: Bound

import QtQuick 2.15
import QtQuick.Controls 2.15
import QtTest 1.2
import "../components"

TestCase {
    id: testCase
    name: "TimelinePage"

    readonly property var navigationResult: ({
        summary: {},
        places: [
            {
                country: "France",
                country_id: "FR",
                locality: "Paris",
                locality_id: "PAR",
                recorded_observations: 2,
                recorded_days: 1,
                timeline_index: 0,
                last_observation_utc: "2024-01-05T11:00:00Z"
            }
        ],
        timeline: [
            {
                date_utc: "2024-01-05",
                latitude: 48.8566,
                longitude: 2.3522,
                country: "France",
                country_id: "FR",
                locality: "Paris",
                locality_id: "PAR",
                first_observation_utc: "2024-01-05T10:00:00Z",
                last_observation_utc: "2024-01-05T11:00:00Z"
            },
            {
                date_utc: "2024-02-09",
                latitude: 41.3874,
                longitude: 2.1686,
                country: "Spain",
                country_id: "ES",
                locality: "Barcelona",
                locality_id: "BCN",
                first_observation_utc: "2024-02-09T08:00:00Z",
                last_observation_utc: "2024-02-09T19:15:00Z"
            }
        ]
    })

    Component {
        id: fakeApiComponent
        QtObject {
            property int statusCalls: 0
            property bool lastIncludedResult: false
            signal timelineSubmitted(string requestId, string jobId)
            signal timelineSubmitFailed(string requestId, string errorMessage)
            signal timelineStatusFetched(string jobId, var status)
            signal timelineStatusFailed(string jobId, string errorMessage, bool includedResult)
            signal importCompleted(var summary, var params)
            signal geodataStatusFetched(var status)
            signal geodataInstallAccepted(string generationId)
            signal geodataInstallFailed(string generationId, string errorMessage)

            function getTimeline(jobId, includeResult) {
                statusCalls++;
                lastIncludedResult = includeResult === true;
            }
            function cancelTimeline(jobId) {}
            function submitTimeline(startDate, endDate, requestId) {}
            function getGeodataStatus() {}
        }
    }

    Component {
        id: mapToolbarComponent
        MapToolBar {
            width: 1000
        }
    }

    Component {
        id: quitSpyComponent
        SignalSpy {
            signalName: "quitRequested"
        }
    }

    Component {
        id: timelinePageComponent
        TimelinePage {
            width: 1000
            height: 700
            active: false
            startDate: "2024-01-01"
            endDate: "2024-12-31"
            result: ({
                summary: {},
                places: [],
                timeline: []
            })
        }
    }

    Component {
        id: timelineWindowComponent
        ApplicationWindow {
            width: 1000
            height: 700
            visible: true
            property alias timelinePage: visibleTimelinePage

            TimelinePage {
                id: visibleTimelinePage
                anchors.fill: parent
                active: false
                startDate: "2024-01-01"
                endDate: "2024-12-31"
                result: ({
                    summary: {},
                    places: [],
                    timeline: []
                })
            }
        }
    }

    function test_detailsSheetTogglesOverTimeline() {
        var window = createTemporaryObject(timelineWindowComponent, null);
        verify(window !== null);
        var timelinePage = window.timelinePage;
        compare(timelinePage.currentView, 0);
        verify(timelinePage.timelineView.visible);
        verify(timelinePage.timelineView.controlsVisible);
        verify(!timelinePage.detailsPanel.visible);

        window.requestActivate();
        tryVerify(function() { return window.active; });
        mouseClick(timelinePage.detailsButton, timelinePage.detailsButton.width / 2, timelinePage.detailsButton.height / 2, Qt.LeftButton);
        compare(timelinePage.currentView, 1);
        verify(timelinePage.timelineView.visible);
        verify(!timelinePage.timelineView.controlsVisible);
        verify(timelinePage.detailsPanel.visible);
        compare(timelinePage.detailsButton.text, "Timeline");
        var yearPill = findChild(timelinePage, "timelineYearPill");
        verify(yearPill !== null);
        verify(!yearPill.visible);

        mouseClick(timelinePage.detailsButton, timelinePage.detailsButton.width / 2, timelinePage.detailsButton.height / 2, Qt.LeftButton);
        compare(timelinePage.currentView, 0);
        verify(timelinePage.timelineView.controlsVisible);
        verify(!timelinePage.detailsPanel.visible);
        compare(timelinePage.detailsButton.text, "Details");
    }

    function test_placeSelectionReturnsToMatchingTimelineStop() {
        var window = createTemporaryObject(timelineWindowComponent, null);
        verify(window !== null);
        var timelinePage = window.timelinePage;
        timelinePage.result = testCase.navigationResult;
        timelinePage.currentView = 1;
        compare(timelinePage.timelineView.currentIndex, 1);
        verify(timelinePage.detailsPanel.visible);
        window.requestActivate();
        tryVerify(function() { return window.active; });
        tryVerify(function() { return findChild(timelinePage.detailsPanel, "timelinePlaceRow-0") !== null; });
        var placeRow = findChild(timelinePage.detailsPanel, "timelinePlaceRow-0");

        mouseClick(placeRow, placeRow.width / 2, placeRow.height / 2, Qt.LeftButton);

        compare(timelinePage.currentView, 0);
        compare(timelinePage.timelineView.currentIndex, 0);
        compare(timelinePage.timelineView.currentStop.locality, "Paris");
        verify(!timelinePage.detailsPanel.visible);

        timelinePage.currentView = 1;
        timelinePage.showPlaceOnTimeline({});
        compare(timelinePage.currentView, 1);
        compare(timelinePage.timelineView.currentIndex, 0);
    }

    function test_quitButtonMatchesMainToolbarAndUsesHostSignal() {
        var window = createTemporaryObject(timelineWindowComponent, null);
        verify(window !== null);
        var timelinePage = window.timelinePage;
        var mapToolbar = createTemporaryObject(mapToolbarComponent, window.contentItem);
        verify(mapToolbar !== null);
        var quitSpy = createTemporaryObject(quitSpyComponent, null, { target: timelinePage });
        verify(quitSpy !== null);

        compare(timelinePage.quitButton.icon.source.toString(), mapToolbar.quitButton.icon.source.toString());
        compare(timelinePage.quitButton.icon.width, mapToolbar.quitButton.icon.width);
        compare(timelinePage.quitButton.icon.height, mapToolbar.quitButton.icon.height);
        verify(Qt.colorEqual(timelinePage.quitButton.icon.color, mapToolbar.quitButton.icon.color));

        timelinePage.quitButton.clicked();
        compare(quitSpy.count, 1);
    }

    function test_completedResultFetchCanRetryAfterFailure() {
        var fakeApi = createTemporaryObject(fakeApiComponent, null);
        verify(fakeApi !== null);
        var timelinePage = createTemporaryObject(timelinePageComponent, null, {
            api: fakeApi,
            jobId: "job-1",
            jobState: "running",
            result: null
        });
        verify(timelinePage !== null);

        fakeApi.timelineStatusFetched("job-1", { state: "completed" });
        compare(timelinePage.jobState, "completed");
        compare(fakeApi.statusCalls, 1);
        verify(fakeApi.lastIncludedResult);
        verify(timelinePage.resultRequested);
        verify(timelinePage.statusRequestPending);

        fakeApi.timelineStatusFailed("job-1", "timeout", true);
        compare(timelinePage.jobState, "completed");
        verify(!timelinePage.resultRequested);
        verify(!timelinePage.statusRequestPending);
    }

    function test_resultFetchRetriesAndStopsOnMissingJob() {
        var fakeApi = createTemporaryObject(fakeApiComponent, null);
        verify(fakeApi !== null);
        var timelinePage = createTemporaryObject(timelinePageComponent, null, {
            api: fakeApi,
            jobId: "job-2",
            jobState: "completed",
            submittedStartDate: "2024-01-01",
            submittedEndDate: "2024-12-31",
            result: null
        });
        verify(timelinePage !== null);

        timelinePage.active = true;
        tryCompare(fakeApi, "statusCalls", 1);
        fakeApi.timelineStatusFailed("job-2", "timeout", true);
        tryCompare(fakeApi, "statusCalls", 2, 1500);

        fakeApi.timelineStatusFailed("job-2", "HTTP 404 timeline not found", true);
        compare(timelinePage.jobState, "failed");
        wait(1100);
        compare(fakeApi.statusCalls, 2);
    }

    function test_importInvalidatesCachedTimeline() {
        var fakeApi = createTemporaryObject(fakeApiComponent, null);
        verify(fakeApi !== null);
        var timelinePage = createTemporaryObject(timelinePageComponent, null, {
            api: fakeApi,
            jobId: "stale-job",
            jobState: "completed",
            resultStartDate: "2024-01-01",
            resultEndDate: "2024-12-31"
        });
        verify(timelinePage !== null);
        verify(timelinePage.result !== null);

        fakeApi.importCompleted({ count: 1, files: 1 }, { dir: "/tmp/import" });

        compare(timelinePage.jobId, "");
        compare(timelinePage.jobState, "idle");
        compare(timelinePage.result, null);
        compare(timelinePage.resultStartDate, "");
        compare(timelinePage.resultEndDate, "");
    }

    function test_emptyImportKeepsCachedTimeline() {
        var fakeApi = createTemporaryObject(fakeApiComponent, null);
        verify(fakeApi !== null);
        var timelinePage = createTemporaryObject(timelinePageComponent, null, {
            api: fakeApi,
            jobId: "cached-job",
            jobState: "completed",
            resultStartDate: "2024-01-01",
            resultEndDate: "2024-12-31"
        });
        verify(timelinePage !== null);
        var cachedResult = timelinePage.result;

        fakeApi.importCompleted({ count: 0, files: 0, skipped: 2 }, { dir: "/tmp/import" });

        compare(timelinePage.jobId, "cached-job");
        compare(timelinePage.jobState, "completed");
        compare(timelinePage.result, cachedResult);
    }

    function test_nullStatusDoesNotCrashHandler() {
        var fakeApi = createTemporaryObject(fakeApiComponent, null);
        verify(fakeApi !== null);
        var timelinePage = createTemporaryObject(timelinePageComponent, null, {
            api: fakeApi,
            jobId: "job-null",
            jobState: "running",
            statusRequestPending: true
        });
        verify(timelinePage !== null);

        fakeApi.timelineStatusFetched("job-null", null);

        compare(timelinePage.jobState, "running");
        verify(!timelinePage.statusRequestPending);
        compare(timelinePage.errorMessage, "Timeline status response was invalid.");
    }
}
