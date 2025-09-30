# API Documentation

Central reference for the QML ↔ Go HTTP API integration used by `whereami`.

This file documents:
1. High‑level architecture
2. QML usage of the central `API.qml` service
3. Available convenience methods, their HTTP endpoints and payloads
4. Signals emitted by the service
5. Data shapes / examples
6. Tag + rename workflows
7. Offline / degraded mode
8. Error handling & patterns
9. Extensibility & migration notes

---

## 1. Architecture Overview

The application uses a single QML service object (`ui/services/API.qml`) as a thin wrapper over the backend HTTP API served by the Go process.

Goals:
- Single surface for all network I/O (easy to audit & refactor)
- Semantic signals instead of ad‑hoc XHR scattered across components
- Predictable offline behavior (when `apiPort < 0`)
- Easy future swap: HTTP → in‑process QObject invokables

Key concepts:
- Every higher‑level UI component (e.g. `MapView`, `WaypointInfoCard`, `SearchBox`) *calls methods* on the `api` object and *listens to signals*; these components do not issue raw `XMLHttpRequest`.
- Mutating calls optimistically update UI state where it makes sense (`MapView` pushes a provisional waypoint on `waypointAddStarted`, rolls back on `waypointAddFailed`).
- Tag management & bookmark rename are now centralized (the view components became passive).

---

## 2. Quick Start (QML Integration)

Place an instance high in the object tree (e.g. inside `MapView.qml`):

```
import "services"

API {
    id: api
    apiPort: 43098  // dynamically injected by Go startup code, or -1 for offline
    onWaypointsLoaded: function(list) { window.waypoints = list }
    onWaypointAdded: function(saved, original) { /* merge into array */ }
    onWaypointAddFailed: function(original, err) { console.error(err) }
}
```

Using the API in child components:

```
WaypointInfoCard {
    id: info
    waypoint: selectedWaypoint
    api: api         // pass the reference down
}
```

Typical calls:

```
api.getWaypoints()
api.getClusters(map.zoomLevel, desiredGridSize)
api.getLocation()
api.addWaypoint({ name: "Camp 1", lat: 51.2345, lon: -0.1234, tags: ["camp","day1"] })
api.deleteWaypoint(waypointObject)
api.renameWaypoint(waypointObject, "New Name")
api.fetchTags(waypointObject)
api.addTag(waypointObject, "newTag")
api.deleteTag(waypointObject, "obsoleteTag")
api.importGpxDirectory({ dir: "/path/to/gpx", recursive: true })
api.suggest("berlin")
```

Generic escape hatch:

```
api.request("/api/tiles/stats", {
    method: "GET",
    onSuccess: function(txt) { console.log("stats:", txt) },
    onError: function(e) { console.warn("stats error:", e) }
})
```

---

## 3. Signals Reference

Generic:
- requestSucceeded(kind, result, context)
- requestFailed(kind, errorMessage, context)

Waypoints / Bookmarks:
- waypointsLoadStarted()
- waypointsLoaded(waypointsArray)
- waypointsLoadFailed(errorMessage)

- waypointAddStarted(originalWaypoint)
- waypointAdded(savedWaypoint, originalWaypoint)
- waypointAddFailed(originalWaypoint, errorMessage)

- waypointDeleteStarted(waypoint)
- waypointDeleted(waypoint)
- waypointDeleteFailed(waypoint, errorMessage)

Rename:
- waypointRenameStarted(originalWaypoint, newName)
- waypointRenamed(updatedWaypoint, originalWaypoint)
- waypointRenameFailed(originalWaypoint, newName, errorMessage)

Tags:
- tagsFetchStarted({ name, lat, lon })
- tagsFetched(name, lat, lon, tagsArray)
- tagsFetchFailed({ name, lat, lon }, errorMessage)

- tagAddStarted(name, lat, lon, tag)
- tagAdded(name, lat, lon, tagsArray, tag)
- tagAddFailed(name, lat, lon, tag, errorMessage)

- tagDeleteStarted(name, lat, lon, tag)
- tagDeleted(name, lat, lon, tagsArray, tag)
- tagDeleteFailed(name, lat, lon, tag, errorMessage)

Clusters:
- clustersFetchStarted(params)
- clustersFetched(clustersArray, params)
- clustersFetchFailed(errorMessage, params)

Location:
- locationFetchStarted()
- locationFetched(locationObj)
- locationFetchFailed(errorMessage)

Import:
- importStarted(params)
- importCompleted(summaryObj, params)
- importFailed(errorMessage, params)

Suggestions:
- suggestStarted(query)
- suggestResults(resultObject, query)
- suggestFailed(errorMessage, query)

---

## 4. Convenience Methods → HTTP Endpoints

