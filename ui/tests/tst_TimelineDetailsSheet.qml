pragma ComponentBehavior: Bound

import QtQuick 2.15
import QtQuick.Controls 2.15
import QtTest 1.2
import "../components"

TestCase {
    id: testCase
    name: "TimelineDetailsSheet"

    readonly property var sampleResult: ({
        summary: {
            recorded_observations: 4821,
            resolved_observations: 4780,
            unresolved_observations: 38,
            invalid_coordinates: 3
        },
        timeline_stop_separation_meters: 100,
        timeline_truncated: true,
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
                recorded_days: 145,
                timeline_index: 0
            },
            {
                locality: "Paris",
                region: "Île-de-France",
                country: "France",
                recorded_observations: 1200,
                recorded_days: 160,
                timeline_index: 1
            },
            {
                locality: "Madrid",
                region: "Community of Madrid",
                country: "Spain",
                recorded_observations: 370,
                recorded_days: 22,
                timeline_index: 2
            }
        ],
        timeline: [{}, {}, {}]
    })

    Component {
        id: sheetWindowComponent
        ApplicationWindow {
            width: 360
            height: 700
            visible: true
            property alias sheet: detailsSheet

            TimelineDetailsSheet {
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

    Component {
        id: placeSpyComponent
        SignalSpy {
            signalName: "placeRequested"
        }
    }

    function test_compactLayoutKeepsCountsAndTimelineMetadata() {
        var window = createTemporaryObject(sheetWindowComponent, null);
        verify(window !== null);
        var sheet = window.sheet;
        verify(sheet.compact);
        compare(sheet.hierarchy(sampleResult.places[0]), "Barcelona · Catalunya · España");
        verify(sheet.movementSummary().indexOf("100 m significant-stop threshold") >= 0);
        verify(sheet.movementSummary().indexOf("timeline limited") >= 0);

        var metadata = findChild(sheet, "timelineDetailsMetadata");
        verify(metadata !== null);
        verify(metadata.text.indexOf("100 m significant-stop threshold") >= 0);
        verify(metadata.text.indexOf("timeline limited") >= 0);
        verify(!metadata.truncated);

        tryVerify(function() { return findChild(sheet, "compactObservations-0") !== null; });
        var observations = findChild(sheet, "compactObservations-0");
        var days = findChild(sheet, "compactDays-0");
        verify(observations !== null);
        verify(days !== null);
        compare(observations.text, "3210 obs");
        compare(days.text, "145 days");

        var compactDaysSort = findChild(sheet, "timelineCompactDaysSortButton");
        verify(compactDaysSort !== null);
        window.requestActivate();
        tryVerify(function() { return window.active; });
        mouseClick(compactDaysSort, compactDaysSort.width / 2, compactDaysSort.height / 2, Qt.LeftButton);
        compare(sheet.sortColumn, "days");
        compare(sheet.displayedPlaces[0].locality, "Paris");
    }

    function test_filterSortScrollbarAndDistinctPlaceCount() {
        var window = createTemporaryObject(sheetWindowComponent, null, { width: 900 });
        verify(window !== null);
        var sheet = window.sheet;
        compare(sheet.distinctPlaceCount, 3);
        compare(sheet.displayedPlaces.length, 3);
        compare(sheet.displayedPlaces[0].locality, "Barcelona");

        var distinctPlaces = findChild(sheet, "timelineSummaryValue-places");
        var filter = findChild(sheet, "timelinePlaceFilter");
        var nameSort = findChild(sheet, "timelineNameSortButton");
        var observationsSort = findChild(sheet, "timelineObservationsSortButton");
        var daysSort = findChild(sheet, "timelineDaysSortButton");
        var header = findChild(sheet, "timelinePlacesHeader");
        verify(distinctPlaces !== null);
        verify(filter !== null);
        verify(nameSort !== null);
        verify(observationsSort !== null);
        verify(daysSort !== null);
        verify(header !== null);
        compare(distinctPlaces.text, "3");
        compare(sheet.placeScrollBar.policy, ScrollBar.AsNeeded);
        fuzzyCompare(header.color.a, 1, 0.001);

        window.requestActivate();
        tryVerify(function() { return window.active; });
        mouseClick(nameSort, nameSort.width / 2, nameSort.height / 2, Qt.LeftButton);
        compare(sheet.sortColumn, "name");
        verify(sheet.sortAscending);
        compare(sheet.displayedPlaces[0].locality, "Barcelona");
        mouseClick(nameSort, nameSort.width / 2, nameSort.height / 2, Qt.LeftButton);
        verify(!sheet.sortAscending);
        compare(sheet.displayedPlaces[0].locality, "Paris");

        mouseClick(observationsSort, observationsSort.width / 2, observationsSort.height / 2, Qt.LeftButton);
        compare(sheet.sortColumn, "observations");
        verify(!sheet.sortAscending);
        compare(sheet.displayedPlaces[0].locality, "Barcelona");
        mouseClick(observationsSort, observationsSort.width / 2, observationsSort.height / 2, Qt.LeftButton);
        verify(sheet.sortAscending);
        compare(sheet.displayedPlaces[0].locality, "Madrid");
        mouseClick(daysSort, daysSort.width / 2, daysSort.height / 2, Qt.LeftButton);
        compare(sheet.sortColumn, "days");
        verify(!sheet.sortAscending);
        compare(sheet.displayedPlaces[0].locality, "Paris");

        filter.text = "  FRANCE  ";
        tryCompare(sheet.placeList, "count", 1);
        compare(sheet.placeList.count, 1);
        compare(sheet.displayedPlaces[0].locality, "Paris");
        compare(sheet.distinctPlaceCount, 3);
        filter.clear();
        tryCompare(sheet.placeList, "count", 3);

        var manyPlaces = [];
        for (var index = 0; index < 20; index++) {
            manyPlaces.push({
                locality: "Place " + index,
                country: "Test",
                recorded_observations: 20 - index,
                recorded_days: index + 1
            });
        }
        sheet.result = {
            summary: {},
            places: manyPlaces,
            dataset: {}
        };
        tryVerify(function() { return sheet.placeList.contentHeight > sheet.placeList.height; });
        tryVerify(function() { return sheet.placeScrollBar.size < 1; });
    }

    function test_placeRowRequestsTimelineNavigation() {
        var window = createTemporaryObject(sheetWindowComponent, null, { width: 900 });
        verify(window !== null);
        var sheet = window.sheet;
        var placeSpy = createTemporaryObject(placeSpyComponent, null, { target: sheet });
        verify(placeSpy !== null);
        window.requestActivate();
        tryVerify(function() { return window.active; });
        tryVerify(function() { return findChild(sheet, "timelinePlaceRow-0") !== null; });
        var firstRow = findChild(sheet, "timelinePlaceRow-0");

        mouseClick(firstRow, firstRow.width / 2, firstRow.height / 2, Qt.LeftButton);

        tryCompare(placeSpy, "count", 1);
        compare(placeSpy.signalArguments[0][0].locality, "Barcelona");

        firstRow.forceActiveFocus();
        tryVerify(function() { return firstRow.activeFocus; });
        keyClick(Qt.Key_Return);
        tryCompare(placeSpy, "count", 2);
    }

    function test_unlinkedPlaceIsNotActionable() {
        var window = createTemporaryObject(sheetWindowComponent, null, { width: 900 });
        verify(window !== null);
        var sheet = window.sheet;
        sheet.result = {
            summary: {},
            dataset: {},
            places: [{ locality: "Unlinked", recorded_observations: 1, recorded_days: 1, timeline_index: -1 }],
            timeline: [{}]
        };
        var placeSpy = createTemporaryObject(placeSpyComponent, null, { target: sheet });
        verify(placeSpy !== null);
        tryVerify(function() { return findChild(sheet, "timelinePlaceRow-0") !== null; });
        var row = findChild(sheet, "timelinePlaceRow-0");
        verify(!row.activeFocusOnTab);
        fuzzyCompare(row.opacity, 0.62, 0.001);
        verify(!sheet.placeIsNavigable({ timeline_index: "0" }));

        mouseClick(row, row.width / 2, row.height / 2, Qt.LeftButton);
        compare(placeSpy.count, 0);
        row.forceActiveFocus();
        keyClick(Qt.Key_Return);
        compare(placeSpy.count, 0);
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

        var summary = findChild(sheet, "timelineDetailsShortSummary");
        var list = findChild(sheet, "timelineDetailsList");
        var attribution = findChild(sheet, "timelineDetailsAttribution");
        var content = findChild(sheet, "timelineDetailsContent");
        verify(summary !== null);
        verify(list !== null);
        verify(attribution !== null);
        verify(content !== null);
        verify(summary.text.indexOf("Recorded 4821") >= 0);
        verify(summary.text.indexOf("Invalid coordinates 3") >= 0);
        verify(summary.text.indexOf("Distinct places 3") >= 0);
        verify(list.height >= 72);
        verify(attribution.text.indexOf("OpenStreetMap contributors") >= 0);
        verify(!attribution.truncated);
        verify(attribution.y + attribution.height <= content.height + 0.5);
    }
}
