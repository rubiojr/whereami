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

## Tile assembly

`CompileTile(layers, sources, options, emit, symbols)` traverses visible layers in
document slice order. It reuses the layer compilers, prepares backgrounds and emits
shared `Primitive` values carrying layer order/identity, owned meshes and evaluated
solid/pattern paint. It applies the original half-open zoom visibility and literal
hidden-state rules. Unsupported layer kinds are skipped. Layer order values are
preserved without sorting; callers normally supply the ordered style compiler output.

The required primitive sink runs synchronously. An optional symbol-layer callback
runs at each visible symbol layer's original position. That callback owns symbol
candidate preparation, its cross-layer limit and storage; it can use
`placement.PrepareSymbols` directly. A nil callback skips symbols. Native fallback,
asset readiness and final scene packing remain outside the compiler.

`TriangleLimit` now applies to **aggregate emitted tile geometry** as well as each
layer batch. Zero retains the 2,000,000-triangle default; callers may lower it.
Indexed meshes count index elements; expanded meshes count vertex elements. Empty
meshes, zero-alpha solids and missing-name/nonpositive-opacity patterns are filtered
before triangle validation/accounting. Symbol geometry is outside this budget.
This bounds emitted geometry, not all temporary batching or expression work;
the source/style input and scheduling contracts above still apply.

Earlier callback effects remain accepted after an error. The parent stages its
legacy primitive slice and publishes it only after success, retaining its previous
partial-symbol behavior on failure. A single value adapter shares mesh buffers and
name strings; there is no intermediate primitive conversion slice. Other consumers
can retain `Primitive` directly. Inputs must remain immutable throughout the call,
including inside callbacks. Sinks may retain emitted owned meshes.

`BackgroundGeometry(indexed)` returns an owned 256-unit rectangle with the original
diagonal and triangle order. Both the tile compiler and parent fallback background
use it. Tile options reject invalid scale/limits even for empty/background-only
jobs. Primitive assembly checks triangle completeness and preserves the MVT
feature-resource error identity; it is not a second validation pass over already
prepared coordinates/indices.

## Font discovery and text preparation

`TextRequest` carries only text, exact font-stack identity and `glyph.LayoutOptions`.
Its `Layout` method wraps the existing metric layout in a `glyph.PreparedLayout`.
It does not select eligibility or font readiness: live callers retain those rules
and Qt fallback policy. The parent shares one request adapter between live shaping
and offline fixture preparation.

`FontStacks` consumes `iter.Seq2[K, TextRequest]` and returns sorted, unique font
identities for every nonempty text. It preserves empty/untrimmed names and includes
unsupported scripts, matching the previous discovery pass. Nil input yields an
empty slice. Request count, iterator work and identity sizes are caller-bounded.
Iterators run synchronously and must honor early stop; no intermediate candidate
or request slice is required.

`DecodeFontRanges` decodes one supplied PBF range per exact font identity, with a
common caller-provided range start (the fixture uses zero). It reuses
`glyph.DecodeRange`, preserving per-response byte/metric limits and owns decoded
maps/bitmaps. Errors include the font identity and return nil output. Map iteration
order and aggregate font count/input bytes are caller-owned; this is not a new
network ingestion policy. Empty input returns an owned empty map.

`PrepareTextLayouts` consumes keyed requests and immutable decoded font maps. It
returns an owned layout map plus sorted, unique missing-font identities under the
fixture's **SDF-only** policy:

- Empty text is skipped. Unsupported scripts, invalid/oversized text and missing
  required glyph keys record the font as missing; no native fallback is attempted.
- `TextComplete` checks the original text before whitespace processing. CR/LF need
  no glyph; other whitespace does, even if layout will later trim it. Coverage
  checks keys and SDF eligibility, not metrics, shaping or atlas rectangles.
- Complete glyph coverage with invalid layout options omits the label without
  reporting a missing font. This preserves the previous distinction.
- At most **10,000 yielded requests**, including empty/duplicate requests, are
  allowed. Overflow stops iteration and returns nil layouts/missing list with
  `geometry.ErrGeometryLimit`, even after earlier successful layouts. Work between
  iterator yields remains caller-owned. Nil input yields empty owned results.
- Keys retain source candidate identity. Repeated keys replace the preceding
  successful layout; skipped requests do not erase it. Layout buffers are owned,
  while glyph bitmap bytes are immutable borrows from the supplied font maps.

