# glyph

Toolkit-neutral SDF glyph-range decoding, atlas preparation, text layout and meshes,
extracted from vecmap. It reuses the existing protobuf reader, deterministic shelf
packer and glyph-metric layout algorithm, adds no production dependency, and
builds without Qt or cgo.

```go
font, err := glyph.DecodeRange(data, "Noto Sans Regular", 0)
if err != nil {
    return err
}
used := make(map[glyph.Key]glyph.Glyph)
for _, id := range []uint32{'M', 'a', 'd', 'r', 'i'} {
    if g, ok := font.Glyphs[id]; ok {
        used[glyph.Key{FontStack: font.FontStack, ID: id}] = g
    }
}
atlas, err := glyph.BuildAtlas(used)
// Handle err and nil atlas. Check Positions for every drawable glyph needed by
// a layout before publishing its quads; an atlas can be partially populated.
```

## Decoding and ownership

`DecodeRange` accepts raw glyph PBF bytes, a caller-owned font-stack identity, and
an aligned range start. Every stack is validated, including nonmatching stacks.
Matching uses the range name rather than the server's font name: servers can
expand fallback stacks. The returned `Range.FontStack` is the requested identity.
Duplicate matching stacks, duplicate glyph IDs, malformed framing and invalid
metrics fail atomically with nil output.

The result owns its glyph map, metadata and bitmaps and does not retain PBF bytes.
Repeated scalar/bitmap fields preserve last-value behavior. The decoder borrows
the last bitmap field while validating metrics, then copies it once. This avoids
copying invalid or superseded bitmap payloads.

Limits:

- **2 MiB** raw input, matching the existing font download/cache ceiling and now
  enforced at decoder entry too.
- **8** stacks per response and **256** glyphs per stack.
- Range starts must be multiples of **256**, from **0** through **65280**. The
  highest supported code point is **65535**. Validation prevents range-end wrap.
- Width, height and advance: **0–255**. Left/top bearings: **-128–127**.
- A drawable bitmap has exactly `(width + 6) * (height + 6)` bytes, including its
  three-texel PBF border. If either dimension is zero, the bitmap must be empty.

## Atlas contract

`BuildAtlas` validates metrics and bitmap sizes before packing. Bitmap-free glyphs
need no rectangle and are omitted. Empty/bitmap-free input returns `(nil, nil)`;
invalid data returns `(nil, error)` rather than risking an out-of-bounds read or
32-bit dimension overflow. Inputs are borrowed synchronously; output pixels and
positions are owned. Treat all published maps/slices as immutable.

The existing ordering is descending height, descending width, ascending font
stack, then ascending code point. Shelf packing tries square powers of two from
**256** through **2048**. At the maximum size it retains the deterministic sorted
prefix that fits. Overflow is normal resource degradation, reported by which
keys appear in `Atlas.Positions`. A layout must have every drawable glyph present
before rendering. `PrepareLayouts` enforces this through `FitsAtlas`.

Pixels are single-channel SDF values. Each rectangle includes the three-texel PBF
border plus one zero-filled guard texel, making total padding four per edge.
Pixels are bounded to **4 MiB**; input-map cardinality, O(n log n) sorting work,
temporary packing maps and scheduling remain caller-owned. These are CPU images
and logical font/glyph keys, not native graphics resources.

Vecmap aliases the shared glyph/range/key/rectangle/atlas data without conversion
maps or bitmap copies. Its manager still owns loading, cancellation, retries,
cache paths, immutable merged font snapshots and retained-state scheduling. Qt fallback
and scene compilation remain caller-owned; collision/placement use the shared
`placement` package. No new shaping engine is introduced.

## Retained atlas selection

`BuildRetainedAtlas(current, retained)` exposes the existing selection policy as
an explicit, stateless operation. It first builds the current-only atlas, then
packs a merge where current glyphs override old values with the same key. The
merged result is used only if it fits at the current-only size and covers every
current key. Otherwise it returns current-only packing. Optional old glyphs never
cause atlas growth. Scheduling and stored retained state remain caller-owned.

The returned resident map contains exactly the selected atlas's positions. The
map and atlas buffers are owned; glyph metrics are copied, while immutable glyph
bitmaps are borrowed. Inputs are not mutated or retained as maps. The existing
two packing passes remain; this extraction adds no bitmap copies or dependencies.
Input cardinality, map merging and sorting work remain caller-bounded, as with
`BuildAtlas`; atlas pixels retain the 2048-square ceiling.

Invalid current data returns nil outputs and an error. Empty/bitmap-free current
data returns nil outputs without inspecting optional retained data. Invalid old
data in the merged candidate causes current-only fallback, preserving the previous
degradation policy. A current-only atlas may itself be partial: this function does
not guarantee complete labels. Continue using `FitsAtlas` before mesh publication.

