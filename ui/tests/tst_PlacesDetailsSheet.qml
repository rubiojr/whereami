pragma ComponentBehavior: Bound

import QtQuick 2.15
import QtQuick.Controls 2.15
import QtTest 1.2
import "../components"

TestCase {
    id: testCase
    name: "PlacesDetailsSheet"

    readonly property var sampleResult: ({
        summary: {
            recorded_observations: 4821,
            resolved_observations: 4780,
            unresolved_observations: 38,
            invalid_coordinates: 3
        },
        journey_separation_meters: 100,
        journey_truncated: true,
        dataset: {
            attribution: "OpenStreetMap contributors",
            license: "ODbL-1.0"
        },
        places: [
            {
                locality: "Barcelona",
                region: "Catalunya",
                country: "España",
                recorded_observations: 3210,
                recorded_days: 145
            }
        ]
    })

    Component {
        id: sheetWindowComponent
        ApplicationWindow {
            width: 360
            height: 700
            visible: true
            property alias sheet: detailsSheet

            PlacesDetailsSheet {
                id: detailsSheet
                anchors.fill: parent
                open: true
                period: "2026-01-01 to 2026-12-31"
                result: testCase.sampleResult
            }
        }
    }

    Component {
        id: closeSpyComponent
        SignalSpy {
            signalName: "closeRequested"
        }
    }

    function test_compactLayoutKeepsCountsAndJourneyMetadata() {
        var window = createTemporaryObject(sheetWindowComponent, null);
        verify(window !== null);
        var sheet = window.sheet;
        verify(sheet.compact);
        compare(sheet.hierarchy(sampleResult.places[0]), "Barcelona · Catalunya · España");
        verify(sheet.movementSummary().indexOf("100 m significant-stop threshold") >= 0);
        verify(sheet.movementSummary().indexOf("journey limited") >= 0);

        var metadata = findChild(sheet, "placesDetailsMetadata");
        verify(metadata !== null);
        verify(metadata.text.indexOf("100 m significant-stop threshold") >= 0);
        verify(metadata.text.indexOf("journey limited") >= 0);
        verify(!metadata.truncated);

        tryVerify(function() { return findChild(sheet, "compactObservations-0") !== null; });
        var observations = findChild(sheet, "compactObservations-0");
        var days = findChild(sheet, "compactDays-0");
        verify(observations !== null);
        verify(days !== null);
        compare(observations.text, "3210 obs");
        compare(days.text, "145 days");
    }

    function test_escapeAndScrimRequestClose() {
        var window = createTemporaryObject(sheetWindowComponent, null);
        verify(window !== null);
        var sheet = window.sheet;
        var closeSpy = createTemporaryObject(closeSpyComponent, null, { target: sheet });
        verify(closeSpy !== null);

        window.requestActivate();
        tryVerify(function() { return window.active; });
        sheet.forceActiveFocus();
        tryVerify(function() { return sheet.activeFocus; });
        keyClick(Qt.Key_Escape);
        tryCompare(closeSpy, "count", 1);

        mouseClick(sheet, 2, 100, Qt.LeftButton);
        tryCompare(closeSpy, "count", 2);
    }

    function test_shortLayoutKeepsSummaryListAndAttributionVisible() {
        var window = createTemporaryObject(sheetWindowComponent, null, { height: 420 });
        verify(window !== null);
        var sheet = window.sheet;
        verify(sheet.shortLayout);

        var summary = findChild(sheet, "placesDetailsShortSummary");
        var list = findChild(sheet, "placesDetailsList");
        var attribution = findChild(sheet, "placesDetailsAttribution");
        var content = findChild(sheet, "placesDetailsContent");
        verify(summary !== null);
        verify(list !== null);
        verify(attribution !== null);
        verify(content !== null);
        verify(summary.text.indexOf("Recorded 4821") >= 0);
        verify(summary.text.indexOf("Invalid coordinates 3") >= 0);
        verify(list.height >= 72);
        verify(attribution.text.indexOf("OpenStreetMap contributors") >= 0);
        verify(!attribution.truncated);
        verify(attribution.y + attribution.height <= content.height + 0.5);
    }
}
