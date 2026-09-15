# liberty

Pinned OpenFreeMap Liberty assets and shared caches, usable without Qt or cgo.
This package owns provider-specific embedding and readiness/cache policy. Parsing,
style evaluation and image preparation remain in `style` and `sprite`.

- `Layers()` returns the 111 compiled layers in original order. The slice, maps
  and reachable expression values are immutable borrows shared across callers.
- `SpriteEntry(name)` returns a value snapshot of sprite metadata, without cropping
  or tinting pixels. Missing entries or failed decoding return false.
- `Sprite(name, color, opacity)` returns a prepared image with shared immutable
  pixel bytes. Missing assets or invalid preparation input return false.

Style and sprite decoding each run through a separate `sync.Once`, caching errors
as well as success. Calls are synchronous and perform no filesystem/network I/O;
use a preparation worker rather than a render callback. The embedded bytes are
private and exist in one package, shared by all consumers.

The mutex-protected sprite cache retains the original **512-entry FIFO** policy.
Reads and replacements do not promote entries. Keys include exact sprite names,
integer color channels and opacity formatted to **four decimal places**. Nearby
opacities can intentionally share the first cached result. Concurrent misses can
prepare the same key independently; later puts replace that entry without changing
its FIFO position. No single-flight behavior is introduced. Evicted pixels remain
valid for callers holding a borrow. Cache size bounds retained entries, not all
caller-held images or total concurrent preparation work.

The pinned index has 264 entries. Decoder dimensions/byte/work limits and pixel
semantics are documented in [`sprite`](../sprite/README.md). No source image,
style map or prepared pixel conversion copy is added by parent vecmap adapters.
Native handles, uploads and the live scheduler remain outside this package.

## Pins and regeneration

| Asset | SHA-256 |
| --- | --- |
| `liberty_style.json` | `6010998863b4876911ac9a2d62c9a28d97c8877f6d20cd158b74808572257b60` |
| `liberty_sprite.json` | `73e75e58d8c7bb62cc25d9d150660500552f4d04f6eac5efa4e236076773c356` |
| `liberty_sprite.png` | `8996a519d218dc5f98015267709dae272a77bb74ef0ecc5a0992dcf276c1be4c` |

From the repository root:

```sh
go generate ./pkg/vecmap/liberty
```

This invokes the existing checksum-enforcing fetcher in `../cmd/libertystylegen`
with this package as its working directory. It requires network access; normal
builds and tests use the checked-in bytes. Asset checksums are also tested offline.

## Verification

```sh
CGO_ENABLED=0 go test -cover ./pkg/vecmap/liberty
CGO_ENABLED=0 GOARCH=386 go test ./pkg/vecmap/liberty
GOAMD64=v1 go test -race ./pkg/vecmap/liberty
```

Coverage is **100%**. Tests check pins, shared layers/pixels, missing assets,
cached decode failures, invalid opacity, quantized key identity, FIFO replacement
and eviction, borrowed-image lifetime and concurrent cold loading/cache pressure.
Parent adapter tests check shared storage; all three full fixture captures remain
byte-identical. See kata **4vqr** and [`docs/vecmap-rhi.md`](../../../docs/vecmap-rhi.md).
