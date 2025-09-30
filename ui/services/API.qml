import QtQuick 2.15

/*
  API.qml
  Centralized HTTP API service (thin wrapper around XMLHttpRequest) that unifies all
  backend interactions currently scattered across QML components (`MapView.qml`,
  `SearchBox.qml`, `WaypointService.qml`, etc).

  Goals:
    - Provide a single surface for network I/O (easy to evolve / refactor to in‑process bridge later).
    - Emit semantic signals (no direct UI mutation; consumers decide optimistic updates / rollbacks).
    - Offer both specialized convenience methods (addWaypoint, getWaypoints, etc) and a generic
      low-level `request()` escape hatch for future endpoints.
    - Replace `WaypointService.qml` incrementally (that file can stay until migration completes).

  Design notes:
    - All methods respect `apiPort`. If `apiPort < 0`, most mutating operations become no-ops
      with "immediate success" semantics so UI logic keeps working in an offline / preview mode.
    - Timeouts implemented with dynamically created `Timer` objects (consistent with existing pattern).
    - Each call emits a `*Started` signal (when appropriate) before the actual HTTP request is sent.
    - Errors are surfaced as human-readable strings (status code + response tail where possible).
    - The service never mutates external arrays or objects; it only emits signals / returns via callbacks.

  Migration strategy (example):
    - Replace usages of `WaypointService.addWaypoint` with `api.addWaypoint`.
    - Move clustered fetch logic to `api.getClusters`.
    - Replace manual XHR in `SearchBox.qml` with `api.suggest`.
    - Move tag + rename logic from `WaypointInfoCard` into here, then refactor the card to be passive.
    - Gradually delete legacy service once no code imports it.

  Signals overview (extended):
    Generic:
      requestSucceeded(kind, result, context)
      requestFailed(kind, errorMessage, context)

    Waypoints / bookmarks:
      waypointsLoadStarted()
      waypointsLoaded(waypoints)
      waypointsLoadFailed(error)

      waypointAddStarted(originalWaypoint)
      waypointAdded(savedWaypoint, originalWaypoint)
      waypointAddFailed(originalWaypoint, error)

      waypointDeleteStarted(waypoint)
      waypointDeleted(waypoint)
      waypointDeleteFailed(waypoint, error)

      waypointRenameStarted(originalWaypoint, newName)
      waypointRenamed(updatedWaypoint, originalWaypoint)
      waypointRenameFailed(originalWaypoint, newName, error)

    Tags:
      tagsFetchStarted(waypointIdent)
      tagsFetched(name, lat, lon, tags)
      tagsFetchFailed(waypointIdent, error)

      tagAddStarted(name, lat, lon, tag)
      tagAdded(name, lat, lon, tags, tag)
      tagAddFailed(name, lat, lon, tag, error)

      tagDeleteStarted(name, lat, lon, tag)
      tagDeleted(name, lat, lon, tags, tag)
      tagDeleteFailed(name, lat, lon, tag, error)

    Clusters:
      clustersFetchStarted(params)
      clustersFetched(clusters, params)
      clustersFetchFailed(error, params)

    Location:
      locationFetchStarted()
      locationFetched(locationObj)
      locationFetchFailed(error)

    Import:
      importStarted(params)
      importCompleted(summary, params)
      importFailed(error, params)

    Suggestions:
      suggestStarted(query)
      suggestResults(resultObj, query)
      suggestFailed(error, query)

  Generic request context:
    - `kind` is a short string (e.g. "GET /api/waypoints").
    - `context` is an arbitrary object the caller can use to correlate.

  Future enhancements:
    - Rate limiting / dedupe (e.g. in-flight suggest queries).
    - Retry/backoff policy.
    - Switch to Go-exposed QObject invokables (no HTTP) without changing higher-level UI code.

*/

