# glyph

Toolkit-neutral SDF glyph-range decoding and atlas preparation, extracted from
vecmap. It reuses the existing protobuf reader and deterministic shelf packer,
adds no production dependency, and builds without Qt or cgo.

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
before rendering. The parent already enforces this through `sdfLayoutFitsAtlas`.

Pixels are single-channel SDF values. Each rectangle includes the three-texel PBF
border plus one zero-filled guard texel, making total padding four per edge.
Pixels are bounded to **4 MiB**; input-map cardinality, O(n log n) sorting work,
temporary packing maps and scheduling remain caller-owned. These are CPU images
and logical font/glyph keys, not native graphics resources.

Vecmap aliases the shared glyph/range/key/rectangle/atlas data without conversion
maps or bitmap copies. Its manager still owns loading, cancellation, retries,
cache paths, immutable merged font snapshots and retained-atlas policy. Text
layout, eligibility and Qt fallback, collision/placement and scene compilation
remain the next extraction boundaries. No new shaping engine is introduced.

## Verification

```sh
CGO_ENABLED=0 go test -cover ./pkg/vecmap/glyph
WHEREAMI_VECTOR_GLYPH_FIXTURE_DIR=/path/to/glyphs \
  CGO_ENABLED=0 GOARCH=386 go test ./pkg/vecmap/glyph
CGO_ENABLED=0 go test ./pkg/vecmap/glyph -run '^$' \
  -fuzz '^FuzzDecodeRange$' -fuzztime=20s
```

Tests cover parser limits and exact boundaries, malformed fields, failure after
valid content, repeated fields, ownership, fallback stack naming, empty glyphs,
signed bearings, deterministic ordering, complete bitmap borders/guards, atlas
growth, partial packing and 32-bit validation. Local font ranges are optional
headless fixtures. Coverage is **99.4%**; only the atlas growth loop's unreachable
final return is uncovered. See kata **sp7z** and
[`docs/vecmap-rhi.md`](../../../docs/vecmap-rhi.md) for full-capture equality and
controlled before/after preparation measurements.
