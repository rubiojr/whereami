# sprite

Toolkit-neutral sprite JSON/PNG decoding and crop/tint/opacity preparation,
extracted from vecmap. It reuses `encoding/json`, `image/png`, `image/draw` and
the existing pixel arithmetic. It builds without Qt or cgo and adds no module
dependency. Colors use the shared `style.Color` value.

```go
atlas, err := sprite.Decode(indexJSON, atlasPNG)
// Handle err before using atlas. Decode returns nil on any error.
entry, exists := atlas.Entries[name]
if exists {
    prepared, ok := sprite.Prepare(atlas.Pixels, entry, tint, opacity)
    // prepared.Pixels owns tightly packed, straight-alpha RGBA bytes.
    // Handle ok before publishing the image.
}
```

## Bounds and ownership

- Encoded index: **2 MiB**; encoded PNG: **16 MiB**. Checked before decoding.
- At most **4096 entry occurrences**, including duplicate names, checked while
  streaming the index. Unknown fields are ignored; duplicate names retain the
  last entry. Names retain their exact identity; the index is a map, not an
  ordered sequence. The top level must be an object with no trailing JSON value.
- Atlas dimensions: positive, at most **4096** per axis and **4,194,304 pixels**
  total. `png.DecodeConfig` checks these before the PNG pixel allocation.
  The decoded image is converted to zero-origin NRGBA with the original draw
  operation. Final atlas pixels occupy at most **16 MiB**; temporary PNG pixels,
  decoder buffers and index storage also consume bounded memory.
- Every final entry must have a finite positive pixel ratio, positive dimensions
  and a nonnegative origin whose rectangle fits the atlas. Origins and remaining
  space are checked before addition, avoiding integer overflow on 386.
- `Prepare` validates zero-origin NRGBA dimensions, stride and accessible storage
  before allocating or reading. Padded row strides are supported; subimages with
  nonzero origins are not. A crop has at most the atlas pixel count. Scheduling,
  repeated preparation and aggregate cache/upload budgets are caller-owned.

`Decode` owns its index and pixels and retains no encoded bytes. It fails atomically
for malformed PNG/JSON or invalid metadata; resource ceilings wrap/return `ErrLimit`,
and invalid metadata/storage uses `ErrInput` or the `Prepare` false result. Standard
decoder errors retain their wrapped identity. `Prepare` borrows inputs only during
the call and returns owned pixels, or a zero `Image` and false. Treat published
maps/images/slices as immutable.

## Pixel compatibility

Non-SDF sprites retain source RGB, ignoring the tint, and round source alpha times
clamped opacity. SDF sprites use the tint's RGB byte values and round source alpha
times opacity times tint alpha divided by 255, in the original arithmetic order.
RGB and alpha are not premultiplied. Integer color channels retain the original
byte-conversion behavior; callers normally use evaluated style colors.

Opacity clamps to 0–1, including infinities; NaN now rejects instead of reaching
an undefined float-to-byte conversion. Invalid index entries now fail the whole
decode rather than surviving until a later crop fails. Valid pinned output is
unchanged. This is the existing SDF sprite tint behavior, not a new SDF shader.

Vecmap keeps embedded pinned assets, `sync.Once`, lookup policy, error reporting
and its mutex-protected **512-entry FIFO** image cache. The cache key still includes
color channels and opacity formatted to four decimal places. Entry aliases share
the decoded map; the small legacy image adapter shares the prepared pixel slice
without conversion buffers. The fixture producer still imports Qt-bound vecmap
for scene compilation and orchestration.

## Verification

```sh
CGO_ENABLED=0 go test -cover ./pkg/vecmap/sprite
CGO_ENABLED=0 GOARCH=386 go test ./pkg/vecmap/sprite
CGO_ENABLED=0 go test ./pkg/vecmap/sprite -run '^$' -fuzz '^FuzzDecode$' -fuzztime=20s
CGO_ENABLED=0 go test ./pkg/vecmap/sprite -run '^$' -fuzz '^FuzzPrepare$' -fuzztime=20s
CGO_ENABLED=0 GOAMD64=v4 go test ./pkg/vecmap/sprite -run '^$' \
  -bench '^BenchmarkPinnedSpriteDecode$' -benchmem -benchtime=2s -count=2
```

Headless coverage is **100%**. Tests cover all 264 pinned sprites, straight-alpha
conversion from several PNG color models, crop origins/row padding, tint and alpha
rounding, ownership, malformed headers/JSON/storage, exact limits, duplicate-entry
work accounting and 32-bit overflow rejection. The cold-decode benchmark compares
the former decoder/conversion with the bounded decoder, excluding file I/O. See
kata **m5dy** and [`docs/vecmap-rhi.md`](../../../docs/vecmap-rhi.md) for retained
measurements and full-scene byte equality.