The parent iterator yields value requests using original tile/candidate keys. The
fixture still calls its loader once, after tile compilation, then invokes these
shared helpers. Single-pass preparation and local file/cache policy are preserved.
Final scene packing uses `SceneBuilder`; headless `fixture` owns overall offline
orchestration and uses shared candidates directly.

All new text helpers have **100% coverage**, including exact identity/discovery,
original-text coverage, missing-font versus invalid-layout results, duplicate keys,
early iterator stop, limits, range-set errors and ownership. Optional pinned-font
tests decode Regular/Bold/Italic PBFs without Qt. Parent tests check candidate indexes,
limit propagation and the existing one-loader-call behavior. Tracked by **wxt5**.

## Scene packing

`NewSceneBuilder(indexed, maximumElements)` creates a single-owner packer for one
retained mesh plus ordered draws and textures. A zero limit selects the fixture's
existing **36,780,000-element** ceiling; a positive limit may lower it. The builder
must not be copied or used concurrently, and its zero value is invalid.

- `Geometry` converts tile-local float64 XY coordinates into scene vertices.
  Expanded output writes directly into the final buffer without a conversion
  slice; indexed output reuses `geometry.Builder` and preserves topology. Indexed
  inputs can also be expanded directly.
- `ExpandedText` accepts packed XYUV triangles for expanded output;
  `IndexedText` accepts text vertices and optional triangle indices for either
  output mode, including icon quads. Both retain the original `math.Sincos` and
  `geometry.TransformTextVertex` arithmetic. Inputs are copied into owned buffers.
- Each call appends an ordered draw with the supplied material and clip, coalescing
  only adjacent compatible draws through `scene.AppendDraw`. No sorting occurs.
  `PackedColor` preserves the original integer-to-float32 division order.
- `Texture` assigns IDs starting at one, in first-seen logical-key order, and
  borrows immutable RGBA bytes for the returned scene's lifetime. Repeated keys
  reuse the first image even if later metadata differs. Dimensions/storage are
  validated on first insertion, with overflow-safe byte arithmetic.
- `GlyphAtlas` converts bounded single-channel SDF pixels into owned RGB distance
  bytes with opaque alpha, using the reserved `glyph-atlas` key. Nil skips it;
  subsequent uses of that key reuse the original texture.

Geometry limits cover total draw elements, unique indexed vertices and each
temporary input-to-packed vertex buffer before allocation. Triangle completeness
and local indices are checked before append. Both output modes now enforce the
ceiling (the old expanded path relied on earlier compilation budgets). At most
**16,384 textures** are retained, each with dimensions at most 16,384 per axis,
matching scene validation. Texture pixels are already caller-owned; aggregate
image bytes, logical-key sizes, scheduling and repeated method-call work remain
caller-bounded. Glyph conversion is separately capped at 2048 per axis.

Methods latch their first error and skip further packing. `Finish` validates the
scene, including finite packed coordinates, material/clip values and texture
references, and returns nil on any error. Thus callers never receive a partially
valid scene. An empty mesh is invalid under the existing scene contract. Successful
Finish seals the builder; further mutations latch `ErrPackingClosed` without
changing previously published data. Finish may be repeated before any attempted
mutation. Returned geometry/draw/texture metadata is owned and immutable after
publication; general texture pixel bytes remain immutable borrows.

Mesh ID and all revisions are one; texture IDs are scene-local. Combining multiple
packed scenes or publishing incremental resource updates requires caller-owned ID
and revision remapping. This is offline retained packing, not a GPU upload queue.
Callers select accepted symbols and layer order. Material and symbol pass assembly
use the shared methods below. The headless `fixture` package now owns top-level
offline orchestration; its command builds without Qt or cgo.

Packing is **100% covered** headlessly, including topology reconstruction, owned
geometry versus borrowed images, glyph conversion, first-key texture identity,
ordered draw coalescing/clipping, input and capacity failures, latched errors,
final scene validation and publication sealing. See kata **tsww**.

## Materials and symbol passes

`SceneBuilder.Primitive(tile, wrap, primitive, lookup)` selects solid or pattern
paint, computes pattern size/phase with `view.PatternPhase` and clips geometry to
the 256-unit tile. Patterns request a white, full-opacity sprite; their evaluated
opacity stays in the material. Missing patterns are skipped. Coordinates remain
tile-local, and caller transforms provide map positioning.