| Method | HTTP | Endpoint | Notes |
|--------|------|----------|-------|
| getWaypoints() | GET | /api/waypoints | Returns array of waypoints (may include `tags` if DB active) |
| addWaypoint(wp) | POST | /api/bookmarks | Body `{ name, lat, lon, tags? }`, creates bookmark |
| deleteWaypoint(wp) | DELETE | /api/bookmarks?name=&lat=&lon= | |
| renameWaypoint(wp, newName) | PATCH | /api/bookmarks | Body `{ oldName, lat, lon, newName }` |
| getClusters(zoom, grid) | GET | /api/clusters?zoom=&grid= | Server clusters waypoints |
| getLocation() | GET | /api/location | System / GeoClue position |
| importGpxDirectory({dir,recursive}) | POST | /api/import | Long‑running (extended timeout) |
| suggest(query) | GET | /api/suggest?q= | Mixed local + geocode suggestions |
| fetchTags(wp) | GET | /api/tags?name=&lat=&lon= | Bookmark tags |
| addTag(wp, tag) | POST | /api/tags | Body `{ name, lat, lon, tags:[tag] }` |
| deleteTag(wp, tag) | DELETE | /api/tags?name=&lat=&lon=&tag= | |
| request(path, options) | custom | (any) | Generic helper |

---

## 5. Data Shapes

Waypoint (typical):
```
{
  "name": "Summit",
  "lat": 51.50001,
  "lon": -0.12003,
  "ele": 215.4,            // optional
  "time": "2025-09-23T07:59:00Z", // optional
  "desc": "Short note",    // optional
  "bookmark": true,        // present if user-added
  "tags": ["peak","day2"]  // present if tag DB available / fetched
}
```

Cluster item:
```
{
  "lat": 51.5,
  "lon": -0.12,
  "count": 14
}
```

Suggestion result object:
```
{
  "query": "ber",
  "suggestions": [
    { "name": "Berlin Memorial", "lat": 52.51, "lon": 13.40, "source": "waypoint" },
    { "name": "Berlin, Germany", "lat": 52.5170, "lon": 13.3889, "source": "geocode", "class": "place", "type": "city" }
  ]
}
```

Tags fetch response (raw backend):
```
{
  "name": "Summit",
  "lat": 51.5,
  "lon": -0.12,
  "tags": ["peak","view"]
}
```

Location object:
```
{
  "lat": 51.50012,
  "lon": -0.12111,
  "accuracy_m": 14.2
}
```

Import summary (may vary):
```
{
  "count": 42,
  "skipped": 3
}
```

---

## 6. Tag Management Workflow

1. User opens a bookmark (only bookmarks persisted server-side).
2. UI calls `api.fetchTags(waypoint)`.
3. On `tagsFetched`: update local `waypoint.tags` and any displayed chip list.
4. Adding a tag:
   - UI calls `api.addTag(wp, "newTag")`.
   - On `tagAdded`: replace displayed list with the authoritative returned `tags`.
5. Deleting a tag:
   - UI calls `api.deleteTag(wp, "oldTag")`.
   - On `tagDeleted`: update list.

Important: The service returns the *full tags array* after add/delete. Always replace your UI list (avoid local incremental mutation errors).

---

## 7. Rename Semantics

- Only bookmarks can be renamed server‑side.
- `renameWaypoint(wp, newName)`:
  - Emits `waypointRenameStarted`.
  - Backend validates and renames; on success emits `waypointRenamed(updated, original)`.
  - On failure emits `waypointRenameFailed(original, attemptedName, error)`.
- Non‑bookmark waypoints: The card (or caller) can directly mutate `wp.name` if desired, then emit a local refresh (`nameEdited` in `WaypointInfoCard`).

Recommendation: Do *not* pre‑mutate the bookmark’s `name` before the server acknowledges; let the success signal drive the UI update (prevents flicker on failure).

---

## 8. Offline / Degraded Mode

If `apiPort < 0`:
- Mutating methods simulate immediate success.
- Lists return empty arrays or safe placeholders.
- Signals still fire in the same sequence (e.g. `waypointsLoadStarted` → `waypointsLoaded([])`).
- Purpose: design-time preview / tests without backend process.

Do not special‑case offline logic in view components—trust the simulated success signals.

---

## 9. Error Handling Patterns

Each operation’s failure signal provides a human-readable `errorMessage` already containing:
- HTTP status code (e.g., `HTTP 409 duplicate`)
- Truncated body tail (if available, capped at 160 chars)
- Or a transport error (`timeout (8000 ms)`, `send error: ...`)

UI guidance:
- For add/delete failures: revert optimistic list modifications (if you applied them).
- For rename failures: reset the displayed name and show a snackbar / console log.
- For tag failures: either re-fetch (`api.fetchTags`) or revert to previous cached list.

---

## 10. Using `request(path, options)`

