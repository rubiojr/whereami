# QML Application Design

This document describes the architecture currently implemented by WhereAmI. It is a repository guide, not a generic Go/QML template.

## Runtime Structure

`main.go` performs the startup sequence:

1. Parses the `debug`, `theme`, `data-dir`, `config-dir`, and `cache-dir` flags.
2. Resolves and creates the application directories.
3. Copies the embedded `bookmarks.gpx` into the data directory when it is absent.
4. Registers the HTTP handlers from `api.go`.
5. Rebuilds the in-memory waypoint list from bookmarks and imported GPX files.
6. Binds and starts the backend on `127.0.0.1:43098`, exiting if the fixed port is unavailable.
7. Creates the Qt application and loads `qrc:/components/MapView.qml`.

The fixed HTTP port is also configured in `MapView.qml` and `API.qml`.

## Source Layout

```text
main.go                 Qt startup, flags, directories, and HTTP server
api.go                  HTTP handlers, tags/history/geocode databases
storage.go              GPX parsing and bookmark persistence
dedupe.go               waypoint merging and deduplication
location.go             GeoClue integration
fsservices.go           XDG directory helpers
pkg/gominatim/          Nominatim client
pkg/logger/              application logging
ui/components/          visual QML components
ui/services/API.qml     semantic wrapper around backend XHR calls
ui/services/ShortcutsController.qml
ui/lib/                 imperative QML JavaScript logic
ui/themes/              theme variants, loader, and font scale
ui/tests/               Qt Quick Test files
ui/resources.qrc        embedded resource manifest
```

## Embedded Resources

`generate.go` runs:

```text
miqt-rcc -Qt6 -Input ui/resources.qrc -OutputGo resources_gen.go -OutputRcc resources_gen.rcc -Package main
```

Only files listed in `ui/resources.qrc` are embedded. `resources_gen.go` and `resources_gen.rcc` are generated and ignored by Git.

QML, JavaScript, icon, `qmldir`, or test changes that affect the resource manifest require resource regeneration and a rebuild. The normal command is:

```bash
make build
```

It runs `go generate` before `go build`. There is no filesystem hot-reload path in the application; runtime loading uses `qrc:/` URLs.

The active root is `ui/components/MapView.qml`, loaded as `qrc:/components/MapView.qml`.

## Backend State and Persistence

The global `allWaypoints` slice is protected by `allWaypointsMu`. It contains bookmarks and imported GPX waypoints. `RebuildAllWaypoints` and `DedupeWaypoints` construct a coordinate/name-deduplicated view for the API.

Bookmark writes are serialized by `bookmarkMu`. `writeBookmarks` writes `bookmarks.gpx.tmp` and then renames it over `bookmarks.gpx`.

Persistent state is split by purpose:

- Data directory: `bookmarks.gpx`, `imports/`, `tags.sqlite`, and `history.sqlite`
- Cache directory: `geocode.sqlite`, the rebuildable observation index, administrative geodata state, the Go renderer's `vector/` tile cache, and the optional legacy `maplibre/` cache

The command-line directory flags override the XDG-derived defaults.

## HTTP Boundary

`RegisterAPI` uses method-aware `http.ServeMux` patterns for bookmarks, waypoints, clusters, location, GPX import, tags, suggestions, search history, and version information. [docs/api.md](docs/api.md) is the endpoint and QML-service reference.

`ui/services/API.qml` owns semantic XHR calls used by the visual components. It provides operation-specific signals plus generic `requestSucceeded` and `requestFailed` signals. Components such as `SearchBox`, `WaypointInfoCard`, and `AboutOverlay` receive the service as a property rather than constructing their own XHR objects.

OpenFreeMap traffic does not cross the local HTTP boundary. The default Go
renderer fetches vector and Natural Earth raster tiles directly. The optional
legacy path uses `OpenFreeMapPlugin.qml`, whose MapLibre QtLocation provider
also fetches its style and tiles directly.

## QML State

`MapView.qml` is the `ApplicationWindow` and owns the main presentation state:

- Waypoint and cluster models
- Selected waypoint and map highlight
- Search, table, and information-card visibility
- Bookmark-only, tag-filter, and UTC date-range state
- Current GeoClue location
- Import and undo notifications

Array updates are generally made by copying and reassigning the array so QML bindings observe the change.

`ShortcutsController.qml` centralizes application shortcuts and emits action signals back to `MapView`. The in-app list is implemented by `HelpOverlay.qml`.

`SearchBox.qml` delegates nonvisual search state changes to `ui/lib/SearchBoxLogic.js`. `MapView.qml` uses `ui/lib/MapViewLogic.js` for waypoint lookup, bounds, filtering, path normalization, and local clustering. The JavaScript functions receive their state as parameters rather than retaining QML objects globally.