`SceneBuilder.SymbolLayer(count, symbolAt, atlasID, lookup)` emits one layer's
accepted icons first, then every eligible text halo, then every accepted text fill.
This preserves the original pass ordering and prevents later halos covering prior
fills. The indexed accessor returns `RenderSymbol`, combining shared evaluated
`placement.Symbol`, collision acceptance and an optional prepared glyph layout.
It must return the same immutable sequence on each of three traversals. No
candidate slice is converted or retained. Negative counts or missing accessors
for nonempty layers latch `ErrPackingInput`; counts above **10,000** fail before
accessor calls. Zero count needs no accessor. Accessor work is caller-bounded.

Icon and text offsets retain their original size multiplication; angles follow
map/viewport alignment through `placement.RenderedSymbolAngle`. Icons use the
original anchor/quad arithmetic and texture key including name/color/opacity.
Text layouts must already have complete atlas coverage and match the builder's
expanded/indexed mode. Missing layouts or rejected text are skipped. Halo passes
require positive width and nonzero halo alpha. The returned count is accepted text
fills and is meaningful only when `Finish` succeeds. Symbols are not tile-clipped.

`SpriteLookup` is synchronous, caller-owned resource readiness/cache policy. False
means unavailable; nil means no sprites. Successful images require finite positive
pixel ratios, and texture insertion validates dimensions/storage. Pixel bytes may
be retained as immutable scene data. Builders stop accessor/packing work after
errors and preserve sealed publication. This adds no native fallback or live upload
queue. Parent adapters copy only value paint fields and share layout/image buffers.

Headless tests cover pass and texture order, labels, solid/pattern materials,
world-wrap pattern phase, pixel ratios, anchors, rotations, offsets, halo/fill
paint, missing/rejected symbols, nil accessors, limits and error propagation in
both representations. All new functions have **100% coverage**. Tracked by **jyqg**.

## Fragment packing with draw provenance

`NewFragmentBuilder(indexed, maximumElements, maximumDraws)` uses the same packing
and material/pass math as SceneBuilder, but keeps a `DrawSource` for every draw.
Zero limits choose `MaxSceneElements` and **65,536 draws**; positive limits may
lower them. Methods latch errors; Finish returns nil scene and metadata on failure.

- `Primitive(tile, primitive, lookup)` records the layer order and original
  **float64 pattern period** before material packing. Initial phase is for wrap
  zero; a compositor can recompute other wraps without recovering a rounded period.
- `SymbolLayer(first, count, symbolAt, atlas, lookup)` records the original candidate
  index (`first + local index`), layer and icon/text part. It preserves icons, then
  all halos, then all fills. Candidate ranges are bounded by 10,000.
- Each primitive/candidate pass starts a draw boundary. Identical materials never
  merge different candidates or layers. Geometry and texture packing are reused;
  this adds metadata, not a second tessellator or image conversion.
- Missing assets and empty geometry emit no orphan source records. Empty fragments
  succeed without an empty mesh or unused atlas texture. The ordinary SceneBuilder
  retains its previous empty-mesh rejection and coalescing behavior.
- Finish seals both outputs; repeated Finish is allowed until another mutation is
  attempted. Returned geometry/metadata are owned and immutable. Sprite RGBA remains
  borrowed; glyph RGBA conversion remains owned.

`tiles.Draw`/`Part` alias the compiler's `DrawSource`/`DrawPart`, so the generic tile
producer can hand these slices to the compositor directly. `SymbolTextRequests`
shares the candidate-to-layout iterator with the fixed fixture and generic producer.

The generic two-stage producer is documented in [`tiles`](../tiles/README.md).
Packing limits bound arrays and draw work, not all caller-owned assets or retained
snapshots. Store and Planner byte/resource admission is still required. The
provenance path is opt-in; the existing fixture path retains its old output.

### Resident geometry across style zooms

`LayerOptions.ExtrudeLines` is opt-in. Undashed, unoffset lines and fill outlines
are then tessellated with `geometry.TessellateExtrudedLines`: a `Primitive` carries
centerline anchors in `Mesh`, parallel unit `Directions`, and `HalfWidth` in logical
pixels. Batching, ordering, colors and topology equal the baked output; only vertex
contents differ. Dashed and offset lines, whose geometry depends on the evaluated
width or offset, keep their baked tessellation and are marked `Dynamic`. Widths at
or below `geometry.Epsilon` tile units emit nothing in either form.

