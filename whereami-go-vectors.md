# WhereAmI Go Vector Map

Status: implementation and design record
Research date: 2026-08-28

## Decision

WhereAmI should keep a real interactive vector map.

The target is a purpose-built, retained 2D vector-tile renderer written mostly
in Go and integrated into Qt Quick through miqt. It is not a raster tile path,
not a full MapLibre Native clone, and not a general implementation of every
MapLibre style feature.

The renderer should be designed around the OpenFreeMap/OpenMapTiles data used
by WhereAmI and around the map operations WhereAmI actually exposes. The main
scope reduction is to compile and control a WhereAmI style instead of accepting
arbitrary MapLibre styles at runtime.

This removes the separately built MapLibre Native C++ engine and its private Qt
ABI coupling. It does not make the application literally free of C++: Qt is a
C++ framework and miqt is a generated CGO/C++ bridge. The intended constraint
is no hand-maintained C++ map engine and no MapLibre Native runtime dependency.

## Executive Summary

- No maintained Go package currently provides a complete client-side vector
  slippy-map renderer comparable to MapLibre Native.
- The lower layers are available off the shelf: MVT decoding, geometry, Web
  Mercator, text shaping, spatial indexes, PMTiles, GPX, polyline decoding, HTTP
  and persistent storage.
- The difficult layers remain custom: style evaluation, retained tile and GPU
  lifecycle, polygon/line rendering, glyph and icon atlases, cross-tile symbol
  placement, collision handling, and Qt Quick scene-graph integration.
- WhereAmI has a much smaller requirement than MapLibre. It needs a flat 2D
  OpenFreeMap basemap, smooth center/zoom/bearing changes, upright labels,
  point overlays, route/track lines, drawing overlays, and a bounded offline
  cache. It does not need terrain, globe, pitch, 3D buildings, heatmaps, or a
  public style-mutation API.
- A credible vertical prototype is a 6-10 week effort for one experienced
  engineer. Current-feature parity with acceptable labels is approximately
  4-6 focused months. Production hardening could extend that to 6-9 months.
  Near-MapLibre polish and generality would be a 9-18+ month map-engine project.
- The first task must be a Qt Quick/miqt integration spike. That is the largest
  architectural unknown and should be proven before implementing the rest of
  the renderer.
- Symbol placement is the largest quality risk. Roads and polygons are not the
  main problem; stable, correctly shaped, upright labels across tile boundaries
  are.

## Goals

- Render OpenFreeMap/OpenMapTiles vector tiles directly in the desktop client.
- Keep vectors as vectors through the interactive rendering pipeline.
- Support smooth fractional zoom, pan, resize, world wrapping, and bearing.
- Keep text and icons upright when the map rotates.
- Preserve or replace all map behavior currently used by WhereAmI.
- Support the persistent main map and the on-demand timeline map while sharing
  source bytes, decoded tiles, fonts, sprites, and GPU resources where safe.
- Render waypoint, cluster, current-location, and search markers as interactive
  QML overlays.
- Render timeline paths, GPX tracks, calculated routes, and drawing geometry.
- Support a persistent, bounded cache and useful behavior without a network.
- Avoid Qt private APIs where possible so Qt patch updates do not recreate the
  current MapLibre plugin ABI problem.
- Use maintained, permissively licensed Go libraries for lower-level work.
- Validate unsupported style features at build time rather than silently
  rendering the map incorrectly.

## Non-Goals

- Raster tiles or a vector-to-raster tile proxy.
- A complete clone of MapLibre Native.
- Arbitrary third-party MapLibre styles.
- Runtime source/layer mutation compatible with MapLibre APIs.
- Full conformance with every MapLibre expression and style property.
- Globe projection.
- Terrain or DEM rendering.
- Pitch or perspective camera support in the initial renderer.
- Fill extrusion or 3D buildings.
- Heatmaps, hillshade, sky, or custom rendering layers.
- General rendered-feature queries against the basemap.
- MapLibre-compatible offline-region APIs.
- Android/iOS SDKs.
- A routing graph embedded in the map renderer.
- Reproduction of accidental or dead behavior in the current QML.

## Rejected Directions

### Raster maps

Rejected. WhereAmI recently moved from Qt's stock OSM raster provider to a
vector map. Returning to raster would lose the reason for that change and would
remain a dead end for smooth zoom, bearing, style control, offline vector data,
and future navigation presentation.

### Per-tile MVT-to-PNG rendering

Rejected as the product architecture. It can demonstrate style parsing and MVT
decoding, but labels become baked into tile images, clip or duplicate at tile
edges, and rotate with the basemap. Metatiles reduce seams but do not turn this
into an interactive vector renderer.

### CPU-rendered viewport images

Not the target. A CPU renderer can be useful for tests, exports, or an early
visual reference. Repainting and uploading a full image during every gesture
does not provide the retained vector behavior required here.

### MapLibre GL JS in Qt WebEngine

Technically viable and still a true vector map, but rejected as the primary Go
architecture. It replaces the MapLibre C++ dependency with Chromium,
JavaScript, WebGL, and another event/data bridge. It is a useful fallback if the
native Qt Quick spike fails, not the intended implementation.

### Another QtLocation provider

Not recommended. Implementing a new QtLocation geoservice would preserve the
existing QML `Map` API, but QtLocation mapping-provider internals use private Qt
interfaces. That would recreate the exact Qt ABI and packaging constraint this
project wants to remove.

### Go bindings over MapLibre Native FFI

Excluded by definition. `github.com/jfberry/maplibre-native-go` is useful work,
but it still wraps the MapLibre Native C++ engine.

## Current WhereAmI Map Architecture

### Runtime boundary

- `main.go:37-193` initializes storage and services, then starts an
  authenticated loopback HTTP API.
- `main.go:195-217` creates `QApplication`, creates the QML engine, injects the
  API token and map-cache directory, loads `Main.qml`, and enters the Qt event
  loop.
- miqt v0.14.0 is declared in `go.mod:5-12`.
- QML is compiled into a Qt resource bundle through `miqt-rcc` and
  `resources_gen.go`.
- There is no first-party C++ map code in this repository.

### MapLibre boundary

WhereAmI does not link directly to MapLibre. It dynamically discovers the
`maplibre` QtLocation geoservice through `OpenFreeMapPlugin.qml:16-31`.

The provider receives only:

- `maplibre.map.styles`
- `maplibre.cache.directory`
- `maplibre.cache.size`

The style is the mutable URL
`https://tiles.openfreemap.org/styles/liberty`.

The Flatpak packaging pins a MapLibre Native Qt commit and builds it against
the same Qt runtime because the provider depends on private Qt APIs. That exact
Qt ABI requirement is the packaging problem the new component should remove.

The pinned provider supplies only mapping. It does not supply Qt geocoding,
routing, or places engines.

### Map surfaces

- The main map is a persistent `QtLocation.Map` in
  `ui/components/MapView.qml:803-853`.
- The timeline map is a second `QtLocation.Map` created by a `Loader` only while
  the timeline page is active and has stops, in
  `ui/components/TimelineView.qml:215-246`.
- Both maps select the last supported map type and use the same provider/style.

### Current MapLibre/QtLocation surface used

| Capability | Current use |
| --- | --- |
| Provider discovery | Checks for the `maplibre` provider |
| Style | One remote OpenFreeMap Liberty style |
| Camera properties | `center`, fractional `zoomLevel`, and `bearing` |
| Camera methods | `pan`, `toCoordinate`, `fromCoordinate`, `alignCoordinateToPoint` |
| Projection | WGS84 coordinates over Web Mercator |
| Gestures | Mouse drag, wheel, double-click, pinch zoom, main-map rotation |
| Point overlays | `MapItemView` and `MapQuickItem` |
| Line overlays | One short `MapPolyline` in the timeline |
| Cache | Shared persistent cache, nominally 256 MiB |
| Attribution | Custom QML attribution instead of built-in copyright UI |

WhereAmI does not use MapLibre style source/layer APIs, feature state, rendered
feature picking, snapshots, terrain, custom images, heatmaps, style switching,
or managed offline regions.

### Camera and interaction behavior to preserve

The main map currently implements:

- Initial center and zoom from `Knobs.qml`.
- Animated center changes over 800 ms.
- Animated zoom changes over 600 ms.
- Pinch zoom anchored at the gesture centroid.
- Pinch rotation anchored at the gesture centroid.
- Wheel zoom.
- Direct drag pan.
- Hover coordinate display.
- Double-click center and zoom.
- Right-click screen-to-coordinate conversion for bookmark creation.
- Programmatic search and waypoint focus animations.
- Zoom notifications used to refresh clusters.
- Screen projection used to place dialogs and filter the waypoint table.

The timeline map currently implements:

- Immediate focus on the selected stop.
- A three-stage flight: zoom out, change center, zoom in.
- Cruise zoom chosen from geographic distance.
- Alignment of the selected stop above the bottom panel.
- Pinch zoom, wheel zoom, and drag pan.
- Cancellation of an automatic flight after user camera interaction.

The replacement does not need to reproduce the duplicate double-click handlers
currently present in `MapView.qml:921-931` and `:975-981`.

### Current markers and overlays

The main map contains inline QML visuals for:

- Waypoints.
- Bookmark waypoints.
- Selected-waypoint halos.
- Clusters and cluster counts.
- Current location.
- A pulsing search result.

The timeline contains:

- Previous-stop marker.
- Current-stop marker and pulse.
- Next-stop marker.
- A two- or three-coordinate context path.

These are mostly application UI, not MapLibre style layers. The renderer should
not absorb their visual design unless profiling proves that batching them in the
engine is necessary.

Important integration correction: after replacing `QtLocation.Map`, existing
`MapQuickItem`, `MapItemView`, and `MapPolyline` objects cannot simply remain.
They are QtLocation map items and require a QtLocation map. The new component
must provide equivalent application-level mechanisms:

- Ordinary QML marker delegates positioned from `fromCoordinate`.
- A camera revision or projected-point model that updates those delegates when
  the camera or viewport changes.
- Hit testing in the QML marker delegates.
- A dynamic vector overlay path for timeline lines, GPX tracks, routes, and
  drawn geometry.

### Application-owned features already outside MapLibre

- Waypoint persistence and GPX waypoint import.
- Bookmark and tag editing.
- Date and tag filtering.
- Backend and QML point clustering.
- Nominatim forward search.
- GeoClue location acquisition.
- Timeline stop generation.
- Administrative location resolution.
- Camera-fit heuristics.
- Custom attribution UI.

These should remain application-owned.

### Current routing and navigation state

WhereAmI does not currently calculate routes or perform turn-by-turn
navigation. The word "navigation" in parts of the UI refers to moving around
the map or timeline.

- `RegisterAPI` has no route endpoint in `api.go:1975-2012`.
- The current GPX model handles only top-level `<wpt>` entries in
  `storage.go:44-79`.
- It does not model GPX `<trk>`, `<trkpt>`, `<rte>`, or `<rtept>` elements.
- The timeline path is not road-snapped and contains at most the previous,
  current, and next stop.

Routing and live navigation are separate future application features. They do
not need to be part of the basemap source or style engine.

### Current cache behavior

- `main.go:220-230` resolves a cache directory under the effective WhereAmI
  cache root.
- `OpenFreeMapPlugin.qml:22-46` requests a 268,435,456-byte MapLibre cache.
- The current cache stores viewed style, tile, glyph, and sprite resources.
- Offline rendering works only for resources previously cached.
- There is no area download, cache inspector, progress UI, PMTiles/MBTiles
  import, or managed offline pack.

The new engine needs equivalent opportunistic caching first and can add
explicit offline areas later.

### Current platform constraints

- Linux/Fedora is the explicitly tested environment.
- The supported distribution is a Flatpak using a KDE/Qt runtime.
- The Flatpak enables Wayland, fallback X11, network, DRI/GPU access, and the
  GeoClue system-bus interface.
