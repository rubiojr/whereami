/*
  MapViewLogic.js

  Extracted JavaScript business logic from MapView.qml to improve maintainability
  and testability. This module contains pure functions and complex logic that
  can be tested independently of QML components.

  Design principles:
    - Pure functions where possible (no side effects)
    - Clear parameter interfaces
    - Return values rather than direct mutations
    - Document expected parameter formats
*/

// Clustering logic for tag-filtered waypoints
function buildLocalClusters(waypoints, zoomLevel, gridSize) {
    if (!waypoints || !Array.isArray(waypoints) || waypoints.length === 0) {
        return [];
    }

    var zoom = Math.round(zoomLevel);
    var grid = gridSize || 30;
    var n = Math.pow(2, zoom);
    var buckets = {};

    // Group waypoints into grid buckets
    for (var i = 0; i < waypoints.length; i++) {
        var wp = waypoints[i];
        if (!wp || wp.lat === undefined || wp.lon === undefined)
            continue;

        var lat = wp.lat;
        var lon = wp.lon;
        var sinLat = Math.sin(lat * Math.PI / 180);
        var x = (lon + 180.0) / 360.0 * 256.0 * n;
        var y = (0.5 - Math.log((1 + sinLat) / (1 - sinLat)) / (4 * Math.PI)) * 256.0 * n;
        var bx = Math.floor(x / grid);
        var by = Math.floor(y / grid);
        var key = bx + ":" + by;

        var bucket = buckets[key];
        if (!bucket) {
            bucket = {
                minX: x,
                maxX: x,
                minY: y,
                maxY: y,
                wps: [],
                count: 0
            };
            buckets[key] = bucket;
        }

        // Update bucket bounds
        if (x < bucket.minX) bucket.minX = x;
        if (x > bucket.maxX) bucket.maxX = x;
        if (y < bucket.minY) bucket.minY = y;
        if (y > bucket.maxY) bucket.maxY = y;

        bucket.wps.push(wp);
        bucket.count++;
    }

    // Convert buckets to output format
    var result = [];
    for (var key in buckets) {
        var bucket = buckets[key];
        if (bucket.count === 1) {
            // Single waypoint: emit original object for identity consistency
            result.push(bucket.wps[0]);
        } else {
            // Multiple waypoints: create cluster
            var centerX = (bucket.minX + bucket.maxX) / 2;
            var centerY = (bucket.minY + bucket.maxY) / 2;
            var scale = 256.0 * Math.pow(2, zoom);
            var clon = (centerX / scale) * 360.0 - 180.0;
            var normY = centerY / scale;
            var clat = Math.atan(Math.sinh(Math.PI * (1 - 2 * normY))) * 180.0 / Math.PI;

            result.push({
                type: "cluster",
                lat: clat,
                lon: clon,
                count: bucket.count
            });
        }
    }

    return result;
}

// Find a waypoint by name (case-insensitive, bookmark preference)
function findWaypointByName(waypoints, name) {
    if (!name || !waypoints || waypoints.length === 0) {
        return null;
    }

    var lower = name.toLowerCase();
    var chosen = null;
    var fallback = null;

    for (var i = 0; i < waypoints.length; i++) {
        var w = waypoints[i];
        if (!w.name) continue;

        if (w.name.toLowerCase() === lower) {
            if (w.bookmark && chosen === null) {
                chosen = w; // Prefer bookmarks
            } else if (!w.bookmark && fallback === null) {
                fallback = w; // Fallback to non-bookmarks
            }
        }
    }

    return chosen || fallback;
}

// Find the index of a waypoint in the array by matching name and coordinates
function findWaypointIndex(waypoints, targetWaypoint) {
    if (!waypoints || !targetWaypoint) {
        return -1;
    }

    for (var i = 0; i < waypoints.length; i++) {
        var w = waypoints[i];
        if (w.name === targetWaypoint.name &&
            Math.abs(w.lat - targetWaypoint.lat) < 1e-9 &&
            Math.abs(w.lon - targetWaypoint.lon) < 1e-9) {
            return i;
        }
    }

    return -1;
}

// Extract bookmark-only waypoints from full waypoint list
function extractBookmarkWaypoints(waypoints) {
    if (!waypoints || !Array.isArray(waypoints)) {
        return [];
    }

    var bookmarks = [];
    for (var i = 0; i < waypoints.length; i++) {
        var w = waypoints[i];
        if (w && w.bookmark) {
            bookmarks.push(w);
        }
    }

    return bookmarks;
}

