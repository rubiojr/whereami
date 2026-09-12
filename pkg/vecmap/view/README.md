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
zero uses the world copy nearest the camera's center; additional wraps offset
that copy. Normalize camera literals with `Normalized` before rendering.

The cover preserves the existing scheduler policy: a one-tile prefetch ring,
maximum source zoom 14, and at most 8x8 canonical tiles, ordered nearest first.
Provider-specific cover policies remain a future extension.

Run all camera and cover regressions without Qt:

```sh
CGO_ENABLED=0 go test ./pkg/vecmap/view
```
