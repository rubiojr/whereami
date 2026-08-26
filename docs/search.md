# Search

The search box is opened with `Ctrl+F`. Its behavior is split between the Go backend, `SearchBox.qml`, and `SearchBoxLogic.js`.

## Normal Search

For text that does not start with `tag:`:

1. `SearchBox.qml` waits 250 ms after the latest edit.
2. It calls `GET /api/suggest?q=<query>` through `API.qml`.
3. The backend finds waypoint names containing the query, case-insensitively.
4. Local matches are sorted alphabetically, case-insensitively.
5. If fewer than eight local results exist, Nominatim geocoding fills the remaining slots.
6. The UI displays at most eight suggestions.

The result source controls the prefix:

| Source | Prefix |
| --- | --- |
| Bookmark | `★` |
| Imported waypoint | `•` |
| Geocode result | `🌐` |
| Recent search | `🕘` |
| Tag vocabulary | `🏷` |

Coordinates are displayed to four decimal places when present.

## Selection Behavior

Choosing a waypoint or bookmark:

- Centers the map and animates it to zoom level 17
- Sets the corresponding waypoint as selected
- Shows a pulsing marker using the active theme's search-result color

Choosing a geocode result performs the same map transition and creates a transient `GEOCODE` selection. Its information card includes an **Add Bookmark** action.

The search marker is cleared by clicking the empty map background or selecting a map waypoint marker. Selecting a row in the waypoint table does not clear it.

## Keyboard Navigation

- `Down` or `Tab`: next suggestion
- `Up` or `Shift+Tab`: previous suggestion
- `Enter` or `Return`: choose the highlighted suggestion

New non-empty result lists highlight the first row. Mouse hover changes the highlighted row, and clicking a row selects it.

## Recent Searches

When the input is empty, the QML service requests the ten most recent distinct queries from `GET /api/recent_suggest?limit=10`. Entries can include coordinates. Explicit selection and Enter submissions are recorded with `POST /api/history`.

History is stored in `history.sqlite` in the effective data directory. It is not stored in the geocoding cache.

## Simple Tag Queries

A simple query has one unquoted term, for example:

```text
tag:mountain
```

For this form, the QML layer:

- Fetches the distinct enriched tag vocabulary from `GET /api/tags?distinct=true&emoji=true`
- Builds up to eight prefix/substring completions locally for text fragments; symbol-only fragments use strict prefix matching
- Compares a completed tag against the loaded waypoint tags using case-insensitive exact equality
- Filters and fits the map when at least one exact local match exists
- Preserves clustering by rebuilding clusters locally over the filtered waypoint set

If no exact local waypoint match exists, the map filter is cleared.

Symbol-only completion preserves repeated symbols. For example, `tag:***` is distinct from `tag:*`.

## Boolean and Quoted Tag Queries

Queries containing ` AND `, ` OR `, or a quote call `/api/suggest` immediately instead of starting the normal 250 ms debounce path. Supported backend forms include:

```text
tag:mountain AND lake
tag:"mountain AND lake"
tag:mountain OR lake
tag:"mountain OR lake"
```

Tag matching is case-insensitive exact equality. `AND` requires every term and `OR` requires at least one term. Results are sorted alphabetically and capped at eight. Geocoding is not used for tag queries.

Boolean and quoted queries affect the suggestion list only. They clear the local map tag filter rather than hiding nonmatching markers.

The backend does not implement `NOT`, parentheses, or mixed `AND`/`OR` precedence. It checks for `AND` first, then `OR`.

## Geocoding Cache and Throttle

The backend initializes Nominatim once. It uses:

- `WHEREAMI_NOMINATIM_SERVER`, defaulting to `https://nominatim.openstreetmap.org`
- `WHEREAMI_NOMINATIM_RETRIES`, defaulting to one retry and accepting values from 0 through 5

Outbound Nominatim requests are separated by at least 400 ms. Successful non-empty responses are stored indefinitely in `geocode.sqlite` in the effective cache directory. There is no TTL or pruning.

## Endpoint Shape

`GET /api/suggest` accepts `q`, with `query` as a fallback parameter, and returns:

```json
{
  "query": "ber",
  "suggestions": [
    {"name":"Berlin Trailhead","lat":52.4801,"lon":13.3205,"source":"bookmark"},
    {"name":"Berlin, Germany","lat":52.517,"lon":13.389,"source":"geocode","class":"place","type":"city"}
  ]
}
```

An empty query returns an empty `suggestions` array.
