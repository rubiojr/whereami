# QML and Go HTTP API

This document describes the HTTP integration implemented by `api.go`, `geodata_service.go`, and `ui/services/API.qml`.

## Architecture

The Go process serves an authenticated HTTP API on the fixed loopback address `127.0.0.1:43098`. A random bearer token is generated for each process, injected into QML, and attached by `API.qml` to every private API request.

Semantic XHR operations from QML components go through `ui/services/API.qml`. The map tile path is the exception: QtLocation requests `http://127.0.0.1:43098/api/tiles/%z/%x/%y.png` directly from the local tile proxy.

The API is loopback-only. Coordinate requests matching `GET /api/tiles/{z}/{x}/{y}.png` are the only unauthenticated routes because QtLocation cannot attach the bearer token. Tile statistics and malformed tile-like paths remain private. The server does not grant browser CORS access.

## QML Usage

```qml
import "../services"

API {
    id: api
    apiPort: 43098
	apiToken: whereamiApiToken
    onWaypointsLoaded: function (waypoints) {
        window.waypoints = waypoints
    }
}
```

Child components receive the same service through an `api` property and react to its semantic signals.

## Convenience Methods

| QML method | HTTP request | Current behavior |
| --- | --- | --- |
| `getWaypoints()` | `GET /api/waypoints?emoji=true` | Loads all waypoints and enriches tag arrays |
| `addWaypoint(wp)` | `POST /api/bookmarks` | Creates a bookmark from `{name,lat,lon,tags?}` |
| `deleteWaypoint(wp)` | `DELETE /api/bookmarks?name=&lat=&lon=` | Deletes a matching bookmark |
| `renameWaypoint(wp, newName)` | `PATCH /api/bookmarks` | Renames a matching bookmark |
| `getClusters(zoom, gridSize, bookmarksOnly)` | `GET /api/clusters?zoom=&grid=&bookmarksOnly=1` | Requests server clusters; the final parameter is optional |
| `getLocation()` | `GET /api/location` | Emits the parsed GeoClue fix; an HTTP 204 empty response is emitted as `locationFetched(null)` |
| `importGpxDirectory(params)` | `POST /api/import` | Atomically imports regular GPX files from a directory; `params` is `{dir,recursive}` and the QML timeout is 110 seconds |
| `suggest(query)` | `GET /api/suggest?q=` | Returns local waypoint/tag results and, for normal text, geocoding results |
| `getRecentSearches(limit)` | `GET /api/recent_suggest?limit=` | Loads distinct recent queries and optional coordinates |
| `recordHistory(query, lat, lon)` | `POST /api/history` | Records a query with optional coordinates |
| `fetchTags(wp)` | `GET /api/tags?name=&lat=&lon=&emoji=true` | Loads enriched tags for one waypoint |
| `addTag(wp, tag)` | `POST /api/tags?emoji=true` | Adds one raw tag and returns the complete enriched tag list |
| `deleteTag(wp, tag)` | `DELETE /api/tags?name=&lat=&lon=&tag=&emoji=true` | Deletes one raw tag and returns the remaining enriched list |
| `fetchDistinctTags(callback)` | `GET /api/tags?distinct=true&emoji=true` | Calls back with the enriched global tag vocabulary |
| `getVersion()` | `GET /api/version` | Loads Go runtime, platform, module, and available VCS build information |
| `getGeodataStatus()` | `GET /api/geodata` | Loads local generation metadata and install progress without exposing URLs or paths |
| `installGeodata(id)` | `POST /api/geodata/install` | Starts one verified background install for a build-selected generation |
| `cancelGeodataInstall()` | `DELETE /api/geodata/install` | Cancels the active geodata install |
| `submitPlaceReport(start, end)` | `POST /api/place-reports` | Queues an offline UTC report for inclusive calendar dates |
| `getPlaceReport(id, includeResult)` | `GET /api/place-reports/{id}` | Polls job state; `includeResult=true` fetches a completed result with a longer timeout |
| `cancelPlaceReport(id)` | `DELETE /api/place-reports/{id}` | Cancels a queued or running report |
| `request(path, options)` | Caller-selected | Low-level request helper for an API path |

`request()` accepts `method`, `body`, `timeout`, `context`, `onSuccess`, and `onError`. It prefixes the fixed loopback base URL and emits `requestSucceeded` or `requestFailed`.

## Backend Endpoints

`RegisterAPI` registers these method-aware routes:

```text
OPTIONS /api/bookmarks
POST    /api/bookmarks
PATCH   /api/bookmarks
DELETE  /api/bookmarks
GET     /api/waypoints
GET     /api/clusters
GET     /api/tiles/stats
GET     /api/tiles/{z}/{x}/{y}.png
GET     /api/location
POST    /api/import
GET     /api/tags
POST    /api/tags
DELETE  /api/tags
GET     /api/suggest
GET     /api/recent_suggest
POST    /api/history
GET     /api/version
GET     /api/geodata
POST    /api/geodata/install
DELETE  /api/geodata/install
POST    /api/place-reports
GET     /api/place-reports/{id}
DELETE  /api/place-reports/{id}
```

The embedded manifest advertises only immutable, hash-pinned WhereAmI-hosted
Xiangshan artifacts. The application never falls back to mutable upstream URLs.

Place-report jobs use one worker, permit at most eight queued jobs, track at most
sixteen jobs, and expire terminal results after fifteen minutes. Each running
job has a ten-minute execution deadline; queue time does not consume that
budget. Date ranges are limited to 7,320 inclusive UTC calendar days. Backend
errors are logged locally; API failures use fixed messages so observation paths
and coordinates cannot leak through error responses.