Signature:
```
api.request("/api/tiles/stats", {
  method: "GET",
  body: null,            // object or string (auto JSON if object)
  timeout: 5000,         // optional override
  context: { purpose: "tilesStats" },
  onSuccess: function(text, ctx) { ... },
  onError:   function(msg,  ctx) { ... }
})
```

Behavior:
- Automatically prefixes base URL (`http://127.0.0.1:<apiPort>`).
- Emits `requestSucceeded` / `requestFailed`.
- Passes raw response text to handler (caller can JSON.parse).

Use-case: Experimental / new endpoints before adding a first‑class helper method.

---

## 11. Recommended UI Integration Pattern

Example (waypoint add):

```
function addBookmark(name, lat, lon) {
  var wp = { name: name, lat: lat, lon: lon, tags: [] }
  api.addWaypoint(wp)
}

Connections {
  target: api
  function onWaypointAddStarted(original) {
    // optimistic: push placeholder
    var arr = waypoints.slice()
    arr.push(original)
    waypoints = arr
  }
  function onWaypointAdded(saved, original) {
    // replace placeholder by coordinate match
    var arr = waypoints.slice()
    for (var i=0; i<arr.length; i++) {
      if (arr[i] === original ||
          (Math.abs(arr[i].lat-original.lat) < 1e-9 &&
           Math.abs(arr[i].lon-original.lon) < 1e-9))
      {
        arr[i] = saved
        break
      }
    }
    waypoints = arr
  }
  function onWaypointAddFailed(original, error) {
    console.warn("Add failed:", error)
    // remove placeholder
    var arr = waypoints.filter(function(w) { return w !== original })
    waypoints = arr
  }
}
```

---

## 12. Performance Notes

- Clusters are fetched only when zoom/grid changes or after a waypoint mutation (avoid spamming).
- Suggest queries should be debounced (SearchBox does this) to limit network chatter.
- Tag add/delete operations are cheap; no need for client-side batching unless you add multi-select.

---

## 13. Extensibility

Adding a new backend endpoint:
1. Implement the handler in Go and register under `/api/...`.
2. Add a convenience method + signals (if semantic events help) to `API.qml`.
3. Replace any prototype raw XHR in QML with that method.
4. Update this document (new table row + signal section).

If multiple upcoming endpoints share patterns (e.g. file uploads), consider factoring an internal `_upload` helper (similar to `_xhr`).

---

## 14. Troubleshooting Quick Reference

| Symptom | Likely Cause | Fix |
|---------|--------------|-----|
| No waypoints displayed | Backend not started / `apiPort` wrong | Ensure Go server running; inject correct port |
| Tags never appear | Not a bookmark or tag DB uninitialized | Confirm waypoint has `bookmark: true`; check log |
| Rename silently does nothing | New name identical or blank | UI should pre-validate / handle `waypointRenameFailed` |
| Suggest always empty | Query length 0 / network blocked | Log network, inspect `/api/suggest?q=test` |
| Timeout errors | Slow I/O / long import | Increase per-call timeout (e.g., import already sets 60s) |
| Cluster list stale | Missing refresh after mutation | Call `getClusters` or rely on existing `clusterFetchDebounce` |

---

## 15. Security / Safety Considerations

- Current local HTTP server binds 127.0.0.1; not exposed externally.
- No auth layer; do not expose outside local machine without adding authentication / CORS tightening.
- Tag & bookmark names are written to files / DB; future enhancement: sanitize or enforce a character set.

---

## 16. Example Complete Minimal Map Integration (Pseudo)

```
API {
  id: api
  apiPort: 43098
  onWaypointsLoaded: function(list) { root.model = list }
  onWaypointDeleted: function(wp) { console.log("Deleted:", wp.name) }
  onWaypointDeleteFailed: function(wp, err) { console.warn(err) }
}

Component.onCompleted: {
  api.getWaypoints()
  api.getLocation()
}

function removeSelected() {
  if (selected && selected.bookmark)
    api.deleteWaypoint(selected)
}
```

---

## 17. Future Improvements (Roadmap Ideas)

- Batch tag operations (POST multiple additions/removals at once)
- Streaming import progress signals (instead of only start/end)
- In-flight request cancellation management
- Rate limiting layer inside `API.qml` (currently each call unconditional)
- Central retry policy for transient network errors
- QAbstractListModel exposure for waypoints (to improve large list binding performance)

---

## 18. Summary

Use `API.qml` as the **only** entrypoint for backend operations. Components should:
- Call the appropriate convenience method.
- Listen to the corresponding signals.
- Keep their own *local presentation state* (selection, temporary form values) without mutating backend objects beyond what signals confirm.

This design ensures that migrating to a non-HTTP bridge or adding richer telemetry later is a drop‑in internal change.

---

End of document.