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

## Go/native boundary

Go handles tile decoding, triangulation, style evaluation, glyph layout, atlas
packing, symbol placement, and retained-scene reconciliation. The private
`internal/miqtquick/renderdata.go` also prepares SDF material uniforms and orders
halo/fill passes. Go writes pattern and SDF vertices directly into Qt-owned
buffers before publishing their nodes to the scene graph.

The remaining handwritten C++ constructs Qt objects, manages native texture
lifetimes, connects the camera signal, and implements Qt's shader callbacks.
Node construction stays batched across cgo. Shader callbacks stay native to
avoid calling Go for every material comparison and uniform update.

### Bridge benchmarks

Run the native allocation/write/destruction benchmarks on Qt's render thread:

```sh
go test -tags integration ./internal/miqtquick -run '^$' \
  -bench 'BufferBridge$' -benchmem -count=5
```

Measured with Go 1.27.1, Qt 6.11.2, linux/amd64, Ryzen AI 7 PRO 350. Medians of
five runs against `06073ef`, using the same benchmark harness:

| Node creation and destruction | Before | Go buffer writes | Go bytes/op before → after |
| --- | ---: | ---: | ---: |
| Pattern, 6,144 vertices | 16.62 µs | 9.73 µs | 98,312 → 16 |
| SDF fill, 32 glyphs | 194.6 ns | 183.8 ns | 40 → 8 |
| SDF halo + fill, 32 glyphs | 335.3 ns | 287.2 ns | 40 → 8 |

These measure the bridge with software-backend textures, not GPU rendering or
whole-map frame time. Native allocations aren't included in Go bytes/op.
Pattern preparation loses a temporary vertex buffer and a full copy. SDF
material preparation stays on the stack through a noescape/nocallback bridge;
vertex copies use Go's architecture-specific runtime implementation.

No explicit SIMD or Go version bump is needed for these changes. Profile full
frames before adding architecture-specific arithmetic: these benchmarks don't
establish how much of the MapLibre Native performance gap comes from this work.

## Go-owned RHI prototype

An opt-in backend now consumes toolkit-neutral data from `pkg/vecmap/scene` and
renders through generated `QSGRenderNode`/`QRhi` bindings. Its standalone viewer
has no dependency on the legacy handwritten Qt map bridge. Build it with
`make rhi-build`; see [the prototype guide](../../docs/vecmap-rhi.md) for fixture
capture, hardware measurements, Flatpak dependency requirements, and the remaining
migration gates tracked by kata `ngrb`.