`SceneBuilder.Extruded` packs anchors as tile-local XY and directions as vertex
offsets, with `Material.MapAligned` and `Material.OffsetScale` set to the half
width. `Split` (also on `FragmentBuilder`) routes `Dynamic` primitives and every
symbol pass to `DynamicMesh` (scene-local ID 2) and everything else to `StableMesh`
(ID 1). IDs keep their meaning when a mesh is empty and omitted. Draw order,
provenance, materials and counts equal the unsplit output; the element limit bounds
both meshes together. A different evaluated width changes draws, never the stable
mesh, so a retained store can keep it resident across style-zoom changes.
Tile clipping evaluates the anchor: a line is cut perpendicular to its direction
where its centerline crosses the clip rectangle, and neighbouring tiles complement
each other without overlap.

### Shader dashes

`LayerOptions.ShaderDashes` is opt-in. A dashed line with a butt cap, no offset and
a pattern `geometry.NewDashPattern` accepts becomes one primitive from
`geometry.TessellateDashedLines`: anchors in `Mesh`, `Directions`, `Distances`,
`HalfWidth` in logical pixels, `DashUnit` (the width in tile units) and `Dashes`.
It is not `Dynamic`. Batching, ordering and colors equal the baked output, and one
primitive is emitted where one was before. Round or square caps, offsets and longer
patterns keep their baked tessellation.

`SceneBuilder.Dashed` packs it like `Extruded`, with the distance in vertex `U`,
and makes the material `scene.Dashed` with `DashUnit` and `Dashes`. With `Split`
the geometry goes to `StableMesh`. Another evaluated width changes `OffsetScale`
and `DashUnit` on the draw and nothing in the mesh. The triangle limit now counts
two triangles per path segment, not two per dash.

### Resident symbols across style zooms

Icon and glyph quads are proportional to the evaluated icon or text size: the
sprite size, the anchored origin, glyph positions and `icon-offset`/`text-offset`
(icon-size multiples and ems) all scale with it. `SceneBuilder.ResidentSymbols`
(also on `FragmentBuilder`) therefore packs icons at size one with `icon-size` in
`Material.OffsetScale`, and packs text whose `glyph.LayoutMesh` is `Unit` at 24
pixels per em with the layout's scale in `OffsetScale`. Every symbol pass goes to
`SymbolMesh` (scene-local ID 3). Anchors, rotation, atlas coordinates, draw order,
provenance, `FontScale` and halo paint equal the baked output, and scaled offsets
equal baked offsets within float32 rounding.

Symbols get their own mesh because their layout does change: texts, anchors along
lines, em-relative spacing and the glyph atlas follow the style zoom at some steps,
and that must not replace the much larger stable mesh. The option is independent of
`Split`; without it symbols stay in `DynamicMesh` or mesh one. An icon size that
float32 cannot carry as a positive finite factor (zero, negative, underflow) keeps
its baked quad with a zero scale. A `Unit` layout with such a scale is
`ErrPackingInput`; `glyph.PrepareUnitLayouts` never produces one. The element limit
bounds the three meshes together.

Checkpoint **re90** keeps compiler coverage at **100%**, including provenance,
pass order, boundary-preserving coalescing, precise periods, empty output, draw and
element limits, failure atomicity and sealed publication.

## Verification

```sh
CGO_ENABLED=0 go test -cover ./pkg/vecmap/compiler
WHEREAMI_VECTOR_TILE_FIXTURE=/path/to/pinned.pbf \
  CGO_ENABLED=0 GOARCH=386 go test ./pkg/vecmap/compiler
CGO_ENABLED=0 go test ./pkg/vecmap/compiler -run '^$' \
  -fuzz '^FuzzLayerCompilation$' -fuzztime=20s
CGO_ENABLED=0 go test ./pkg/vecmap/compiler -run '^$' \
  -fuzz '^FuzzScenePacking$' -fuzztime=20s
```

Coverage is **100%**. Tests cover paint/defaults, filters, solid/pattern/outline
ordering, extrusion behavior, gaps and scale, ring holes, dashes/key precision,
ownership, lowered limits, nil sinks and streaming failures. Optional pinned-tile
tests compare every expanded/indexed coordinate bit at zooms 10 and 10.5.
See kata **zfjf** and [`docs/vecmap-rhi.md`](../../../docs/vecmap-rhi.md) for full-scene
byte equality and controlled preparation-cost measurements.

Tile assembly (kata **fzr1**) also has 100% coverage: mixed-layer callback ordering,
visibility, background paint/topology/ownership, aggregate limits across solid and
pattern layers in both modes, filtering, incomplete triangles and streaming sink
errors. The parent regression verifies that failed geometry cannot publish an
earlier background or stale retained primitives.
