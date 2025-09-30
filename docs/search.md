# Search Feature Documentation

This document describes the unified backend‑driven search feature in `whereami`.

## Overview

The search box (shown with `Ctrl+F`) provides:
- Incremental suggestions (up to 8) while typing.
- Combined local waypoint + remote geocode (Nominatim) results.
- Ranking: local waypoints first (alphabetical), then geocode results.
- Keyboard navigation (Up / Down / Tab / Shift+Tab, Enter to select).
- A persistent highlight marker for the chosen location.
- Bookmark and waypoint differentiation in the UI (icons).
- Tag‑prefixed tag filtering queries (`tag:` …) with AND / OR logic (exact tag matches).
- Zero client‑side filtering logic (server returns already ranked data).
- Indefinite, SQLite‑backed geocoding cache (no TTL / pruning).

Everything related to search suggestions & geocoding now lives in the Go backend. The QML layer only:
1. Debounces user input.
2. Calls `/api/suggest?q=...`.
3. Renders results.
4. Emits `suggestionChosen(...)` when user selects one (keyboard or mouse).

---

## Data Flow

1. User types in the `SearchBox`.
2. After 250ms of idle (debounce), the QML component issues `GET /api/suggest?q=<query>`.
3. Backend:
   - For normal queries: collects **all waypoints** whose names contain the (case‑insensitive) substring.
   - Sorts waypoint matches alphabetically (simple ascending, case‑insensitive).
   - If fewer than 8 suggestions, queries Nominatim (unless cached).
   - Appends geocode results (keeping Nominatim order) until reaching 8 or exhausting remote results.
   - For `tag:` queries: performs tag filtering (no geocode fallback) and returns up to 8 matching waypoints/bookmarks.
4. Backend responds with:
   ```json
   {
     "query": "ber",
     "suggestions": [
       { "name":"Berlin West Trailhead", "lat":52.48, "lon":13.32, "source":"bookmark" },
       { "name":"Berlin Memorial", "lat":52.51, "lon":13.40, "source":"waypoint" },
       { "name":"Berlin, Germany", "lat":52.5170, "lon":13.3889, "source":"geocode", "class":"place", "type":"city" }
     ]
   }
   ```
5. QML displays suggestions with:
   - ★ prefix for `"bookmark"`
   - • prefix for `"waypoint"`
   - 🌐 prefix for `"geocode"`
   - Coordinates formatted to 4 decimals.
6. User selects a suggestion:
   - Map centers at its coordinates.
   - Zoom level is raised to at least 14 (if lower).
   - Persistent highlight marker is placed at the selection (even for bookmarks & waypoints).
   - If a waypoint/bookmark, that waypoint becomes the selected waypoint.
7. Highlight clears if:
   - User clicks on empty map background.
   - User explicitly selects a waypoint (e.g., by clicking it).

---

## Tag Filtering (`tag:` Prefix)

`tag:` queries allow searching waypoints/bookmarks by their associated tags (exact match, case‑insensitive).  
Supported forms (server side):

- `tag:mountain`  
  Returns waypoints/bookmarks having a tag exactly equal (case‑insensitive) to `mountain`.

- `tag:mountain AND lake` or `tag:"mountain AND lake"`  
  Returns items that have **both** tags.

- `tag:mountain OR lake` or `tag:"mountain OR lake"`  
  Returns items that have **either** tag.

Notes:
- Operators `AND` / `OR` are detected case‑insensitively (the backend uppercases internally).
- Quotes around multi‑term expressions are optional; they are accepted for clarity.
- Matching is exact on normalized tag values (case‑insensitive equality). No substring / prefix / fuzzy logic.
- Result set is capped at 8 (same UI limit as normal search). Geocode results are never merged into tag queries. While a `tag:` query is active, the map hides all non‑matching waypoints (and disables clustering) so only the matching tagged waypoints remain visible.
- Returned suggestion objects have `source` set to `"bookmark"` or `"waypoint"` (no `"geocode"` for tag queries).

Offline fallback (when the QML layer has no active API backend):
- Only the single‑tag form (`tag:<single>`) is supported.
- Boolean `AND` / `OR` expressions are NOT evaluated offline.
- Local fallback scans the in‑memory `waypoints` list and matches tags by exact (case‑insensitive) equality.