`AtlasNeedsRebuild` checks key coverage only, not changed metrics or pixels. Empty
requirements return false even for a nil atlas. Every supplied key is checked,
including bitmap-free entries; callers normally pass drawable layout glyphs.
Bitmap-free current entries therefore force current-only fallback when mixed with
drawable entries. This preserves the original behavior rather than silently
changing the completeness test.

Headless retained-atlas tests cover current-wins merging, owned maps/pixels versus
borrowed bitmaps, optional growth, same-size displacement at the maximum atlas,
partial current-only output, empty inputs and invalid-current/optional-old behavior.
New functions have 100% coverage; overall glyph coverage is now **99.8%**, with
the same unreachable atlas-loop return uncovered. Tracked by kata **d22h**.

## Text layout

```go
layout, ok := glyph.LayoutText("Madrid", font.FontStack, font.Glyphs, glyph.LayoutOptions{
    TextSize: 16, LineHeight: 1.2, MaximumWidth: 10,
    Anchor: "center", Justify: "auto", HaloWidth: 1,
})
// layout.Glyphs contains unscaled origins plus the original glyph metrics.
// layout.Scale converts origins/metrics to logical pixels.
// layout.Bounds is already in logical pixels and includes the rendered halo.
```

`LayoutText` reuses the existing 24-unit em, -17 baseline, line measurement,
wrapping and alignment arithmetic. It lays out glyph metrics; it does not provide
OpenType shaping, bidi, kerning or script eligibility. Callers choose suitable
text and validated metrics, normally from `DecodeRange`. Font-stack and style
strings are application-owned.

- CRLF becomes a line break; blank paragraphs retain their vertical space.
  Whitespace within paragraphs collapses to a single space. Overlong words split
  at glyph boundaries, retaining the existing behavior even when one glyph alone
  exceeds the requested width.
- Text size and halo values use logical pixels. Letter spacing, maximum width and
  line height use ems. Nonpositive maximum width disables wrapping; nonpositive
  line height defaults to 1.2 ems. Negative letter spacing remains supported.
- Anchor strings retain substring matching, defaulting to center; `left`/`top`
  win over `right`/`bottom`. Justification accepts left/right/center, otherwise
  following the horizontal anchor.
- Halo width is clamped to the scaled PBF border; nonnegative blur extends bounds.
  Glyphs without ink use the original advance/block rectangle fallback.
- Input is limited to the existing **4096 bytes and 256 runes**, checked before
  layout allocation. Invalid UTF-8, nonpositive text size, nonfinite options or
  overflowing output coordinates fail with a zero layout and `false`. Empty text
  or missing glyphs needed by the final lines also fail atomically. A missing
  space can be harmless when wrapping removes it.

The returned positioned slice is owned. Metrics are value copies; immutable
bitmaps are borrowed. The input glyph map is not retained or modified. The parent
aliases `PreparedLayout`, sharing the metric layout and mesh buffers directly.
Projection copies only the small bounds value into its collision type. Scene
packing remains caller-owned.

## Eligibility and atlas-dependent geometry

`TextEligible` exposes the existing SDF script subset: Latin, Greek, Cyrillic,
Armenian, Georgian, Han, Hiragana, Katakana, Hangul and Bopomofo, plus the existing
whitespace/Common characters. Combining marks and code points beyond the BMP are
excluded. It rejects empty/invalid UTF-8 and oversized text. This is a capability
filter for the metric layout, not a font-availability or OpenType-shaping guarantee.
`LegacyFallbackEligible` shares the old native fallback predicate with the Qt
adapter and offline fixture collision policy. It rejects empty/invalid UTF-8,
more than 256 runes and Thaana (which stalled the legacy Qt font search). This is
a compatibility gate, not a shaping capability guarantee or a new fallback renderer.
The fixture can reserve collision space for eligible fallback text without drawing it.

```go
if !glyph.FitsAtlas(&layout, atlas) {
    // Wait for complete drawable-glyph coverage before rendering this label.
    return
}
mesh, err := glyph.BuildLayoutMesh(&layout, atlas, true)
// Handle err. mesh.Vertices and mesh.Indices are owned direct-indexed geometry.
```

`BuildLayoutMesh` reuses the existing `geometry.TextQuad` and topology builder,
retaining four-texel padding, normalized UVs, signed zeros and the original
`0,1,2,0,2,3` triangle order. Indexed mode populates `Vertices` and `Indices`;
expanded mode populates `Expanded` with packed XYUV float32 triangle vertices.
Only one representation is populated, avoiding a legacy conversion buffer.

The low-level mesh/emitter APIs skip missing atlas entries and bitmap-free glyphs,
as before. `FitsAtlas` is the caller's whole-label coverage gate; it checks key
membership rather than validating geometry. `EmitLayoutQuads` supports custom
sinks and preserves earlier emissions on a later sink/validation error. In
contrast, `BuildLayoutMesh` returns an empty mesh on any error. Nil layout/atlas
inputs produce no geometry. Inputs and callbacks are not retained or mutated;
returned buffers are owned and immutable after publication.