QtObject {
    id: api

    // Port injected by the root / main view (e.g. MapView)
    property int apiPort: 43098

    // Default timeout (ms) for requests
    property int requestTimeoutMs: 8000

    // --- Generic signals ---
    signal requestSucceeded(string kind, var result, var context)
    signal requestFailed(string kind, string errorMessage, var context)

    // --- Waypoints / Bookmarks ---
    signal waypointsLoadStarted
    signal waypointsLoaded(var waypoints)
    signal waypointsLoadFailed(string errorMessage)

    signal waypointAddStarted(var originalWaypoint)
    signal waypointAdded(var savedWaypoint, var originalWaypoint)
    signal waypointAddFailed(var originalWaypoint, string errorMessage)

    signal waypointDeleteStarted(var waypoint)
    signal waypointDeleted(var waypoint)
    signal waypointDeleteFailed(var waypoint, string errorMessage)

    signal waypointRenameStarted(var originalWaypoint, string newName)
    signal waypointRenamed(var updatedWaypoint, var originalWaypoint)
    signal waypointRenameFailed(var originalWaypoint, string newName, string errorMessage)

    // --- Tag management ---
    signal tagsFetchStarted(var waypointIdent)          // { name, lat, lon }
    signal tagsFetched(string name, real lat, real lon, var tags)
    signal tagsFetchFailed(var waypointIdent, string errorMessage)

    signal tagAddStarted(string name, real lat, real lon, string tag)
    signal tagAdded(string name, real lat, real lon, var tags, string tag)
    signal tagAddFailed(string name, real lat, real lon, string tag, string errorMessage)

    signal tagDeleteStarted(string name, real lat, real lon, string tag)
    signal tagDeleted(string name, real lat, real lon, var tags, string tag)
    signal tagDeleteFailed(string name, real lat, real lon, string tag, string errorMessage)

    // --- Clusters ---
    signal clustersFetchStarted(var params)
    signal clustersFetched(var clusters, var params)
    signal clustersFetchFailed(string errorMessage, var params)

    // --- Location ---
    signal locationFetchStarted
    signal locationFetched(var locationObject)
    signal locationFetchFailed(string errorMessage)

    // --- Import ---
    signal importStarted(var params)
    signal importCompleted(var summary, var params)
    signal importFailed(string errorMessage, var params)

    // --- Suggestions ---
    signal suggestStarted(string query)
    signal suggestResults(var resultObject, string query)
    signal suggestFailed(string errorMessage, string query)

    // --- Recent Searches ---
    // Emitted when requesting recent search queries (stored in geocode cache DB)
    signal recentSearchesFetchStarted(int limit)
    signal recentSearchesFetched(var queries, int limit)          // queries: array of strings (most recent first, legacy)
    signal recentSearchEntriesFetched(var entries, int limit)     // entries: array of objects { query, lat?, lon? } (new enriched form)
    signal recentSearchesFetchFailed(string errorMessage, int limit)

    // --- Version Info ---
    signal versionFetchStarted
    signal versionFetched(var versionInfo)
    signal versionFetchFailed(string errorMessage)

    // ------------- Public Convenience API -------------

    // Load all waypoints/bookmarks
    function getWaypoints() {
        if (api.apiPort < 0) {
            api.waypointsLoadStarted();
            api.waypointsLoaded([]);
            api.requestSucceeded("GET /api/waypoints (offline)", [], null);
            return;
        }
        api.waypointsLoadStarted();
        _xhr("GET", "/api/waypoints?emoji=true", null, function (txt) {
            var arr = [];
            try {
                arr = JSON.parse(txt);
            } catch (e) {}
            // Defensive: ensure each waypoint's tags are enriched objects
            for (var i = 0; i < arr.length; i++) {
                var wp = arr[i];
                if (wp && wp.tags && Array.isArray(wp.tags))
                    wp.tags = ensureEnrichedTagArray(wp.tags);
            }
            api.waypointsLoaded(arr);
            api.requestSucceeded("GET /api/waypoints?emoji=true", arr, null);
        }, function (err) {
            api.waypointsLoadFailed(err);
            api.requestFailed("GET /api/waypoints?emoji=true", err, null);
        });
    }

    // Add a waypoint/bookmark
    function addWaypoint(wp) {
        if (!wp || typeof wp.name === "undefined" || typeof wp.lat === "undefined" || typeof wp.lon === "undefined") {
            console.error("API.addWaypoint: invalid waypoint payload", wp);
            return;
        }
        waypointAddStarted(wp);
        if (api.apiPort < 0) {
            waypointAdded(wp, wp);
            requestSucceeded("POST /api/bookmarks (offline)", wp, wp);
            return;
        }
        var payload = {
            name: wp.name,
            lat: wp.lat,
            lon: wp.lon
        };
        if (wp.tags && wp.tags.length > 0)
            payload.tags = wp.tags;

        _xhr("POST", "/api/bookmarks", payload, function (txt) {
            var saved = wp;
            try {
                saved = JSON.parse(txt);
            } catch (e) {}
            waypointAdded(saved, wp);
            requestSucceeded("POST /api/bookmarks", saved, wp);
        }, function (err) {
            waypointAddFailed(wp, err);
            requestFailed("POST /api/bookmarks", err, wp);
        });
    }

    // Delete waypoint
    function deleteWaypoint(wp) {
        if (!wp || typeof wp.name === "undefined" || typeof wp.lat === "undefined" || typeof wp.lon === "undefined") {
            console.error("API.deleteWaypoint: invalid waypoint payload", wp);
            return;
        }
        waypointDeleteStarted(wp);
        if (api.apiPort < 0) {
            waypointDeleted(wp);
            requestSucceeded("DELETE /api/bookmarks (offline)", wp, wp);
            return;
        }
        var path = "/api/bookmarks?name=" + encodeURIComponent(wp.name) + "&lat=" + wp.lat + "&lon=" + wp.lon;
        _xhr("DELETE", path, null, function () {
            waypointDeleted(wp);
            requestSucceeded("DELETE /api/bookmarks", wp, wp);
        }, function (err) {
            waypointDeleteFailed(wp, err);
            requestFailed("DELETE /api/bookmarks", err, wp);
        });
    }

    // Rename waypoint/bookmark (bookmark only on server)
    function renameWaypoint(wp, newName) {
        if (!wp || typeof wp.name === "undefined" || typeof wp.lat === "undefined" || typeof wp.lon === "undefined") {
            console.error("API.renameWaypoint: invalid waypoint payload", wp);
            return;
        }
        newName = (newName || "").trim();
        if (newName.length === 0 || newName === wp.name) {
            // No-op; still emit failure to allow UI to revert editing state gracefully
            waypointRenameFailed(wp, newName, "no change");
            return;
        }
        waypointRenameStarted(wp, newName);
        if (api.apiPort < 0) {
            // Offline: simulate success
            var updated = {};
            for (var k in wp)
                updated[k] = wp[k];
            updated.name = newName;
            waypointRenamed(updated, wp);
            requestSucceeded("PATCH /api/bookmarks (offline)", updated, wp);
            return;
        }
        var payload = {
            oldName: wp.name,
            lat: wp.lat,
            lon: wp.lon,
            newName: newName
        };
        _xhr("PATCH", "/api/bookmarks", payload, function (txt) {
            var resp = null;
            try {
                resp = JSON.parse(txt);
            } catch (e) {}
            var updated = {};
            for (var k2 in wp)
                updated[k2] = wp[k2];
            updated.name = newName;
            waypointRenamed(updated, wp);
            requestSucceeded("PATCH /api/bookmarks", resp || updated, wp);
        }, function (err) {
            waypointRenameFailed(wp, newName, err);
            requestFailed("PATCH /api/bookmarks", err, wp);
        });
    }

    // Fetch clusters
    // Added optional bookmarksOnly flag: when true, backend receives ?bookmarksOnly=1 and only bookmark waypoints are clustered.
    function getClusters(zoom, gridSize, bookmarksOnly) {
        var only = !!bookmarksOnly;
        if (api.apiPort < 0) {
            clustersFetchStarted({
                zoom: zoom,
                grid: gridSize,
                bookmarksOnly: only
            });
            clustersFetched([], {
                zoom: zoom,
                grid: gridSize,
                bookmarksOnly: only
            });
            requestSucceeded("GET /api/clusters (offline)", [], {
                zoom: zoom,
                grid: gridSize,
                bookmarksOnly: only
            });
            return;
        }
        var params = {
            zoom: zoom,
            grid: gridSize,
            bookmarksOnly: only
        };
        clustersFetchStarted(params);
        var path = "/api/clusters?zoom=" + encodeURIComponent(zoom) + "&grid=" + encodeURIComponent(gridSize);
        if (only)
            path += "&bookmarksOnly=1";
        _xhr("GET", path, null, function (txt) {
            var data = [];
            try {
                data = JSON.parse(txt);
            } catch (e) {}
            clustersFetched(data, params);
            requestSucceeded("GET /api/clusters", data, params);
        }, function (err) {
            clustersFetchFailed(err, params);
            requestFailed("GET /api/clusters", err, params);
        });
    }

    // Current location
    function getLocation() {
        locationFetchStarted();
        if (api.apiPort < 0) {
            var fake = {
                lat: 0,
                lon: 0,
                accuracy_m: 0
            };
            locationFetched(fake);
            requestSucceeded("GET /api/location (offline)", fake, null);
            return;
        }
        _xhr("GET", "/api/location", null, function (txt) {
            var loc = null;
            try {
                loc = JSON.parse(txt);
            } catch (e) {
                loc = null;
            }
            locationFetched(loc);
            requestSucceeded("GET /api/location", loc, null);
        }, function (err) {
            locationFetchFailed(err);
            requestFailed("GET /api/location", err, null);
        });
    }

    // Import GPX directory
    function importGpxDirectory(params) {
        if (!params || !params.dir) {
            console.error("API.importGpxDirectory: missing params.dir");
            return;
        }
        importStarted(params);
        if (api.apiPort < 0) {
            var summary = {
                count: 0,
                skipped: 0
            };
            importCompleted(summary, params);
            requestSucceeded("POST /api/import (offline)", summary, params);
            return;
        }
        _xhr("POST", "/api/import", {
            dir: params.dir,
            recursive: !!params.recursive
        }, function (txt) {
            var resp = null;
            try {
                resp = JSON.parse(txt);
            } catch (e) {}
            importCompleted(resp, params);
            requestSucceeded("POST /api/import", resp, params);
        }, function (err) {
            importFailed(err, params);
            requestFailed("POST /api/import", err, params);
        }, 60000);
    }

    // Suggest (search)
    function suggest(query) {
        if (!query || query.trim() === "")
            return;
        suggestStarted(query);
        if (api.apiPort < 0) {
            suggestResults({
                suggestions: []
            }, query);
            requestSucceeded("GET /api/suggest (offline)", {
                suggestions: []
            }, query);
            return;
        }
        var path = "/api/suggest?q=" + encodeURIComponent(query);
        _xhr("GET", path, null, function (txt) {
            var obj = null;
            try {
                obj = JSON.parse(txt);
            } catch (e) {
                obj = {
                    suggestions: []
                };
            }
            suggestResults(obj, query);
            requestSucceeded("GET /api/suggest", obj, query);
        }, function (err) {
            suggestFailed(err, query);
            requestFailed("GET /api/suggest", err, query);
        });
    }

    // Fetch recent search queries (from backend geocode cache).
    // limit: optional (defaults to 10; capped server-side).
    function getRecentSearches(limit) {
        var lim = (typeof limit === "number" && limit > 0) ? limit : 10;
        recentSearchesFetchStarted(lim);
        if (api.apiPort < 0) {
            // Offline: nothing recorded
            recentSearchesFetched([], lim);
            recentSearchEntriesFetched([], lim);
            requestSucceeded("GET /api/recent_suggest (offline)", [], {
                limit: lim
            });
            return;
        }
        var path = "/api/recent_suggest?limit=" + encodeURIComponent(lim);
        _xhr("GET", path, null, function (txt) {
            var obj = null;
            try {
                obj = JSON.parse(txt);
            } catch (e) {
                obj = null;
            }
            var qs = (obj && obj.queries && Array.isArray(obj.queries)) ? obj.queries : [];
            // Enriched entries (new server field) fallback to synthesized from queries if absent.
            var entries = [];
            if (obj && obj.entries && Array.isArray(obj.entries)) {
                for (var i = 0; i < obj.entries.length; i++) {
                    var it = obj.entries[i];
                    if (!it || !it.query)
                        continue;
                    var rec = {
                        query: ("" + it.query)
                    };
                    if (typeof it.lat === "number")
                        rec.lat = it.lat;
                    if (typeof it.lon === "number")
                        rec.lon = it.lon;
                    entries.push(rec);
                }
            } else {
                for (var j = 0; j < qs.length; j++) {
                    entries.push({
                        query: qs[j]
                    });
                }
            }
            recentSearchesFetched(qs, lim);
            recentSearchEntriesFetched(entries, lim);
            requestSucceeded("GET /api/recent_suggest", {
                queries: qs,
                entries: entries
            }, {
                limit: lim
            });
        }, function (err) {
            recentSearchesFetchFailed(err, lim);
            requestFailed("GET /api/recent_suggest", err, {
                limit: lim
            });
        });
    }

    // Convenience: record a history entry (optionally with coordinates)
    function recordHistory(query, lat, lon) {
        var q = (query || "").trim();
        if (q.length === 0)
            return;
        if (api.apiPort < 0) {
            // offline: no-op
            return;
        }
        var body = {
            query: q
        };
        if (typeof lat === "number")
            body.lat = lat;
        if (typeof lon === "number")
            body.lon = lon;
        request("/api/history", {
            method: "POST",
            body: body
        });
    }
    // --------- Tag Management (Bookmark waypoints only on server) ----------

    function fetchTags(wp) {
        if (!wp || !wp.name || typeof wp.lat === "undefined" || typeof wp.lon === "undefined") {
            console.error("API.fetchTags invalid waypoint", wp);
            return;
        }
        var ident = {
            name: wp.name,
            lat: wp.lat,
            lon: wp.lon
        };
        tagsFetchStarted(ident);
        if (api.apiPort < 0) {
            // Offline: normalize any existing raw string tags into enriched objects
            var offlineRaw = (wp.tags || []);
            var offlineEnriched = ensureEnrichedTagArray(offlineRaw);
            tagsFetched(wp.name, wp.lat, wp.lon, offlineEnriched);
            requestSucceeded("GET /api/tags (offline)", offlineEnriched, ident);
            return;
        }
        var path = "/api/tags?name=" + encodeURIComponent(wp.name) + "&lat=" + wp.lat + "&lon=" + wp.lon + "&emoji=true";
        _xhr("GET", path, null, function (txt) {
            var obj = null;
            try {
                obj = JSON.parse(txt);
            } catch (e) {
                obj = null;
            }
            var tags = (obj && obj.tags) ? obj.tags : [];
            // Normalize again just in case backend returned raw strings (future compatibility)
            tags = ensureEnrichedTagArray(tags);
            tagsFetched(wp.name, wp.lat, wp.lon, tags);
            requestSucceeded("GET /api/tags?emoji=true", tags, ident);
        }, function (err) {
            tagsFetchFailed(ident, err);
            requestFailed("GET /api/tags?emoji=true", err, ident);
        });
    }

    function addTag(wp, tag) {
        if (!wp || !wp.name || typeof wp.lat === "undefined" || typeof wp.lon === "undefined" || !tag || tag.trim() === "") {
            console.error("API.addTag invalid arguments", wp, tag);
            return;
        }
        tag = tag.trim();
        tagAddStarted(wp.name, wp.lat, wp.lon, tag);
        if (api.apiPort < 0) {
            // Offline: append locally (dedupe, still raw)
            var existing = (wp.tags || []).slice();
            if (existing.indexOf(tag) === -1)
                existing.push(tag);
            tagAdded(wp.name, wp.lat, wp.lon, existing, tag);
            requestSucceeded("POST /api/tags?emoji=true (offline)", existing, {
                name: wp.name,
                lat: wp.lat,
                lon: wp.lon,
                tag: tag
            });
            return;
        }
        var payload = {
            name: wp.name,
            lat: wp.lat,
            lon: wp.lon,
            tags: [tag]
        };
        _xhr("POST", "/api/tags?emoji=true", payload, function (txt) {
            var obj = null;
            try {
                obj = JSON.parse(txt);
            } catch (e) {}
            var tags = (obj && obj.tags) ? obj.tags : [];
            tagAdded(wp.name, wp.lat, wp.lon, tags, tag);
            requestSucceeded("POST /api/tags?emoji=true", tags, {
                name: wp.name,
                lat: wp.lat,
                lon: wp.lon,
                tag: tag
            });
        }, function (err) {
            tagAddFailed(wp.name, wp.lat, wp.lon, tag, err);
            requestFailed("POST /api/tags?emoji=true", err, {
                name: wp.name,
                lat: wp.lat,
                lon: wp.lon,
                tag: tag
            });
        });
    }

    function deleteTag(wp, tag) {
        if (!wp || !wp.name || typeof wp.lat === "undefined" || typeof wp.lon === "undefined" || !tag || tag.trim() === "") {
            console.error("API.deleteTag invalid arguments", wp, tag);
            return;
        }
        tag = tag.trim();
        tagDeleteStarted(wp.name, wp.lat, wp.lon, tag);
        if (api.apiPort < 0) {
            var remaining = [];
            var src = (wp.tags || []);
            for (var i = 0; i < src.length; i++) {
                if (src[i] !== tag)
                    remaining.push(src[i]);
            }
            tagDeleted(wp.name, wp.lat, wp.lon, remaining, tag);
            requestSucceeded("DELETE /api/tags?emoji=true (offline)", remaining, {
                name: wp.name,
                lat: wp.lat,
                lon: wp.lon,
                tag: tag
            });
            return;
        }
        var path = "/api/tags?name=" + encodeURIComponent(wp.name) + "&lat=" + wp.lat + "&lon=" + wp.lon + "&tag=" + encodeURIComponent(tag) + "&emoji=true";
        _xhr("DELETE", path, null, function (txt) {
            var obj = null;
            try {
                obj = JSON.parse(txt);
            } catch (e) {}
            var tags = (obj && obj.tags) ? obj.tags : [];
            tagDeleted(wp.name, wp.lat, wp.lon, tags, tag);
            requestSucceeded("DELETE /api/tags?emoji=true", tags, {
                name: wp.name,
                lat: wp.lat,
                lon: wp.lon,
                tag: tag
            });
        }, function (err) {
            tagDeleteFailed(wp.name, wp.lat, wp.lon, tag, err);
            requestFailed("DELETE /api/tags?emoji=true", err, {
                name: wp.name,
                lat: wp.lat,
                lon: wp.lon,
                tag: tag
            });
        });
    }
    // Helper: ensure an array of tags (strings or objects) is returned as enriched objects
    function ensureEnrichedTagArray(arr) {
        if (!arr || arr.length === 0)
            return [];
        var out = [];
        for (var i = 0; i < arr.length; i++) {
            var t = arr[i];
            if (t === null || t === undefined)
                continue;
            if (typeof t === "object") {
                // Assume already enriched (has at least raw)
                if (t.raw === undefined && t.display === undefined) {
                    // Fallback: try to stringify unknown object
                    var rawGuess = "";
                    try {
                        rawGuess = JSON.stringify(t);
                    } catch (e) {
                        rawGuess = "" + t;
                    }
                    out.push({
                        raw: rawGuess,
                        display: rawGuess
                    });
                } else {
                    // Ensure display field exists
                    if (!t.display) {
                        if (t.emoji && t.raw)
                            t.display = t.emoji + " " + t.raw;
                        else if (t.raw)
                            t.display = "" + t.raw;
                        else
                            t.display = "" + t;
                    }
                    out.push(t);
                }
            } else {
                var rawStr = "" + t;
                out.push({
                    raw: rawStr,
                    display: rawStr
                });
            }
        }
        return out;
    }
    // Fetch global distinct tag list (enriched objects when emoji=true)
    function fetchDistinctTags(callback) {
        if (api.apiPort < 0) {
            callback && callback([]);
            return;
        }
        _xhr("GET", "/api/tags?distinct=true&emoji=true", null, function (txt) {
            var obj = null;
            try {
                obj = JSON.parse(txt);
            } catch (e) {
                obj = null;
            }
            var tags = (obj && obj.tags) ? obj.tags : [];
            callback && callback(tags);
        }, function (err) {
            callback && callback([]);
        });
    }

    // Generic request helper (escape hatch). options:
    function request(path, options) {
        if (api.apiPort < 0) {
            var kind = (options && options.method ? options.method : "GET") + " " + path + " (offline)";
            api.requestSucceeded(kind, null, options ? options.context : null);
            if (options && options.onSuccess)
                options.onSuccess(null, options.context);
            return;
        }
        var method = (options && options.method) ? options.method : "GET";
        var body = options ? options.body : null;
        var timeout = (options && options.timeout) ? options.timeout : api.requestTimeoutMs;
        var ctx = options ? options.context : null;
        var kindReal = method + " " + path;
        _xhr(method, path, body, function (txt) {
            api.requestSucceeded(kindReal, txt, ctx);
            if (options && options.onSuccess)
                options.onSuccess(txt, ctx);
        }, function (err) {
            api.requestFailed(kindReal, err, ctx);
            if (options && options.onError)
                options.onError(err, ctx);
        }, timeout, ctx);
    }

    // Get version information
    function getVersion() {
        if (api.apiPort < 0) {
            api.versionFetchStarted();
            var fallbackVersion = {
                go_version: "unknown (offline)",
                go_os: "unknown",
                go_arch: "unknown"
            };
            api.versionFetched(fallbackVersion);
            api.requestSucceeded("GET /api/version (offline)", fallbackVersion, null);
            return;
        }
        api.versionFetchStarted();
        _xhr("GET", "/api/version", null, function (txt) {
            var versionInfo = {};
            try {
                versionInfo = JSON.parse(txt);
            } catch (e) {
                console.error("Failed to parse version info:", e, txt);
                versionInfo = {
                    go_version: "parse error",
                    go_os: "unknown",
                    go_arch: "unknown"
                };
            }
            api.versionFetched(versionInfo);
            api.requestSucceeded("GET /api/version", versionInfo, null);
        }, function (err) {
            api.versionFetchFailed(err);
            api.requestFailed("GET /api/version", err, null);
        });
    }

    // ------------- Internal Helpers -------------

    function _baseUrl() {
        return "http://127.0.0.1:" + api.apiPort;
    }

    function _xhr(method, path, body, ok, fail, timeoutOverride, context) {
        if (api.apiPort < 0) {
            if (ok)
                ok(null);
            return;
        }
        var xhr = new XMLHttpRequest();
        var timedOut = false;
        var timeoutMs = (timeoutOverride !== undefined && timeoutOverride !== null) ? timeoutOverride : api.requestTimeoutMs;
        var timer = api._startTimeout(timeoutMs, function () {
            timedOut = true;
            try {
                xhr.abort();
            } catch (e) {}
            if (fail)
                fail("timeout (" + timeoutMs + " ms)");
        });

        var url = api._baseUrl() + path;
        xhr.open(method, url);
        if (method === "POST" || method === "PUT" || method === "PATCH") {
            xhr.setRequestHeader("Content-Type", "application/json");
        }

        xhr.onreadystatechange = function () {
            if (xhr.readyState === XMLHttpRequest.DONE) {
                api._clearTimeout(timer);
                if (timedOut)
                    return;
                var status = xhr.status;
                if (status >= 200 && status < 300) {
                    if (ok)
                        ok(xhr.responseText);
                } else {
                    var msg = "HTTP " + status;
                    if (xhr.responseText)
                        msg += " " + api._truncate(xhr.responseText, 160);
                    if (fail)
                        fail(msg);
                }
            }
        };

        try {
            if (body === null || typeof body === "undefined") {
                xhr.send();
            } else if (typeof body === "string") {
                xhr.send(body);
            } else {
                xhr.send(JSON.stringify(body));
            }
        } catch (eSend) {
            api._clearTimeout(timer);
            if (fail)
                fail("send error: " + eSend);
        }
    }

    function _truncate(s, maxLen) {
        if (!s || s.length <= maxLen)
            return s;
        return s.substring(0, maxLen) + "…";
    }

    function _startTimeout(ms, cb) {
        var t = Qt.createQmlObject('import QtQuick 2.15; Timer { interval: ' + ms + '; repeat: false; running: true }', api);
        t.triggered.connect(function () {
            cb();
            t.destroy();
        });
        return t;
    }

    function _clearTimeout(timerObj) {
        if (timerObj && timerObj.running)
            timerObj.stop();
        if (timerObj)
            timerObj.destroy();
    }
}