Example (server mode):
```
GET /api/suggest?q=tag:alpine
```
Response (example):
```json
{
  "query": "tag:alpine",
  "suggestions": [
    { "name": "Matterhorn Summit", "lat": 45.9763, "lon": 7.6586, "source": "waypoint" },
    { "name": "Alpine Hut Nord", "lat": 46.0021, "lon": 7.6120, "source": "bookmark" }
  ]
}
```

Example (boolean):
```
GET /api/suggest?q=tag:glacier AND pass
```
Matches waypoints that have both `glacier` and `pass` as tags.

Limitations / future ideas for tag search:
- No NOT / exclusion operator.
- No grouping parentheses (precedence is a simple single operator: either all ANDs or all ORs detected).
- No partial matching; consider adding prefix search or fuzzy expansion later.
- Offline fallback could be extended to parse AND/OR if needed.

---

## Backend Implementation Notes

### Endpoint: `/api/suggest`

- Method: `GET`
- Query parameter: `q` (or `query` as a fallback).
- Response: JSON object (not a bare array) with fields:
  - `query`: original user query.
  - `suggestions`: ordered list (length ≤ 8) of objects:
    - `name` (string)
    - `lat` (float64)
    - `lon` (float64)
    - `source` ("bookmark" | "waypoint" | "geocode")
    - `class`, `type` (optional; geocode only)

### Ranking

1. Local waypoint set (bookmarks + non‑bookmarks) filtered by substring match.
2. Alphabetical (case‑insensitive) order.
3. Geocode fills remaining slots (normal queries only; suppressed for `tag:` searches).

### Geocoding

- Library: `github.com/muesli/gominatim` (uses `SearchQuery.Get()` API).
- Server: initialized once via:
  - `WHEREAMI_NOMINATIM_SERVER` env var, or
  - default `https://nominatim.openstreetmap.org`.

### Throttling

A simple throttle enforces a minimum interval (400ms) between outbound Nominatim queries to avoid spamming the upstream server when users type quickly and the cache is cold.

### SQLite Cache

- Location: `${XDG_CACHE_HOME:-$HOME/.cache}/whereami/geocode.sqlite`
- Table: `geocode_cache(query TEXT PRIMARY KEY, json TEXT NOT NULL, fetched_at TIMESTAMP NOT NULL)`
- Strategy:
  - Exact query match → reuse cached JSON array.
  - No TTL or pruning (intentionally simple).
  - Stored data: serialized minimal representation of geocode results (display name + lat/lon + class/type).
- Driver: `modernc.org/sqlite` (cgo-free).

---

## QML SearchBox Behavior

- Always backend-driven (legacy in‑QML filtering removed).
- Debounce interval: 250ms (`debounceInterval` property if future tuning is needed).
- Maintains `internalSuggestions` as capped copy of backend results.
- Keyboard support:
  - Arrow Down / Tab → next suggestion
  - Arrow Up / Shift+Tab → previous suggestion
  - Enter:
    - If a suggestion is highlighted → choose it
    - Else if any suggestions exist → chooses the first
    - Else → emits `search(q)` (currently not relied upon by MapView, but kept as extension point)
- Highlights first row automatically when new suggestions arrive (if non-empty).
- Mouse hover updates the highlight index.
- Clicking a suggestion both highlights and emits the selection signal.
- For offline single-tag queries (`tag:<t>`), a local scan is performed without AND/OR support.

---

## Map Highlight Logic

- `searchResultLocation` (QtPositioning.coordinate or null) drives the animated green marker.
- The highlight persists until:
  - Another search suggestion is selected (it moves), or
  - User left-clicks empty map area (cleared), or
  - A waypoint is explicitly selected (cleared).
- Bookmarks & waypoints now also set the highlight (uniform behavior with geocode selections).

---

## Icons / Visual Conventions

| Source     | Prefix | Notes                          |
|------------|--------|--------------------------------|
| bookmark   | ★      | Persisted user bookmark        |
| waypoint   | •      | Imported / non-bookmark point  |
| geocode    | 🌐     | Remote Nominatim result        |

Coordinates appear as `(lat, lon)` with 4 decimal precision.

---

## Configuration & Environment Variables

| Variable                       | Purpose                                        | Default                                |
|--------------------------------|------------------------------------------------|----------------------------------------|
| `WHEREAMI_NOMINATIM_SERVER`    | Override Nominatim base URL                    | `https://nominatim.openstreetmap.org`  |