- Current location acquisition is Linux-specific GeoClue2 over D-Bus.
- The existing MapLibre Native Qt build is OpenGL-only in the Flatpak manifest.
- The application currently documents Qt 6.5 or newer and Go 1.25 or newer.
- There is no maintained Windows, macOS, Android, or iOS package.

The replacement should preserve Linux/Wayland/X11 first. Cross-platform Qt RHI
support is desirable, but it should not be allowed to hide an unmade product
decision about the initial supported backend.

### Relevant existing defects and limitations

These are not renderer requirements:

- `MapView.qml` has two double-tap/double-click zoom implementations.
- `MapView.qml:1300` calls an undefined `focusZoomTimer`.
- Current fit-to-waypoints logic is heuristic and does not handle the
  antimeridian or projection-aware padding.
- Current viewport filtering assumes an unrotated rectangular lat/lon view.
- Current point clustering has no spiderfying or expansion-bounds operation.
- Accuracy is acquired with current location but not rendered by the active
  inline marker.
- Several standalone marker QML files are source-present but not embedded or
  instantiated; the active marker visuals are inline in `MapView.qml`.

## Required Replacement Surface

The public QML surface should be deliberately small and close to the operations
WhereAmI already calls:

```qml
WhereAmIMap {
    id: map

    center: QtPositioning.coordinate(40.7128, 0)
    zoomLevel: 2
    bearing: 0

    minimumZoomLevel: 0
    maximumZoomLevel: 20

    onCameraChanged: { /* refresh clusters and QML overlays */ }
    onMapError: function(message) { /* show provider error */ }

    function pan(dx, dy) {}
    function toCoordinate(point, clipToViewport) {}
    function fromCoordinate(coordinate, clipToViewport) {}
    function alignCoordinateToPoint(coordinate, point) {}
    function fitBounds(bounds, padding) {}
}
```

Likely additional properties and signals:

- `mapReady`
- `styleReady`
- `loading`
- `errorString`
- `cameraRevision`
- `viewportWidth` and `viewportHeight`
- `devicePixelRatio`
- `visibleRegion`
- `renderStats`, gated behind debug mode
- `idle`, when all currently needed resources have settled

Overlay API candidates:

```go
type Overlay struct {
    ID       string
    Geometry orb.Geometry
    Style    OverlayStyle
    Selected bool
    ZIndex   int
}
```

The overlay path should be optimized for dynamic updates and remain separate
from immutable basemap tile buckets.

## Target Architecture

```text
QML WhereAmI UI
    |
    | camera properties, gestures, marker models, overlay commands
    v
WhereAmIMap Qt Quick item through miqt
    |
    +-- camera and Web Mercator transforms
    +-- visible-tile selection
    +-- source/style manager
    +-- request scheduler and persistent cache
    +-- MVT decoder workers
    +-- compiled WhereAmI style
    +-- immutable geometry buckets
    +-- font resolver, glyph atlas, and sprite atlas
    +-- symbol placement and collision index
    +-- dynamic route/track/drawing overlays
    +-- render-thread GPU resource manager
    |
    v
Qt Quick scene graph
    |
    v
Qt-selected graphics backend
```

### Package boundaries

A possible Go layout is:

```text
internal/mapview/
    camera/       Web Mercator camera and screen transforms
    source/       style, TileJSON, MVT, sprite, and font resources
    cache/        persistent HTTP/resource cache
    scheduler/    tile cover, priorities, cancellation, and workers
    style/        build-time compiler and runtime programs
    bucket/       immutable fill, line, icon, and symbol data
    symbol/       shaping, anchors, collisions, and placement state
    overlay/      routes, tracks, polygons, handles, and hit testing
    render/       scene graph and GPU resource lifecycle
    qml/          Qt Quick object and QML-facing types
```

This is illustrative rather than a requirement. Package splits should follow
actual dependency boundaries discovered during the vertical spike.

### Threading model

- The Qt GUI thread owns QML-visible state and receives input/property changes.
- Worker goroutines fetch resources, decode MVT, evaluate layout filters, shape
  text, and build immutable CPU buckets.
- Workers never call scene-graph or GPU APIs.
- Prepared immutable results move through a bounded handoff queue.
- The Qt scene-graph render thread creates, updates, and destroys GPU resources.
- Camera-only frames should update transforms/uniforms without rebuilding tile
  geometry.
- Cancellation must discard stale work after fast camera changes.
- Resource generations should prevent an old style/tile result from entering a
  newer scene.

### Frame pipeline

1. GUI thread publishes the latest camera and viewport snapshot.
2. Tile cover computes visible tiles plus a small prefetch ring.
3. Scheduler assigns priorities and cancels obsolete requests.
4. Cache supplies bytes or source workers fetch them.
5. Decode workers produce MVT layer/feature data.
6. Style/layout workers produce immutable geometry and symbol candidates.
7. Symbol placement builds a viewport-wide placement result.
8. Render thread uploads newly available buffers and atlas regions.
9. Render thread draws style layers in order with tile clipping.
10. Dynamic application overlays draw above the basemap.
11. QML marker delegates draw above or around the custom map item.

## OpenFreeMap and Liberty Scope

### Current source

The current Liberty style has:

| Layer type | Count |
| --- | ---: |
| Background | 1 |
| Fill | 16 |
| Line | 67 |
| Symbol | 25 |
| Raster | 1 |
| Fill extrusion | 1 |
| Total | 111 |

The OpenFreeMap vector source currently advertises source zooms 0 through 14.
WhereAmI focuses points at zoom 17, so source overzoom is required.

Liberty makes meaningful use of:

- Road casing and foreground layers.
- Line caps and joins.
- Line dashes.
- Line gap width.
- Fill opacity and outlines.
- Fill patterns.
- Zoom interpolation, including exponential curves.
- Feature filters and data-driven values.
- Point and line symbols.
- Text anchors, offsets, max widths, and spacing.
- Sprites and road shields.
- Text and icon rotation alignment.
- Collision/overlap options.

Observed property frequency in the hosted style helps prioritize the first
compiler/renderer implementation:

| Property | Layer count |
| --- | ---: |
| `line-color` | 67 |
| `line-width` | 66 |
| `line-join` | 44 |
| `text-field`, `text-font`, `text-size` | 23 each |
| `text-color`, `text-halo-width` | 20 each |
| `text-halo-color` | 18 |
| `line-dasharray`, `text-max-width` | 17 each |
| `line-cap` | 15 |
| `fill-color`, `icon-image` | 14 each |
| `symbol-placement` | 10 |
| `icon-size`, `text-anchor` | 9 each |
| `text-offset` | 7 |
| `fill-opacity`, `text-rotation-alignment` | 6 each |
| `line-opacity`, `symbol-spacing`, `text-letter-spacing` | 5 each |
| `fill-outline-color`, `fill-pattern`, `text-transform` | 2 each |
| `line-gap-width`, `icon-rotate`, `raster-opacity` | 1 each |

This is not a substitute for full property validation. It is a prioritization
guide captured from the style on the research date.

The current style also contains one Natural Earth raster layer and one building
extrusion layer. Both can be omitted from the initial WhereAmI vector style.
The low-zoom raster layer should eventually be replaced by controlled vector
land/ocean data if its absence is visually significant.

### Do not consume mutable Liberty directly in production

The hosted style changes independently of WhereAmI. A new property or
expression could silently break a partial renderer.

Instead:

1. Pin a known Liberty source revision.
2. Derive a `whereami-liberty` style from it.
3. Remove unsupported raster and 3D layers.
4. Simplify duplicate or visually low-value layers.
5. Compile the style during the build.
6. Fail the build on unsupported properties or expressions.
7. Keep attribution and upstream revision metadata in the generated output.

A useful first style may contain approximately 40-60 layers rather than all
111. The exact number should be chosen by visual comparison, not by an
arbitrary target.

### Required layer types

Initial:

- `background`
- `fill`
- `line`
- `symbol`

Optional after parity:

- `circle`, useful for simple engine-side overlays
- A dedicated application overlay layer, not a MapLibre style type

Not initially required:

- `raster`
- `fill-extrusion`
- `heatmap`
- `hillshade`
- `sky`
- custom layers

### Required expression subset

The controlled style is expected to need:

- `literal`
- `get`
- `has`
- `!`
- `==`, `!=`, `<`, `<=`, `>`, `>=`
- `in` and `!in`, if retained in the compiled style
- `all`
- `any`
- `match`
- `case`
- `coalesce`
- `concat`
- `to-string`
- `to-number`, if retained
- `step`
- `interpolate`
- `zoom`
- `geometry-type`

The compiler should distinguish:

- Layout-time properties that affect bucket or symbol generation.
- Paint-time properties that can be uniforms or per-feature attributes.
- Camera-only expressions.
- Feature-data expressions.

Unsupported operations should be compile errors. Runtime fallback should be
reserved for bad feature data, not unsupported style syntax.

### Compiled style representation

Example only:

```go
type LineLayer struct {
    ID          string
    SourceLayer string
    MinZoom     float32
    MaxZoom     float32
    Filter      FilterProgram
    Color       ColorProgram
    Width       NumberProgram
    GapWidth    NumberProgram
    Opacity     NumberProgram
    DashArray   []float32
    Join        LineJoin
    Cap         LineCap
}
```

Programs can be compact typed bytecode or generated Go data interpreted by a
small evaluator. Generated Go closures are another option, but they can make
debugging and serialization harder. Start with the simplest representation
that is measurable and testable.

## Go Ecosystem Findings

Research cutoff: 2026-08-28.

### Complete renderers

No maintained off-the-shelf Go project currently provides a complete,
interactive MapLibre-style vector map suitable for embedding in Qt Quick.

| Project | Assessment |
| --- | --- |
| `akhenakh/maprender` | Closest Go code, but an experimental CPU viewport renderer rather than a retained interactive map engine |
| `jfberry/maplibre-native-go` | Go binding over MapLibre Native C++; excluded by the dependency requirement |
| `maplibre-gl-js` | Complete vector map, but JavaScript/WebGL and Qt WebEngine rather than a Go-native renderer |
| `go-gui-org/go-map` | Interactive raster map, no vector style renderer |
| `fyne-io/fyne-x/widget.Map` | Basic raster widget for Fyne, not Qt and not MVT |
| `beetlebugorg/tile57` | Native Zig nautical-chart engine, domain-specific and not a general OpenFreeMap renderer |

### MVT decoding

#### Recommended: `github.com/paulmach/orb/encoding/mvt`

- MIT.
- Pure Go for the relevant packages.
- Version 0.13.0 was released in March 2026.
- Encodes and decodes raw or gzipped MVT.
- Integrates with `orb`, `geojson`, and `maptile`.
- Supports projection between tile coordinates and WGS84.
- Also provides clipping, simplification, quadtree, and tile math packages.

Limitations for a renderer:

- It materializes feature geometry rather than exposing a specialized lazy
  render decoder.
- It does not implement style evaluation or GPU bucket construction.
- Decoder memory behavior should be profiled on dense OpenFreeMap tiles.

This is the safest initial decoder and should replace the unlicensed `mvtgo`
dependency used by `maprender`.

The narrow Phase 1 tile spike uses a bounded internal protobuf reader instead.
Adding `orb` 0.13.0 in the offline development environment pulled its broader
module graph through `geojson`, including the MongoDB driver, whose checksum
could not be resolved without network access. The internal reader deliberately
supports only the MVT fields needed for one `transportation` line bucket. This
does not make it a general replacement for `orb`; revisit the dependency versus
parser scope before Phase 2 broadens source and geometry support.

#### Other candidates

- `go-spatial/geom/encoding/mvt`: MIT and lower-level, but less ergonomic for
  this client renderer and tied to the broader Tegola geometry ecosystem.
