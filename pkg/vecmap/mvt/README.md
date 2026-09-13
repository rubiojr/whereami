# mvt

Toolkit-neutral MVT tile preparation, extracted from vecmap's existing decoder.
This package builds without Qt or cgo and adds no production dependency. It reuses
the existing protobuf parser, geometry-command decoding, ring grouping and Go
Earcut implementation.

```go
tile, err := mvt.DecodeTile(data, true) // preserve Earcut indices
if err != nil {
    return err
}
for _, feature := range tile.Layers["transportation"] {
    // feature.Properties, Points, Lines and Polygons are Go-owned source data.
}
```

`DecodeTile` validates PBF framing, decodes properties and source geometry, groups
polygon rings, and triangulates within shared tile budgets. It preserves feature
order and appends duplicate source-layer names in input order. It performs no I/O,
style evaluation, logging, or toolkit work. The bool selects direct indexed versus
expanded polygon output, with the same triangle order and source rings.

## Ownership and failure policy

`Tile`, `Feature`, `Properties` and `Polygon` are reusable source data. Completed
tiles do not retain input PBF buffers; treat all reachable slices/maps as immutable
after publication. Polygon `Vertices` is an expanded triangle list when `Indices`
is nil, or indexed positions otherwise. Features also retain original rings for
outlines and placement.

Structural tile/layer errors fail atomically. Malformed individual features are
skipped, preserving earlier accepted features. Resource-limited features also
produce `LayerLimits` summaries, carrying the layer name, skip count and last
original feature index/error. The caller chooses how to report degradation.
Rejected features do not charge accepted-output counts, but triangulation work
already spent stays charged.

For callers such as vecmap's production fallback adapter, `DecodeLayers` and a
`Decoder` created by `NewDecoder(indexed)` expose the same preparation in stages.
Call `decoder.DecodeLayer(layer)` with one decoder per tile so budgets span every
layer. Parsed layers borrow their feature
payloads from the input; `FeatureData` borrows both outer and inner slices. Keep
these bytes unchanged during preparation. Metadata strings and prepared features
are owned. `DecodeFeature` owns its raw tag/command arrays.

## Bounds

- Raw tile/feature data: **2 MiB**, enforced at the parser boundary as well as in
  vecmap's existing I/O path; exhaustion wraps `ErrTileResourceLimit`.
- Accepted features per tile: **100,000**.
- Source points per point/line feature and accepted points across the whole tile:
  **1,000,000**.
- Polygon ring points per feature, including holes: **32,768**.
- Accepted styled triangles per tile: **500,000**.
- Shared polygon preparation work: **50,000,000** conservative operations.

Feature/aggregate output exhaustion wraps `ErrFeatureResourceLimit`; polygon
point/work exhaustion wraps `geometry.ErrResourceLimit`. Preparation capacity is
capped at the remaining feature slots before allocation. Protobuf field numbers
are checked against the 29-bit wire-format bound before converting to `int`, so
oversized keys cannot alias known fields on 32-bit targets. Packed values, lengths,
wire types, extents and tag indexes retain the existing validation.

## Geometry commands

`DecodePoints`, `DecodeLineStrings` and `DecodePolygonRings` accept decoded uint32
commands and a layer's nonzero extent, returning `geometry.Point` positions in
256-unit tile-local coordinates. Buffered coordinates outside the nominal tile
bounds are retained. Inputs are neither modified nor retained; results own their
slices, and failures return nil.

The decoders preserve int64 delta accumulation across paths/rings, repeated
points, winding and arithmetic. ClosePath does not append a point or reset the
delta cursor. Empty streams retain the original nil-result behavior; the layer
parser rejects zero extent even without geometry. `GroupRings` borrows its input
rings and only groups winding-ordered exteriors and holes.

Vecmap uses aliases of the shared feature/property/polygon types, avoiding
conversion slices/maps. Its legacy fallback work and reporting remain interleaved
with layer preparation as before. Document/layer preparation and expression
evaluation now live in the headless `style` package. The `glyph` package owns
text layout and atlas preparation; `placement` owns anchors and evaluated symbol
candidates, projected boxes and collision/priority decisions. Resource readiness
policy and scene compilation remain in the Qt-bound parent package. The offline
fixture command is therefore still Qt-bound.

## Verification

Tracked by kata **fc0r** (geometry commands) and **ydc3** (tile preparation).
See [`docs/vecmap-rhi.md`](../../../docs/vecmap-rhi.md) for full-capture equality and
the controlled before/after preparation measurement.

```sh
CGO_ENABLED=0 go test -cover ./pkg/vecmap/mvt ./pkg/vecmap/internal/pbf
CGO_ENABLED=0 GOARCH=386 go test ./pkg/vecmap/mvt ./pkg/vecmap/internal/pbf
WHEREAMI_VECTOR_TILE_FIXTURE=/path/to/pinned.pbf \
  CGO_ENABLED=0 go test ./pkg/vecmap/mvt -run TestDecodePinnedTileHeadless
CGO_ENABLED=0 go test ./pkg/vecmap/mvt -run '^$' \
  -fuzz '^FuzzDecodeTile$' -fuzztime=20s
CGO_ENABLED=0 GOAMD64=v4 go test ./pkg/vecmap/mvt \
  -run '^$' -bench '^BenchmarkDecodeGeometry$' -benchmem -count 3
```

Tests cover typed values, packed/unpacked fields, explicit zero IDs, malformed
framing/tags/commands, input ownership, duplicate layers, cross-layer budgets,
spent-work retention and bit-identical indexed reconstruction. The synthetic
geometry benchmark isolates command decoding for 1024 repeated points; it excludes
protobuf parsing, style preparation, tessellation and rendering.
