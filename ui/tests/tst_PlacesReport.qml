import QtQuick 2.15
import QtQuick.Controls 2.15
import QtTest 1.2
import "../components"

TestCase {
    name: "PlacesReport"

    Component {
        id: fakeApiComponent
        QtObject {
            property int statusCalls: 0
            property bool lastIncludedResult: false
            signal placeReportSubmitted(string requestId, string jobId)
            signal placeReportSubmitFailed(string requestId, string errorMessage)
            signal placeReportStatusFetched(string jobId, var status)
            signal placeReportStatusFailed(string jobId, string errorMessage, bool includedResult)
            signal importCompleted(var summary, var params)
            signal geodataStatusFetched(var status)
            signal geodataInstallAccepted(string generationId)
            signal geodataInstallFailed(string generationId, string errorMessage)

            function getPlaceReport(jobId, includeResult) {
                statusCalls++;
                lastIncludedResult = includeResult === true;
            }
            function cancelPlaceReport(jobId) {}
            function submitPlaceReport(startDate, endDate, requestId) {}
            function getGeodataStatus() {}
        }
    }

    Component {
        id: reportComponent
        PlacesReport {
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
        id: reportWindowComponent
        ApplicationWindow {
            width: 1000
            height: 700
            visible: true
            property alias report: visibleReport

            PlacesReport {
                id: visibleReport
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

    function test_detailsSheetTogglesOverJourney() {
        var window = createTemporaryObject(reportWindowComponent, null);
        verify(window !== null);
        var report = window.report;
        compare(report.currentView, 0);
        verify(report.timelineView.visible);
        verify(report.timelineView.controlsVisible);
        verify(!report.detailsPanel.visible);

        window.requestActivate();
        tryVerify(function() { return window.active; });
        mouseClick(report.detailsButton, report.detailsButton.width / 2, report.detailsButton.height / 2, Qt.LeftButton);
        compare(report.currentView, 1);
        verify(report.timelineView.visible);
        verify(!report.timelineView.controlsVisible);
        verify(report.detailsPanel.visible);
        compare(report.detailsButton.text, "Journey");
        var yearPill = findChild(report, "placesYearPill");
        verify(yearPill !== null);
        verify(!yearPill.visible);

        mouseClick(report.detailsButton, report.detailsButton.width / 2, report.detailsButton.height / 2, Qt.LeftButton);
        compare(report.currentView, 0);
        verify(report.timelineView.controlsVisible);
        verify(!report.detailsPanel.visible);
        compare(report.detailsButton.text, "Details");
    }

    function test_completedResultFetchCanRetryAfterFailure() {
        var fakeApi = createTemporaryObject(fakeApiComponent, null);
        verify(fakeApi !== null);
        var report = createTemporaryObject(reportComponent, null, {
            api: fakeApi,
            jobId: "job-1",
            jobState: "running",
            result: null
        });
        verify(report !== null);

        fakeApi.placeReportStatusFetched("job-1", { state: "completed" });
        compare(report.jobState, "completed");
        compare(fakeApi.statusCalls, 1);
        verify(fakeApi.lastIncludedResult);
        verify(report.resultRequested);
        verify(report.statusRequestPending);

        fakeApi.placeReportStatusFailed("job-1", "timeout", true);
        compare(report.jobState, "completed");
        verify(!report.resultRequested);
        verify(!report.statusRequestPending);
    }

    function test_resultFetchRetriesAndStopsOnMissingJob() {
        var fakeApi = createTemporaryObject(fakeApiComponent, null);
        verify(fakeApi !== null);
        var report = createTemporaryObject(reportComponent, null, {
            api: fakeApi,
            jobId: "job-2",
            jobState: "completed",
            submittedStartDate: "2024-01-01",
            submittedEndDate: "2024-12-31",
            result: null
        });
        verify(report !== null);

        report.active = true;
        tryCompare(fakeApi, "statusCalls", 1);
        fakeApi.placeReportStatusFailed("job-2", "timeout", true);
        tryCompare(fakeApi, "statusCalls", 2, 1500);

        fakeApi.placeReportStatusFailed("job-2", "HTTP 404 place report not found", true);
        compare(report.jobState, "failed");
        wait(1100);
        compare(fakeApi.statusCalls, 2);
    }

    function test_importInvalidatesCachedReport() {
        var fakeApi = createTemporaryObject(fakeApiComponent, null);
        verify(fakeApi !== null);
        var report = createTemporaryObject(reportComponent, null, {
            api: fakeApi,
            jobId: "stale-job",
            jobState: "completed",
            resultStartDate: "2024-01-01",
            resultEndDate: "2024-12-31"
        });
        verify(report !== null);
        verify(report.result !== null);

        fakeApi.importCompleted({ count: 1, files: 1 }, { dir: "/tmp/import" });

        compare(report.jobId, "");
        compare(report.jobState, "idle");
        compare(report.result, null);
        compare(report.resultStartDate, "");
        compare(report.resultEndDate, "");
    }

    function test_emptyImportKeepsCachedReport() {
        var fakeApi = createTemporaryObject(fakeApiComponent, null);
        verify(fakeApi !== null);
        var report = createTemporaryObject(reportComponent, null, {
            api: fakeApi,
            jobId: "cached-job",
            jobState: "completed",
            resultStartDate: "2024-01-01",
            resultEndDate: "2024-12-31"
        });
        verify(report !== null);
        var cachedResult = report.result;

        fakeApi.importCompleted({ count: 0, files: 0, skipped: 2 }, { dir: "/tmp/import" });

        compare(report.jobId, "cached-job");
        compare(report.jobState, "completed");
        compare(report.result, cachedResult);
    }

    function test_nullStatusDoesNotCrashHandler() {
        var fakeApi = createTemporaryObject(fakeApiComponent, null);
        verify(fakeApi !== null);
        var report = createTemporaryObject(reportComponent, null, {
            api: fakeApi,
            jobId: "job-null",
            jobState: "running",
            statusRequestPending: true
        });
        verify(report !== null);

        fakeApi.placeReportStatusFetched("job-null", null);

        compare(report.jobState, "running");
        verify(!report.statusRequestPending);
        compare(report.errorMessage, "Report status response was invalid.");
    }
}