- `akhenakh/mvtgo`: small and used by `maprender`, but has no license file and
  should not be adopted without a license grant.
- `flywave/go-mapbox/mvt`: potentially useful lazy access, but the repository
  has no declared license and a broad, uneven dependency surface.
- `tidwall/mvt`: an encoder despite its "Draw MVT" wording, not the required
  decoder/renderer.

### Geometry and projection

#### Recommended hot path: `github.com/paulmach/orb`

- Natural Go slice-backed geometry types.
- Web Mercator tile math.
- Clipping and simplification.
- MVT integration.
- Small enough for renderer-facing data adapters.

#### Recommended robust editing operations: `github.com/peterstace/simplefeatures`

- MIT.
- Active in 2026.
- `geom`, `carto`, and `rtree` are pure Go.
- Robust topology, validation, predicates, buffers, overlay operations, and
  Web Mercator.
- Optional `geos` and `proj` packages use CGO and are not needed initially.

Use `orb` for tile and overlay transport. Add `simplefeatures` where drawing
needs robust validity checks, intersections, unions, buffers, or snapping.

### Polygon and line tessellation

No mature, clearly dominant Go package removes all custom map tessellation
work.

Candidates worth evaluating:

- `oliverbestmann/earcut-go`: ISC, direct Go Earcut port created in late 2025,
  but very new and with essentially no adoption.
- `rclancey/earcut`: ISC, older Go Earcut port, but effectively unchanged since
  2018.
- `tchayen/triangolatte`: polygon and basic normal/miter line triangulation,
  but must be audited for maintenance, correctness, joins, caps, and licensing
  before use.
- `lestrrat-go/polyclip`: new 2026 pure-Go polygon operations and
  triangulation; promising but also too new to trust without focused tests.

Likely outcome:

- Adopt or vendor a small, well-tested Earcut implementation for simple MVT
  polygon rings.
- Implement map-specific line meshes or a shader-expanded line representation.
- Add fuzz and golden tests for malformed, degenerate, clipped, and holed MVT
  polygons.

### Style parsing and expressions

There is no maintained reusable Go implementation of the complete current
MapLibre Style Specification and expression evaluator.

- `maprender` has a small ad hoc parser/evaluator matching its renderer.
- `flywave/go-mapbox/style` contains broad structures and expression parsing,
  but no complete evaluator and no declared repository license.
- `go-spatial/tegola/mapbox/style` is intentionally minimal and server/debug
  oriented.
- `maplibre-style-spec` is authoritative and active, but implemented in
  TypeScript.

Parsing JSON structs is not the hard part. Correct defaults, expression typing,
layout/paint evaluation timing, interpolation, transitions, imports, feature
state, and source semantics are the hard part. The controlled build-time style
compiler is therefore core custom work.

### Text shaping

#### Recommended: `github.com/go-text/typesetting`

- Pure Go.
- Unlicense or BSD-3-Clause, depending on files/package distribution.
- Active and used by Fyne, Gio, and Ebitengine.
- OpenType parsing, HarfBuzz-style shaping, scripts, bidi, and segmentation.

It does not provide:

- MapLibre glyph PBF decoding.
- Glyph atlas management.
- SDF/MSDF generation.
- Point or line label placement.
- Collision handling.
- Cross-tile symbol deduplication.

Use bundled/local OpenType fonts rather than Mapbox glyph PBFs initially. Glyph
PBF contains raster glyph data and metrics, but does not replace access to font
layout tables needed for high-quality shaping.

### CPU vector rendering

#### `github.com/tdewolff/canvas`

- MIT, active, and capable.
- Paths, fills, strokes, dashes, fonts, shaping, and raster output.
- Useful for reference images, exports, style experiments, and golden tests.
- Its default bidi path includes an LGPL dependency; distribution obligations
  need review.
- Its OpenGL backend first CPU-rasterizes and uploads an image. It is not a
  retained GPU vector map pipeline.

It should not define the interactive renderer architecture.

### GPU foundations

#### Qt Quick scene graph

This is the preferred durable integration because WhereAmI is already a Qt
Quick application. Public scene-graph nodes and materials let Qt select and own
the graphics backend and render target.

#### `go-gl/gl`

- MIT bindings, but CGO/native OpenGL.
- Only low-level API calls.
- Does not provide a scene, map renderer, tessellation, text, or Qt adapter.
- Requires strict context and render-thread ownership.

It may support a Linux/OpenGL prototype, but it is not a portable Qt RHI
solution.

#### `gogpu/gg` and the GoGPU ecosystem

- New in late 2025 and active in 2026.
- MIT and claims pure-Go CPU/GPU 2D rendering, retained scenes, GPU paths,
  textured quads, and MSDF/glyph-mask text.
- Potentially relevant to tessellation, atlases, and GPU drawing.
- Very new, rapidly versioned, and not proven as a Qt-owned render-target
  integration.

It should receive a focused technical evaluation, especially for reusable
geometry and text code. Do not commit the project architecture to it until it
can render into, or efficiently share a texture with, the Qt Quick scene graph
without a second conflicting GPU device/context.

### Sprites and glyph utilities

- `flywave/go-mapbox/font` and `sprite` contain relevant code for glyph PBF,
  SDF, and sprite packing, but the repository has no declared license.
- `jpbede/fontmachine` is useful historical reference but not sufficiently
  active for a new core dependency.
- A small sprite-sheet loader is straightforward compared with symbol
  placement and can be owned locally if no suitable licensed package exists.

### Persistent and memory caches

#### Existing `modernc.org/sqlite`

WhereAmI already uses pure-Go SQLite. It is a strong fit for a unified resource
index with byte accounting, LRU timestamps, ETag/Last-Modified metadata, source
versions, and stale-resource behavior.

#### `github.com/dgraph-io/ristretto`

- Apache-2.0.
- Active in 2026.
- Cost-bounded in-memory cache suitable for decoded tile and shaped-symbol
  data.
- Admission is asynchronous, which must be considered by callers.

A small explicit LRU may be preferable when deterministic ownership and GPU
release order matter more than hit-rate sophistication.

#### HTTP caches

- `bartventer/httpcache`: Apache-2.0, RFC 9111-oriented client transport,
  filesystem store, revalidation, and stale behavior. It does not cache remote
  PMTiles HTTP range responses.
- `kenshaw/diskcache`: MIT and active, but intentionally not fully RFC
  compliant.

A map engine still needs map-specific coordination and a shared byte budget
across style, TileJSON, MVT, sprites, and fonts.

### PMTiles and MBTiles

#### `github.com/protomaps/go-pmtiles`

- BSD-3-Clause.
- Active and mature for PMTiles v3 operations.
- Supports local files, HTTP range requests, cloud sources, extraction, and
  serving.
- Does not decode MVT or provide a ready client map source abstraction.
- Has a broad server/cloud dependency graph that should be evaluated before
  embedding.

#### `github.com/twpayne/go-mbtiles`

- BSD-2-Clause.
- Uses pure-Go `modernc.org/sqlite`.
- Small read/write and XYZ/TMS API.
- Low activity and an apparent prepared-statement issue observed in its reader
  code; audit or implement the small required reader locally before production
  use.

Offline archive support should follow online renderer parity, not block it.

### GPX and route geometry

#### `github.com/twpayne/go-gpx`

- MIT.
- Models waypoints, routes, tracks, and segments.
- Suitable for replacing the current waypoint-only file boundary.
- Round-trip tests are required before replacing bookmark serialization because
  arbitrary vendor extensions may not be preserved exactly.

#### `github.com/twpayne/go-polyline`

- BSD-2-Clause.
- Encodes/decodes Google polylines and supports configurable precision.
- Suitable for OSRM polyline5/polyline6 route geometry.

Explicit coordinate adapters are required:

- GeoJSON and most route APIs: longitude, latitude.
- QML `coordinate`: named latitude and longitude.
- GPX attributes: latitude then longitude.
- OSRM URL coordinates: longitude,latitude.

### Spatial indexing and hit testing

No new spatial index is required for the immutable basemap tile cover itself;
tile IDs already partition that data. Spatial indexes are useful for mutable
application geometry, symbol collisions, picking, and persisted viewport
queries.

Recommended options:

- Existing `modernc.org/sqlite` with SQLite R*Tree for persisted waypoint and
  application-geometry bounding boxes. The build used by WhereAmI includes
  SQLite R*Tree support on the generated targets inspected during research.
- `github.com/tidwall/rtree` for mutable in-memory editing and hit-testing
  candidates.
- `orb/quadtree` for a simpler point-only index.
- `simplefeatures/rtree` for static bulk-loaded geometry.
- A small screen-space grid for symbol collisions and QML marker picking.

SQLite R*Tree caveats:

- It stores bounding-box candidates, so exact geometry filtering remains
  necessary.
- Its coordinate storage is lower precision than Go `float64`.
- Dateline-crossing queries should be split into two longitude ranges.
- An R*Tree should be added only when a repository query actually uses it; the
  existing `(longitude, latitude)` B-tree can remain until then.

## `maprender` Evaluation

Repository: `https://github.com/akhenakh/maprender`

### What it demonstrates

- Fetches the OpenFreeMap Liberty style and TileJSON.
- Fetches MVT tiles and handles gzip.
- Performs source overzoom/underzoom.
- Renders background, fill, line, and symbol layers.
- Uses system fonts.
- Loads traditional JSON/PNG sprite sheets.
- Performs basic viewport collision checks.
- Supports point, line, polygon, and multi-geometries.
- Renders overlays.
- Produces `image.RGBA` and vector-canvas output.
- Has an incremental-pan CPU-image mode.

### Why it is not the engine

- Created in August 2026, with 17 commits and no release history at research
  time.
- One contributor and essentially no adoption.
- Requires Go 1.26.4 while WhereAmI currently declares Go 1.25.
- Depends on `akhenakh/mvtgo`, which has no license file.
- Integer zoom only.
- No bearing, pitch, retained scene, or Qt component.
- Keeps only the first vector source generally.
- Drops raster and fill-extrusion layers.
- Parses but does not apply several paint properties.
- Treats important interpolation and data-driven cases approximately.
- Simplified line-symbol placement, usually one line midpoint.
- No robust repeated line labels or cross-tile symbol identity.
- No glyph PBF use.
- Incomplete SDF sprite semantics.
- Sequential visible-tile fetching.
- Small decoded cache that flushes wholesale.
- Disk TTL but no byte budget, HTTP revalidation, or resource-wide eviction.
- Returns frames but owns no interactive camera, tile lifecycle, feature
  picking, attribution, or Qt presentation.

### Local experiment

The repository was cloned and tested against the current hosted Liberty style.

- `go test ./...` passed.
- Its unmodified Paris example produced a recognizable styled map.
- On an AMD Ryzen AI 7 PRO 350, its cached 512x512 zoom-17 benchmark measured
  approximately 77-84 ms per full `Render` call.
- The same benchmark allocated approximately 54 MiB and 570,000 allocations per
  render.
- `RenderCanvas` measured approximately 18-19 ms before rasterization in that
  benchmark.
- Its incremental-pan benchmark was much faster, but that optimization reuses
  CPU pixels and does not provide the retained vector/GPU behavior required by
  WhereAmI.

Use it as:

- A reference for Liberty property discovery.
- A source of test ideas.
- A temporary CPU golden-image renderer.
- Evidence that the Go ecosystem can decode and approximately portray the
  current source.

Do not adopt it as the interactive engine without effectively redesigning its
architecture and resolving its dependency licensing.

## Camera and Projection

Estimated focused effort: 1-2 weeks for the mathematical core, then ongoing
integration work.

Required behavior:

- WGS84 latitude/longitude and Web Mercator.
- Latitude clamping to the Web Mercator limit.
- Fractional zoom.
- Bearing around viewport center or a gesture anchor.
- Pixel pan.
- Coordinate-to-screen and screen-to-coordinate conversion.
- `alignCoordinateToPoint` semantics.
- World wrapping in longitude.
- Rotated viewport tile cover.
- Resize and device-pixel-ratio changes.
- Antimeridian-aware bounds fitting.
- Minimum and maximum zoom.

The camera should expose immutable snapshots to workers. Rendering should use
camera matrices/uniforms so panning, fractional zoom, and bearing do not rebuild
geometry buckets.

Pitch can remain fixed at zero. This simplifies projection, tile cover, symbol
placement, clipping, and hit testing substantially.

## Tile Scheduler and Source Manager

Estimated focused effort: 2-4 weeks for the initial implementation, followed by
performance hardening.

Required responsibilities:

- Parse the controlled source configuration and TileJSON.
- Compute visible source tiles for a rotated viewport.
- Add a configurable prefetch ring.
- Prioritize viewport-center and already visible tiles.
- Coalesce duplicate requests across the two map surfaces.
- Limit network, decode, layout, and upload concurrency independently.
- Cancel or ignore stale work after camera/style changes.
- Retain parent tiles while child tiles load.
- Overzoom OpenFreeMap source zoom 14 through WhereAmI zoom 17+.
- Fall back gracefully to stale cached resources.
- Back off repeated failures.
- Expose loading/error/idle state.
- Avoid displaying an old style generation after a style update.

Suggested cache levels:

```text
persistent source bytes
        -> decoded MVT features
        -> immutable CPU render buckets
        -> GPU buffers and atlas regions
```

Each level needs an explicit owner and cost model. GPU resources must be
released on the Qt render thread.

## Style Compiler

Estimated focused effort: 3-6 weeks for the chosen style subset.

Build-time responsibilities:

- Load a pinned style source.
- Resolve constants and defaults.
- Validate source and source-layer references.
- Validate all retained expressions and property types.
- Classify layout and paint expressions.
- Generate compact runtime programs/data.
- Record source/style version metadata.
- Produce a human-readable unsupported-feature report.
- Fail generation if a retained layer cannot be rendered correctly.

Runtime responsibilities:

- Evaluate feature filters.
- Evaluate layout properties at the selected layout zoom.
- Evaluate paint properties from zoom and feature data.
- Interpolate colors and numbers correctly, including exponential bases used by
  the controlled style.
- Supply defaults already resolved by the compiler.

Do not build a generic style parser first. Begin with the pinned style and grow
the compiler only when an intentional style change requires it.

## Geometry Buckets and GPU Rendering

Estimated focused effort: 6-10 weeks for the first credible renderer.

### Fill pipeline

- Decode polygon rings.
- Normalize winding as needed.
- Clip to an expanded tile boundary.
- Triangulate concave polygons and holes.
- Keep tile-local coordinates in compact vertex buffers.
- Draw in style order.
- Use tile clipping to avoid overlap artifacts.
- Support solid color, opacity, and outlines initially.
- Add fill patterns only when the controlled style retains them.

### Line pipeline

- Handle line and multiline geometry.
- Support butt, square, and round caps where retained.
- Support miter, bevel, and round joins where retained.
- Respect miter limits.
- Support screen-space width.
- Support road casings and gap widths.
- Support dash patterns.
- Carry cumulative line distance for dashes and symbol placement.
- Clip with enough buffer to avoid visible joins at tile edges.
- Preserve stable visual width while fractional zoom changes.

Two implementation strategies:

1. CPU-expand lines into triangle meshes during bucket construction.
2. Upload centerline adjacency and expand segments/joins in the vertex shader.

CPU expansion is simpler initially. Shader expansion better preserves
screen-space widths across fractional zoom and may reduce rebuilds. The spike
should measure both on dense urban tiles before committing.

### Render ordering and clipping

- Style layer order must be preserved globally, not tile by tile.
- For each style layer, draw all relevant tiles before advancing to the next
  layer.
- Use per-tile clip geometry or scissor/stencil behavior to prevent duplicate
  geometry from tile buffers.
- Parent and child fallback tiles must not double-paint the same region.
- Transparent layers need deterministic blending order.

### GPU resource model

- CPU buckets are immutable and backend-independent.
- GPU bucket handles belong to a specific Qt scene-graph context/window.
- Shared CPU resources can serve both map surfaces.
- GPU sharing between windows/contexts must not be assumed without proof.
- Device/context loss needs a way to discard and recreate all GPU resources
  from retained CPU/source data.

## Text, Icons, and Symbol Placement

Estimated focused effort: 6-12 weeks for acceptable initial quality. This is
the largest source of schedule and quality uncertainty.

### Font strategy

- Bundle a controlled Noto font subset or declare exact runtime font
  requirements.
- Verify font licenses and include required notices.
- Resolve style font stacks deterministically.
- Use `go-text/typesetting` for shaping, scripts, and bidi.
- Cache shaped runs by text, font, size/layout bucket, language/script, and
  relevant style options.
- Avoid the hosted glyph PBF path initially.

### Glyph atlas

- Rasterize shaped glyphs to alpha, SDF, or MSDF atlas entries.
- Track bearings, advances, bounds, and atlas UVs.
- Allocate and evict atlas regions deterministically.
- Upload atlas changes only on the render thread.
- Keep glyph identity stable across maps and frames where possible.
- Handle device-pixel ratio and zoom without constant rerasterization.

Alpha-mask glyphs are simpler and may be sufficient for the controlled set of
text sizes. SDF/MSDF offers more scale tolerance and halo flexibility but adds
generation and shader complexity. Start with the smallest strategy that meets
visual tests.

### Sprite atlas

- Parse sprite JSON metadata.
- Load normal and HiDPI sprite sheets.
- Preserve pixel ratio and content boxes.
- Draw icon quads.
- Support the exact SDF recoloring semantics retained by the style.
- Support icon/text combinations used by POIs and road shields.

### Placement

Required initial placement behavior:

- Point labels.
- Point icons.
- Text and icon anchors.
- Text offsets.
- Halo and opacity.
- Basic multiline wrapping or controlled preformatted labels.
- One or repeated labels along sufficiently long lines.
- Keep-upright line labels.
- Viewport-aligned road shields where retained.
- Viewport-wide collision index.
- Cross-tile symbol deduplication.
- Stable placements during small pans.

Can be deferred:

- Variable anchors.
- Fully general formatted text spans.
- Complex icon-text-fit behavior.
- Every MapLibre overlap/optional combination.
- Symbol fading exactly matching MapLibre.
- Vertical writing modes unless required by target-language testing.

### Stability model

Recomputing all placements every frame produces popping. A practical first
model is:

- Build symbol candidates at an integer or quantized layout zoom.
- Give candidates stable IDs from source tile, source layer, feature ID,
  geometry anchor, and style layer.
- Keep accepted placements while they remain valid after small camera changes.
- Re-run collision placement after a movement, bearing, or zoom threshold.
- Cross-fade old/new placements only after deterministic placement is correct.

## Qt Quick and miqt Integration

Estimated focused effort: 3-6 weeks for a robust first integration. This is the
first spike because feasibility is not yet proven.

### Current miqt gap

miqt v0.14.0 exposes QtCore, QtGui, QML, `QImage`, `QPainter`, and
`QQmlEngine::addImageProvider`, but its generated surface does not currently
include the important Qt Quick rendering classes:

- `QQuickItem`
- `QQuickWindow`
- `QQuickImageProvider`
- `QQuickFramebufferObject`
- `QSGNode`
- `QSGGeometryNode`
- `QSGTransformNode`
- `QSGTexture`
- `QSGMaterial`
- `QSGMaterialShader`
- `QSGRenderNode`

`QQmlImageProviderBase` alone cannot implement the interactive vector surface.

### Preferred durable path

Extend miqt's generator/allowlist to bind the public Qt Quick scene-graph APIs
needed by a custom `QQuickItem` subclass.

The item should:

- Set `ItemHasContents`.
- Expose center, zoom, bearing, readiness, and error properties to QML.
- Request updates when camera or resources change.
- Synchronize GUI state into a render-thread object during the scene-graph
  synchronization phase.
- Construct/update a retained `QSGNode` tree.
- Allocate and release GPU resources only in valid scene-graph lifecycle
  callbacks.
- Handle scene-graph invalidation and context loss.

Start with public `QSGGeometryNode`/`QSGMaterial` APIs. Avoid depending directly
on private QRhi headers because that would reintroduce a Qt private-ABI risk.

### Scene-graph options

#### `QSGGeometryNode` tree

Advantages:

- Public Qt Quick API.
- Qt owns backend selection and command submission.
- Straightforward for flat-color geometry and textured quads.
- Natural retained-node model.

Risks:

- Many tile/layer nodes can create scene-graph overhead.
- Map-specific batching and custom line behavior may require custom materials
  and shaders.
- miqt must bind virtual methods and relevant value types correctly.

This is the recommended first durable path.

#### `QSGRenderNode`

Advantages:

- More control over batching and commands.
- Can map more directly to a custom renderer.

Risks:

- Backend integration is more complex.
- Public cross-backend access may be insufficient for all desired operations.
- It is easier to accidentally depend on private QRhi internals.

Use only after the simpler node/material path is measured.

#### `QQuickFramebufferObject` plus OpenGL

Advantages:

- Potentially the fastest Linux-only prototype path.
- Familiar raw OpenGL model.

Risks:

- Explicitly legacy and OpenGL-only.
- Does not work with Vulkan, Metal, or Direct3D Qt Quick backends.
- Still requires missing miqt Qt Quick bindings.
- Requires strict render-thread/context handling.

It is acceptable for a throwaway feasibility spike if public scene-graph
binding work blocks progress, but it should not silently become the permanent
architecture without an explicit Linux/OpenGL-only decision.

### What "without C++" means here

Subclassing Qt objects through miqt necessarily compiles generated C++ wrapper
code. This is already true for the rest of WhereAmI. The desired result is:

- Go owns camera, scheduling, style, geometry, symbols, cache, and renderer
  policy.
- miqt owns generated ABI glue.
- No MapLibre Native shared library.
- No custom standalone C++ map implementation.
- No Qt private mapping-provider ABI.

If miqt cannot generate the required subclasses safely, a tiny maintained C++
Qt Quick shim would reduce project risk, but that would be an explicit
constraint change rather than the default plan.

## QML Marker Replacement

Point visuals should remain QML because they are few after clustering and need
normal QML animations and hit areas.

A replacement component can look conceptually like:

```qml
Item {
    id: overlayRoot
    anchors.fill: map

    Repeater {
        model: markerModel

        delegate: WaypointMarkerVisual {
            required property var modelData
            property point projected: map.fromCoordinate(
                QtPositioning.coordinate(modelData.lat, modelData.lon))
            x: projected.x - anchorX
            y: projected.y - anchorY
            visible: map.cameraRevision >= 0 && map.containsScreenPoint(projected)
        }
    }
}
```

In practice, a plain function binding may not automatically depend on all map
state. `cameraRevision` should be read by the projection binding, or the engine
should publish projected positions as a model after each camera update.

Options:

1. QML calls `fromCoordinate` for each visible clustered marker after
   `cameraRevision` changes. Simplest and likely adequate.
2. Go publishes a projected marker model in one batch. Better for many points
   but adds model synchronization.
3. Engine renders point symbols and QML creates only the selected/interactable
   visuals. Best at large scale but unnecessary until profiling demands it.

Start with option 1 because clustering keeps the visible item count low.

## Dynamic Routes, Tracks, and Drawing Overlays

Basemap buckets are immutable per source tile. Application overlays are
dynamic and should use a separate pipeline.

Required primitives:

- Point.
- Polyline with casing and foreground.
- Polygon fill and outline.
- Selection highlight.
- Optional circle for accuracy or handles.

