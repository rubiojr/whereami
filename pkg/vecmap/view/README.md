# view

Toolkit-neutral Web Mercator camera and tile geometry extracted from vecmap.
This package depends only on the Go standard library. It can be used by another
renderer without Qt, cgo, native graphics handles, or the parent vecmap package.

```go
camera := view.NewCamera(view.Coordinate{Latitude: 40.4168, Longitude: -3.7038},
    10, 0, 800, 600)
cover := view.VisibleTileCover(camera)
transform := view.TileTransform(camera, cover[0], 0)
point := transform.MapPoint(view.ScreenPoint{X: 128, Y: 128})
```

Coordinates use latitude/longitude in degrees; screen and tile-local positions
use logical pixels. Tile size is 256 pixels at its native zoom. Transform wrap
zero uses the tile-center world copy nearest the camera's center; additional wraps offset
that copy. Normalize camera literals with `Normalized` before rendering.

The cover preserves the existing scheduler policy: a one-tile prefetch ring,
maximum source zoom 14, and at most 8x8 canonical tiles, ordered nearest first.
Provider-specific cover policies remain a future extension.

`VisibleTileCoverAt(camera, coarser)` takes tiles from `coarser` zoom levels below
the camera zoom (0 to `MaxCoarser`). One draws a tile 512 pixels wide, as MapLibre
draws the same tile, so a view needs fewer tiles. Prepare those tiles with the
same `tiles.PrepareOptions.Coarser` and the style zoom of `StyleZoomAt`, which
lies `coarser` levels below the camera zoom. Above camera zoom 14 plus `coarser`
the tiles are the same as without the option and only the style zoom differs.
Below camera zoom `coarser` the style zoom stays at zero.

`LoadOrder` shares immediate-parent-first, deduplicated loading priority between
the production scheduler and the headless producer. It preserves target order;
transport completion order, cancellation and readiness remain caller-owned.

`GroupTiles` and `SelectCover` now share the production scheduler's fallback policy:
requested siblings refine together, detailed previous coverage survives zoom-out,
and missing targets can use an immediate parent or eligible ancestor/descendant
continuity. Selection is canonical and nonoverlapping; world instancing remains
separate. Ready/continuity state belongs to the caller. The helpers take trusted,
bounded canonical inputs at source zoom <=14 and do no I/O or native work.

The [tiles compositor](../tiles/README.md) supplies validated bounds, retained scenes,
layer/wrap assembly and cross-tile placement. It takes continuity explicitly from
the acknowledged Current rather than remembering its last queued target. The
production scheduler also delegates its existing selection to these shared helpers.

`StyleZoom` shares the production sixteenth-zoom preparation policy. Transforms
still use continuous camera zoom. `WorldWraps` also retains an adjacent world copy
for narrow antimeridian views, where a root fallback can otherwise leave half the
viewport blank. This is a conservative shared policy, not per-tile instance culling.
Headless viewport sampling and native pixel tests cover the **p5nh** correction.

Run all camera and cover regressions without Qt:

```sh
CGO_ENABLED=0 go test ./pkg/vecmap/view
```