// Build tag vocabulary from waypoints
function buildTagVocabulary(waypoints) {
    if (!waypoints || !Array.isArray(waypoints)) {
        return [];
    }

    var tagSet = {};

    for (var i = 0; i < waypoints.length; i++) {
        var waypoint = waypoints[i];
        if (!waypoint || !waypoint.tags) continue;

        for (var j = 0; j < waypoint.tags.length; j++) {
            var tag = waypoint.tags[j];
            if (tag !== undefined && tag !== null) {
                var key = ("" + tag).trim();
                if (key.length > 0) {
                    tagSet[key] = true;
                }
            }
        }
    }

    // Convert to sorted array
    var vocabulary = [];
    for (var key in tagSet) {
        vocabulary.push(key);
    }

    vocabulary.sort(function(a, b) {
        var al = a.toLowerCase();
        var bl = b.toLowerCase();
        return al < bl ? -1 : (al > bl ? 1 : 0);
    });

    return vocabulary;
}

// Normalize directory path for GPX import
function normalizeDirectoryPath(dirUrl) {
    if (!dirUrl) {
        return "";
    }

    var path = dirUrl;

    // Convert to string if needed
    if (typeof path !== "string") {
        if (path.toString) {
            path = path.toString();
        } else {
            path = "" + path;
        }
    }

    // Remove file:// prefix
    if (path.slice(0, 7) === "file://") {
        path = path.substring(7);
    }

    // Remove trailing slash
    if (path.length > 1 && path.charAt(path.length - 1) === "/") {
        path = path.slice(0, -1);
    }

    return path;
}

// Remove a waypoint from array by matching name and coordinates
function removeWaypointFromArray(waypoints, targetWaypoint) {
    if (!waypoints || !Array.isArray(waypoints) || !targetWaypoint) {
        return waypoints.slice(); // Return copy of original array
    }

    var result = [];

    for (var i = 0; i < waypoints.length; i++) {
        var w = waypoints[i];
        var isMatch = (
            w.name === targetWaypoint.name &&
            Math.abs(w.lat - targetWaypoint.lat) < 1e-9 &&
            Math.abs(w.lon - targetWaypoint.lon) < 1e-9
        );

        if (!isMatch) {
            result.push(w);
        }
    }

    return result;
}

// Update waypoint tags in array
function updateWaypointTags(waypoints, name, lat, lon, newTags) {
    if (!waypoints || !Array.isArray(waypoints)) {
        return waypoints.slice();
    }

    var result = [];

    for (var i = 0; i < waypoints.length; i++) {
        var w = waypoints[i];
        var isMatch = (
            w.name === name &&
            Math.abs(w.lat - lat) < 1e-9 &&
            Math.abs(w.lon - lon) < 1e-9
        );

        if (isMatch) {
            // Create updated waypoint with new tags
            var updatedWaypoint = {};
            for (var key in w) {
                updatedWaypoint[key] = w[key];
            }
            updatedWaypoint.tags = newTags ? newTags.slice() : [];
            result.push(updatedWaypoint);
        } else {
            result.push(w);
        }
    }

    return result;
}

// Check if selection should be cleared when bookmark filter changes
function shouldClearSelection(showNonBookmarkWaypoints, selectedWaypoint) {
    return !showNonBookmarkWaypoints &&
           selectedWaypoint &&
           !selectedWaypoint.bookmark;
}

// Calculate bounds for fit-all-waypoints functionality
function calculateWaypointBounds(waypoints) {
    if (!waypoints || waypoints.length === 0) {
        return null;
    }

    var minLat = Infinity;
    var maxLat = -Infinity;
    var minLon = Infinity;
    var maxLon = -Infinity;
    var validCount = 0;

    for (var i = 0; i < waypoints.length; i++) {
        var wp = waypoints[i];
        if (!wp || wp.lat === undefined || wp.lon === undefined) continue;

        minLat = Math.min(minLat, wp.lat);
        maxLat = Math.max(maxLat, wp.lat);
        minLon = Math.min(minLon, wp.lon);
        maxLon = Math.max(maxLon, wp.lon);
        validCount++;
    }

    if (validCount === 0) {
        return null;
    }

    var centerLat = (minLat + maxLat) / 2;
    var centerLon = (minLon + maxLon) / 2;
    var latDiff = maxLat - minLat;
    var lonDiff = maxLon - minLon;
    var maxDiff = Math.max(latDiff, lonDiff);
    var zoom = Math.max(1, Math.min(18, 10 - Math.log2(maxDiff * 10)));

    return {
        centerLat: centerLat,
        centerLon: centerLon,
        zoom: zoom,
        bounds: {
            minLat: minLat,
            maxLat: maxLat,
            minLon: minLon,
            maxLon: maxLon
        }
    };
}