Required operations:

- Add/update/remove by stable ID.
- Z-order independent of basemap style layers.
- Screen-space line widths.
- Viewport culling.
- Zoom-dependent simplification for large tracks.
- Geographic and screen-space hit testing.
- Fast updates while dragging one vertex.

Suggested division:

- Engine renders line/polygon geometry and simple batched points.
- QML renders interactive selected-point and vertex-handle visuals.
- Go owns persistent geometry and editor commands.

## Routing and Navigation

Routing is separate from vector rendering. The renderer only needs to display
route geometry and maneuver markers.

### Recommended service boundary

```text
WhereAmI Go backend
    -> provider-neutral route request
    -> OSRM or Valhalla HTTP service
    -> route/leg/step/maneuver DTO
    -> dynamic renderer overlay and QML instructions
```

### Routing engines

#### OSRM

- BSD-2-Clause C++ service.
- Fast route, match, table, trip, and nearest operations.
- Route responses can include full/simplified GeoJSON, polyline5/polyline6,
  legs, steps, maneuvers, intersections, and lanes.
- Good first provider for route previews and semantic turn steps.
- Does not itself provide complete localized narrative text.

#### Valhalla

- MIT C++ service.
- Rich turn-by-turn narratives, multimodal costing, lanes, voice/banner
  instructions, map matching, and other navigation-oriented output.
- Better first choice when localized written/spoken guidance is a core
  requirement.

#### GraphHopper/openrouteservice

- Viable external Java services.
- Heavier deployment and different licensing/provider considerations.

#### Pure-Go routing projects

Projects found in 2026 include PathCraft, Navigatorx, and small OSM graph
builders. They are promising research or prototype code but are not yet a clear
production substitute for OSRM/Valhalla preprocessing, restrictions, profiles,
query performance, and navigation semantics.

### Go client

A narrow hand-written `net/http` adapter is preferable to adopting an old or
restrictively licensed OSRM client. It needs only:

- URL/request construction.
- Context cancellation and timeouts.
- Response structs for fields WhereAmI preserves.
- Provider error normalization.
- Coordinate and polyline conversion.
- Fixture-based tests.

Clients evaluated during research:

| Library | Assessment |
| --- | --- |
| `mojixcoder/gosrm` | Broad OSRM 5.x API, but GPL-3.0 and lightly adopted |
| `gojuno/go.osrm` | Pure-Go client, but old and built around a deprecated geometry dependency |
| `stremovskyy/gosrm` | Pure-Go client with limited recent activity |
| `thinwrap/location-go` | New, standard-library-only multi-provider facade, but its normalized result omits detailed maneuver/intersection data WhereAmI should preserve |
| `iwpnd/valhalla-go` | Experimental cgo binding that requires a local Valhalla installation rather than a narrow HTTP boundary |

None is clearly better than a small application-owned adapter for one provider.

Define provider-neutral types that preserve:

- Routes and alternatives.
- Legs.
- Steps.
- Maneuver type and modifier.
- Street/ref/destination names.
- Intersections.
- Lanes.
- Distance, duration, and weight.
- Full overview geometry.

### Live navigation work that remains custom

- Match current position to the route.
- Determine progress along the route.
- Advance the active step.
- Use heading and speed appropriately.
- Detect stale or low-accuracy fixes.
- Detect off-route movement.
- Trigger and reconcile reroutes.
- Detect arrival.
- Update ETA.
- Schedule visual, banner, and voice instructions.
- Integrate TTS.
- Handle suspend/resume and loss of network/location.

Estimated effort:

- Route request, route line, alternatives, and maneuver list: 2-4 weeks.
- Credible live navigation: another 2-4 months, independent of most basemap
  work.

## Drawing and Editing

Basic rendered drawing is not difficult once dynamic overlays exist. A usable
editor still needs substantial application behavior.

Required editor state:

- Current mode: select, point, line, polygon, delete, or pan.
- Selected feature and selected vertex.
- Stable feature and vertex IDs.
- Draft geometry.
- Snapping candidates.
- Undo/redo command stack.

Required interactions:

- Click/tap to add a point or vertex.
- Drag a vertex.
- Insert a vertex on a segment.
- Delete a vertex or feature.
- Complete/cancel a draft.
- Segment and polygon hit testing.
- Optional snapping to existing application geometry.
- Validation and error feedback.
- Persistence and import/export.

Libraries can provide geometry and spatial indexes, but not the editor state
machine or UX.

Estimated effort:

- Basic point/polyline/polygon creation: 2-4 weeks after dynamic overlays.
- Polished editor with snapping, undo/redo, validation, and persistence: 1-2
  months.

## Cache and Offline Design

Estimated focused effort: 2-4 weeks for parity and bounded eviction.

### Persistent resources

Cache:

- Compiled style/version metadata.
- TileJSON.
- Raw compressed MVT bytes.
- Sprite JSON and images.
- Font files or bundled-font version metadata.
- Optional offline archive directory metadata.

Do not persist decoded feature objects or GPU buffers initially. They are tied
to code versions, architecture details, and graphics contexts and can be
recreated from source bytes.

### Suggested SQLite metadata

- Resource key and type.
- Canonical URL/source ID.
- Style/source generation.
- ETag.
- Last-Modified.
- Fetch time.
- Last access time.
- Expiry/revalidation time.
- Byte size.
- Content hash.
- Filesystem blob path or inline small content.
- Failure/backoff metadata.

### Required behavior

- Shared nominal 256 MiB persistent budget at parity.
- Cost-aware in-memory budgets for decoded tiles, CPU buckets, shaped runs, and
  symbol candidates.
- LRU or equivalent deterministic pruning.
- Conditional HTTP requests.
- Stale-on-network-failure.
- Request coalescing.
- Negative-result backoff.
- Atomic writes.
- Cache-version migration or safe discard.
- Debug statistics.

### Future explicit offline support

- Import local PMTiles/MBTiles.
- Download an area and zoom range.
- Show expected size and progress.
- Pin an offline area against LRU eviction.
- Validate archive attribution and source/style compatibility.

Do not block renderer parity on these features.

## Performance Model and Initial Budgets

These are engineering targets to validate, not guarantees:

- Camera-only frame at 60 Hz on the supported Linux hardware, with no geometry
  rebuild in the render path.
- Render-thread p95 below 16.7 ms during ordinary pan/zoom after warm-up.
- No synchronous network, MVT decode, text shaping, or filesystem I/O on GUI or
  render threads.
- Warm cached first useful frame below roughly 250 ms for a typical viewport.
- Progressive rendering: background/land/water, roads, then symbols.
- Bounded worker queues so rapid camera changes do not create unbounded memory
  or stale work.
- Persistent cache around the current 256 MiB default.
- Explicit decoded/bucket/GPU memory budgets established through profiling.
- Stable memory after a long scripted pan/zoom/rotate session.

Measure dense city, rural, low zoom, high zoom/overzoom, HiDPI, and two-map
scenarios separately.

## Testing Strategy

### Camera tests

- Projection round trips.
- Pixel pan and inverse pan.
- Fractional zoom.
- Bearing around center and arbitrary anchors.
- Antimeridian wrapping.
- Latitude limits.
- Rotated tile cover.
- Bounds fitting with padding.
- HiDPI invariants.

### Style compiler tests

- Compile the pinned WhereAmI style.
- Fail on every unsupported property/operator.
- Typed expression validation.
- Linear and exponential interpolation fixtures.
- Filter fixtures using real OpenFreeMap feature properties.
- Generated-output determinism.
- Upstream style-diff report.

### MVT and geometry tests

- Known MVT fixture corpus.
- Raw and gzip payloads.
- Empty and malformed features.
- Polygon holes and winding.
- Degenerate lines and polygons.
- Tile-edge clipping.
- Overzoom transforms.
- Fuzz MVT decode adapters and tessellation boundaries.

### Rendering golden tests

Capture deterministic images for:

- Low-zoom world/continent view.
- Dense road network.
- Water and coastlines.
- Bridges and tunnels.
- Buildings at source max zoom and overzoom.
- Administrative boundaries.
- POIs and road shields.
- Point labels.
- Curved/long road labels.
- Adjacent-tile label boundaries.
- Multiple writing systems and bidi text.
- Bearings 0, 45, 90, and 180 degrees.
- Device pixel ratios 1 and 2.

Golden tests can initially use an offscreen/CPU reference backend even when the
interactive renderer is GPU-backed. Integration tests must still verify the Qt
scene-graph output.

### Scheduler/cache tests

- Priority ordering.
- Request coalescing.
- Cancellation after camera changes.
- Parent fallback and replacement.
- Stale resource behavior.
- ETag/Last-Modified revalidation.
- Byte-budget eviction.
- Atomic cache writes.
- Concurrent main/timeline map requests.
- Network and disk failure injection.

### Symbol tests

- Shaping fixtures for Latin, Arabic, Hebrew, Devanagari, CJK, and mixed bidi.
- Font fallback.
- Stable symbol IDs.
- Collision determinism.
- Cross-tile deduplication.
- Keep-upright behavior through bearing changes.
- Line repetition and spacing.
- Atlas allocation/eviction.

### Qt integration tests

- QML type registration and construction.
- Property/signal behavior.
- Scene-graph initialization and invalidation.
- Resize and device-pixel-ratio changes.
- Application page switching.
- Persistent main-map camera state.
- Creation/destruction of the timeline map.
- Waypoint marker projection and hit testing.
- Wayland and X11.
- Software backend failure behavior, if unsupported.

### Long-running tests

- Scripted pan/zoom/rotate for at least an hour.
- Repeated timeline map construction/destruction.
- Network loss and recovery.
- Cache fills and eviction.
- Scene-graph/context recreation.
- Memory and goroutine leak checks.

## Implementation Plan

Estimates overlap and are not additive. They assume one experienced engineer
working primarily on the renderer.

### Phase 0: Qt Quick vertical spike

Target: 1-2 weeks.

Deliverables:

- Extend miqt locally or upstream to construct a custom `QQuickItem` with
  contents.
- Display one triangle or textured quad through the Qt Quick scene graph.
- Animate a transform without CPU geometry rebuild.
- Create and destroy the item repeatedly.
- Verify Wayland and X11.
- Verify render-thread callbacks and resource destruction.
- Document whether all used Qt APIs are public.

Exit criteria:

- A Go-owned retained scene renders reliably inside current WhereAmI QML.
- No private QtLocation API.
- No hand-written C++ renderer.
- No GUI/render-thread violations under race-oriented stress testing.

Stop and reconsider architecture if this cannot be achieved cleanly.

### Phase 1: Camera and one vector tile

Target cumulative time: 3-4 weeks.

Deliverables:

- QML center, zoom, bearing, and viewport properties.
- Web Mercator transforms and projection methods.
- Mouse/touch handlers reused from current QML.
- Fetch and decode one known OpenFreeMap tile.
- Render water, land, or one road layer as GPU geometry.
- Position one ordinary QML marker through `fromCoordinate`.

Exit criteria:

- Smooth camera transform without rebuilding the tile.
- Marker remains aligned during pan, zoom, resize, and bearing.

### Phase 2: Tile cover and basic basemap

Target cumulative time: 6-8 weeks.

Deliverables:

- Rotated visible-tile cover.
- Concurrent scheduler and cancellation.
- Parent fallback and overzoom.
- Background, fill, and basic line buckets.
- Layer-correct draw order and tile clipping.
- Progressive loading and errors.
- Basic in-memory cache.

Exit criteria:

- A recognizable, smooth vector map across normal WhereAmI camera movement.
- No visible tile seams in core fills/roads.
- Stable memory during a scripted tour.

### Phase 3: Controlled WhereAmI style

Target cumulative time: 8-12 weeks.

Deliverables:

- Pinned `whereami-liberty` source.
- Build-time style validation/compiler.
- Retained fill/line properties used by the style.
- Road casings, joins/caps, opacity, gap width, and selected dash support.
- Visual golden suite.

Exit criteria:

- Unsupported retained style features are zero.
- Core road/water/land/building appearance is intentionally accepted.

### Phase 4: Text and icons

Target cumulative time: 14-20 weeks.

Deliverables:

- Bundled/local fonts and font resolution.
- Text shaping.
- Glyph and sprite atlases.
- Point labels and icons.
- Basic line labels and road shields.
- Viewport-wide collision handling.
- Stable symbol IDs and cross-tile deduplication.
- Keep-upright behavior.

Exit criteria:

- Labels remain readable and upright while rotating.
- No systematic tile-edge duplicates.
- Panning does not cause unacceptable label churn.
- Target scripts pass shaping and visual tests.

### Phase 5: WhereAmI parity

Target cumulative time: 18-24 weeks.

Deliverables:

- Main and timeline map lifecycle.
- QML marker replacement.
- Dynamic timeline path.
- Shared resource/cache manager.
- Persistent 256 MiB cache.
- Existing camera animations and controls.
- Attribution and explicit style/source errors.
- Packaged Flatpak without MapLibre Native.

Exit criteria:

- Current WhereAmI map tests are adapted and pass.
- Manual feature-parity checklist passes.
- Offline cached-map behavior matches or improves on the current application.
- Long-running memory test is stable.

### Phase 6: Routes, tracks, and drawing

Can overlap after dynamic overlays are stable.

Deliverables chosen by product priority:

- Full GPX track/route model.
- Route provider and maneuver DTO.
- Route and alternative rendering.
- Drawing tools and persistence.
- Live-navigation state machine later.

## Estimates and Confidence

| Result | Estimate | Confidence |
| --- | ---: | --- |
| Qt Quick retained-rendering feasibility spike | 1-2 weeks | Medium |
| First OpenFreeMap vector geometry in QML | 3-4 weeks cumulative | Medium-high after spike |
| Smooth road/water/building prototype | 6-10 weeks cumulative | Medium |
| Controlled style plus acceptable point labels | 3-4 months cumulative | Medium |
| Current WhereAmI parity with credible labels/cache | 4-6 months cumulative | Medium-low |
| Production hardening and broad text/platform testing | 6-9 months cumulative | Medium-low |
| Near-MapLibre polish/generality | 9-18+ months | Low; scope-dependent |
| Basic route preview and maneuver list | 2-4 weeks after overlay path | Medium-high |
| Credible live navigation | Additional 2-4 months | Medium-low |
| Basic geometry drawing | 2-4 weeks after overlay path | Medium-high |
| Polished geometry editor | 1-2 months | Medium |

The estimates are dominated by Qt Quick integration and symbols. Adding a
second engineer will help scheduler/cache/testing work, but it will not halve
the critical path because renderer, scene-graph, and symbol architecture are
tightly coupled.

## Main Risks

### Qt Quick integration risk

miqt does not currently bind the required module surface. Generator extension,
subclass callbacks, thread affinity, and scene-graph lifecycle may expose gaps.
Mitigation: prove this first with a disposable vertical spike.

### Label-quality risk

Correct text shaping is available, but map label placement is not. Poor symbol
stability makes an otherwise correct renderer look broken. Mitigation: keep a
controlled style, define script coverage, build symbol goldens early, and avoid
promising complete MapLibre semantics.

### Style-scope creep

Trying to support arbitrary style JSON turns this into a MapLibre clone.
Mitigation: pin and compile one WhereAmI style; unsupported syntax fails the
build.

### GPU backend/private API risk

Direct QRhi usage may require private Qt headers. Mitigation: start with public
QSG nodes/materials, and make any backend restriction explicit.

### Resource lifetime risk

Go goroutines, Qt GUI objects, scene-graph render threads, and GPU contexts have
different ownership rules. Mitigation: immutable worker results, bounded
handoff queues, generation IDs, and render-thread-only GPU allocation/free.

### Performance and allocation risk

Straightforward Go MVT materialization and geometry building can allocate
heavily. Mitigation: benchmark dense real tiles, pool only after profiles,
retain buckets, and separate camera frames from layout work.

### Upstream data/style drift

OpenFreeMap source schemas and hosted styles evolve. Mitigation: pin style
source, validate TileJSON/schema assumptions, and make source updates an
intentional tested process.

### Font and sprite licensing

Bundled fonts and copied style/sprite assets have their own notices and terms.
Mitigation: perform a dependency/asset license audit before committing assets
to the application.

### Offline archive dependency weight

Some PMTiles packages include broad server/cloud dependencies. Mitigation: add
archive support after parity and consider a narrow internal reader if the
dependency cost is excessive.

## Concrete First Prototype

The first prototype should answer architecture questions, not attempt visual
completeness.

Acceptance test:

- A `WhereAmIMap` QML type backed by a Go-owned custom Qt Quick item.
- OpenFreeMap MVT at a fixed source zoom.
- One polygon layer and one road line layer rendered as retained GPU geometry.
- Fractional zoom.
- Bearing.
- Pixel pan.
- `toCoordinate`, `fromCoordinate`, and `alignCoordinateToPoint`.
- One ordinary QML waypoint visual projected over the map.
- One dynamic engine-side polyline.
- Render-thread resource cleanup after leaving the timeline page.
- Wayland and X11 verification.
- No MapLibre Native library in the process.

Do not include in this spike:

- General style JSON.
- Labels beyond one hard-coded shaped test label.
- Persistent cache.
- Navigation.
- Drawing editor.
- PMTiles.
- Multiple styles.

After the spike, make these decisions from measurements:

- Public QSG node/material path versus a temporary OpenGL FBO path.
- CPU-expanded versus shader-expanded lines.
- Adopt/vendor an Earcut implementation versus local triangulation code.
- Alpha glyph atlas versus SDF/MSDF.
- Whether any GoGPU components can integrate without conflicting with Qt's
  graphics ownership.
- Per-tile node granularity and batching strategy.

## Definition of Current-Feature Parity

- OpenFreeMap-derived vector basemap renders at zoom 0-20 with source overzoom.
- Fractional zoom, bearing, pan, resize, and HiDPI work smoothly.
- Text stays upright during bearing changes.
- Main map persists across page changes.
- Timeline map can be created and destroyed on demand.
- Existing center/zoom flight animations remain usable.
- Touch pinch zoom and rotation remain anchored correctly.
- Mouse drag, wheel, hover coordinates, double-click, and right-click work.
- Waypoint/cluster/current/search markers remain interactive and animated.
- Timeline context polyline and three markers render correctly.
- Cluster refresh receives zoom/camera notifications.
- Waypoint table can determine the real rotated visible region.
- Cache is persistent, bounded, shared, and usable offline after viewing.
- Attribution remains visible and correct.
- Style/source/network failures are surfaced explicitly.
- Flatpak no longer builds or packages MapLibre Native.
- No Qt private mapping-provider ABI is introduced.

## Phase 0 Prototype Result

Implemented on 2026-08-28 behind the opt-in `--vector-prototype` flag:

- A Go-owned `QQuickItem` subclass with `ItemHasContents`.
- A retained public-QSG `QSGGeometryNode` containing one triangle.
- `updatePaintNode` dispatch from Qt's render thread into Go through a
  `runtime/cgo.Handle`; no Go pointer is retained by C++.
- Go-controlled vertex generation, retained-node reuse, and atomic diagnostic
  counters.
- A focused generated miqt binding using only public Qt Quick and QSG classes.
  Small lifecycle and vertex-array helpers contain no application rendering
  policy.
- QML context-property integration and transform animation without replacing
  or modifying either production `QtLocation.Map` path.
- Explicit shutdown ordering: QML bindings/window/scene graph are destroyed
  before the C++-owned item, whose destructor releases the cgo handle.
- A `--vector-prototype-duration` diagnostic flag for repeatable smoke runs.

The following command completed successfully using the Qt software backend and
again using the active X11 `xcb` platform backend:

```bash
./bin/whereami \
  --vector-prototype \
  --vector-prototype-duration=3s \
  --debug
```

Both runs reported:

```text
Vector prototype scene graph: paint updates=1 geometry builds=1 geometry updates=0
```

This proves that Qt can render a Go-owned retained scene-graph node and animate
its item transform without rebuilding CPU geometry each frame. The repository
build, Go tests, `go vet`, QML lint, and all 75 QML tests pass.

A focused offscreen `QApplication` integration test now renders and destroys
ten QML windows, engines, items, and camera property maps without starting the
full WhereAmI application or binding its fixed API port. The test and the
package's camera tests pass under the race detector:

```bash
go test -race -tags=integration ./pkg/vecmap
```

Remaining Phase 0 work before treating the integration as production-ready:

- Exercise explicit scene-graph context invalidation in addition to the
  repeated window, engine, item, and callback lifetime covered by the focused
  integration test.

## Phase 1 Camera Prototype Result

The first Phase 1 increment replaces the local Phase 0 renderer shim and adds a
shared camera model:

- `internal/miqtquick` contains a tracked, focused miqt-generated binding for
  public `QQuickItem`, `QSGGeometry`, `QSGNode`, `QSGGeometryNode`, and
  `QSGFlatColorMaterial` APIs. The repository ignores `/vendor`, so the package
  cannot live in the dependency's vendored tree.
- The old handwritten quick-item bridge is
  removed. Go now constructs and updates retained QSG objects through the
  generated package.
- The generated virtual `QQuickItem` destructor releases its cgo callback
  handle. A child `QObject` guard releases the QML camera callback handle when
  the property map is destroyed.
- The immutable camera snapshot supports WGS84/Web Mercator projection,
  Mercator latitude clamping, fractional zoom, bearing, viewport resize,
  longitude wrapping, pixel pan, coordinate round trips, and
  coordinate-to-point alignment.
- QML can update center latitude/longitude, zoom, bearing, viewport size, and
  pan and anchored-zoom commands through a `QQmlPropertyMap`. Each accepted
  update publishes a camera revision and requests a scene-graph update.
- Wheel zoom preserves the coordinate under the pointer. Pinch captures the
  coordinate under the initial gesture centroid, then keeps it under the moving
  centroid while applying the gesture's absolute scale.
- A normal QML marker uses the same projection model as the retained geometry,
  exercising overlay alignment during bearing, pan, and resize changes.

Offscreen software, active X11 `xcb`, and native Wayland smoke runs completed
with one retained geometry allocation. A representative three-second run
reported:

```text
Vector prototype scene graph: paint updates=192 geometry builds=1 geometry updates=191
```

Go 1.27 builds, all Go tests, `go vet`, QML lint, all 75 QML tests, and the
focused lifecycle test under the race detector pass. The repository also passes
`staticcheck` with the generated miqt receiver-name check disabled.

The second Phase 1 increment fetches and renders one real OpenFreeMap road tile:

- The source is pinned to OpenFreeMap snapshot `20260823_080002_pt`, XYZ tile
  `9/250/193`, with SHA-256
  `5007c887f3c99a2c0737b9a3afdf3813ef3e1ce939a63aa09a8407b7c3770c79`.
- A worker goroutine reads a one-file cache or fetches the tile with a 15-second
  timeout. Both paths cap decompressed bytes at 2 MiB and verify the checksum
  before decode. Valid fetched bytes are written through an atomic rename.
- A narrow dependency-free protobuf/MVT reader validates wire types, lengths,
  varints, tag indexes, layer extent, line commands, and a 100,000-segment
  maximum. It reads supported road classes from the `transportation` layer and
  retains integer tile coordinates as small tile-local values.
- The pinned real fixture decodes to 71 supported road features and 15,704 line
  segments. The ordinary QML marker remains projected through the shared camera.