## Map Rendering

`main.go` creates two independent Go vector items and cameras by default: one
for the persistent main view and one for the loader-controlled timeline view.
They share the same on-disk tile cache but retain independent cameras, workers,
and Qt scene graphs. `MapAdapter.qml` presents the map API used by both views,
attaches the selected Go item to its visual host, and projects ordinary QML
markers and polylines through the Go camera bridge.

`--legacy-map-renderer` makes each adapter load a QtLocation `Map` configured by
`OpenFreeMapPlugin.qml`. The same path is activated automatically when Go
renderer initialization fails or when every requested Go tile fails. If no
MapLibre provider is installed, the plugin selects QtLocation's overlay-only
provider so waypoint interaction remains available and
`MapProviderOverlay.qml` explains that the basemap is unavailable.

`MapProviderOverlay.qml` supplies explicit, clickable OpenFreeMap, OpenMapTiles,
and OpenStreetMap attribution. Each view declares it beside its map rather than
inside it, so the attribution shares a coordinate space with the other map-area
overlays and cannot be painted over; the Timeline panel reserves room for it
through `attributionHeight`.

The main map is directly navigable: panning uses `DragHandler`, wheel zoom uses
`WheelHandler`, and touch zoom/rotation uses `PinchHandler`. The Timeline map
declares no gesture handlers; its camera is driven entirely by timeline
navigation.

The Go renderer shares `${effectiveCacheDir}/vector` between both items. It
uses checksum sidecars, atomic pair commits, read recency, stale temporary-file
cleanup, and pair-wise oldest-first eviction under a 256 MiB limit. The pinned
fixture tile is retained. The Liberty style and sprite atlas are embedded and
checksum-pinned. Map labels use OpenFreeMap's range-based SDF glyph PBFs,
checksum-backed disk caching, metric-based layout, a bounded per-scene atlas,
and a public-QSG shader with numeric halo width and blur. The committed shader
packs target Qt 6.5 and include Vulkan, OpenGL/ES, Direct3D, and Metal variants.
Checksum-pinned KlokanTech Noto Sans remains registered for labels that still
need Qt complex-script shaping while the SDF shaper is expanded.

`main.go` also injects `whereamiMapCacheDir` for the optional legacy backend.
`OpenFreeMapPlugin.qml` passes it as `maplibre.cache.directory` together with a
256 MB `maplibre.cache.size`. Without that directory MapLibre uses an in-memory
cache for the session.

The active waypoint, cluster, current-location, and search-result marker delegates are defined inline in `MapView.qml`. The standalone marker QML files registered in `ui/components/qmldir` are not themselves listed in `ui/resources.qrc`; the `qmldir` registry is embedded.

Backend clustering handles the complete waypoint set and the bookmark-only subset. A tag- or date-filtered subset is clustered locally by `MapViewLogic.buildLocalClusters`.

The toolbar's `DateRangePicker.qml` selects an inclusive UTC calendar-date range. It supports individual days, custom ranges, and preset ranges; waypoints without a valid timestamp do not match an active date filter.

## Themes and Typography

`ui/themes/ThemeLoader.qml` selects a theme from `Qt.application.arguments`. Supported values are:

- `orange`
- `green`
- `purple`
- `adwaita-dark`
- `nord-polar`
- `nord-frost`

`nord-polar` is the fallback and default. Selection is runtime behavior:

```bash
bin/whereami --theme=nord-frost
```

Theme files live in `ui/themes/`. `ThemeLoader` forwards the common color and component-style properties from the loaded theme.

`Fonts.qml` is a QML singleton declared in `ui/themes/qmldir`. Its default modular scale uses `minFontSize` 12 and `fontScaleRatio` 1.15. `ThemeLoader.scale(step)` substitutes those defaults when the active theme does not provide positive numeric overrides, then calls `Fonts.scale`.

Components should use `theme.scale(step)` for theme-aware text sizing. `Fonts.scale` clamps the step to 32 and rounds calculated sizes to whole pixels.

## Adding Embedded QML

For a new component:

1. Add the QML file under the appropriate `ui/` directory.
2. Add it to `ui/resources.qrc` if the runtime must load it.
3. Add a `qmldir` entry when the local module registry must resolve it.
4. Run `make build` to regenerate resources and compile the application.
5. Run the QML checks.

## Checks

Go checks:

```bash
go test ./...
go vet ./...
```

QML checks:

```bash
make lint-qml
make qml-test
```

`make lint-qml` runs `qmllint-qt6` when available. `make qml-test` runs Qt Quick Test against `ui/tests` and fails when no QML test runner is installed.

Qt Quick Test discovers files named `tst_*.qml`. The current tests import the JavaScript libraries directly and exercise them with QML mock objects.
