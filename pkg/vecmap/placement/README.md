# placement

Toolkit-neutral symbol-candidate preparation, projected boxes and collision selection extracted
from vecmap. It reuses
existing text/token handling, style defaults, feature-anchor selection, line
interpolation, angle and polygon-centroid algorithms, using shared MVT/style/Go
geometry values. It builds without Qt or cgo and introduces no production dependency.

```go
anchors := placement.FeatureAnchors(feature, "line", tileLocalSpacing)
for _, anchor := range anchors {
    // anchor.Point is tile-local; Angle and RawAngle are radians.
    // The caller attaches evaluated text/icon paint and performs collision.
}
```

## Selection and compatibility

- `line` distributes repeated anchors across each line; `line-center` selects
  one midpoint per usable line. Other placement strings choose all points, then
  exterior-ring centroids, falling back to line midpoints only if neither points
  nor polygons produced anchors. Source order is preserved.
- `RepeatedLineAnchors` uses at most **16 anchors per line**, evenly spaced at
  `(i + 0.5) / count`. Nonpositive spacing or a line shorter than the spacing
  selects its midpoint. The count is now clamped before float-to-int conversion,
  so tiny positive spacing cannot overflow on amd64 or 386 and incorrectly
  collapse the count to one.
- `LineAnchor` clamps fractions to 0–1. At a vertex, it uses the incoming segment.
  Length accumulation includes tiny segments, but search skips segments at or
  below `geometry.Epsilon`, preserving the existing unreachable-target behavior.
  Degenerate/nonfinite line lengths and NaN fractions/spacing produce no anchors.
- `RawAngle` is the source segment direction. `Angle` adds pi when needed for
  upright text. It retains the original unnormalized result: a westbound segment
  has raw angle pi and upright angle 2*pi. Text/icon keep-upright policy is external.
- `PolygonCentroid` uses only the exterior ring. It preserves the original
  near-zero-area fallback, including adding point coordinates to the accumulated
  numerator before averaging. This is not an interior-point algorithm and may
  return a point outside a concave or self-intersecting polygon. Empty rings retain
  the origin result and still count as polygon anchors.

## Ownership and bounds

Input features/rings are finite, bounded tile-local source geometry, normally
produced by `mvt.DecodeTile`. Inputs are neither modified nor retained. Returned
anchor slices are owned; the parent `libertySymbolAnchor` alias avoids conversion
slices. Treat published data as immutable.

The 16-anchor limit applies per line, not per feature or tile. Total output/work
follows source point/ring/line cardinality; caller-owned tile candidate budgets
remain in force. The standalone line/ring helpers do not impose an input point
limit. Schedule preparation away from rendering callbacks.

## Evaluated symbol candidates

```go
err := placement.PrepareSymbols(features, layer, placement.SymbolOptions{
    SourceZoom: tileZoom, Zoom: styleZoom,
    Limit: placement.MaxSymbols - len(symbols),
}, func(candidate placement.Symbol) error {
    symbols = append(symbols, candidate)
    return nil
})
```

The caller selects visible symbol layers and the matching source-layer features.
`PrepareSymbols` applies filters, expands text/icon tokens, transforms and normalizes
text, selects fonts and anchors, and evaluates per-anchor text/icon paint. It emits
`Symbol` values in feature/anchor order through a synchronous sink. A Symbol has
logical font names, geometry, colors and paint values, with no native resources or
property maps. String payloads may borrow immutable input data.

Pass remaining capacity across all layers of a tile. `Limit` is capped at the
existing **10,000** candidates. Reaching the limit returns `ErrSymbolLimit` only
when another candidate would be emitted. Sink errors stop preparation and are
returned unchanged. Earlier sink calls remain accepted on error, preserving
legacy partial-budget behavior. A nil sink is rejected. Layer type, zoom visibility
and hidden-state selection remain caller policy; this API does not repeat them.

Text preparation retains the existing **4096-byte/256-rune** ceiling, **256 token
substitutions**, missing-property empty strings, literal unmatched braces and
nonrecursive replacements. Upper/lowercase runs after substitution; CR/LF and
Unicode whitespace normalization then runs before the final bound check. Icons
receive token expansion without text transforms. `ExpandTokens`, `BoundedText` and
`NormalizeText` expose these operations; normalization alone has caller-bounded
input. Properties normally contain MVT primitives. Arbitrary style/property object
formatting is not an untrusted-input work budget.