(You can also eventually add a contact email or custom UA if upstream policy requires; currently the library uses its defaults after server init.)

---

## Extensibility / Future Ideas

1. **Pagination / More Results**  
   Add an optional `limit` query param or a "More…" sentinel row in UI.

2. **Prefix Optimization**  
   Could reuse prior geocode results intelligently for incremental queries (e.g., "ber" → "berl") without extra Nominatim calls.

3. **Geocode Result Details**  
   Provide bounding boxes or feature types for advanced map transitions.

4. **Waypoints Scope Filtering**  
   Optional toggle to restrict search to bookmarks only.

5. **Highlight Management**  
   Add a small “X” button or context menu to remove the highlight without interacting with map background.

6. **User Agent / Contact**  
   Expose config / env var for identifying the application to the Nominatim server.

7. **Result Grouping**  
   Visually group local vs remote results with section headers.

8. **Cache Pruning / Vacuum**  
   Add optional command or periodic task if cache grows (currently unbounded).

---

## Gotchas / Design Rationale

| Aspect | Decision | Rationale |
|--------|----------|-----------|
| Server-side ranking | Yes | Ensures UI stays dumb/simple & consistent. |
| No client substring filtering | Removed | Avoids divergence & reduces QML complexity. |
| Alphabetical waypoint order | Simple & predictable | Avoids fuzzy complexity pending real scoring metrics. |
| No TTL / pruning | Simplicity first | Accept small storage footprint; revisit only if needed. |
| Highlight persistence | User keeps visual reference | Reduces flicker & disorientation vs auto-fade. |
| Always assign highlight (even for bookmarks) | Consistency | Single mental model: selection => highlight. |
| Throttle at 400ms | Politeness to Nominatim | Matches typical debounce > 250ms; low overhead. |
| Tag queries exclude geocode | Clarity & predictable scope | Tag intent implies local data focus. |
| Offline tag fallback single-term only | Simplicity | Avoid partial boolean implementation inconsistencies. |

---

## Minimal Example (API Request)

```
curl 'http://127.0.0.1:43098/api/suggest?q=ber'
```

Example response (truncated):

```json
{
  "query": "ber",
  "suggestions": [
    {"name": "Berlin Trailhead", "lat": 52.4801, "lon": 13.3205, "source": "bookmark"},
    {"name": "Berlin, Germany", "lat": 52.5170365, "lon": 13.3888599, "source": "geocode", "class": "place", "type": "city"}
  ]
}
```

Tag example:

```
curl 'http://127.0.0.1:43098/api/suggest?q=tag:alpine'
```

---

## Maintenance Checklist

When modifying search logic:

1. Keep `/api/suggest` response shape stable (or version it).
2. Update this doc if:
   - Ranking changes
   - Limit changes
   - Cache strategy changes
   - Highlight behavior changes
   - Tag query semantics change
3. Run QML lint:  
   `qmllint-qt6 ui/**/*qml`
4. Confirm keyboard navigation still works after structural changes to the delegate.
5. Optionally add integration tests (future improvement) hitting `/api/suggest` with:
   - Empty query
   - Only waypoint matches
   - Only geocode (no waypoint matches)
   - Mixed results
   - Repeated query (cache hit)
   - Tag single-term
   - Tag AND / OR expressions

---

## FAQ

**Q: Why not fuzzy matching for local waypoints?**  
A: Simplicity & deterministic behavior. Can be upgraded later (e.g., edit distance / trigram scoring).

**Q: Why not auto-clear highlight when map pans?**  
A: User may pan to explore context around the highlighted coordinate; manual clearing gives control.

**Q: Why return an object (with `query`) instead of a bare array?**  
A: Future extensibility (add metadata, paging tokens, diagnostics) without breaking clients.

**Q: How big can the cache get?**  
A: Each entry stores a single compact JSON array. Under typical usage (hundreds of unique queries) size stays modest. If it becomes an issue, pruning or a secondary index can be added.

**Q: Why exclude geocode results for tag queries?**  
A: Geocoding does not understand local tagging semantics; mixing would dilute intent and introduce ordering ambiguity.

---

If you introduce new search enhancements, please update this file to keep contributors aligned.

End of document.