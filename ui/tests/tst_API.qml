import QtQuick 2.15
import QtTest 1.2
import "../services"

TestCase {
    name: "API"

    Component {
        id: apiComponent
        API {
            apiPort: -1
        }
    }

    Component {
        id: statusSpyComponent
        SignalSpy {}
    }

    function test_offlinePlaceReportStatusIsReportedAsFailure() {
        var api = createTemporaryObject(apiComponent, null);
        verify(api !== null);
        var fetchedSpy = createTemporaryObject(statusSpyComponent, null, {
            target: api,
            signalName: "placeReportStatusFetched"
        });
        var failedSpy = createTemporaryObject(statusSpyComponent, null, {
            target: api,
            signalName: "placeReportStatusFailed"
        });
        verify(fetchedSpy !== null);
        verify(failedSpy !== null);

        api.getPlaceReport("job-1", false);

        compare(fetchedSpy.count, 0);
        compare(failedSpy.count, 1);
        compare(failedSpy.signalArguments[0][0], "job-1");
        compare(failedSpy.signalArguments[0][1], "invalid place report status response");
        compare(failedSpy.signalArguments[0][2], false);
    }
}