- Roads are retained as 31,408 endpoint vertices using portable one-pixel
  `DrawLines`. The vertex array is uploaded once when the geometry node is
  built; it is not rewritten for camera changes.
- Vertices use source-tile pixels in the range normally covered by `0..256`,
  keeping float32 values small without a Madrid-specific origin. A retained
  public `QSGTransformNode` applies fractional zoom, pan, resize, bearing, and
  the wrapped tile origin through one affine matrix update per camera frame.
- The focused binding adds only transform-node construction, child attachment,
  and a stack-allocated `QMatrix4x4` affine setter. It uses public Qt Quick APIs
  and does not retain Go memory.
- The QML host clips the custom scene graph to the prototype panel. No raster
  tile, MapLibre Native library, private Qt API, or production map path is used.
- Unit tests cover synthetic MVT line and multiline decode, malformed geometry,
  HTTP status, checksum, size limits, and cache hits. An opt-in fixture test
  validates the exact pinned PBF through `WHEREAMI_VECTOR_TILE_FIXTURE`.

An offscreen software run using the pinned cached tile reported:

```text
Vector prototype scene graph: paint updates=182 geometry builds=1 geometry updates=0 transform updates=181 tile loaded=true road features=71 road segments=15704
```

The build, Go tests, `go vet`, QML lint, all 75 QML tests, and the ten-window
lifecycle test under the race detector pass after this increment. Camera
transform tests compare retained-reference projection against `FromCoordinate`
across fractional zoom, bearing, center, and viewport changes. The lifecycle
test explicitly requires one geometry build, zero geometry updates, and a
subsequent transform update. A focused binding test also maps a known point
through the actual C++ `QMatrix4x4`, covering row/column placement and float32
conversion rather than testing only the Go-side affine calculation.

Remaining Phase 1 work is interactive marker-alignment verification during
drag, zoom, resize, and bearing. Wider styled roads will need portable shader or
CPU bucket expansion in a later phase; Qt guarantees only one-pixel line width
across all scene-graph backends.

## Phase 2 Tile Cover Prototype Result

The first Phase 2 increment expands the road prototype from one known tile to a
camera-driven cover while retaining the pinned OpenFreeMap snapshot:

- The source zoom follows the integer camera zoom and clamps to OpenFreeMap's
  zoom 14 maximum. Four rotated viewport corners determine the cover, which has
  a one-tile prefetch ring, canonical wrapped X indexes, clamped Y indexes,
  center-first priority, deduplication at low zoom, and a hard 8-by-8 bound.
- A scheduler allows at most four tile loads at once and publishes immutable,
  progressive snapshots. It retains loaded tiles still in the current cover,
  cancels requests that become stale, drops stale results, and retries transient
  failures at 250-millisecond intervals with a three-attempt ceiling.
- Cover requests use the `QQuickItem`'s actual render size rather than depending
  on callers to mirror width and height into camera properties. Concurrent
  request writers coalesce through a locked latest-value slot and one wakeup.
- The snapshot URL and cache path are generated per XYZ tile. The original
  fixture keeps its published SHA-256 and compatible cache name. Other fetched
  tiles receive an atomically written local SHA-256 sidecar, which is checked on
  every cache read before MVT decode.
- MVT line coordinates remain tile-local rather than round-tripping through
  WGS84. Each retained tile subtree has its own public `QSGTransformNode`; its
  affine matrix chooses the world copy nearest the camera, including at the
  antimeridian.
- One root `QSGNode` owns the active tile subtrees. Progressive arrivals append
  geometry without rebuilding existing tiles. Cover changes detach and delete
  stale subtrees on the render thread. Camera changes update matrices only;
  geometry updates remain zero.
- Diagnostic counters now report requested, loading, loaded, and failed tiles,
  cumulative geometry builds and removals, matrix updates, road features, and
  road segments. A one-million-segment ceiling bounds current target and parent
  resources in addition to the 100,000-segment per-tile decoder limit. Previous
  cover continuity is separately bounded by the prior 8-by-8 selection and is
  released as replacements arrive.
- Unit tests cover rotated tile selection, wrapping, hard cover bounds, dynamic
  cache sidecars, four-request concurrency, cancellation, stale-result
  suppression, and failure deduplication. The ten-window race-enabled lifecycle
  test loads multiple tiles, updates their transforms, pans by four tiles,
  removes stale QSG children, and tears every item down.

A four-second offscreen software run loaded the complete current cover from the
pinned snapshot and reported:

```text
Vector prototype scene graph: paint updates=258 geometry builds=20 geometry updates=0 geometry removals=4 transform updates=3984 tiles requested=16 loaded=16 loading=0 errors=0 road features=356 road segments=58493
```

The second Phase 2 increment adds loading continuity and one-level parent
fallback:

- Target tiles, load resources, previous-cover continuity, and rendered tiles
  are separate scheduler sets. Immediate parents are deduplicated and
  interleaved ahead of the first child in each group without changing target
  completion accounting.
- A loaded parent remains the only rendered node for its visible child group
  until every requested child in that group is ready. The snapshot then removes
  the parent and exposes all children together, avoiding overlapping parent and
  child road geometry.
- Zoom-in retains the previous source tile as the new child group's parent.
  Zoom-out retains a complete set of four previous children until the new lower
  zoom target arrives, preferring those children over a coarser grandparent.
- Failed children keep a valid parent visible through the bounded retry cycle.
  Parent failures are omitted from final error reporting when all target
  children succeed. Satisfied parents are canceled or released from memory;
  their verified bytes remain in the disk cache.
- Completed request contexts are canceled immediately, successful retries clear
  prior failure state, duplicate starts are rejected, and delayed retries remain
  part of the loading count. Continuity selection accepts only exact tiles,
  immediate parents, or complete/partial direct-child sets, so a stale coarse
  ancestor cannot suppress ready neighboring targets.
- Runtime statistics include the number of currently rendered fallback nodes.
  Deterministic tests release children one at a time and assert that no partial
  child group is published. Additional tests cover zoom-out continuity, child
  failure, stale cancellation, scheduler shutdown, and antimeridian hierarchy.

A rebuilt four-second offscreen run transitioned through parent fallback and
finished on the complete 16-tile target cover:

```text
Vector prototype scene graph: paint updates=253 geometry builds=22 geometry updates=0 geometry removals=6 transform updates=3904 tiles requested=16 loaded=16 fallback=0 loading=0 errors=0 road features=356 road segments=58493
```

The third Phase 2 increment adds the first complete background/land/water/road
basemap stack:

- The dependency-free MVT reader now decodes polygon command streams from
  `land`, `landcover`, `landuse`, `park`, and `water`. It validates command
  order, truncation, closure, ring size, winding, and exterior-before-interior
  ordering.
- A bounded Go ear-clipping implementation removes duplicate and collinear ring
  points and triangulates concave rings. Interior rings become explicit
  cutout meshes. Water cutouts restore the land color; land cutouts restore the
  background. The decoder allows at most 32,768 transient points in one polygon
  feature, spends at most ten million cleanup/triangulation operations per tile,
  and retains at most 100,000 fill triangles per tile. The scheduler caps the
  current target and parent resources at one million fill triangles.
- Every retained tile subtree now has the fixed public-QSG order
  `transform -> rectangular clip -> background -> land -> land cutouts -> water
  -> water cutouts -> roads`. Each local clip covers `0..256`, so buffered source
  geometry cannot paint neighboring tiles. Camera changes still update only the
  outer transform matrix.
- The focused miqt binding adds construction and rectangle access for public
  `QSGClipNode`. Each clip owns the four-vertex rectangle geometry required by
  Qt when rotation moves clipping from the optimized scissor path to the stencil
  path. Fill and background nodes use `DrawTriangles` with flat-color materials;
  roads remain portable one-pixel `DrawLines` above them.
- Synthetic tests cover concave and reversed rings, collinear cleanup, holes,
  malformed polygon commands, resource limits, draw-input ordering, and triangle
  area. Malformed road or polygon features are skipped without discarding valid
  geometry from the tile; resource-limit failures reject the tile. The
  race-enabled ten-window lifecycle test now renders out-of-bounds land, water,
  a water cutout, and roads through the clip hierarchy.
- The pinned Madrid fixture decodes 649 land features into 20,567 triangles and
  17 water features into 842 triangles, in addition to its 71 road features and
  15,704 road segments.

A clean four-second offscreen software run completed a 16-tile cover with no
tile errors and reported:

```text
Vector prototype scene graph: paint updates=224 geometry builds=29 geometry updates=0 geometry removals=13 transform updates=3100 tiles requested=16 loaded=16 fallback=0 loading=0 errors=0 land features=5286 triangles=249806 water features=135 triangles=13296 road features=356 segments=58493
```

Feature-level hole-aware triangulation remains preferable to the current
bucket-level cutout approximation. Bounded cache eviction, wider styled roads,
buildings, and long scripted-tour memory validation also remain outstanding.

## Phase 3 Liberty Style Prototype Result

Implemented through 2026-09-01. The Go renderer is now the default production
map backend; `--vector-prototype` forces it while retaining diagnostic smoke
options, and `--legacy-map-renderer` explicitly selects QtLocation/MapLibre.

### Pinned inputs

- The embedded OpenFreeMap Liberty style has 111 ordered layers: one
  background, one raster, 16 fills, one fill extrusion, 67 lines, and 25 symbol
  layers. Its SHA-256 is
  `6010998863b4876911ac9a2d62c9a28d97c8877f6d20cd158b74808572257b60`.
- The embedded 264-entry sprite index has SHA-256
  `73e75e58d8c7bb62cc25d9d150660500552f4d04f6eac5efa4e236076773c356`.
  The sprite PNG has SHA-256
  `8996a519d218dc5f98015267709dae272a77bb74ef0ecc5a0992dcf276c1be4c`.
- `go generate` fetches all three resources with explicit size limits and
  refuses checksum drift.
- Vector tiles remain pinned to OpenFreeMap snapshot
  `20260823_080002_pt`. Low zooms also load the Liberty Natural Earth PNG
  source, accepting its 256- and 512-physical-pixel variants as one logical
  256-pixel tile.

### Decode and style compilation

- The dependency-free MVT decoder preserves feature IDs, points, lines,
  hole-aware polygons, source-layer membership, and typed string, float,
  double, signed, unsigned, and boolean properties.
- All expression operators used by this Liberty snapshot are implemented:
  `!`, `!=`, `<`, `<=`, `==`, `>=`, `all`, `case`, `coalesce`, `concat`,
  `geometry-type`, `get`, `has`, `interpolate`, `match`, `step`, `to-string`,
  and `zoom`. Missing `get` values use Style Spec null semantics. Expression
  recursion is capped at 64 levels.
- Style evaluation creates immutable prepared tile snapshots on a coalescing
  worker. Fractional style zoom is quantized to 1/16. Stale worker results are
  discarded, so continuous zoom does not run layer filtering, tessellation, or
  symbol generation in `updatePaintNode`.
- Ordered primitives cover background, raster, fill, flattened fill extrusion,
  line, and symbol layers. Paint includes data-driven/interpolated colors,
  opacity, widths, line offsets and gaps, dash arrays, caps, joins, fill
  outlines, and sprite-backed fill patterns.
- Default miter joins, round joins/caps, miter-correct offsets, zero-length dash
  entries, and explicit dash resource failures are handled. Pattern UVs use a
  world-coordinate phase, so the wetland and pedestrian patterns continue
  across tile and wrapped-world boundaries.
- Polygon holes and large rings use the vendored Mapbox Earcut Go port at
  commit `f3ec78d874709ec6e9f85da3cd0e7e641d73d897`, with its ISC license,
  corrected z-order quantization, and empty-hole handling. Input and operation
  budgets are checked before and after triangulation.

### Public QSG rendering