Validation caps a layout at **256 positioned glyphs** before allocation arithmetic
(`geometry.ErrGeometryLimit`). Nonempty layouts require a finite nonnegative scale
and positive atlas dimensions at most **2048** each. Drawable rectangles must fit
the atlas; origins and remaining extents are checked before integer addition.
Nonfinite float32 quad output is rejected (`ErrLayoutGeometry`). Rectangle/scale
validation is independent of atlas pixel buffers, which this code does not read.
An empty layout needs no usable atlas metadata.

## Prepared layout maps

`PreparedLayout` embeds `TextLayout` and `LayoutMesh` without allocating another
object. `PrepareLayouts[K comparable]` accepts caller-keyed pointers to these
values, builds their meshes and returns a map containing complete drawable labels.
The parent aliases this type, so its layout maps pass directly into the shared
engine and back to rendering; there are no conversion maps/slices or duplicate
positioned glyph buffers.

- The result map is owned, with the original keys and layout pointers. The input
  map itself is not modified. Build with exclusive access to unpublished layouts;
  treat the results and all reachable data as immutable once published.
- Successful preparation replaces the entire mesh in place, clearing the alternate
  representation when switching expanded/indexed modes. Metric layout is unchanged.
- Nil layouts and incomplete atlas coverage are omitted without touching old mesh
  data. Successful bitmap-free/empty layouts replace old meshes with empty output
  and are omitted from the returned map. Coverage rejection includes the existing
  per-label positioned-glyph cap.
- Empty input or nil atlas returns nil. Otherwise at most **10,000 map entries**
  are allowed, checked before allocation (`geometry.ErrGeometryLimit`). This shares
  the existing parent scene-layout ceiling. Mesh bounds retain the per-label
  256-positioned-glyph cap; scheduling and total retained memory are caller-owned.
- Any mesh error returns nil output. Earlier successful mesh attachments remain;
  iteration order is unspecified, and the failing layout retains its old mesh.
  This preserves the previous in-place preparation policy rather than promising
  transactional input rollback. No partial map is published on error.

`LayoutGlyphs[K comparable]` collects drawable glyphs into an owned map, copying
metrics and borrowing immutable bitmap slices. It skips nil layouts and bitmap-free
glyphs. Repeated keys must have identical glyph values; conflicting values follow
unspecified traversal order, as before. Collection input cardinality/work remains
caller-bounded; it does not impose the mesh-set ceiling itself.

Native fallback rendering, font resolution and live candidate selection belong to
adapters. The parent `sdfScene` retains rendering counters and native readiness
policy. Shared `compiler` packing and headless `fixture` orchestration use the
same layout data without that native scene wrapper.

## Verification

```sh
CGO_ENABLED=0 go test -cover ./pkg/vecmap/glyph
WHEREAMI_VECTOR_GLYPH_FIXTURE_DIR=/path/to/glyphs \
  CGO_ENABLED=0 GOARCH=386 go test ./pkg/vecmap/glyph
CGO_ENABLED=0 go test ./pkg/vecmap/glyph -run '^$' \
  -fuzz '^FuzzDecodeRange$' -fuzztime=20s
CGO_ENABLED=0 go test ./pkg/vecmap/glyph -run '^$' \
  -fuzz '^FuzzLayoutText$' -fuzztime=20s
CGO_ENABLED=0 go test ./pkg/vecmap/glyph -run '^$' \
  -fuzz '^FuzzLayoutMesh$' -fuzztime=20s
CGO_ENABLED=0 go test ./pkg/vecmap/glyph -run '^$' \
  -fuzz '^FuzzPrepareLayouts$' -fuzztime=20s
```

Tests cover parser limits and exact boundaries, malformed fields, failure after
valid content, repeated fields, ownership, fallback stack naming, empty glyphs,
signed bearings, deterministic ordering, complete bitmap borders/guards, atlas
growth, partial packing and 32-bit validation. Local font ranges are optional
headless fixtures. Layout regressions cover anchors/justification, spacing,
wrapping, missing/bitmap-free glyphs, bounds/halo arithmetic, ownership, UTF-8,
limits and nonfinite/overflow rejection. Eligibility/mesh regressions cover scripts,
exact packed/indexed reconstruction, ownership, missing/bitmap-free glyphs, limits,
atlas coordinates, signed zeros and atomic/streaming failures. Prepared-map tests
cover shared identity, owned map storage, mode transitions, completeness, limits,
mesh errors and drawable-glyph collection. Coverage is **99.8%**,
with layout, eligibility and mesh functions at **100%**; only the atlas growth loop's
unreachable final return is uncovered. See kata **sp7z**, **hk7t**, **gpwj**, **r5xn** and
[`docs/vecmap-rhi.md`](../../../docs/vecmap-rhi.md) for full-capture equality and
controlled before/after preparation measurements.