`GET /api/place-reports/{id}` omits the potentially large report payload by
default. Add `?result=true` after the state reaches `completed` to fetch it.
The authenticated result contains coordinates only for its significant journey
stops so the local QML map can navigate between them. Administrative aggregate
rows remain coordinate-free, and source paths are never serialized in the
result.

## Signals

The service declares the following signal groups.

Generic:

- `requestSucceeded(kind, result, context)`
- `requestFailed(kind, errorMessage, context)`

Waypoints and bookmarks:

- `waypointsLoadStarted`, `waypointsLoaded`, `waypointsLoadFailed`
- `waypointAddStarted`, `waypointAdded`, `waypointAddFailed`
- `waypointDeleteStarted`, `waypointDeleted`, `waypointDeleteFailed`
- `waypointRenameStarted`, `waypointRenamed`, `waypointRenameFailed`

Tags and clusters:

- `tagsFetchStarted`, `tagsFetched`, `tagsFetchFailed`
- `tagAddStarted`, `tagAdded`, `tagAddFailed`
- `tagDeleteStarted`, `tagDeleted`, `tagDeleteFailed`
- `clustersFetchStarted`, `clustersFetched`, `clustersFetchFailed`

Location, import, and search:

- `locationFetchStarted`, `locationFetched`, `locationFetchFailed`
- `importStarted`, `importCompleted`, `importFailed`
- `suggestStarted`, `suggestResults`, `suggestFailed`
- `recentSearchesFetchStarted`, `recentSearchesFetched`, `recentSearchEntriesFetched`, `recentSearchesFetchFailed`
- `versionFetchStarted`, `versionFetched`, `versionFetchFailed`
- `geodataStatusFetched`, `geodataStatusFailed`
- `geodataInstallAccepted`, `geodataInstallFailed`, `geodataCancelAccepted`, `geodataCancelFailed`
- `placeReportSubmitted`, `placeReportSubmitFailed`
- `placeReportStatusFetched`, `placeReportStatusFailed`, `placeReportCancelAccepted`, `placeReportCancelFailed`

## Data Shapes

### Waypoint returned by `getWaypoints()`

```json
{
  "name": "Summit",
  "lat": 51.50001,
  "lon": -0.12003,
  "ele": 215.4,
  "time": "2025-09-23T07:59:00Z",
  "desc": "Short note",
  "bookmark": true,
  "tags": [
    {
      "raw": "mountain",
      "emoji": "⛰️",
      "name": "mountain",
      "display": "⛰️ mountain",
      "normal": "mountain"
    }
  ]
}
```

`ele`, `time`, `desc`, `bookmark`, and `tags` can be absent. The QML service requests `emoji=true` and normalizes any raw-string tags into objects containing at least `raw` and `display`. Tag mutation methods still accept raw tag strings.

### Cluster response items

A cluster response can contain an individual waypoint:

```json
{"type":"waypoint","name":"Camp","lat":51.5,"lon":-0.12,"bookmark":true}
```

or an aggregate:

```json
{"type":"cluster","lat":51.5,"lon":-0.12,"count":14}
```

### Suggestion response

```json
{
  "query": "ber",
  "suggestions": [
    {"name":"Berlin Memorial","lat":52.51,"lon":13.40,"source":"waypoint"},
    {"name":"Berlin, Germany","lat":52.517,"lon":13.389,"source":"geocode","class":"place","type":"city"}
  ]
}
```

`source` is `bookmark`, `waypoint`, or `geocode`. Tag-query results also set `class` to `tag` and `type` to `single`, `AND`, or `OR`.

### Recent-search response

```json
{
  "queries": ["Berlin"],
  "entries": [{"query":"Berlin","lat":52.517,"lon":13.389}]
}
```

Coordinates are optional. The backend accepts limits from 1 through 200 and defaults to 10; the current QML search box requests 10.

### Import response

```json
{
  "imported": true,
  "dir": "/path/to/gpx",
  "count": 42,
  "files": 3,
  "added": 2,
  "replaced": 1,
  "skipped_files": ["existing.gpx"],
  "skipped": 1,
  "duplicate_files": ["existing.gpx"],
  "duplicates": 1,
  "unsupported_files": [],
  "unsupported": 0,
  "failed_files": ["broken.gpx"],
  "failed": 1,
  "dedup_count": 120
}
```

Relative paths are preserved below the private imports directory. A changed file replaces its previous relative path atomically. Unsupported entries, exact duplicates, and malformed GPX files are reported separately; valid files in the same request can still be imported.

## Persistence

- `bookmarks.gpx`, copied imports, `tags.sqlite`, and `history.sqlite` live in the effective data directory.
- `geocode.sqlite` and the default `tiles/` tree live in the effective cache directory.
- Search history is separate from the geocoding cache.

## Offline Preview Mode

Setting `apiPort` below zero enables the service's QML-only fallback behavior. Waypoint, cluster, suggestion, recent-search, and distinct-tag requests return empty results. `fetchTags` normalizes tags already present on the waypoint. Bookmark and tag mutations emit local simulated results, imports report zero counts, the location is `(0,0)`, history recording is a no-op, and version fields report unknown values. Simulated tag mutation arrays are not enriched in this mode. The normal application uses port 43098.

## Errors and Timeouts

The default QML request timeout is 8 seconds; GPX import uses 60 seconds. Failures include an HTTP status and up to 160 response characters, or a timeout/send error. Each specialized method emits its operation-specific failure signal and the generic `requestFailed` signal.
