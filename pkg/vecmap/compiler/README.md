# compiler

Toolkit-neutral fill and line layer compilation extracted from vecmap. It reuses
the existing style evaluator, shared MVT source model, geometry topology builder
and complete line tessellator. It builds without Qt or cgo and adds no module
dependency.

```go
err := compiler.CompileLine(features, layer, compiler.LayerOptions{
    SourceZoom: 9, Zoom: 10.5, Indexed: true,
}, func(mesh geometry.Mesh, color style.Color) error {
    // Account for tile-wide triangle limits before retaining the owned mesh.
    return appendSolid(mesh, color)
})
```

`CompileFill` additionally takes a pattern sink receiving a mesh, sprite name,
tile-unit pattern scale and raw evaluated opacity. Callers select the layer kind,
visibility and source-layer features, and supply asset readiness and publication
policy. The functions do not perform I/O or own caches/native resources.

## Compatibility

- Fill batches group by evaluated RGBA; patterns group by name and full-precision
  opacity string. Output remains **solids, then patterns, then outlines**, with
  first-seen batch order inside each group and source feature order within batches.
  This preserves the previous grouping, including its treatment of transparency.
- `fill-extrusion` remains flat fill geometry using extrusion color/opacity.
  Pattern lookup still uses `fill-pattern`, and outline opacity still uses
  `fill-opacity`. Invalid fill colors skip both fill and outline for that feature.
  A pattern takes precedence over fill-color parsing.
- Outlines use exterior rings and holes, a one-screen-pixel width, butt caps and
  round joins. Ring closure appends the first point even for already-closed rings;
  the geometry engine performs its existing cleanup.
- Width, gap and offset divide by `2^(zoom-sourceZoom)`. Pattern scale is its
  reciprocal. Gap lines subtract `(gap+width)/2` from the offset for the first
  side, then add it for the second. Line features borrow paths during preparation;
  polygon line features use owned closed-ring scratch copies.
- Line batch keys retain **nine significant digits** for width/offset, exact
  integer RGBA and full-precision dash values, plus cap/join strings. Near-identical
  widths can therefore merge intentionally, retaining the first batch's paint.
- Dash arrays retain their separate legacy validation: missing, non-array,
  nonnumeric or negative components disable dashing. Signed zero, NaN and positive
  infinity survive this helper; the geometry engine retains its own dash handling.
  This differs from `style.NumberArrayValue`, which accepts negative components.
- Empty meshes and transparent fills/patterns may reach sinks. The parent filters
  them as before; this API does not add another filtering pass.

## Ownership, limits and failure

Features must be bounded, finite, prepared MVT source data, normally from
`mvt.DecodeTile`. Layers are application-owned compiled styles under the `style`
package's input/work contract. These functions are not an untrusted Go-object
validation boundary. Feature/path counts, aggregate scratch/output memory, style
expression work and scheduling remain caller-bounded.

Each fill/pattern builder or line batch is limited to **2,000,000 triangles**.
`LayerOptions.TriangleLimit` can lower that ceiling; zero selects the default.
This is a **per-batch** limit, not a layer/tile-wide budget. The parent still counts
emitted triangles across all layers and publishes primitives only on successful
tile compilation. The line engine retains its dash-segment and iteration bounds.

Options reject a nonpositive/nonfinite derived scale or an out-of-range triangle
limit before preparation. Nil required sinks also return `ErrOptions`. Geometry
failures preserve the parent's `mvt.ErrFeatureResourceLimit` wrapping; sink errors
propagate unchanged. Earlier successful emissions remain accepted after later
errors. Fill accumulation fails before any emission, but an outline failure can
occur after fills/patterns have been emitted. Consumers needing atomic output
must stage it until success.

Inputs and sinks are neither mutated nor retained beyond the call. Emitted meshes
own their slices; logical pattern names may borrow immutable style/property data.
Treat all published data as immutable. Vecmap's adapters pass its shared feature
slices directly and attach the original layer identity at each sink, without
conversion slices/maps or a second geometry-preparation pass.

Tile-wide orchestration, background preparation, symbol collection, retained
primitive data and scene packing remain in vecmap. Extracting those is the next
step toward a fully headless fixture producer.

## Verification

```sh
CGO_ENABLED=0 go test -cover ./pkg/vecmap/compiler
WHEREAMI_VECTOR_TILE_FIXTURE=/path/to/pinned.pbf \
  CGO_ENABLED=0 GOARCH=386 go test ./pkg/vecmap/compiler
CGO_ENABLED=0 go test ./pkg/vecmap/compiler -run '^$' \
  -fuzz '^FuzzLayerCompilation$' -fuzztime=20s
```

Coverage is **100%**. Tests cover paint/defaults, filters, solid/pattern/outline
ordering, extrusion behavior, gaps and scale, ring holes, dashes/key precision,
ownership, lowered limits, nil sinks and streaming failures. Optional pinned-tile
tests compare every expanded/indexed coordinate bit at zooms 10 and 10.5.
See kata **zfjf** and [`docs/vecmap-rhi.md`](../../../docs/vecmap-rhi.md) for full-scene
byte equality and controlled preparation-cost measurements.