- The focused `internal/miqtquick` bridge uses public `QSGGeometryNode`,
  `QSGTransformNode`, `QSGClipNode`, `QSGImageNode`, `QSGTextNode`, texture
  material, and geometry APIs. No Qt private mapping or scene-graph API is used.
- PNG/RGBA image nodes copy borrowed Go memory before returning. Pattern
  materials own their textures. Text uses `QTextLayout` for wrapping, font
  weight/style, letter spacing, line height, anchors, and Qt shaping.
- The render thread builds QSG objects from prepared snapshots and then handles
  ordinary camera frames with affine transform updates only. Scene-graph
  invalidation clears all retained Go wrappers without deleting nodes Qt has
  already destroyed.
- Low zoom renders bounded repeated world copies. Pattern phases, collision
  keys, symbols, rasters, fills, and lines all include the selected wrap.
- Line-symbol spacing is converted from screen pixels to source-tile units,
  including source overzoom. Viewport-aligned shields ignore road angle, while
  icon and text keep-upright defaults retain their separate Style Spec behavior.
- Text and icons have independent collision boxes, overlap rules, padding, and
  optionality. Collision placement uses a bounded 64-pixel screen-space grid
  instead of an all-pairs scan.

### Resource and lifecycle limits

- Network and cache reads are capped at 2 MiB and use checksum sidecars plus
  atomic rename. Malformed MVT, PNG, geometry, and non-retryable resource errors
  do not enter the retry loop.
- Per-tile limits cover features, points, road segments, polygon points,
  triangulation operations, styled triangles, and symbol candidates. Cover
  limits include one million road segments, one million legacy fill triangles,
  and 250,000 symbol candidates. Viewport collision references are capped at
  100,000.
- The tinted sprite cache is mutex-protected and capped at 512 entries. Sprite
  atlas arithmetic is performed in `int64` before offsets are formed.
- Camera latitude, dimensions, bearing, and zoom are normalized; supported zoom
  is restricted to 0 through 20. Wrapped-world enumeration is capped at eight
  copies in either direction.
- The race-enabled lifecycle test repeatedly creates, renders, destroys, and
  closes QML engines, windows, items, worker compilers, schedulers, text nodes,
  image nodes, and callback handles. The item remains parentless at the QObject
  level and uses explicit C++ ownership.

### Current verification

- `go test ./...`, `go vet ./...`, focused race tests, and the offscreen
  race-enabled lifecycle integration test pass with Go 1.27.
- `staticcheck` passes for the new renderer, QSG bridge, and Earcut packages
  with package-comment and generated miqt receiver naming checks excluded.
- All 77 QML tests pass. QML lint completes non-fatally; in addition to existing
  warnings, it reports static-type warnings for dynamic Loader and context
  properties used by the backend adapter.
- Offscreen software smoke runs completed with zero tile errors at zooms 0, 6,
  9, and 12. Representative final prepared-scene output at zoom 6 loaded all 48
  requested tiles, 48 rasters, 5,916,976 styled triangles, and 3,616 symbol
  candidates.

### Remaining visual gaps

- Line text is shaped as one rigid run at the local path angle rather than
  placing glyphs along a curve.
- SDF labels use OpenFreeMap glyph advances for collision dimensions. Labels
  that still require Qt complex-script shaping retain estimated collision
  dimensions. Collision placement is refreshed on prepared-scene, glyph-atlas,
  or viewport changes rather than every small camera transform.
- `text-halo-width` and `text-halo-blur` are numeric shader inputs. Halos render
  in a complete pass before glyph fills so adjacent glyph quads cannot paint a
  halo over an earlier fill.
- `fill-antialias` cannot be selected per primitive in the current flat public
  QSG geometry path.
- Fill extrusion is intentionally flat. With the current pitch-free camera its
  roof footprint is equivalent to a top-down extrusion, but no building sides
  or pitched view exist.
- OpenFreeMap remote glyph PBFs are the primary simple-script and CJK label
  source. Complex shaping, bidirectional text, combining sequences, and
  supplementary-plane code points still use the Qt text path.

## Phase 4 Production Integration Result

The production QML cutover was completed on 2026-09-01.

- `main.go` creates separate main and timeline Go items with independent
  cameras and scene graphs. They share `${effectiveCacheDir}/vector`; the
  timeline item's QML host remains loader-controlled.
- `MapAdapter.qml` provides the center, zoom, bearing, pan, projection,
  unprojection, and alignment surface formerly supplied by `QtLocation.Map`.
  `ProjectedMapItem.qml` and `ProjectedMapPolyline.qml` replace QtLocation map
  overlays with ordinary projected QML content.
- `MapView.qml` and `TimelineView.qml` use the adapter in production. The old
  diagnostic overlay was removed from `Main.qml`.
- Status polling publishes requested, loaded, loading, and failed tile counts
  through each camera property map. A total Go tile failure activates the
  legacy loader; partial errors remain visible without being mislabeled as an
  active fallback.
- `--legacy-map-renderer` explicitly selects QtLocation/MapLibre. Go renderer
  initialization failure also selects it automatically. If MapLibre is absent,
  the existing overlay-only QtLocation provider preserves application overlay
  interaction.
- The shared tile cache is bounded to 256 MiB and coordinates atomic
  payload/checksum writes, recency updates, stale temporary cleanup, pair-wise
  eviction, traversal rejection, and pinned-fixture preservation.
- Policy and data preparation moved out of handwritten C++: MVT point geometry,
  PNG decoding, pattern UV generation, font/text layout selection, affine point
  mapping, and queued item updates are now implemented in Go or generated miqt
  calls. Handwritten C++ remains only where the public QSG ABI or guarded Qt
  callback lifetime requires it.
- Repository-wide Go tests, focused race tests, Go vet, lifecycle integration,
  all 77 QML tests, resource generation, and the application build pass with Go
  1.27. Focused renderer coverage is 63.5%; the QSG bridge is primarily covered
  by lifecycle integration rather than ordinary unit coverage.
- A production smoke exposed generated miqt finalizers on `QTextLine` and
  `QRectF` wrappers that had also been deleted deterministically. The bridge now
  clears those finalizers before deletion, and the lifecycle integration test
  forces two garbage collections while a text-bearing scene is live.
- Camera revision acknowledgements no longer retarget active QML zoom
  animations. Low-zoom panning is constrained to the rotated Mercator viewport,
  and projected overlays observe a guarded revision property instead of a
  lifecycle-sensitive dynamic `Connections` target.
- The production renderer fetches bounded 256-code-point OpenFreeMap glyph
  ranges asynchronously, verifies checksum-backed cache pairs, decodes Fontnik
  metrics without a protobuf dependency, and packs only needed glyphs into a
  deterministic scene atlas. A public QSG material renders MapLibre-compatible
  SDF fill and two-pass halo thresholds from shader packs committed for every
  Qt RHI backend. KlokanTech Noto Sans remains the temporary complex-shaping
  path.

### Transition hardening

Camera-flight continuity was hardened on 2026-09-02 after rapid waypoint zooms
could leave only the previously detailed map area visible:

- The tile scheduler loads direct parent fallbacks before detailed targets, so
  cold covers fill coarsely before they refine.
- Retained coverage now accepts the nearest loaded ancestor or a complete,
  non-overlapping descendant cover at any source-zoom depth. Mixed-depth
  descendants are measured by their quadtree area rather than requiring exactly
  four direct children.
- Parent fallbacks are still replaced atomically. Aggregate road, fill, and
  symbol limits are checked against the tiles selected for rendering, not the
  simultaneously loaded fallback plus its partial replacements. A genuinely
  over-limit selected cover still keeps its coarser fallback.
- Tile load, style compilation, and glyph range failures are reported when they
  occur with tile or range context. Error and fatal log lines use stdout so a
  complete failure report can be copied directly; informational and debug logs
  remain on stderr.
- Per-feature MVT geometry and triangulation limits degrade a tile instead of
  rejecting it. The decoder retains bounded content processed before the limit,
  skips the offending and subsequent over-budget features, and logs one summary
  per source layer and rendering stage with tile coordinates and feature index.
  These summaries are stdout warnings; they do not increment scheduler tile
  errors or activate the map error overlay.
- Repository-wide Go tests, focused and lifecycle race tests, `go vet`, focused
  `staticcheck`, the production build, and all 81 QML tests pass. OpenGL and
  Vulkan zoom-6 production smokes each loaded all 48 requested tiles with zero
  errors while rendering 6,883,181 styled triangles and 92 SDF labels.

## Open Decisions

- Which exact OpenFreeMap style revision and visual subset becomes
  `whereami-liberty`?
- Which scripts/languages are release-blocking for label shaping and fallback?
- When should the temporary Qt complex-script path be replaced by HarfBuzz
  shaping against OpenFreeMap glyph IDs?
- Is source/style selection a future product requirement, or can one compiled
  style remain a deliberate constraint?
- What are the target GPU and memory floors for supported Linux systems?
- Should offline PMTiles be an early requirement or a post-parity feature?
- Is route preview or geometry drawing the first feature after basemap parity?

## Source Links

- MapLibre Style Specification:
  `https://maplibre.org/maplibre-style-spec/`
- OpenFreeMap:
  `https://openfreemap.org/`
- OpenFreeMap repository:
  `https://github.com/hyperknot/openfreemap`
- OpenFreeMap styles:
  `https://github.com/hyperknot/openfreemap-styles`
- Current Liberty style:
  `https://tiles.openfreemap.org/styles/liberty`
- Current OpenFreeMap TileJSON:
  `https://tiles.openfreemap.org/planet`
- `paulmach/orb`:
  `https://github.com/paulmach/orb`
- `peterstace/simplefeatures`:
  `https://github.com/peterstace/simplefeatures`
- `go-text/typesetting`:
  `https://github.com/go-text/typesetting`
- `tdewolff/canvas`:
  `https://github.com/tdewolff/canvas`
- `akhenakh/maprender`:
  `https://github.com/akhenakh/maprender`
- `protomaps/go-pmtiles`:
  `https://github.com/protomaps/go-pmtiles`
- `twpayne/go-gpx`:
  `https://github.com/twpayne/go-gpx`
- `twpayne/go-polyline`:
  `https://github.com/twpayne/go-polyline`
- `dgraph-io/ristretto`:
  `https://github.com/dgraph-io/ristretto`
- `tidwall/rtree`:
  `https://github.com/tidwall/rtree`
- GoGPU `gg`:
  `https://github.com/gogpu/gg`
- `oliverbestmann/earcut-go`:
  `https://github.com/oliverbestmann/earcut-go`
- `rclancey/earcut`:
  `https://github.com/rclancey/earcut`
- miqt:
  `https://github.com/mappu/miqt`
- Qt Quick scene graph overview:
  `https://doc.qt.io/qt-6/qtquick-visualcanvas-scenegraph.html`
- Qt custom scene-graph items:
  `https://doc.qt.io/qt-6/qtquick-scenegraph-customgeometry-example.html`
- OSRM HTTP API:
  `https://project-osrm.org/docs/`
- Valhalla route API:
  `https://valhalla.github.io/valhalla/api/route/api-reference/`
- PMTiles:
  `https://github.com/protomaps/PMTiles`
- Mapbox Vector Tile specification:
  `https://mapbox.github.io/vector-tile-spec/`

## Research Notes

- Research was performed against the repository state present on 2026-08-28.
- The current hosted Liberty style and TileJSON are mutable; counts and source
  revisions in this document are observations from that date.
- The workspace vulnerability scan reported Go advisory `GO-2026-5024` in the
  standard library (`NewNTUnicodeString` length overflow). It was unrelated to
  this map-renderer research and had no reported call trace from WhereAmI, but
  should remain a separate toolchain follow-up.
- The initial research pass changed only this document; the later Phase 0 and
  Phase 1 prototype sections record the implementation work that followed.
