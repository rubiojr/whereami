# geometry

Toolkit-neutral geometry preparation extracted from vecmap. It reuses the existing
Go Earcut implementation, ring cleanup, complete line tessellation (cleanup,
offsets, dashing, segment extrusion, caps and joins), and glyph/icon quad
arithmetic. It builds without Qt or cgo and emits
data for the shared `scene` contract.

```go
budget := geometry.Budget{Remaining: 50_000_000}
mesh, err := geometry.TriangulatePolygon(exterior, holes, &budget)
// mesh.Vertices contains float64 tile-local points.
// mesh.Indices contains uint32 triangle indices, in Earcut order.
```

Input coordinates must be finite tile-local values, as validated by the MVT
decoder. Input rings are neither modified nor retained. Results own their slices;
treat them as immutable after publication. Share a budget across polygons in one
preparation job, including unsuccessful calls: spent work remains charged.

## Lines

```go
mesh, err := geometry.TessellateLines(paths, geometry.LineStyle{
    Width: 2, Offset: 1,
    Dashes: []float64{2, 1}, Cap: "round", Join: "miter",
}, 100_000, true) // maximum triangles; indexed output
```

Paint is already evaluated into tile-local units; dash lengths are multiples of
width. This reuses the previous compiler's behavior, including its eight-section
round approximation, a miter bound of four half-widths, an offset bound of four
times the requested offset, and butt/miter fallback for unknown cap/join strings.
It does not introduce shader-driven styling.

The caller's triangle limit includes segment quads, caps and joins, and cannot
exceed `MaxLineTriangles` (2,000,000). Dash work retains the 100,000-segment ceiling
per path and the original iteration budget. Failure returns an empty mesh rather
than partially prepared geometry. Allocation estimates saturate before integer
multiplication; headless tests also run on 32-bit Go.

`TessellateLines` neither modifies nor retains input paths/dashes. `OffsetLine`
borrows the input for zero offset and returns an owned copy otherwise. Vecmap's
paint adapter uses its existing `roadPoint` alias, so no point-slice conversion
is needed. The line engine and its regression tests are tracked by kata **1fy1**.

### Width-independent extrusion

`TessellateExtrudedLines` emits the triangles of `TessellateLines`, in the same
order, for lines without dashes or offsets, but leaves extrusion to the consumer.
Each `ExtrudedVertex` has a centerline `Anchor` and a `Direction`; the rendered
position is `Anchor + halfWidth*Direction`. Directions are unit normals on segment
edges and cap/join rings, zero at disk centers and rejected miters, a normal plus
or minus the tangent on square caps, and miter vectors of at most four half
widths. Every one is independent of the width, including the miter limit, so one
mesh serves every evaluated line width. Tests prove equality with the baked
geometry within `1e-9` relative tolerance for every cap and join, four widths from
1/32 to 40, expanded and indexed output, and 200 random path sets. The existing
arithmetic is untouched for the legacy renderer and pinned fixtures.

### Width-independent dashes

Dash lengths are multiples of the line width, so `TessellateLines` bakes them and
a width change replaces every vertex. `TessellateDashedLines` emits one butt-capped
quad per path segment instead. Each `DashedVertex` has an `Anchor`, a unit normal
`Direction` and the `Distance` of its anchor along the path in tile units; both
vertices across the line share the distance, so its interpolation over a quad is
the centerline distance of a fragment. Distances restart at every path and skip
segments no longer than `Epsilon`, as the baked walk does. There are no joins,
as in the baked form.

`NewDashPattern` turns an evaluated dash array into a `DashPattern` of at most
four entries and reports whether the consumer can reproduce the baked output.
It refuses arrays of more than four entries once an odd array is repeated,
negative or nonfinite entries, entries the baked walk would skip as shorter than
`Epsilon`, values float32 cannot carry, and a first dash of zero. Round and square
dash caps and path offsets are the caller's to exclude. `DashPattern.Covers` is
the reference for the per-fragment test: half-open dash intervals from the first
point of the path.

Tests prove the equivalence for eight patterns, four widths from 1/32 to 40, six
paths including duplicate points, a closed ring and sub-`Epsilon` segments, and
200 random paths: every baked dash lies on one segment quad with the same lateral
extent, and 512 sampled distances per segment are covered exactly where a baked
dash is. The mesh is identical for every width and pattern. One difference is
deliberate: the baked walk fails beyond 100,000 dashes per path, and the mesh has
no such limit.

## Fixture integration

`TriangulatePolygonExpanded` serves existing triangle-list consumers. It expands
Earcut's native indices directly, avoiding an intermediate uint32 buffer. The
parent vecmap package uses this compatibility path and aliases `roadPoint` to
`geometry.Point`, so no point-slice conversion is required.

Direct indexed compilation is tracked by kata **tgde**.
The fixture's `-direct-indexed` path now retains polygon indices through feature
storage and fill batching, builds indexed line quads/disks, and emits indexed
glyph/icon quads. `Builder[T]` appends known topology without hashing or changing
triangle order. It validates local indices and element limits before each append,
rebases indices, and can also produce expanded output for legacy comparisons.

Construction shares vertices within each polygon, segment, disk or quad. It does
not deduplicate across features, style layers, or glyph halo/fill passes. Direct
output can therefore be larger than `scene.IndexMesh` output while costing less
to prepare. A triangle that has no construction-level sharing remains three
vertices plus three indices inside the indexed scene.

The default compiler/fixture remains expanded; `-indexed` still invokes the
optional `scene.IndexMesh` pass. These comparison modes are mutually exclusive
with `-direct-indexed`. The fixture uses styled-only decoding and compiles once
at zoom 10. It skips unused legacy road/fill buckets and source-zoom compilation.
The existing production decoder still prepares its fallback buckets and compiles
at source zoom. Styled feature limits remain enforced independently of the
fallback-only limits. Migrating the live scheduler is a separate step.

The fixture CLI uses headless `fixture.CompileWithGlyphLoader` to load the
requested font ranges after tile/style preparation, before layout and scene
packing. It no longer compiles a throwaway scene for font discovery. The loader
uses only Go values; file/cache access stays with its caller. The command builds
with `CGO_ENABLED=0`; parent fixture APIs are compatibility wrappers.

```sh
CGO_ENABLED=0 go test -cover ./pkg/vecmap/geometry
CGO_ENABLED=0 GOAMD64=v4 go test ./pkg/vecmap/geometry \
  -run '^$' -bench BenchmarkPolygonPreparation -benchmem -count 3
```

The benchmark includes polygon triangulation and float32 scene packing in all
three modes, plus `IndexMesh` in the post-indexed mode. It does not measure the
full tile compiler or rendering. See `docs/vecmap-rhi.md` for measured results.

Run the full-scene bitwise comparison and preparation benchmark with local assets:

```sh
export WHEREAMI_VECTOR_TILE_FIXTURE=/path/to/openfreemap-20260823-z9-250-193.pbf
export WHEREAMI_VECTOR_GLYPH_FIXTURE_DIR=/path/to/glyphs
GOAMD64=v4 go test ./pkg/vecmap -run 'TestDirect(RenderFixture|LineGeometry)'
GOAMD64=v4 go test ./pkg/vecmap -run '^$' \
  -bench '^BenchmarkCompileRenderFixture$' -benchmem -benchtime=2s -count 3
```
