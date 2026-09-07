# vecmap

`vecmap` is a Qt Quick vector map renderer written in Go. It fetches bounded
OpenFreeMap vector tiles, renders the pinned Liberty style through public Qt
scene-graph APIs, and exposes camera state through a `QQmlPropertyMap`.

The package requires Qt 6 QML and Quick development libraries through
`pkg-config`, and a `QGuiApplication` must exist before constructing an item.

```go
camera := vecmap.NewCamera(
	vecmap.Coordinate{Latitude: 51.5074, Longitude: -0.1278},
	10, 0, 800, 600,
)
item, err := vecmap.NewWithOptions(vecmap.Options{
	CacheDir:      cacheDir,
	InitialCamera: &camera,
})
```

Expose `item.QObject()` and `item.CameraQObject()` to QML. Keep the Go item
alive until the Qt event loop stops, then call `item.Close()` on the GUI thread.

## Package layout

- `quickitem.go`: public item API, construction, publication, and shutdown
- `quickitem_scene.go`: render-thread updates and retained-scene reconciliation
- `quickitem_liberty.go`: Liberty layer, SDF atlas, and symbol orchestration
- `quickitem_nodes.go`: Qt scene-graph node construction
- `quickitem_stats.go`: public renderer statistics and tile aggregation
- `geometry.go`: tile-local geometry, transforms, wrapping, and vertex helpers
- `tile_bucket.go`: decoded tile payload and fill bucket types
- `scheduler.go`: asynchronous loading, cancellation, and retries
- `scheduler_cover.go`: cover transitions, continuity, and render selection
- `scheduler_budget.go`: selected-cover resource admission
- `scheduler_snapshot.go`: immutable renderer status snapshots
- `liberty.go`: embedded style loading and compiled layer access
- `liberty_expression.go`: style expression evaluation and coercion
- `liberty_color.go`: style color parsing and interpolation
- `tile.go`, `mvt.go`, and `feature.go`: bounded network, cache, and MVT decode

The generated QSG bridge and Earcut implementation remain private module
dependencies under `internal/`; no internal types are exposed by the public API.