Paint/default behavior is reused through typed `style.CompiledLayer` methods.
Scalar numbers require finiteness; point-array components retain the previous
looser semantics. Invalid present colors remain parse failures, distinct from
missing-property defaults. Font stacks retain trimmed order and duplicates, with
Noto Sans Regular fallback. Spacing converts style pixels to source-tile units;
rotation, keep-upright, alignment, padding and overlap/optional flags retain their
previous values. These are evaluated candidates, not collision acceptance results.

Vecmap's legacy renderer uses a single value adapter at the sink. It allocates no
intermediate candidate slice and copies no maps/string payloads. Other consumers
can retain `placement.Symbol` directly. SDF eligibility and atlas-dependent quads
live in `glyph`; sprite decoding and pixel preparation live in `sprite`. Qt fallback,
live asset loading/cache policy remain in vecmap. Headless `fixture` owns offline
scene orchestration and uses `liberty` for pinned sprites.

## Projected text and icon boxes

```go
projected := placement.ProjectSymbol(&candidate, placement.ProjectionContext{
    Transform: tileTransform, Width: viewportWidth, Height: viewportHeight,
    TextReady: textReady, TextBounds: textBounds,
    Sprite: spriteMetrics,
})
// Use projected.Text and projected.Icon in a CollisionReference.
```

`ProjectSymbol` takes the shared `view.Affine`, an evaluated `Symbol`, explicit text
readiness and optional metric snapshots. It returns `CollisionPart` values,
including policy flags, presence, projected boxes and viewport visibility. It
retains no pointers or string data and performs no loading, shaping or native work.

- Text is present when nonempty with positive color alpha. `TextReady` is explicit
  caller policy: a bounds pointer alone does not imply readiness. Ready text with
  bounds uses them directly in logical pixels, including their existing halo.
  Ready text without bounds uses the previous fallback character/line estimate.
- Sprite readiness comes from an optional `SpriteMetrics` value (pixel dimensions
  and pixel ratio). Missing metrics or nonpositive/NaN pixel ratios make the icon
  absent. Positive ratios retain the original dimension arithmetic, including zero
  dimensions. Icon collision presence still ignores icon opacity/color alpha.
- Anchors follow the map transform, while text/icon sizes and offsets remain
  logical-pixel values. Map-aligned angles use the affine direction transform;
  viewport alignment ignores the line direction and map rotation. Rotation uses
  all four box corners before applying signed padding. Edge visibility is inclusive.
- `RenderedSymbolAngle`, `ScreenSymbolAngle`, `AnchoredOrigin`, `RotatedBox` and
  `Box.Visible` expose the same pure math for adapters. Anchor substring rules and
  signed-zero center/edge arithmetic remain unchanged.

The fallback estimate now keeps its wrapped line count in floating point instead
of converting an arbitrarily large ratio to `int`. This fixes architecture-dependent
overflow for tiny positive maximum widths without changing normal estimates.
It remains an estimate, not OpenType shaping or a text-layout validator. Inputs
are bounded/evaluated symbols and caller-owned transform/metric snapshots;
`SelectSymbols` supplies finite visible-box and work validation. The projector
preserves the existing raw arithmetic for unusual paint/metric values.

Vecmap's adapter resolves Qt text eligibility, available SDF bounds and pinned
sprite metadata, then passes value snapshots to the headless projector. No native
objects, resource loaders or cache handles enter the shared API.

## Collision and priority selection

```go
accepted, err := placement.SelectSymbols(references, placement.CollisionOptions{
    Width: viewportWidth, Height: viewportHeight,
})
// references is consumed as scratch and sorted in place.
// accepted maps each caller-owned key to independent Text/Icon decisions.
```

`CollisionReference[K]` takes an opaque comparable logical key, layer order, sort
key, and `CollisionPart` values for text/icon. Each part carries its projected
logical-pixel `Box`, presence, visibility, overlap and optional flags. Keys should
be unique/stable. The caller supplies projected parts, normally through
`ProjectSymbol`, and owns glyph/sprite readiness.

