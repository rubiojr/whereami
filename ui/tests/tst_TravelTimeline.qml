pragma ComponentBehavior: Bound

import QtQuick 2.15
import QtQuick.Controls 2.15
import QtTest 1.2
import "../components"

TestCase {
    id: testCase
    name: "TravelTimeline"

    Component {
        id: timelineComponent
        TravelTimeline {
            width: 1000
            height: 700
            year: 2024
            active: false
        }
    }

    Component {
        id: timelineWindowComponent
        ApplicationWindow {
            width: 1000
            height: 700
            visible: true
            property alias timeline: visibleTimeline

            TravelTimeline {
                id: visibleTimeline
                anchors.fill: parent
                active: true
                result: testCase.sampleResult
            }
        }
    }

    Component {
        id: focusSpyComponent
        SignalSpy {
            signalName: "focusRequested"
        }
    }

    readonly property var sampleResult: ({
        timeline: [
            {
                date_utc: "2024-01-05",
                latitude: 48.8566,
                longitude: 2.3522,
                country: "France",
                country_id: "FR",
                locality: "Paris",
                locality_id: "PAR",
                recorded_observations: 2,
                first_observation_utc: "2024-01-05T11:00:00Z",
                last_observation_utc: "2024-01-05T11:00:00Z"
            },
            {
                date_utc: "2024-02-09",
                latitude: 41.3874,
                longitude: 2.1686,
                country: "Spain",
                country_id: "ES",
                region: "Catalonia",
                region_id: "CAT",
                locality: "Barcelona",
                locality_id: "BCN",
                recorded_observations: 3,
                first_observation_utc: "2024-02-09T08:00:00Z",
                last_observation_utc: "2024-02-09T19:15:00Z"
            }
        ],
        journey_separation_meters: 100,
        journey_truncated: true
    })

    function test_navigationStartsAtLatestAndMovesChronologically() {
        var timeline = createTemporaryObject(timelineComponent, null, { result: sampleResult });
        verify(timeline !== null);
        compare(timeline.stopCount, 2);
        compare(timeline.currentIndex, 1);
        compare(timeline.currentStop.locality, "Barcelona");
        verify(timeline.atLatest);
        verify(!timeline.atOldest);

        timeline.previous();
        compare(timeline.currentIndex, 0);
        compare(timeline.currentStop.locality, "Paris");
        verify(timeline.atOldest);

        timeline.next();
        compare(timeline.currentIndex, 1);
        timeline.scrubber.value = 0;
        timeline.scrubber.moved();
        compare(timeline.currentIndex, 0);
        timeline.next();
        tryCompare(timeline.scrubber, "value", 1);
        timeline.selectStop(-10);
        compare(timeline.currentIndex, 0);
        timeline.selectStop(100);
        compare(timeline.currentIndex, 1);
    }

    function test_placeLabelsAndTimeRange() {
        var timeline = createTemporaryObject(timelineComponent, null, { result: sampleResult });
        verify(timeline !== null);
        compare(timeline.placeName(sampleResult.timeline[1]), "Barcelona");
        compare(timeline.hierarchy(sampleResult.timeline[1]), "Catalonia / Spain");
        compare(timeline.timeRange(sampleResult.timeline[1]), "08:00 - 19:15 UTC");
        compare(timeline.timeRange(sampleResult.timeline[0]), "11:00 UTC");
        compare(timeline.coordinateLabel(sampleResult.timeline[1]), "41.38740, 2.16860");
    }

    function test_panelOffsetsMapAndPreservesJourneyMetadata() {
        var timeline = createTemporaryObject(timelineComponent, null, {
            width: 360,
            result: sampleResult
        });
        verify(timeline !== null);
        verify(timeline.panelVisible);
        verify(timeline.focusTargetY < timeline.height / 2);
        var metadata = findChild(timeline, "journeyMetaLabel");
        verify(metadata !== null);
        verify(metadata.text.indexOf("LIMITED") >= 0);
        verify(metadata.text.indexOf("100 m") >= 0);
        verify(!metadata.truncated);

        timeline.controlsVisible = false;
        verify(!timeline.panelVisible);
        compare(timeline.focusTargetY, timeline.height / 2);
    }

    function test_detailsRoundTripPreservesMapZoom() {
        var window = createTemporaryObject(timelineWindowComponent, null);
        verify(window !== null);
        var timeline = window.timeline;
        tryCompare(timeline, "mapReady", true, 2000);
        wait(100);
        timeline.mapItem.zoomLevel = 11;
        timeline.cameraUserAdjusted = false;
        mouseWheel(timeline.mapItem, timeline.mapItem.width / 2, timeline.mapItem.height / 2, 0, 120, Qt.NoButton, Qt.NoModifier);
        tryVerify(function() { return timeline.cameraUserAdjusted; });
        var adjustedZoom = timeline.mapItem.zoomLevel;

        timeline.controlsVisible = false;
        wait(150);
        fuzzyCompare(timeline.mapItem.zoomLevel, adjustedZoom, 0.01);
        timeline.controlsVisible = true;
        wait(150);
        fuzzyCompare(timeline.mapItem.zoomLevel, adjustedZoom, 0.01);
    }

    function test_sliderArrowMovesExactlyOneStop() {
        var window = createTemporaryObject(timelineWindowComponent, null);
        verify(window !== null);
        var timeline = window.timeline;
        window.requestActivate();
        tryVerify(function() { return window.active; });
        timeline.result = {
            timeline: sampleResult.timeline.concat([
                {
                    date_utc: "2024-03-11",
                    latitude: 40.4168,
                    longitude: -3.7038,
                    locality: "Madrid"
                }
            ])
        };
        compare(timeline.currentIndex, 2);
        timeline.scrubber.forceActiveFocus();
        tryVerify(function() { return timeline.scrubber.activeFocus; });
        keyClick(Qt.Key_Left);
        compare(timeline.currentIndex, 1);
    }

    function test_immediateFocusClearsManualCameraState() {
        var timeline = createTemporaryObject(timelineComponent, null, {
            active: true,
            result: sampleResult
        });
        verify(timeline !== null);
        tryCompare(timeline, "mapReady", true, 2000);
        timeline.cameraUserAdjusted = true;

        timeline.focusCurrent(true);

        verify(!timeline.cameraUserAdjusted);
    }

    function test_mapLoadsForJourney() {
        var timeline = createTemporaryObject(timelineComponent, null, {
            active: true,
            result: sampleResult
        });
        verify(timeline !== null);
        tryCompare(timeline, "mapReady", true, 2000);
        var focusSpy = createTemporaryObject(focusSpyComponent, null, { target: timeline });
        verify(focusSpy !== null);
        timeline.previous();
        compare(timeline.currentStop.locality, "Paris");

        timeline.result = {
            timeline: [
                { date_utc: "2023-01-01", latitude: 40, longitude: -3, locality: "Madrid" },
                { date_utc: "2023-02-01", latitude: 37, longitude: -6, locality: "Seville" }
            ]
        };
        compare(timeline.currentIndex, 1);
        compare(timeline.currentStop.locality, "Seville");
        tryVerify(function() { return focusSpy.count > 0; }, 2000);
    }
}
