# style

Toolkit-neutral document/layer preparation, expression and color evaluation
extracted from vecmap's existing Liberty compiler. It uses only the Go standard
library. The parent renderer
uses thin adapters and aliases `mapColor` to `style.Color`; colors and feature
properties pass through without conversion allocations.

```go
value, ok := style.Evaluate(expression, style.Context{
    Zoom: 12,
    GeometryType: feature.GeometryType,
    Properties: feature.Properties,
})
```

## Document and layer preparation

```go
layers, err := style.Parse(styleJSON)
// Handle err before using layers.
for _, layer := range layers {
    if !layer.VisibleAt(context.Zoom) || layer.Hidden() || !layer.Matches(context) {
        continue
    }
    value, ok := layer.Value("line-width", context)
    // Apply the caller's paint defaults and geometry preparation.
    _, _ = value, ok
}
```

`Parse` decodes the supported version-8 document subset and compiles layers in
original order. `Compile(Document)` supports callers with an already-decoded
document. `Document`, `Layer` and `CompiledLayer` use only Go values. Unsupported
layer types and duplicate IDs retain their original order; unknown JSON fields
are ignored. This does not add support for further MapLibre style features.

- Zoom ranges are half-open, with defaults of zero and positive infinity.
  `VisibleAt` checks only zoom; `Hidden` separately checks literal layout
  `"visibility": "none"`, without evaluating an expression.
- Missing filters match. Failed or falsey filters do not.
- `Value` evaluates paint before layout. Present paint shadows layout even when
  it is null or fails evaluation. Missing values return `(nil, false)`.
- Compilation errors return nil layers, including errors after earlier valid
  layers. Errors retain the layer/section/property context and wrap JSON errors.
- `Parse` does not retain its byte buffer. `Compile` borrows filter expressions,
  owns decoded paint/layout maps and expressions, and copies zoom values. Keep
  published layers and all reachable expression data immutable.

The pinned asset and `sync.Once` cache stay caller-owned in vecmap. Its compiled
layer alias shares the output slice/maps without conversions. The package owns
no filesystem/network access, scheduling, global style cache or input-size policy.

## Supported behavior

The existing subset supports `get`, `has`, `zoom`, `geometry-type`, `!`, `all`,
`==`, `!=`, `<`, `<=`, `>`, `>=`, `match`, `case`, `coalesce`, `concat`, `to-string`,
`interpolate` and `step`. Interpolation retains linear/exponential arithmetic,
numeric mixing, rounded RGBA mixing and the original fallback selection.
Geometry codes match MVT: 1=Point, 2=LineString, 3=Polygon.

This is the current Liberty subset, not a complete MapLibre style implementation.
Compatibility details matter:

- Missing `get` returns `(nil, true)`; `has` distinguishes absence.
- `coalesce` accepts an empty string; `concat` skips failed operands.
- All-string arrays can be font literals even when their first string is not a
  supported operator. Unknown mixed arrays fail. Existing permissive arity and
  fallback behavior is retained, including literal `[]any{"case"}`.
- Numeric comparison accepts the existing Go numeric types and `json.Number`;
  numeric strings are not coerced. `Truthy` retains the original type-specific
  rules, including only treating float64 zero as false among numeric values.
- Arrays/maps have no scalar equality and are unequal. This also avoids the
  original panic when malformed expressions compared non-comparable Go values.

`Number`, `String`, `Truthy`, `ParseColor` and `ColorWithOpacity` expose the same
primitive evaluation operations for layer compilers. Color parsing supports the
existing named colors (transparent/black/white), short/long hex, RGB(A) and HSL(A)
forms. It retains permissive channel ranges and rounding; opacity is clamped to
0–1. A pre-existing bug in four/eight-digit hex parsing is fixed: the blue byte is
now distinct from alpha (`#01020304` becomes RGBA 1,2,3,4).

## Ownership and bounds

Expressions are application-owned JSON-style values, normally from the pinned
style document. Context and its property map are borrowed for synchronous
evaluation. The interpreter does not mutate or retain inputs, but returned
literal/property arrays and maps can alias them. Keep published inputs and results
immutable; no deep copies or global caches are introduced.

`MaxExpressionDepth` remains 64. It bounds recursive descent, not total work,
expression width or output-string size. Operator-specific short-circuit/fallback
semantics can absorb an operand failure. This API is not an untrusted style-file
ingestion policy; its caller owns style size/validation and scheduling.

Glyph decoding, atlas packing and text layout now live in the headless `glyph`
package. Placement and scene compilation are still parent-bound.
No renderer/scheduler migration is involved.

## Verification and performance

```sh
CGO_ENABLED=0 go test -cover ./pkg/vecmap/style
CGO_ENABLED=0 GOARCH=386 go test ./pkg/vecmap/style
CGO_ENABLED=0 go test ./pkg/vecmap/style -run '^$' \
  -fuzz '^FuzzEvaluateJSON$' -fuzztime=20s
CGO_ENABLED=0 GOAMD64=v4 go test ./pkg/vecmap/style -run '^$' \
  -bench '^BenchmarkEvaluateFilter$' -benchmem -count 3
```

Tests cover the operator subset, depth boundary/cycles, borrowed literals,
interpolation/step boundaries, typed conversions, malformed composite comparisons,
color syntax, blue/alpha distinction and opacity. Document tests cover the pinned
111-layer asset headlessly, order/duplicates, zoom boundaries, value precedence,
visibility, ownership and atomic failures. Headless statement coverage is 100%.
Parent tests and all three fixed-scene captures retain their output.

Keep `Color` on the scalar-comparability fast path: Go 1.27's reflective struct
field iterator introduced roughly 4,400 tiny allocations per full fixture during
development. The fast path removes those allocations while the reflection fallback
still handles unusual composite values safely. See kata **ddp2** and
[`docs/vecmap-rhi.md`](../../../docs/vecmap-rhi.md) for the measured comparisons and
retained variable timing samples.