Selection order is descending layer order, then ascending sort key. Ties retain
input order. Vecmap retains its original tile/wrap/reverse-candidate collection
order and 100,000-reference collection cap. The selector reads that slice without
reordering it and returns an owned acceptance map; parent aliases share the map
without conversion. It orders compact ranks (order, key, index) rather than the
references: a counting sort by layer order, which keeps input order within a
layer, and a comparison sort only within layers whose sort keys vary. Orders
spread wider than the reference count plus 256 take one comparison sort instead.
Tiles already collect references in descending layer order, which a comparison
sort gains little from: on 12 such tiles of 330 references, selection takes
0.45 ms against 1.2 ms with the earlier reflection-based stable sort.

The 64-pixel grid, clipped/floored inclusive cell ranges and strict rectangle
intersections are preserved. Touching edges do not collide. Overlap-enabled parts
neither query nor occupy cells. A candidate's text/icon are queried against earlier
accepted candidates, not against each other. Required text that is unavailable or
colliding suppresses its icon; a colliding required icon suppresses text. An
unavailable/offscreen icon does **not** suppress text. This asymmetry matches the
previous renderer. Inverted rectangle ordering is not normalized.

The headless boundary adds explicit limits:

- At most **100,000 references**, also bounding validation and stable-sort work.
- Finite, nonnegative viewport dimensions, at most **1,048,576 logical pixels**
  each, keeping cell indexes safe on 386 as well as amd64.
- Finite sort keys and visible-part rectangles. Invisible parts' unused boxes
  need not be finite.
- At most **1,000,000 combined cell visits and occupied-box comparisons** per job.
  `WorkLimit` can lower this cap; zero selects the default. Cell insertion visits
  are counted too, bounding stored box copies. Sorting and caller projection are
  separately bounded by reference/source cardinality, not charged to this counter.

Invalid input returns `ErrCollisionInput`; resource exhaustion returns
`ErrCollisionLimit`. Both return **nil acceptance**, even after earlier references
were processed. Scratch may already be sorted. This atomic collision-job policy
is distinct from `PrepareSymbols`' streaming partial-result policy. Vecmap reports
the failure and skips symbol acceptance for that job. No global cache, logger,
native graphics resource or asynchronous work is owned by the selector.

## Verification

```sh
CGO_ENABLED=0 go test -cover ./pkg/vecmap/placement
CGO_ENABLED=0 GOARCH=386 go test ./pkg/vecmap/placement
CGO_ENABLED=0 go test ./pkg/vecmap/placement -run '^$' \
  -fuzz '^FuzzLineAnchors$' -fuzztime=20s
CGO_ENABLED=0 go test ./pkg/vecmap/placement -run '^$' \
  -fuzz '^FuzzSymbolText$' -fuzztime=20s
CGO_ENABLED=0 go test ./pkg/vecmap/placement -run '^$' \
  -fuzz '^FuzzCollisionSelection$' -fuzztime=20s
CGO_ENABLED=0 go test ./pkg/vecmap/placement -run '^$' \
  -fuzz '^FuzzProjectSymbol$' -fuzztime=20s
```

Tests cover source-order/fallback selection, ownership, arc-length interpolation,
vertex angles, clamped fractions, reversal/upright behavior, repeated/degenerate
points, tiny spacing, nonfinite inputs, winding and legacy centroid fallbacks.
Candidate tests also cover complete default/custom text/icon paint, filters,
text/token rules, font fallback, zoom spacing, caller-owned visibility, budgets,
sink errors and partial output. Collision tests cover stable ties, optional/overlap
rules, clipping/cell edges, an independent brute-force text-selection comparison,
invalid inputs, bounds and atomic work exhaustion. Projection tests cover readiness,
glyph/fallback bounds, pixel-ratio/offset arithmetic, map/viewport alignment,
ownership, signed padding/zeros and 32-bit fallback estimates. Headless coverage
is **100%**. See kata **zxys**, **0yzm**, **h3nq**, **madt** and
[`docs/vecmap-rhi.md`](../../../docs/vecmap-rhi.md) for full-capture equality and
controlled preparation measurements.
