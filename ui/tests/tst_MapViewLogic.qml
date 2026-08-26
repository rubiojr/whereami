import QtQuick 2.15
import QtTest 1.2
import "../lib/MapViewLogic.js" as MapViewLogic

TestCase {
    name: "MapViewLogic"

    readonly property var waypoints: [
        { name: "before", lat: 1, lon: 1, time: "2025-03-09T23:59:59Z", bookmark: false },
        { name: "start", lat: 2, lon: 2, time: "2025-03-10T00:00:00Z", bookmark: true },
        { name: "offset", lat: 3, lon: 3, time: "2025-03-11T01:30:00+02:00", bookmark: false },
        { name: "end", lat: 4, lon: 4, time: "2025-03-12T23:59:59Z", bookmark: true },
        { name: "after", lat: 5, lon: 5, time: "2025-03-13T00:00:00Z", bookmark: true },
        { name: "missing", lat: 6, lon: 6, bookmark: true },
        { name: "invalid", lat: 7, lon: 7, time: "not-a-date", bookmark: true }
    ]

    function names(items) {
        return items.map(function (waypoint) { return waypoint.name; });
    }

    function test_waypointDateKey_usesUTC() {
        compare(MapViewLogic.waypointDateKey(waypoints[2]), "2025-03-10");
        compare(MapViewLogic.waypointDateKey(waypoints[5]), "");
        compare(MapViewLogic.waypointDateKey(waypoints[6]), "");
    }

    function test_filterSingleDate_isInclusive() {
        var result = MapViewLogic.filterWaypointsByDateRange(waypoints, "2025-03-10", "2025-03-10");
        compare(names(result), ["start", "offset"]);
    }

    function test_filterDateRange_includesBothEdges() {
        var result = MapViewLogic.filterWaypointsByDateRange(waypoints, "2025-03-10", "2025-03-12");
        compare(names(result), ["start", "offset", "end"]);
    }

    function test_filterDateRange_acceptsReverseOrder() {
        var result = MapViewLogic.filterWaypointsByDateRange(waypoints, "2025-03-12", "2025-03-10");
        compare(names(result), ["start", "offset", "end"]);
    }

    function test_filterForMap_composesBookmarkAndDateFilters() {
        var result = MapViewLogic.filterWaypointsForMap(waypoints, false, "2025-03-10", "2025-03-12");
        compare(names(result), ["start", "end"]);
    }

    function test_noDateFilter_preservesEntries() {
        var result = MapViewLogic.filterWaypointsByDateRange(waypoints, "", "");
        compare(result.length, waypoints.length);
        verify(result !== waypoints);
    }

    function test_containsWaypoint_matchesCanonicalIdentity() {
        verify(MapViewLogic.containsWaypoint(waypoints, { name: "start", lat: 2, lon: 2 }));
        verify(!MapViewLogic.containsWaypoint(waypoints, { name: "start", lat: 2, lon: 3 }));
    }

    function test_datePresets_singleDaysAndLeapYear() {
        var leapDay = Date.UTC(2024, 1, 29, 12, 0, 0);
        compare(MapViewLogic.datePresetRange("today", leapDay), { start: "2024-02-29", end: "2024-02-29" });
        compare(MapViewLogic.datePresetRange("yesterday", leapDay), { start: "2024-02-28", end: "2024-02-28" });
        compare(MapViewLogic.datePresetRange("last-year", leapDay), { start: "2023-02-28", end: "2023-02-28" });
    }

    function test_datePresets_ranges() {
        var marchFirst = Date.UTC(2024, 2, 1, 12, 0, 0);
        compare(MapViewLogic.datePresetRange("last-7", marchFirst), { start: "2024-02-24", end: "2024-03-01" });
        compare(MapViewLogic.datePresetRange("this-month", marchFirst), { start: "2024-03-01", end: "2024-03-01" });
        compare(MapViewLogic.datePresetRange("last-month", marchFirst), { start: "2024-02-01", end: "2024-02-29" });
    }
}
