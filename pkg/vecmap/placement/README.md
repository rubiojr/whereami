# placement

Toolkit-neutral feature-anchor preparation extracted from vecmap. It reuses the
existing selection, line interpolation, angle and polygon-centroid algorithms,
using shared `mvt.Feature` and `geometry.Point` values. It builds without Qt or cgo
and introduces no production dependency.

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

This package currently prepares anchor candidates. Evaluated symbol paint/text,
text normalization/eligibility, collision/priority/optional-symbol decisions,
atlas-dependent quads and scene compilation remain in vecmap.

## Verification

```sh
CGO_ENABLED=0 go test -cover ./pkg/vecmap/placement
CGO_ENABLED=0 GOARCH=386 go test ./pkg/vecmap/placement
CGO_ENABLED=0 go test ./pkg/vecmap/placement -run '^$' \
  -fuzz '^FuzzLineAnchors$' -fuzztime=20s
```

Tests cover source-order/fallback selection, ownership, arc-length interpolation,
vertex angles, clamped fractions, reversal/upright behavior, repeated/degenerate
points, tiny spacing, nonfinite inputs, winding and legacy centroid fallbacks.
Headless coverage is **100%**. See kata **zxys** and
[`docs/vecmap-rhi.md`](../../../docs/vecmap-rhi.md) for full-capture equality and
controlled preparation measurements.
