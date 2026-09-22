# Go-owned retained renderer prototype

Tracked by kata **ngrb**. The goals are no handwritten application C++, a
toolkit-neutral Go map engine, and MapLibre Native-class performance at comparable
visual quality. Flatpak gives us control over the Qt/QRhi dependency version.

## What is implemented

- `pkg/vecmap/view`: the existing camera/projection, tile hierarchy, bounded
  nearest-first cover, tile transforms, pattern phase and world-wrap algorithms,
  moved with their regression tests into a Qt-free package. The public vecmap
  camera types remain source-compatible aliases. No cover conversion copies are
  introduced between the existing scheduler and the shared package.
- `pkg/vecmap/scene`: immutable meshes, textures, ordered draws, materials, and
  camera transforms. Resource IDs and revisions are logical Go values. There are
  no Qt types, native pointers, or cgo dependencies. Adjacent compatible draws
  coalesce without sorting transparent content out of order.
  Meshes support optional uint32 indices; draw ranges address indices when present.
- `pkg/vecmap/geometry`: bounded polygon cleanup and Earcut preparation, indexed
  topology assembly, complete line preparation (cleanup, offsets, dashing,
  extrusion, caps and joins), and glyph/icon quads.
  The fixture can carry direct uint32 indices through feature storage, fill
  batching, line construction and symbol packing.
- `pkg/vecmap/mvt`: bounded PBF tile preparation, shared feature/property/polygon
  data, geometry decoding, ring grouping, Earcut and aggregate budgets without Qt
  or cgo. `pkg/vecmap/internal/pbf` shares the existing reader with glyph parsing.
- `pkg/vecmap/style`: document/layer preparation, the existing expression subset,
  primitive conversions, numeric/color interpolation and color parsing/opacity.
  It depends only on the standard library; pinned assets and caching live in
  the headless `pkg/vecmap/liberty` package.
- `pkg/vecmap/glyph`: bounded SDF glyph PBF decoding, shared metrics/bitmaps and
  deterministic atlas packing, SDF eligibility, glyph-metric text layout and
  expanded/direct-indexed glyph meshes. Font I/O/cache/retry, retained-atlas policy
  and toolkit fallback stay caller-owned.
- `pkg/vecmap/placement`: feature-anchor selection, line interpolation/repetition,
  upright/raw angles, exterior-ring centroids and streaming evaluated text/icon
  candidates, projected boxes and stable collision/priority/optional-symbol
  selection. Glyph/sprite readiness and fallback policy remain caller-owned.
- `pkg/vecmap/sprite`: bounded sprite JSON/PNG decoding, straight-alpha atlas
  conversion and crop/tint/opacity preparation. Native uploads remain caller-owned.
- `pkg/vecmap/liberty`: single-copy pinned style/sprite embedding, once-only
  decoding, immutable layer/metric/pixel sharing and the 512-entry FIFO image cache.
- `pkg/vecmap/compiler`: evaluated fill/extrusion/pattern/outline and line/gap
  batching and geometry preparation, visible tile-layer traversal, background
  meshes, shared primitive values, aggregate triangle accounting, text preparation,
  scene packing, materials and ordered icon/halo/fill passes.
- `pkg/vecmap/fixture`: top-level pinned offline orchestration using the shared
  engine and Liberty assets. The fixture producer builds with `CGO_ENABLED=0`.
- `pkg/vecmap/retained`: bounded atomic fragment replacement/removal, stable
  store-wide resource IDs/revisions, explicit ordered snapshot composition and
  bounded acknowledged upload/release planning. Not yet wired to live/native execution.
- `internal/vecmaprhi`: a Qt backend that retains buffers/textures and records
  draws inline with Qt Quick through `QSGRenderNode` and `QRhi`. It handles parent
  scissor/stencil clipping, inherited opacity, resize, resource replacement, and
  render-thread cleanup. Revision-keyed caches and allocation staging allow old/new
  versions to coexist; planner batch execution is not yet wired. Glyph offsets stay
  in screen pixels during camera motion.
- `internal/qtrhi`: a focused, generated Qt adapter. Native virtual callbacks and
  lifetime notifications are generated; rendering logic is Go. Returned native
  value copies require explicit `Delete`; GPU ownership never relies on finalizers.
- `cmd/vecmap-fixture`: captures the existing compiler's pinned Madrid-area tile
  as a self-contained, portable JSON scene, including textures and prepared glyphs.
- `cmd/vecmap-rhi`: displays that scene and replays a pan/zoom/bearing trajectory.
  Its dependency graph contains no legacy `internal/miqtquick` bridge and no
  handwritten application C++. It reports CPU preparation/submission, available
  GPU timestamps, render-callback intervals, uploads, and live resource counts.

Geographic fixture captures carry a `view.Camera` and tile-space descriptions.
The viewer reprojects those through `Document.FrameAt` without replacing scene
resources. Older affine-only captures remain readable.

The fixture producer uses `pkg/vecmap/fixture` and the extracted headless MVT,
style, glyph/sprite, text layout, symbol/collision, geometry and scene packing
packages. Its dependency graph has no Qt, cgo or parent `pkg/vecmap` import.
Parent fixture APIs are compatibility wrappers. Live resource readiness and
scheduling remain in the production renderer; this does not migrate production.

The native bindings and Go renderer are separate packages. Editing rendering
logic rebuilds the Go backend without recompiling the binding package's C++.

## Build and run

The checked-in bindings target **Qt 6.11.2**. Build and runtime version checks
reject another Qt version. Install the matching Qt Quick, QML, Qt Shader Tools,
and Qt base private development headers.

```sh
make rhi-build

# Uses the existing compiler and verifies the pinned PBF checksum.
CGO_ENABLED=0 go run ./cmd/vecmap-fixture \
  -tile /path/to/openfreemap-20260823-z9-250-193.pbf \
  -glyph-dir /path/to/glyphs \
  -out /tmp/madrid-scene.json

QSG_RHI_BACKEND=vulkan bin/vecmap-rhi \
  -scene /tmp/madrid-scene.json -duration 8s -foreground -diagnostics \
  -screenshot /tmp/madrid.png
```

The glyph directory contains `Noto%20Sans%20Regular.pbf`,
`Noto%20Sans%20Bold.pbf`, and `Noto%20Sans%20Italic.pbf`, each the corresponding
OpenFreeMap `0-255.pbf` glyph range. The producer reports missing fonts instead
of silently substituting one. The viewer does no networking. Preserve the scene
file itself for exact replay; it contains all uploaded texture and vertex data.

The source tile is:

```text
https://tiles.openfreemap.org/planet/20260823_080002_pt/9/250/193.pbf
SHA256 5007c887f3c99a2c0737b9a3afdf3813ef3e1ce939a63aa09a8407b7c3770c79
```

Set `QT_RHI_INCLUDE` when private headers are in a separate SDK prefix:

```sh
make rhi-build QT_RHI_INCLUDE=/sdk/include/qt6/QtGui/6.11.2/QtGui
```

`scripts/qt-rhi-env.sh` supplies the include path without globally changing
`CGO_CXXFLAGS`, which would invalidate cached MIQT builds unnecessarily.

## Generated bindings and reuse

`make rhi-bindings` runs **MIQT v0.14.0's existing Clang-based generator**. It does
not implement a new C++ parser or binding generator. `cmd/qt-rhi-gen` supplies a
small allowlist and adaptations for qualified RHI types, iterator setters,
namespaced C ABI symbols, callback cleanup, and explicit native ownership.
The generator version is pinned; a test regenerates the adapters and compares
them byte-for-byte with the checked-in files.

The focused adapter supports one vertex stream and one dynamic uniform offset
per draw. MIQT's pointer-to-pair projection represents one pair, so generated
guards reject larger counts instead of allowing native out-of-bounds reads.

The callbacks are ordinary generic Qt method adapters. Moving a handwritten
`drawMap()` implementation into a generator template would not meet the goal.

Reuse decisions for this prototype:

| Component | Decision |
| --- | --- |
| Qt bindings | Reuse MIQT's generator and existing Qt Core/QML bindings. |
| Triangulation | Reuse the existing Go Earcut implementation and its tests. |
| MVT/style/collision/atlas preparation | Reuse vecmap's existing implementations for identical input to backend comparisons. |
| Complex text shaping | `go-text/typesetting` is the candidate for toolkit-neutral shaping; do not implement another shaping engine. Not integrated by this prototype. |
| Geometry/MVT utilities | `paulmach/orb` is a candidate when extracting the engine; benchmark and preserve decoder resource limits before replacing existing code. |
| General Go GPU renderers | GoGPU/gg and Go WebGPU were considered. Their documented GPU interfaces do not provide a drop-in adapter to Qt's active QRhi command buffer. Using them would require separately proving device/texture sharing and synchronization. |

No new production Go module dependency was needed for this stage. Qt Bridges
and purego remain integration/build options, rather than prerequisites for the
rendering path or substitutes for measured performance.

## Verification

```sh
make rhi-test

# Headless GPU-API tests use OpenGL through Mesa, not Qt's software scene graph.
QSG_RHI_BACKEND=opengl xvfb-run -a make rhi-test

# On a desktop with Vulkan presentation support:
QSG_RHI_BACKEND=vulkan make rhi-test

# Race checks for callbacks, resource replacement, and destruction:
sh scripts/qt-rhi-env.sh go test -race -tags 'vecmap_rhi integration' \
  ./internal/qtrhi ./internal/vecmaprhi
```

Tests cover solid/image/pattern/SDF output, parent clipping including rotated
stencil clips, inherited opacity, camera-only retention, resource replacement
and eviction, resize, repeated teardown, and rejected duplicate callbacks.
The pixel tests also pass at `QT_SCALE_FACTOR=2`.

Vulkan presentation does not work with the tested Xvfb setup (no DRI3). It was
verified on the desktop GPU instead. Always record the reported device: an
OpenGL/Vulkan API alone does not establish hardware acceleration.

## Initial measurements

Measured on 2026-09-12 with Go 1.27.1 (`GOAMD64=v1`), Qt 6.11.2, Mesa 26.1.8,
and a Radeon 860M. `make rhi-build` defaults to `GOAMD64=v4`, like the main build.
The viewport was 512x512 logical pixels at desktop DPR 2. One fixed-style source
tile produced 782,409 vertices, 45 draw batches, and 62 labels with no missing
fonts. Each run lasted eight seconds; the first 30 render callbacks were omitted
from timing samples.

| Measurement | OpenGL | Vulkan |
| --- | ---: | ---: |
| CPU preparation p95 | 71 µs | 77 µs |
| CPU draw submission p95 | 69 µs | 130 µs |
| Previous completed GPU frame p95 | 0.884 ms | 0.862 ms |
| Render-callback interval p50 | 16.71 ms | 16.62 ms |
| Render-callback interval p99 | 18.92 ms | 22.05 ms |

Both runs uploaded one mesh and five textures (including the shared white
texture), totaling 19,045,116 bytes. Camera motion caused no further geometry or
texture uploads. Teardown reported zero live meshes/textures.

These are prototype measurements, **not evidence of MapLibre Native parity**.
GPU timestamps cover the previous completed Qt frame; callback cadence is not a
presentation timestamp. The fixture freezes style widths, feature visibility,
and collision decisions at zoom 10. It does not exercise tile arrivals, source
zoom transitions, live placement, or the full application. Cold build times and
a controlled MapLibre comparison have not been measured yet.

### Control window placement when measuring pacing

Use `-foreground` for desktop benchmark runs. It requests activation and keeps
the window on top, reducing occlusion-related presentation throttling. This is
an opt-in benchmark setting, not a change to production map windows.

`-diagnostics` reports GUI timer intervals, active/exposed tick counts, and gaps
over 100 ms with window state and Qt's queued `frameSwapped` count. Diagnostics
retain at most 60,000 timer samples and 64 gap records. Startup's first 30 render
callbacks are excluded from gap records, consistently with rendering timings.
The report also records the Qt platform plugin, renderer, device and trace type.

Investigation of kata `vx93` on the XCB desktop found:

| Trace | Foreground | Frames / duration | Callback p99 |
| --- | --- | ---: | ---: |
| Geographic, Vulkan | no | 153 / 8 s | 1.001 s |
| Geographic, Vulkan | yes | 478 / 8 s | 22.66 ms |
| Affine, Vulkan | yes | 361 / 6 s | 18.40 ms |
| Geographic, OpenGL | yes | 358 / 6 s | 18.38 ms |

Both trace implementations exhibited gaps without the foreground control. The
GUI timer stalled alongside rendering, while CPU draw work and GPU time remained
small. Keeping the window on top removed the one-second gaps in these runs,
including periods when the window was not active. Qt sometimes still reported
`visible=true` and `exposed=true` during stalls, so those flags alone cannot certify
a reliable timing run.

This rules out the geographic camera as the specific cause of these observations
and points to window/presentation pacing. It does not identify the exact blocking
call in Qt, the driver or compositor. `frameSwapped` is **not** an actual display
presentation timestamp; real presentation tracking remains a separate requirement
for the MapLibre comparison. Poor cadence samples are reported, not discarded.

## Optional indexed meshes

Use `vecmap-fixture -indexed` to capture a compact representation. This is an
offline preparation step and defaults to false; it is never run on the render
thread. `scene.IndexMesh` uses Go maps to identify bit-identical vertices, retaining
UVs, pixel offsets, signed zero, triangle order, and all draw ranges. It returns
the original mesh when indexing would increase buffer bytes. Already indexed
meshes pass through unchanged.

Mesh IDs/revisions remain producer-owned. Index before publication, or advance
the revision when publishing a replacement. The Qt adapter retains both buffers
and reuses the existing generated QRhi indexed-draw APIs. It supports switching
between indexed and expanded revisions and releases both buffers on eviction,
resource invalidation and destruction.

For the same Madrid fixture:

| Geometry | Expanded | Indexed |
| --- | ---: | ---: |
| Vertices | 782,409 | 272,038 |
| uint32 indices | 0 | 782,409 |
| Geometry buffer bytes | 18,777,816 | 9,658,548 |
| Total initial upload bytes, including textures | 19,045,116 | 9,925,848 |

Geometry buffers shrink **48.6%**. Two alternating six-second foreground Vulkan
runs per representation had GPU p95 of 0.897–0.908 ms expanded and 0.914–0.917 ms
indexed. Both maintained approximately 60 Hz. This establishes a memory/upload
benefit, **not a steady-frame GPU speedup** for this scene.

The generic post-processing pass initially took about 101 ms on this fixture.
A plain-XY key fast path reduced one measured run to 77 ms. The synthetic grid
benchmark dropped from about 31 ms to 23.5 ms. Packing still has a meaningful CPU
and transient-allocation cost, so it remains opt-in. Live geometry preparation
should eventually emit indices directly from triangulation/tessellation instead
of expanding vertices and deduplicating them again.

Headless tests verify exact attribute reconstruction, range validation,
idempotence, and skipping unhelpful indexing. Qt tests compare complete
framebuffers across representations and exercise both layout transitions,
resource reconstruction and teardown.

### Direct polygon preparation checkpoint

The first checkpoint, commit `9ad9284` under kata **tgde**, introduced direct
polygon preparation. `pkg/vecmap/geometry` retains
Earcut topology as owned float64 points and uint32 indices, without expanding and
hashing triangles. Ring cleanup and conservative operation accounting were
extracted from the existing implementation. `roadPoint` is an alias, avoiding
conversion allocations. The expanded adapter uses Earcut's original integer
indices directly, so existing consumers do not pay for a temporary uint32 buffer.

On the same Ryzen AI 7 PRO 350, Go 1.27.1, `GOAMD64=v4`, three sequential runs of
`BenchmarkPolygonPreparation` gave these ranges for a synthetic 4096-point ring:

| Preparation | Time/op | Allocated bytes/op | Allocs/op | Scene geometry bytes |
| --- | ---: | ---: | ---: | ---: |
| Expanded | 688–736 µs | 1,399,790–1,399,791 | 8,214 | 294,768 |
| Direct indexed | 619–653 µs | 1,055,721 | 8,214 | 147,432 |
| Expanded + `IndexMesh` | 1,248–1,254 µs | 1,695,021–1,695,022 | 8,233 | 147,432 |

Each mode includes the same triangulation and float32 scene packing. Direct
indexing reduces allocation volume by about 24.6% versus expansion and 37.7%
versus post-indexing here. It does not reduce the dominant Earcut allocation
count. A 16-point ring measured 1.37–1.47 µs expanded, 1.21–1.22 µs direct, and
3.55–3.84 µs post-indexed. An initial run overlapped build/race checks and had
wider timing ranges (large ring: 733–893 / 584–621 / 1,218–1,265 µs respectively);
the table is the rerun with no concurrent agent checks.

These are isolated polygon measurements, not full-tile speedups. At this
checkpoint feature storage and fill batching still expanded triangles, and
line/glyph construction still needed direct indexed output.

Headless geometry coverage is 97.4%. Tests cover holes, concavity, cleanup,
degeneracy, signed zero, input ownership, large polygons, index validation, and
budget exhaustion. Regenerating the complete expanded Madrid capture with the
same `GOAMD64=v1` setting produced byte-for-byte identical JSON, including draw
order, all vertices and textures. Comparing a `v4` capture to that older `v1`
capture instead exposed 12 tiny glyph-offset differences; pin the Go architecture
setting as well as the binary, data and camera options when testing equality.

### End-to-end direct indexed fixture preparation

`vecmap-fixture -direct-indexed` now preserves Earcut indices through decoding and
material batching and constructs line and symbol topology directly:

- Four vertices per line segment, preserving its original diagonal/winding.
- Nine vertices per eight-section disk, sharing its center and closing point.
- Four vertices per glyph/icon quad, preserving UVs and screen-pixel offsets.
- Original triangle order and material draw ranges throughout; no hashing pass.

The reusable builder, extrusion, disks, quads and text-vertex transform are in
`pkg/vecmap/geometry`. Existing style evaluation, path cleanup/dashing, miter
joins, shaping and collision logic are reused. No new dependency or native
binding is involved. The Qt-bound compiler remains an adapter around the portions
of the engine extracted so far; full engine extraction is still pending.

```sh
go run ./cmd/vecmap-fixture \
  -tile /path/to/openfreemap-20260823-z9-250-193.pbf \
  -glyph-dir /path/to/glyphs -direct-indexed \
  -out /tmp/madrid-direct-scene.json
```

`-direct-indexed` and `-indexed` are mutually exclusive. The latter remains the
post-processing baseline. Defaults remain expanded for controlled comparisons;
the production scheduler has not been migrated.

Full Madrid fixture preparation at commit `bb12a2e`, before removing unused decode
work, Ryzen AI 7 PRO 350, Go 1.27.1, `GOAMD64=v4`,
GOMAXPROCS 16; three final sequential two-second benchmark samples:

| Preparation | Time/op | Allocated MB/op (decimal) | Allocs/op | Geometry bytes |
| --- | ---: | ---: | ---: | ---: |
| Expanded | 66.2–68.6 ms | 176.48–176.49 | 128,972–128,978 | 18,777,816 |
| Direct indexed | 51.6–55.1 ms | 120.38 | 132,309–132,312 | 11,567,028 |
| Expanded + `IndexMesh` | 143.3–148.1 ms | 224.58 | 130,053–130,054 | 9,658,548 |

The direct scene has **351,558 vertices and 782,409 indices**, with the same 45
draws, 62 labels and complete fonts. Geometry bytes shrink 38.4% versus expanded;
allocation volume falls about 31.8%, though allocation count increases. Total
initial uploads including textures fall from 19,045,116 to **11,834,328 bytes**.
Direct construction does not share vertices across separate features, style
layers or glyph halo/fill passes, so it is less compact than global deduplication.

The benchmark includes checksum verification, decoding (including the existing
unused legacy fallback buckets), source-zoom and zoom-10 compilation, glyph
decoding/layout, atlas preparation, collision, scene packing and validation. It
excludes file I/O, JSON encoding and GPU upload. All modes use the same warm
style/sprite caches. This is a fixed-scene preparation benchmark, not a tile
arrival/presentation benchmark or a MapLibre comparison.

Timing was variable: initial one-second samples before reserving index capacity
per append measured 68.8–70.8 ms expanded, 96.6–157.1 ms direct, and 220.1–254.8 ms
post-indexed. A direct-only CPU profile on that same earlier code measured
54.9 ms, so the broad timing swing cannot be attributed to the reservation
optimization. The profile showed copying, slice growth and GC among the main
costs. Reserving index capacity with `slices.Grow` reduced direct allocated bytes
from about 122.74 MB to 120.38 MB. The table records the final repeated run; the
earlier slow samples are retained here rather than discarded.

Verification:

- Full-scene tests reconstruct every vertex bit-for-bit (all six float32
  attributes), and compare ordered draws, textures, camera and labels against
  expanded and post-indexed baselines. They verify that direct polygon/glyph
  storage has indices rather than expanded triangles.
- The default expanded capture remains byte-for-byte identical to the previous
  geographic JSON with matching `GOAMD64=v1`.
- The same viewer binary with `-animate=false -foreground` produced byte-identical
  expanded/direct PNGs on both Vulkan and OpenGL, at desktop DPR 2.
- A six-second foreground Vulkan camera trace uploaded the direct mesh/textures
  once and released everything on teardown. It recorded 356 frames, callback
  p50/p99 of 16.68/22.55 ms, and previous-frame GPU p95 of 0.924 ms. This is a
  retention check, not evidence of a steady-frame speedup or presentation timing.
- Headless geometry coverage is 98.0%; tests include builder bounds/atomic
  rejection, index rebasing, disk closure, quad topology, line caps/joins/dashes,
  signed-zero reconstruction, and full-scene comparison with local fixture assets.
- Normal full-scene tests and CPU benchmarks used `GOAMD64=v4`; the final
  integration/race pass used `GOAMD64=v1`. An attempted `v4` race run timed out
  while rebuilding the uncached MIQT bindings, before tests ran. Vulkan RHI
  integration tests, staticcheck and the normal application build also passed.

The full-scene test and benchmark commands are in
[`pkg/vecmap/geometry/README.md`](../pkg/vecmap/geometry/README.md).

### Styled-only fixture decoding

Kata **fte6** separates feature decoding from legacy fallback preparation and
automatic source-zoom compilation. The fixture now decodes styled features and
compiles once at zoom 10, in all three representations. The existing production
decoder still prepares road/fill fallback buckets and compiles at source zoom.
Both paths share the same MVT parsing and bounded styled feature decoder; the
fallback-only budgets apply only when generating fallback geometry.

Using the same full-fixture benchmark and three sequential two-second samples:

| Mode | Before: time/op | Styled-only: time/op | Before: allocated MB/op | Styled-only: allocated MB/op |
| --- | ---: | ---: | ---: | ---: |
| Expanded | 66.2–68.6 ms | 49.8–51.5 ms | 176.48–176.49 | 142.70 |
| Direct indexed | 51.6–55.1 ms | 31.7–33.3 ms | 120.38 | 90.75 |
| Expanded + `IndexMesh` | 143.3–148.1 ms | 124.5–129.3 ms | 224.58 | 190.80 |

Direct preparation's allocation volume drops another 24.6%, and allocation count
falls from about 132,310 to 102,312 per operation. This removes unused preparation
work; it does not change the scene's geometry buffers or steady-frame GPU work.

Regenerated expanded, direct-indexed and post-indexed JSON captures are each
byte-for-byte identical to their earlier captures with matching `GOAMD64=v1`.
Tests compare both decoders' source features, primitives and symbols at zooms 10
and 10.5, check that styled-only decoding leaves geometry uncompiled and fallback
buckets empty, and retain malformed-input validation. The fixture still freezes
style and placement decisions at zoom 10.

At commit `9500392`, the command's font-discovery pass still called fixture
compilation once without glyphs before recompiling with the available glyph
ranges. That command-level duplication is outside `BenchmarkCompileRenderFixture`;
the following checkpoint removes it.

### Single-pass font loading

Kata **k00j** adds `CompileRenderFixtureWithGlyphLoader`. It invokes a caller-owned
`FixtureGlyphLoader` once after successful bounded tile/style preparation, with
sorted, unique font-stack names, before glyph layout, collision and scene packing.
The callback returns the 0–255 PBF ranges keyed by exact font stack. A nil loader
means no glyph data; omitted ranges retain the existing missing-font reporting.
Loader errors abort compilation before publication and preserve error wrapping.

`cmd/vecmap-fixture` uses this callback for `-glyph-dir`, reading only requested,
URL-escaped filenames. Filesystem access stays in the command. The callback is
synchronous; callers schedule preparation off GUI/render threads. Existing
callers with preloaded ranges can keep using
`CompileRenderFixtureWithOptions`.

`BenchmarkFixtureGlyphDiscovery` compares the old two-pass command preparation
against the callback, using preloaded glyph bytes in both cases. It includes
font discovery, tile/style preparation, layout and scene packing, but excludes
file I/O, JSON encoding, post-deduplication and GPU work. The preceding table
already measured a single compilation; this table measures the discovery
overhead separately. Same hardware/toolchain, `GOAMD64=v4`, three sequential
two-second samples:

| Geometry | Old two-pass time/op | Loader time/op | Old allocated MB/op | Loader allocated MB/op |
| --- | ---: | ---: | ---: | ---: |
| Expanded | 102.6–111.0 ms | 57.6–60.8 ms | 283.58 | 142.70 |
| Direct indexed | 75.3–81.7 ms | 34.2–35.5 ms | 179.07 | 90.75 |

For direct indexing, allocation volume falls about 49.3% and allocation count
falls from about 197,645 to 102,316. Expanded, direct-indexed and post-indexed CLI
captures remain byte-for-byte identical to their previous `GOAMD64=v1` captures.
Tests cover sorted/deduplicated requests, one callback invocation, complete and
partial ranges, nil/missing ranges, invalid tiles, loader failures, malformed PBF
data, escaped filenames, absent files/directories and read errors.

```sh
WHEREAMI_VECTOR_TILE_FIXTURE=/path/to/openfreemap-20260823-z9-250-193.pbf \
WHEREAMI_VECTOR_GLYPH_FIXTURE_DIR=/path/to/glyphs \
GOAMD64=v4 go test ./pkg/vecmap -run '^$' \
  -bench '^BenchmarkFixtureGlyphDiscovery$' -benchmem -benchtime=2s -count 3
```

Remaining CPU-engine extraction and live scheduler work are tracked by **ngrb**.

### Toolkit-neutral line engine

Kata **1fy1** moves the complete existing line preparation pipeline into
`geometry.TessellateLines`, taking a Go-only `LineStyle` and producing expanded
or indexed meshes. Path cleanup, offsets, dash phase across path vertices,
segment extrusion, miter/round joins and endpoint caps now run without importing
the Qt-bound parent package. Vecmap supplies evaluated paint and maps failures
back to its existing feature-resource error classification.

The extraction preserves the 2,000,000-triangle policy, 100,000 dashed segments
per path, dash iteration budget, miter bound of four half-widths, offset bound of
four times the requested offset, and original triangle order. Capacity estimation
now saturates before multiplication, preventing an
integer overflow for repeated/shared input paths on 32-bit targets. The result
owns its data, and failed preparation publishes no partial mesh.

Headless geometry coverage is **97.4%**, with regressions for dash phase and odd
patterns, zero/invalid dash entries, closed-path caps, sharp/degenerate miters,
offset bounds, input ownership, exact indexed expansion and limit exhaustion.
Both `CGO_ENABLED=0` amd64 and 386 tests pass, as do the existing parent-package
tests, integration/race checks, staticcheck and application build. All three
Madrid representation captures remain byte-for-byte identical at `GOAMD64=v1`.

The solid, round-joined 1024-point headless line benchmark measured 200–206 µs
expanded (557,057–557,058 B/op, four allocations) and 177–193 µs indexed
(434,176 B/op, five allocations),
using Go 1.27.1 and `GOAMD64=v4`. These compare representations in the extracted
engine, not before/after extraction or rendering performance.
The benchmark also includes a dashed, round-capped case to track dash-buffer
allocation behavior.

Full-fixture timing was variable: an initial sweep measured 97.2–111.5 ms expanded,
40.0–68.1 ms direct, and 143.6–158.3 ms post-indexed; a direct-only repeat measured
45.5–49.9 ms. Those samples are retained here. To check extraction overhead,
separate test binaries were built from the previous `99d28ab` implementation
(through a Go build overlay) and the working implementation, then run in
before/after/after/before order with the same inputs and architecture setting.

That comparison first exposed roughly 16 extra dash-buffer allocations after
extraction. Keeping allocation and growth in the consuming function, while a
helper scales the caller-owned buffer, preserved Go's stack-allocation option.
The final alternating run measured **33.3–35.0 ms before** and **32.9–33.5 ms after**,
both about **90.75 MB/op** and 102,312–102,315 allocations. This supports retaining
the preparation cost; no extraction speedup or MapLibre parity is claimed.

```sh
CGO_ENABLED=0 go test -cover ./pkg/vecmap/geometry
CGO_ENABLED=0 GOARCH=386 go test ./pkg/vecmap/geometry
CGO_ENABLED=0 GOAMD64=v4 go test ./pkg/vecmap/geometry \
  -run '^$' -bench '^BenchmarkTessellateLines$' -benchmem -count 3
```

### Toolkit-neutral MVT geometry commands

Kata **fc0r** extracts the existing point, line-string and polygon-ring command
decoders into `pkg/vecmap/mvt`. Parent adapters use `geometry.Point` directly and
share the feature resource-error identity and point-limit constant. No conversion
slices, new library, or changes to the pending line engine are involved.

This preserves int64 delta accumulation across paths/rings, repeated points,
winding, implicit polygon closure, command validation and nil output on failure.
The ceilings remain 1,000,000 points per point/line feature and 32,768 points per
polygon feature, including all rings. At this checkpoint, protobuf parsing,
feature data, ring grouping and aggregate tile budgets were the next extraction
boundary; the following checkpoint provides standalone PBF tile preparation.

Verification on Go 1.27.1:

- Headless amd64 tests: **100.0% statement coverage** in `mvt`; headless 386 tests
  pass. Cases cover malformed commands, bounds across multiple paths/rings, exact
  limits, signed coordinates beyond int32, ownership and atomic failure.
- `GOAMD64=v4 go test -cover ./...` passes with the pinned tile/glyph assets.
- `GOAMD64=v1 go test -race -tags integration` passes for vecmap, mvt and geometry.
- Expanded, direct-indexed and post-indexed captures regenerated with the same
  `GOAMD64=v1` binary each compare byte-for-byte equal to the previous references:
  45 draws, 62 labels, complete fonts and unchanged geometry counts.
- Targeted staticcheck and `go build ./...` pass. Repository-wide staticcheck
  reports only existing ST1006 receiver-name diagnostics in generated
  `internal/miqtquick` bindings. Gopls reports no new build errors.
- Complexity review retains the two original sequential command state machines
  (line/ring decoding each scores 16); their branches encode distinct validation
  and path-transition rules. A future decoder-wide refactor could share command
  traversal, but is not needed for this behavior-preserving extraction.
- The pre-edit vulnerability scan reported standard-library **GO-2026-5024**
  (`NewNTUnicodeString` length overflow). This checkpoint adds no dependencies or
  toolchain changes and does not resolve that baseline finding.

The synthetic command benchmark is a maintenance tool, not extraction speedup
or full-map parity evidence. Renderer/GPU code is unchanged; verification here is
CPU preparation and full-capture equality, rather than a fresh backend benchmark.

### Headless MVT tile preparation and source model

Kata **ydc3** extends `pkg/vecmap/mvt` with `DecodeTile(data, indexed)`, returning
owned source-layer features, properties, polygon topology and degradation
summaries. The existing protobuf reader is shared with glyph decoding through
`pkg/vecmap/internal/pbf`. The existing parser, ring grouping and Earcut behavior
are reused; no production dependency is added.

Vecmap's source types are aliases of `mvt.Feature`, `mvt.Properties` and
`mvt.Polygon`. Exporting their fields lets style and symbol consumers share the
same data without conversion slices/maps. Production uses `DecodeLayers` plus
one `Decoder` per tile, keeping legacy fallback work and warning delivery in the
original per-layer order. The standalone decoder instead returns ordered resource
summaries and owns no logger, I/O or Qt state. Parsed layer payloads borrow the PBF
buffer until preparation ends; completed features do not retain it.

Shared limits remain 100,000 accepted features, 1,000,000 source points, 500,000
styled triangles and 50,000,000 polygon-preparation operations per tile. Skipped
features do not charge accepted output, while spent triangulation work stays
charged. The parser now enforces the existing 2 MiB I/O ceiling itself, and feature
slice capacity is capped at remaining slots before allocation. An oversized
protobuf field number is rejected before conversion to `int`, preventing a
malformed key from aliasing a known field on 32-bit targets. These explicit
boundary checks leave valid fixture output unchanged.

Verification:

- Headless MVT coverage **100.0%**, shared reader coverage **98.8%**; amd64/386
  tests pass. The reader's remaining uncovered return is the original unreachable
  fallthrough after its bounded varint loop.
- Tests cover typed values, packed/unpacked fields, explicit zero IDs, borrowed
  parsing versus owned output, duplicate layers, malformed data, aggregate budgets,
  spent-work retention and exact indexed reconstruction of the pinned tile.
- A 20-second headless fuzz run completed **1,115,109 executions** without failure.
- Full `GOAMD64=v4 go test -cover ./...`, `GOAMD64=v1` integration/race checks for
  vecmap/mvt/pbf/geometry, targeted staticcheck and the application build pass.
  The previously recorded generated ST1006 warnings and standard-library
  GO-2026-5024 vulnerability baseline remain unresolved.
- All three regenerated `GOAMD64=v1` captures compare byte-for-byte equal to their
  references: 45 draws, 62 labels, complete fonts and unchanged geometry counts.
- Complexity review retains the existing parser's explicit field dispatch
  (`decodeValue` 18, `DecodeFeature` 14, layer fields 13); the new aggregate
  preparation functions are at most 10. No renderer or binding code is changed.

Two v4 test binaries, built immediately before and after this extraction, ran the
direct fixture benchmark in before/after/after/before order, with identical pinned
tile/glyph inputs, Go 1.27.1, GOMAXPROCS 16 and two-second samples:

| Version | Time/op | Allocated bytes/op | Allocs/op |
| --- | ---: | ---: | ---: |
| Before tile extraction | 30.81 / 30.27 ms | 90,747,860 / 90,747,880 | 102,311 / 102,312 |
| After tile extraction | 30.45 / 30.38 ms | 90,748,797 / 90,748,317 | 102,314 / 102,313 |

This supports preserving preparation cost, not an extraction speedup or full-map
parity claim. The benchmark excludes I/O, JSON and GPU work. Style evaluation,
glyph/atlas preparation, placement and scene compilation are the next CPU-engine
boundaries; live scheduling/upload migration and broader rendering gates remain
open. The offline fixture producer still imports the Qt-bound parent package.

### Headless expression and color evaluation

Kata **ddp2** moves the existing Liberty interpreter into `pkg/vecmap/style`, with
`Evaluate(expression, Context)`, primitive conversion helpers and shared `Color`
values. Parent adapters borrow the existing feature property map, and the color
type is an alias. The package imports only the standard library. The pinned style
JSON, document/layer compiler and cache remain parent-owned for a later checkpoint.

The operator subset, 64-level recursion bound, numeric/color interpolation,
literal font arrays, missing-property handling and short-circuit/fallback behavior
are preserved. This remains an application-owned style interpreter, not a complete
MapLibre expression engine or a total work/output budget for arbitrary styles.
Returned literal/property arrays/maps may alias immutable inputs.

New regressions exposed two existing correctness bugs, fixed in this checkpoint:

- Interface equality on array/map values could panic; non-comparable values now
  compare unequal, including structs containing non-comparable interface fields.
- Four/eight-digit hex parsing incorrectly read blue from the alpha byte. It now
  extracts the correct blue byte (`#01020304` is RGBA 1,2,3,4).

All three final `GOAMD64=v1` captures remain byte-for-byte identical to their
references: 45 ordered draws, 62 labels, complete fonts and unchanged geometry.
Headless style coverage is **100.0%**, with amd64/386, full v4 normal tests, v1
integration/race tests, targeted staticcheck and application build passing. A
20-second JSON fuzz run completed **1,306,828 executions** without failure. Existing
generated ST1006 and standard-library GO-2026-5024 baseline findings remain as
previously recorded. Gopls reports no new errors. Complexity review retains the
original explicit dispatcher (43) and comparison dispatch (now 20); separating
operator groups is a possible focused follow-up, not part of this extraction.

The first v4 direct-fixture comparison found a small-byte but significant-count
allocation regression: 102,313–102,314 before versus 106,715 after, with roughly
5 KB extra allocated. Profiling identified `reflect.Value.Comparable` on Color:
Go 1.27 iterates struct fields through an allocating iterator. A scalar fast path
now bypasses reflection for Color and the normal primitive values, retaining the
safe fallback for composites. Allocation count is back to the original range.

Before/after/after/before runs used separate v4 test binaries, identical pinned
tile/glyph bytes, Go 1.27.1, GOMAXPROCS 16 and two-second direct-fixture samples:

| Run | Before time/op | After time/op | Before / after allocations |
| --- | ---: | ---: | ---: |
| Initial, reflective guard | 30.96 / 30.39 ms | 29.95 / 30.06 ms | 102,313–102,314 / 106,715 |
| Scalar fast path, variable samples | 45.30 / 36.92 ms | 32.23 / 32.68 ms | 102,314–102,317 / 102,310–102,312 |
| Final repeat | 32.66 / 35.45 ms | 31.50 / 33.40 ms | 102,312–102,313 / 102,311–102,313 |

Final allocation volume is about **90.75 MB/op** in both versions. Slow samples
are retained; this establishes removal of the allocation regression, not a robust
preparation speedup or rendering-parity claim. The separate synthetic filter
benchmark measured **42.65–47.69 ns/op, zero allocations** for one small filter.
It does not measure complete style compilation, map preparation or GPU work.

### Headless document and layer preparation

Kata **hbxm** extracts the existing version-8 document model and layer compiler
into `style.Parse` and `style.Compile`. `CompiledLayer` provides `VisibleAt`,
`Hidden`, `Matches` and `Value`. Parent type aliases share compiled slices/maps;
the pinned JSON asset, generator and once/cache policy stay in vecmap. Preparation
uses the existing standard-library JSON decoder and expression evaluator.

Order includes duplicate IDs and unsupported layer types. Zoom ranges remain
half-open with defaults of zero/infinity, paint shadows layout even for null or
failed expressions, and hidden visibility recognizes only literal layout `none`.
Compilation now returns nil on any error instead of exposing earlier compiled
layers. `Parse` owns decoded data; `Compile` borrows its already-decoded filter
expressions but owns paint/layout output and copies zoom bounds. This remains an
application-owned style subset, with caller-owned input/work policy.

Verification on Go 1.27.1:

- Headless style tests retain **100.0% coverage** on amd64 and pass on 386.
  Regressions cover order/duplicates, the pinned 111-layer style, ownership, zoom
  boundaries, filter/value/visibility semantics and atomic malformed-JSON failures.
- Full v4 module coverage tests, v1 parent/style integration-race tests, targeted
  staticcheck and application build pass. Gopls reports no build errors.
- Expanded/direct/post-indexed v1 captures are byte-for-byte identical to their
  references: 45 draws, 62 labels, complete fonts and unchanged geometry counts.
- Repository-wide staticcheck retains the existing generated MIQT ST1006 warnings;
  the pre-edit scan still reports standard-library GO-2026-5024. New document
  preparation functions are all below the complexity-review threshold of 11;
  the earlier expression dispatch is unchanged.

Separate v4 binaries from immediately before/after this checkpoint ran the direct
fixture benchmark in before/after/after/before order, with the same pinned assets,
GOMAXPROCS 16 and two-second samples:

| Version | Time/op | Allocated bytes/op | Allocs/op |
| --- | ---: | ---: | ---: |
| Before document extraction | 45.70 / 32.33 ms | 90,749,369 / 90,747,860 | 102,314 / 102,311 |
| After document extraction | 40.46 / 33.95 ms | 90,749,254 / 90,748,892 | 102,314 / 102,314 |

Allocation cost is preserved at about 90.75 MB and 102,311–102,314 allocations.
Timing varies substantially; these samples do not establish a speedup. The warm
cache benchmark checks full preparation and layer evaluation, not cold JSON
compilation, I/O, GPU work or MapLibre parity. Glyph/atlas/layout/placement and
scene compilation are the next headless boundaries; the fixture producer still
imports the Qt-bound parent package.

### Headless glyph decoding and atlas preparation

Committed the preceding line/MVT/expression/document checkpoints as `c909c3c`.
Kata **sp7z** continues with `pkg/vecmap/glyph`: `DecodeRange` returns owned glyph
metrics/bitmaps, and `BuildAtlas` returns a single-channel image plus logical glyph
rectangles. Parent aliases share those values without conversion maps or extra
bitmap copies. The existing PBF reader and shelf packer are reused. Font fetching,
retry/cancellation/cache policy, retained-atlas selection, text layout and Qt
fallback remain caller-owned.

The decoder preserves server fallback-stack naming, matching by requested range,
required metrics, signed bearings, duplicate rejection and atomic errors. It now
enforces the existing 2 MiB download limit directly, validates aligned BMP range
starts before range-end arithmetic, and copies only the final validated bitmap
field. Existing limits are eight stacks, 256 glyphs per stack, dimensions/advance
up to 255, bearings -128 through 127 and a three-texel PBF border.

Atlas packing retains descending height/width, ascending font/code-point order,
256–2048 square sizes, one extra zero guard texel, and the deterministic prefix
that fits at the maximum size. The shared entry point validates bitmap sizes and
metrics and omits bitmap-free glyphs; callers check completeness before rendering
layouts. Pixel output is bounded to 4 MiB, while input cardinality and sorting/
temporary-map work remain caller-owned. This is not live placement/upload work.

Verification on Go 1.27.1:

- Headless glyph coverage **99.4%**; the only uncovered statement is the atlas
  loop's unreachable final return. Tests cover parser/metric boundaries, repeated
  fields, ownership, empty glyphs, deterministic packing, every bitmap/guard pixel,
  partial atlases and invalid data. Local Regular/Bold/Italic ranges pass headless
  decoding on amd64 and 386.
- A 20-second headless fuzz run completed **663,217 executions** without failure.
- Full v4 module coverage tests, v1 vecmap/glyph integration-race tests, targeted
  staticcheck, application build and gopls diagnostics pass. Existing generated
  ST1006 and standard-library GO-2026-5024 baseline findings remain unresolved.
- All three v1 scene captures remain byte-for-byte identical: 45 draws, 62 labels,
  complete fonts and unchanged geometry/texture data. No GPU algorithm or binding
  changes are involved; this verifies CPU output rather than new GPU timings.
- The former glyph decoder (complexity 32) is split into traversal, field decoding and
  metric validation. Complexity review flags range/field/atlas functions at 14 and
  metric validation at 11; the explicit validation branches are retained.

Separate v4 binaries from immediately before/after extraction ran with the same
pinned tile/glyph inputs, GOMAXPROCS 16 and two-second direct-fixture samples:

| Sequence | Before time/op | After time/op | Before / after allocations |
| --- | ---: | ---: | ---: |
| Before/after/after/before | 51.88 / 66.18 ms | 55.90 / 60.70 ms | 102,310–102,311 / 102,313–102,316 |
| After/before/before/after | 51.11 / 51.09 ms | 51.99 / 54.25 ms | 102,311–102,312 / 102,313–102,316 |

Allocation volume remains about **90.75 MB/op** in both binaries. Samples are
slower than earlier checkpoints in both versions, with drift and slightly slower
after samples in the second sequence. These runs establish no speedup or precise
timing equivalence; they show comparable allocation cost. The benchmark includes
CPU preparation, excludes I/O/JSON/GPU work, and is not a MapLibre comparison.
Text layout, placement/collision and scene compilation remain the next headless
boundaries; the fixture producer still imports the Qt-bound parent.

### Headless glyph-metric text layout

Committed glyph decoding/atlas preparation as `049342f`, then continued with kata
**hk7t**. `glyph.LayoutText` now owns the existing metric layout, line breaking,
measurement, anchor/justification alignment, positioned glyphs and halo bounds.
The parent adapter passes evaluated options and shares the returned positioned
slice through an alias, copying only the small bounds value. Text eligibility,
normalization and Qt fallback policy, atlas-dependent quads, collision/placement
and scene packing remain caller-owned.

This reuses the existing algorithm, including its 24-unit em, -17 baseline,
whitespace/paragraph handling, overlong-word splitting, negative spacing, line
height fallback, anchor substring rules and scaled halo clamp. It adds no shaping
dependency and does not implement OpenType shaping, bidi or kerning. The shared
entry point enforces the existing 4096-byte/256-rune text limits before allocation,
rejects invalid UTF-8 and nonpositive text sizes, and rejects nonfinite options or
overflowing positioned/bounds arithmetic. Failures return a zero layout, never a
partial slice. Glyph metrics are copied by value; bitmaps remain immutable borrows.

Verification:

- Headless glyph coverage is **99.7%**, with all new layout functions at **100%**.
  Regressions cover metrics/halo bounds, multiline anchors/justification, spacing,
  wrapping and blank paragraphs, missing/bitmap-free glyphs, ownership, exact text
  limits, UTF-8 and floating-point overflow. Headless 386 tests pass.
- A 20-second headless layout fuzz run completed **281,047 executions** with no
  failures. Full v4 module tests, v1 parent/glyph integration-race checks, targeted
  staticcheck and application build pass; gopls reports no build errors.
- Expanded, direct-indexed and post-indexed v1 captures are byte-for-byte identical
  to their references: 45 draws, 62 labels, complete fonts and unchanged geometry.
- Repository-wide staticcheck retains only the generated MIQT ST1006 warnings.
  The earlier GO-2026-5024 toolchain baseline remains unresolved. Complexity review
  retains the original line-break state machine (18); layout scores 16 including
  the new finite-output guards. Separating line-break transitions is a possible
  focused refactor, rather than part of this behavior-preserving extraction.

Separate before/after v4 binaries used the same pinned tile/glyph inputs and
GOMAXPROCS 16. Direct-fixture preparation samples, including the initially slower
after measurements:

| Sequence / duration | Before time/op | After time/op | Before / after allocations |
| --- | ---: | ---: | ---: |
| Before/after/after/before, 2 s | 32.38 / 32.52 ms | 34.71 / 35.22 ms | 102,311–102,312 / 102,313–102,315 |
| After/before/before/after, 3 s | 34.63 / 34.50 ms | 34.63 / 35.34 ms | 102,313–102,314 / 102,313 |

Both versions allocate about **90.75 MB/op**. The first sequence showed a 2–3 ms
after difference; the reversed sequence mostly overlapped. These measurements
support comparable allocation cost but do not establish a speedup or precise
timing equivalence. They measure CPU preparation, excluding I/O/JSON/GPU work;
MapLibre-quality and live-presentation gates remain open. Next extraction targets
are symbol candidate preparation, placement/collision and scene compilation.

### Headless feature-anchor preparation

Committed the text-layout checkpoint as `026e54d`, then continued with kata
**zxys**. `placement.FeatureAnchors` now selects point, polygon-centroid, line-center
and repeated-line anchors from shared MVT features. `LineAnchor`,
`RepeatedLineAnchors` and `PolygonCentroid` expose the existing geometry helpers;
the parent aliases `Anchor` so no result conversion slices are needed.

The source ordering, fallback selection, incoming-segment vertex direction,
unnormalized upright/raw angles, epsilon behavior and centroid arithmetic are
preserved. The centroid remains an exterior-ring calculation, not an interior
label-point solver. Its near-zero-area fallback retains the original residual
numerator behavior. Text/icon paint, keep-upright choices, collision and overall
tile candidate budgets remain caller-owned.

A correctness fix clamps repeated-anchor counts to 16 before converting to `int`.
The old conversion could overflow for very small positive spacing and collapse
the count to one, including on 386. NaN spacing/fractions and nonfinite line lengths
now reject cleanly. Total input/output work remains bounded by caller-owned MVT
source cardinality, with the existing per-line cap; this is not live scheduling.

Verification:

- Headless amd64/386 tests pass with **100% placement coverage**. Regressions cover
  source selection/order, ownership, clamped fractions, midpoint/vertex directions,
  upright angles, degenerate/tiny segments, tiny spacing and centroid fallbacks.
- A 20-second headless fuzz run completed **853,014 executions** without failure.
- Full v4 module coverage tests, v1 vecmap/placement integration-race checks,
  targeted staticcheck and application build pass; gopls reports no build errors.
  Existing generated ST1006 and standard-library GO-2026-5024 baseline findings
  remain unresolved. Complexity review retains the original selection dispatcher
  (11); other new production functions are at most 10.
- All three v1 captures remain byte-for-byte identical: 45 draws, 62 labels,
  complete fonts and unchanged geometry/texture data. GPU algorithms and generated
  adapters are unaffected by this CPU extraction.

Separate v4 test binaries from immediately before/after extraction ran in
before/after/after/before order, with identical pinned tile/glyph inputs,
GOMAXPROCS 16 and two-second direct-fixture samples:

| Version | Time/op | Allocated bytes/op | Allocs/op |
| --- | ---: | ---: | ---: |
| Before anchors | 61.70 / 46.63 ms | 90,751,305 / 90,749,038 | 102,319 / 102,312 |
| After anchors | 52.74 / 54.01 ms | 90,750,613 / 90,750,023 | 102,316 / 102,315 |

Both allocate about **90.75 MB/op**. Timing varied substantially across the control
runs; no speedup or precise timing equivalence is established. These are CPU
preparation measurements, excluding I/O/JSON/GPU work, rather than full-map parity
evidence. The next boundaries are evaluated symbol candidate preparation,
collision/priority decisions and scene compilation.

### Headless evaluated symbol candidates

Committed anchors as `2aabbcf`, then continued with kata **0yzm**.
`placement.PrepareSymbols` now applies feature filters, bounded token/text
preparation, font stacks, source-zoom spacing and per-anchor text/icon paint. It
streams Go-only `Symbol` values to a caller-owned sink in feature/anchor order.
Vecmap retains its renderer representation through a small value adapter, without
an intermediate candidate slice or map/string-payload copies. Other consumers
can retain the shared Symbol values directly.

Typed paint helpers moved into standard-library-only `style.CompiledLayer` methods
and are reused by both symbol preparation and the parent fill/line compiler.
Existing defaults, finite scalar-number checks, loose point-array components,
invalid-color distinctions and font-stack trimming/order are preserved. Text
expansion keeps its 4096-byte/256-rune and 256-substitution limits, nonrecursive
replacement and missing-property semantics. Text transformation/normalization and
icon expansion retain their original order.

The caller selects visible symbol layers and supplies remaining tile capacity.
The shared API caps it at 10,000; an additional candidate returns `ErrSymbolLimit`
without undoing earlier sink calls. Sink errors stop emission and propagate.
The parent maps the limit identity to its existing feature-resource error and
preserves partial candidates, covered by a new adapter regression. Text
eligibility/Qt fallback, collision acceptance and priority, atlas-dependent quads
and scene packing remain parent-owned.

Verification:

- Headless placement/style coverage is **100%**, with amd64/386 tests passing.
  Regressions cover complete default/custom symbol paint, filter/order behavior,
  text/icon transformations, font stacks, alignment/upright angles, zoom spacing,
  malformed values, owned numeric arrays, budgets and sink errors.
- A 20-second symbol-text fuzz run completed **389,271 executions** without failure.
- Full v4 module coverage tests, v1 vecmap/placement/style integration-race tests,
  targeted staticcheck and application build pass. Gopls reports no build errors.
  Repository-wide staticcheck retains the generated MIQT ST1006 warnings, and the
  earlier standard-library GO-2026-5024 baseline remains unresolved.
- All three v1 captures are byte-for-byte identical: 45 draws, 62 labels, complete
  fonts and unchanged geometry/textures. This verifies CPU output; QRhi algorithms
  and generated bindings are unchanged.
- Complexity review flags the streaming preparation loop at 12 (the previous
  combined compiler scored 15); its evaluation helper and new typed helpers are
  at most 10. Existing expression/anchor complexity remains documented above.

Separate before/after v4 binaries used identical pinned inputs and GOMAXPROCS 16:

| Sequence / duration | Before time/op | After time/op | Before / after allocations |
| --- | ---: | ---: | ---: |
| Before/after/after/before, 2 s | 34.50 / 35.41 ms | 41.85 / 44.65 ms | 102,313 / 102,313–102,316 |
| After/before/before/after, 3 s | 32.28 / 31.42 ms | 31.22 / 31.37 ms | 102,311–102,313 / 102,312–102,315 |

Both versions allocate about **90.75 MB/op**. The first sequence suggested a
slowdown, but the reversed sequence did not reproduce it. Both sequences are
retained; they establish comparable allocation cost, not a robust speedup or exact
timing equivalence. The benchmark excludes I/O, JSON and GPU work. Collision/
priority decisions and scene compilation are the next CPU boundaries before
bounded live updates and matched-quality MapLibre validation.

### Headless collision and priority selection

Committed evaluated candidates as `4145029`, then continued with kata **h3nq**.
`placement.SelectSymbols` consumes Go-only projected text/icon boxes and policy
flags with opaque comparable keys. It reuses the existing stable sort, 64-pixel
collision grid and acceptance rules. The parent still collects references in its
original tile/wrap/reverse-candidate order and owns camera projection, text/glyph
readiness, sprite lookup and viewport visibility. Shared reference/acceptance
aliases avoid an extra scratch slice or acceptance-map conversion.

Order remains descending style layer, then ascending sort key, preserving input
ties. Overlap-enabled parts neither test nor occupy the grid; a candidate's own
parts do not block each other. Required unavailable/colliding text suppresses its
icon; required colliding icons suppress text, while unavailable icons do not.
Strict edge intersections and the original floored cell traversal are preserved.

The new boundary enforces the existing 100,000-reference ceiling and adds finite
sort-key/visible-box validation, finite nonnegative viewport dimensions capped at
1,048,576 logical pixels, and a 1,000,000-operation grid budget. Cell visits and
occupied-box comparisons both count; insertion visits also bound stored box copies.
An optional smaller budget supports callers and failure tests. Clipped/floored
empty ranges are checked before integer conversion, preventing extreme offscreen
coordinates from overflowing cell indexes on 386. Validation/sorting are bounded
separately by reference count; caller-side projection is outside the grid budget.

Collision errors return no partial acceptance map. The input scratch slice may
already be sorted; the parent reports a warning and skips acceptance for that job.
This is a new bounded failure policy for extreme jobs, distinct from the preceding
streaming candidate compiler's partial-output policy. The production renderer and
scheduler remain in place.

Verification:

- Placement remains **100% covered** headlessly, with amd64/386 tests passing.
  Tests cover stable priority/ties, optional/overlap asymmetry, strict edges,
  clipping, extreme coordinates, inverted-range compatibility, every exhaustion
  path and atomic errors. Grid text selection matches an independent brute-force
  reference. The parent regression verifies failure reporting and nil acceptance.
- A 20-second headless collision fuzz run completed **787,808 executions** without
  failure. Full v4 module tests, v1 vecmap/placement integration-race checks,
  targeted staticcheck and application build pass. Gopls reports no build errors.
- All three v1 captures remain byte-for-byte identical: 45 draws, 62 labels,
  complete fonts and unchanged geometry/texture data. No fresh GPU timing claim is
  made for this CPU extraction.
- Existing generated ST1006 warnings and the GO-2026-5024 vulnerability baseline
  remain unresolved. Complexity review retains explicit acceptance dependencies
  and budget propagation (`accept` 20, `SelectSymbols` 13); parent collection drops
  from the combined function's 33 to 11. Policy decomposition can be a focused
  follow-up without changing these tested rules.

Separate before/after v4 binaries used the same pinned inputs, GOMAXPROCS 16 and
two-second direct-fixture samples in before/after/after/before order:

| Version | Time/op | Allocated bytes/op | Allocs/op |
| --- | ---: | ---: | ---: |
| Before collision extraction | 40.87 / 47.31 ms | 90,749,434 / 90,749,459 | 102,314 / 102,314 |
| After collision extraction | 44.40 / 45.60 ms | 90,781,854 / 90,782,195 | 102,314 / 102,315 |

The reference records now carry priority/policy values rather than a pointer to
the parent candidate. Their larger scratch representation adds about **33 KB**
allocated per fixture (roughly 0.04%), with the same allocation-count range and
unchanged retained scene size. Timings vary across the controls; these measurements
establish no speedup or precise timing equivalence. They exclude I/O/JSON/GPU work.
Next CPU boundaries are projected collision boxes/readiness policy, atlas-dependent
quads and scene compilation, followed by bounded live updates and parity gates.

### Headless projected symbol boxes

Committed collision selection as `1fa1e48`, then continued with kata **madt**.
`placement.ProjectSymbol` now prepares projected glyph/fallback text and icon boxes
from shared Symbol values, `view.Affine`, explicit text readiness/bounds and optional
sprite metrics. It returns ready-to-select `CollisionPart` values, retaining no
input pointers or native resources. The parent supplies its existing Qt fallback
eligibility, SDF bounds and pinned sprite availability through value adapters.

The extraction preserves logical-pixel sizes and offsets during camera scaling,
map/viewport alignment, affine screen angles, halo handling, signed padding,
inclusive visibility, anchor substrings and signed-zero arithmetic. Supplied text
bounds already include the glyph halo, while fallback text uses the prior
character/line estimate. Sprite presence still follows name/metric availability
and positive pixel ratio, independently of icon opacity or color alpha.

Fallback line count now stays floating point instead of converting a potentially
huge wrap ratio to `int`. Tiny positive maximum widths therefore avoid the previous
architecture-dependent overflow/collapse. Normal estimates and the pinned fixture
remain unchanged. This remains a fallback estimate, not a new shaping engine;
callers provide bounded evaluated symbols and readiness snapshots, and the selector
retains finite visible-box/work validation.

Verification:

- Headless placement coverage remains **100%**, with amd64/386 tests passing.
  New cases cover explicit readiness, supplied glyph bounds, fallback wrapping,
  pixel ratios/offsets, map/viewport alignment, raw icon presence, pointer ownership,
  signed padding/zeros, viewport edges and large fallback estimates.
- A 20-second projection-to-selection fuzz run completed **1,113,520 executions**
  without failure. Full v4 module tests, v1 vecmap/placement integration-race checks,
  targeted staticcheck and application build pass; gopls reports no build errors.
- Expanded, direct-indexed and post-indexed v1 captures are each byte-for-byte
  identical to their references: 45 draws, 62 labels, complete fonts and unchanged
  geometry/texture data. GPU algorithms and generated adapters are unaffected.
- All new production projection functions are below the complexity threshold of
  11. Existing generated ST1006 and GO-2026-5024 baseline findings remain unresolved.

Separate v4 binaries from before/after extraction ran in before/after/after/before
order, using identical pinned inputs, GOMAXPROCS 16 and two-second direct-fixture
samples:

| Version | Time/op | Allocated bytes/op | Allocs/op |
| --- | ---: | ---: | ---: |
| Before projection extraction | 33.54 / 32.97 ms | 90,781,183 / 90,780,938 | 102,313 / 102,312 |
| After projection extraction | 32.97 / 32.99 ms | 90,781,688 / 90,780,921 | 102,315 / 102,312 |

Both remain about **90.78 MB/op**, with the same allocation-count range and similar
preparation times. This supports preserving cost rather than a speedup claim.
The benchmark excludes I/O, JSON and GPU work and does not establish rendering
parity. The remaining CPU boundaries include text eligibility/fallback policy,
atlas-dependent quads, asset preparation and scene compilation, followed by
bounded live updates and matched-quality MapLibre validation.

### Headless SDF eligibility and glyph meshes

Committed projected boxes as `59d24c0`, then continued with kata **gpwj**.
`glyph.TextEligible`, `FitsAtlas`, `BuildLayoutMesh` and `EmitLayoutQuads` now own
the existing SDF script filter, drawable-glyph atlas coverage, quad arithmetic and
expanded/direct-indexed assembly. They reuse `geometry.TextQuad` and `Builder`;
the parent passes shared positioned slices and retains scene orchestration and
Qt-specific fallback policy.

The original padding, normalized UVs, signed zeros and triangle order are preserved.
`LayoutMesh` carries either packed expanded XYUV floats or direct vertices/indices,
avoiding another conversion buffer. Missing entries and bitmap-free glyphs are
skipped by low-level geometry preparation; callers use `FitsAtlas` to require
complete labels. Mesh errors publish no partial output, while streaming sinks
retain earlier successful emissions. Parent scene preparation now propagates
invalid mesh errors before attaching new geometry.

The shared entry point caps positioned glyphs at 256 before capacity arithmetic,
checks scale/atlas dimensions, validates rectangle extents before integer addition,
and rejects nonfinite float32 vertices. Atlas pixel bytes are not read. Eligibility
preserves the existing script/BMP/mark rules and now explicitly rejects invalid
UTF-8; the Qt fallback adapter also rejects invalid bytes so they cannot bypass
SDF rejection. This adds no new shaping engine or renderer migration.

Verification:

- Glyph coverage remains **99.7%**, with new eligibility/mesh functions at **100%**.
  Headless amd64/386 tests cover script selection, packed/indexed bit reconstruction,
  ownership, missing/bitmap-free glyphs, nil inputs, exact limits, invalid atlas
  coordinates, float overflow, signed zeros and atomic/streaming failure behavior.
- A 20-second headless mesh fuzz run completed **1,235,603 executions** without
  failure. Full v4 module tests, v1 vecmap/glyph integration-race checks, targeted
  staticcheck and application build pass; gopls reports no build errors.
- All three v1 captures remain byte-for-byte identical: 45 draws, 62 labels,
  complete fonts and unchanged geometry/texture data. GPU code and generated
  adapters are unchanged.
- Complexity review retains the explicit script filter (13 including new input
  guards) and quad emission/validation (11); other new production functions are
  at most 10. Existing generated ST1006 and GO-2026-5024 baseline findings persist.

Separate v4 test binaries ran before/after/after/before with identical pinned
tile/glyph inputs, GOMAXPROCS 16 and two-second direct-fixture samples:

| Version | Time/op | Allocated bytes/op | Allocs/op |
| --- | ---: | ---: | ---: |
| Before glyph mesh extraction | 32.69 / 32.91 ms | 90,780,770 / 90,780,934 | 102,311 / 102,312 |
| After glyph mesh extraction | 32.88 / 34.63 ms | 90,780,330 / 90,782,233 | 102,311 / 102,316 |

Allocation volume remains about **90.78 MB/op**, with the same allocation-count
range. Timing samples, including the slower after sample, are retained without a
speedup claim. This measures CPU preparation, excluding I/O/JSON/GPU work, rather
than full-map parity. Asset preparation and scene compilation are the next CPU
boundaries; toolkit-specific font fallback remains an adapter concern.

### Headless sprite asset preparation

Kata **m5dy** continues after the verified, still-uncommitted **gpwj** checkpoint.
`sprite.Decode` and `sprite.Prepare` now own JSON/PNG decoding, NRGBA conversion,
entry validation and crop/tint/opacity arithmetic. The parent aliases entry metadata
and shares decoded maps and prepared pixel slices directly. Embedding, `sync.Once`,
the mutex-protected 512-entry FIFO cache and its four-decimal opacity key remain
parent-owned. This uses standard-library image/JSON code and shared `style.Color`,
with no module or generated binding changes.

The decoder caps JSON at 2 MiB, PNG at 16 MiB and entry occurrences at 4096,
including duplicate names. Header dimensions are checked before PNG pixel decoding:
at most 4096 per axis and 4,194,304 pixels total (16 MiB normalized RGBA storage).
Final metadata requires finite positive ratios and positive rectangles within the
atlas. Crop preparation validates zero-origin NRGBA stride/storage and rectangle
arithmetic without 32-bit overflow. Decode errors return nil; crop failures return
a zero image. Invalid metadata now fails the whole decode and NaN opacity rejects;
valid RGB/SDF alpha rounding, tint byte conversion and clamped opacity are retained.

Verification on Go 1.27.1:

- Headless sprite coverage is **100%**; amd64/386 tests cover all 264 pinned sprites,
  color-model conversion, crop/stride arithmetic, RGB/SDF alpha rounding, ownership,
  malformed input/storage, byte/count/dimension bounds and atomic failures.
- Twenty-second fuzz runs completed **1,046,011 decoder executions** and
  **1,738,238 crop executions**, with no failures.
- Full v4 module coverage tests, v1 vecmap/sprite integration-race checks and the
  application build pass. Gopls reports no new build errors. Repository-wide
  staticcheck still reports only existing generated MIQT ST1006 warnings; the
  pre-edit scan still reports standard-library GO-2026-5024. New production
  functions are all at most 10 in complexity review.
- Expanded/direct/post-indexed v1 captures remain byte-for-byte identical to the
  original references: 45 draws, 62 labels, complete fonts and unchanged geometry
  and textures. This verifies CPU output, not fresh GPU/presentation performance.

Separate v4 binaries built immediately before/after this extraction ran the direct
fixture benchmark in before/after/after/before order, with identical pinned inputs,
GOMAXPROCS 16 and two-second samples:

| Version | Time/op | Allocated bytes/op | Allocs/op |
| --- | ---: | ---: | ---: |
| Before sprite extraction | 28.83 / 27.81 ms | 90,781,199 / 90,780,706 | 102,314 / 102,312 |
| After sprite extraction | 28.28 / 27.98 ms | 90,781,616 / 90,781,321 | 102,315 / 102,314 |

Allocation cost remains about **90.78 MB/op**. These overlapping samples do not
establish a speedup. The fixture benchmark uses warm sprite caches, so a separate
headless benchmark compares cold decoding of the pinned index/PNG against the
former `json.Unmarshal` + PNG decode + NRGBA conversion. Same v4/GOMAXPROCS setting,
two two-second samples, excluding file I/O:

| Cold decode | Time/op | Allocated bytes/op | Allocs/op |
| --- | ---: | ---: | ---: |
| Former decoder | 1.223 / 1.231 ms | 1,222,421 / 1,222,365 | 291 / 291 |
| Bounded decoder | 1.285 / 1.279 ms | 1,249,070 / 1,249,079 | 912 / 912 |

Header validation and streaming entry bounds add about **27 KB and 621 allocations**
per cold asset load (roughly 0.05–0.06 ms in these samples). The existing once/cache
policy pays this at initial loading, not each tile preparation. All timing samples
are retained; neither benchmark measures GPU work, presentation or MapLibre parity.
Scene compilation and asset/layout orchestration remain the next CPU boundary
before a fully headless fixture producer and bounded live updates.

### Headless fill and line layer compilation

Committed glyph meshes and sprites as `8b797b1`, then continued with kata **zfjf**.
`compiler.CompileFill` and `CompileLine` now evaluate paint and batch shared MVT
features before calling the existing topology builder and line tessellator.
The parent passes source slices directly and attaches layer identity in synchronous
sinks, without conversion slices/maps or another preparation pass.

Solid/pattern/outline ordering, first-seen batch order, extrusion-as-flat-fill,
outline opacity, gap offsets, screen-to-tile scale, ring closure and triangle order
are preserved. Line keys keep their nine-digit width/offset precision and full
dash strings. The dash-array helper intentionally rejects negative components,
unlike the general style number-array helper. Layer visibility/source selection,
tile-wide triangle accounting and atomic primitive publication stay caller-owned.

The shared options reject invalid derived scales, out-of-range triangle limits and
nil sinks. A caller can lower the existing 2,000,000-triangle **per-batch** bound;
zero selects that default. Aggregate work/memory still relies on bounded MVT data,
application-owned styles and the caller's tile budget. Earlier successful sink
calls remain accepted on a later error, including an outline failure after valid
fills. Geometry errors retain the existing MVT feature-resource identity.

Verification:

- Headless compiler coverage is **100%**, with amd64/386 tests passing. Tests cover
  paint/defaults, ordering, patterns, outlines/holes, extrusion, gaps/dashes, key
  precision, ownership, lowered limits and streaming errors. Pinned-tile tests
  compare every expanded/indexed coordinate bit at zooms **10 and 10.5**.
- A twenty-second headless fuzz run completed **1,109,914 executions** without
  failure. Full v4 module coverage tests, v1 parent/compiler integration-race tests,
  targeted staticcheck, application build and gopls build diagnostics pass.
- All three regenerated v1 captures are byte-for-byte identical to the original
  references: 45 draws, 62 labels, complete fonts and unchanged geometry/textures.
  GPU algorithms and generated bindings are unaffected.
- Complexity review retains the existing explicit fill grouping loop (24, down
  from 26) and line grouping (13, down from 16); helpers are at most 10. Splitting
  fill accumulation by batch kind is a possible focused follow-up. Unused legacy
  paint fields and value adapters exposed by extraction were removed. The generated
  ST1006 warnings and earlier GO-2026-5024 vulnerability baseline remain unresolved.

Separate before/after v4 binaries ran with identical pinned inputs, GOMAXPROCS 16
and two-second direct-fixture samples in before/after/after/before order:

| Version | Time/op | Allocated bytes/op | Allocs/op |
| --- | ---: | ---: | ---: |
| Before layer compiler extraction | 30.51 / 28.93 ms | 90,781,657 / 90,780,584 | 102,315 / 102,312 |
| After layer compiler extraction | 29.47 / 29.96 ms | 90,780,495 / 90,782,236 | 102,311 / 102,317 |

Both allocate about **90.78 MB/op**, with overlapping preparation times. This
supports comparable cost, not a speedup or MapLibre parity claim. The benchmark
excludes I/O, JSON and GPU work. Tile-wide compilation/retained primitive data,
asset/layout orchestration and final scene packing remain the next headless
boundaries before live scheduler/upload work and matched-quality rendering gates.

### Headless tile geometry orchestration

Committed layer compilation as `8f7ece7`, then continued with kata **fzr1**.
`compiler.CompileTile` now owns visible-layer traversal, background paint/geometry,
shared `Primitive` values and aggregate rendered-triangle accounting. It calls the
existing headless fill/line compilers and dispatches visible symbol layers through
an optional synchronous callback in their original position. The parent retains
symbol storage/budgets and stages its primitive slice until the whole call succeeds.

A value adapter shares mesh slices and immutable names directly, without another
primitive slice or geometry pass. Shared consumers can retain `Primitive` directly;
the legacy renderer still uses its own small record. `BackgroundGeometry` serves
both the tile compiler and existing fallback background with the same 256-unit
rectangle, diagonal and expanded/indexed triangle order.

The existing 2,000,000-triangle tile cap now lives in the shared compiler. A lower
`TriangleLimit` applies both per batch and across emitted tile geometry. Empty
meshes, zero-alpha solids and missing-name/nonpositive-opacity patterns are filtered
before counting. Indexed output counts indices, expanded output counts vertices;
symbol meshes remain separately budgeted. This is an emitted-geometry bound, not
a total scratch-memory/expression-work budget. MVT/style/caller input contracts
remain required. Invalid options reject before traversal, including empty jobs.

Callbacks retain earlier successful effects on later errors. The parent keeps its
existing atomic primitive publication and partial-symbol behavior; a new regression
checks that malformed later geometry publishes neither old primitives nor an
earlier valid background. Error text is compiler-neutral while the resource-error
identity is preserved. Renderer/scheduler and generated bindings are unaffected.

Verification:

- Headless compiler coverage remains **100%**, including every new assembly and
  background function. amd64/386 tests cover mixed-layer ordering/visibility,
  background paint/topology/ownership, budgets across batch kinds and layers,
  filtering, malformed triangles, nil callbacks and streaming failures.
- Full v4 module coverage tests, v1 parent/compiler integration-race checks,
  targeted staticcheck and application build pass; gopls reports no new build
  errors. Repository-wide staticcheck retains the generated ST1006 baseline;
  the previously recorded GO-2026-5024 finding remains unresolved.
- All three regenerated v1 captures are byte-identical to the original references:
  45 draws, 62 labels, complete fonts and unchanged geometry/texture bytes.
- New production functions are all at most 10 in complexity review. This is CPU
  output verification, not a new GPU or presentation measurement.

Separate v4 before/after test binaries used identical pinned tile/glyph inputs,
GOMAXPROCS 16 and two-second direct-fixture samples, before/after/after/before:

| Version | Time/op | Allocated bytes/op | Allocs/op |
| --- | ---: | ---: | ---: |
| Before tile assembly extraction | 29.30 / 30.70 ms | 90,780,937 / 90,781,787 | 102,313 / 102,315 |
| After tile assembly extraction | 30.35 / 30.40 ms | 90,782,025 / 90,781,602 | 102,316 / 102,315 |

Allocation volume remains about **90.78 MB/op** and timing ranges overlap; no
speedup is established. All samples are retained. These exclude I/O, JSON and GPU
work and do not establish MapLibre parity. Next boundaries are shared symbol/layout
orchestration and final scene packing toward a headless fixture producer, followed
by bounded live updates and matched-quality rendering/presentation gates.

### Headless retained glyph atlas selection

Committed tile orchestration as `8eabfae`, then continued with kata **d22h**.
`glyph.BuildRetainedAtlas` now implements the current-only versus merged-retained
selection policy; `AtlasNeedsRebuild` exposes the existing key coverage check.
Current glyphs override old values. Optional prior glyphs are kept only if the
merged atlas does not grow and retains every current key. Otherwise preparation
falls back to the current-only atlas, including its deterministic partial-prefix
behavior at maximum size. Complete label coverage still requires `FitsAtlas`.

Inputs and bitmap data are not copied unnecessarily. The atlas and resident map
are owned; resident metrics are value copies and bitmap slices are immutable
borrows. Invalid current input returns an error with nil outputs; invalid optional
retained input causes current-only fallback. Empty/bitmap-free current input
returns nil without examining retained data. The parent preserves nil-atlas
degradation for invalid current data and still owns scheduling/retained state.
The two original packing passes, caller-bounded input/merge/sort work and 2048-square
pixel bound remain. Key coverage is not a metric/pixel revision check.

Verification:

- New functions have **100% coverage**, overall glyph coverage **99.8%**. Tests
  cover current-wins merging, ownership/borrowing, empty/invalid inputs, optional
  growth, same-size displacement, partial current output and bitmap-free keys.
  Headless amd64/386 tests pass.
- Full v4 module coverage tests, v1 parent/glyph integration-race checks, targeted
  staticcheck and application build pass; gopls reports no build errors. New
  functions are at most 10 in complexity review. Existing generated ST1006 and
  the recorded GO-2026-5024 baseline remain unresolved.
- All three v1 fixture captures remain byte-identical: 45 draws, 62 labels,
  complete fonts and unchanged geometry/textures. This is CPU compatibility
  evidence, not a new GPU or live-presentation measurement.

Separate v4 binaries ran before/after/after/before with identical pinned inputs,
GOMAXPROCS 16 and two-second direct-fixture preparation samples:

| Version | Time/op | Allocated bytes/op | Allocs/op |
| --- | ---: | ---: | ---: |
| Before retained atlas extraction | 29.08 / 29.27 ms | 90,780,980 / 90,780,962 | 102,313 / 102,313 |
| After retained atlas extraction | 29.42 / 29.87 ms | 90,782,120 / 90,781,926 | 102,315 / 102,315 |

Allocation volume remains about **90.78 MB/op**. The slightly slower after samples
are retained; no speedup is established. Measurements exclude I/O, JSON and GPU
work. Shared layout-map orchestration and scene packing remain the next boundaries
toward a headless fixture producer; live scheduling and MapLibre parity gates stay
open.

### Shared prepared layouts and mesh-set orchestration

Committed retained atlas selection as `c91d836`, then continued with kata **r5xn**.
`glyph.PreparedLayout` combines shared metric layout and atlas-dependent mesh
values. The parent aliases this model, passing layout maps directly into
`glyph.PrepareLayouts` and `LayoutGlyphs` without conversion maps/slices or extra
positioned-glyph buffers. Projection and legacy/fixture renderers now read the
shared fields; bounds are copied only at the projection boundary.

The generic mesh-set API preserves caller keys and layout pointers, filters through
whole-label atlas coverage, attaches successful geometry in place and returns an
owned map of drawable layouts. Successful mode switches replace all mesh fields;
missing coverage leaves old geometry untouched but excludes that label from the
result. Empty/bitmap-free layouts produce no renderable entry. Nil layouts are
skipped. Empty maps/nil atlases return nil. Nonempty jobs with an atlas are capped
at the existing **10,000 layout** ceiling before map allocation; glyph mesh limits
remain 256 positioned glyphs per label.

As before, failed jobs return nil output but do not roll back earlier successful
attachments; input map traversal order is unspecified. The failing layout retains
its previous mesh. Callers must prepare unpublished layouts with exclusive access,
then publish immutable data. Drawable-glyph collection owns its result map and
borrows bitmap bytes; cardinality and work remain caller-bounded. Repeated glyph
keys must describe identical data. The parent retains font resolution, candidate
selection, Qt fallback and rendering counters.

Verification:

- New functions have **100% coverage**; overall glyph coverage remains **99.8%**.
  Headless amd64/386 tests cover key/pointer identity, ownership, mode changes,
  metric preservation, missing coverage, empty glyphs, nil values, layout ceilings,
  mesh failures and drawable collection. A 20-second mesh-set fuzz run completed
  **867,083 executions** without failure.
- Full v4 module coverage tests, v1 parent/glyph integration-race checks and the
  application build pass. Gopls reports no new build errors. Repository-wide
  staticcheck reports only existing generated ST1006 warnings; the recorded
  GO-2026-5024 baseline remains unresolved.
- Expanded/direct/post-indexed v1 scene captures remain byte-identical to their
  original references: 45 draws, 62 labels, complete fonts and unchanged buffers.
- Complexity review retains the short sequential mesh-set loop (11 including
  bounds/filter/error branches); collection is below the threshold. GPU code and
  generated bindings are unaffected.

Separate v4 binaries ran before/after/after/before with identical pinned inputs,
GOMAXPROCS 16 and two-second direct-fixture preparation samples:

| Version | Time/op | Allocated bytes/op | Allocs/op |
| --- | ---: | ---: | ---: |
| Before shared layout sets | 32.57 / 30.95 ms | 90,780,970 / 90,781,763 | 102,313 / 102,315 |
| After shared layout sets | 30.97 / 30.28 ms | 90,781,402 / 90,781,163 | 102,314 / 102,314 |

Allocation volume remains about **90.78 MB/op** with the same allocation-count
range. Variable samples do not establish a speedup. This measures CPU preparation,
excluding I/O, JSON and GPU work, rather than MapLibre parity. Remaining extraction
includes candidate-to-layout/font orchestration and final scene packing so the
fixture producer can build without Qt/cgo, before live update and presentation gates.

### Headless font discovery and text requests

Committed shared prepared layouts as `279c65c`, then continued with kata **wxt5**.
`compiler.TextRequest` and its `Layout` method share candidate-to-metric-layout
adaptation between live and offline consumers. `FontStacks`, `DecodeFontRanges`,
`TextComplete` and `PrepareTextLayouts` extract the fixture's discovery, range-set
decoding and SDF-only layout preparation. They reuse existing glyph algorithms and
standard Go iterators, adding no module dependency.

The parent yields value requests with original tile/candidate keys, avoiding a
converted candidate slice or map. Font discovery retains sorted unique exact names,
including unsupported text and empty/untrimmed font identities. Availability checks
the original text before whitespace processing; only CR/LF need no glyph. Missing
glyphs or unsupported/invalid text record the font as missing, while a metric-layout
failure after complete coverage simply omits the label. Live font resolution and
Qt fallback remain adapter-owned; the low-level request Layout method does not
enforce the fixture's SDF-only policy.

Text preparation caps yielded requests at the existing 10,000-layout ceiling,
including duplicates/empty text, stops the iterator on overflow and returns nil
outputs with a geometry-limit error. Iterator work between yields, font-discovery
cardinality and aggregate font input bytes remain caller-bounded. Range decoding
retains per-PBF bounds and atomic font-context errors. Layout/map buffers are owned;
bitmap slices are immutable borrows. The fixture retains its single loader call
and compilation pass. Final scene packing remains parent-bound.

Verification:

- Compiler coverage remains **100%**, including new discovery/layout/range-set
  functions. Headless amd64/386 tests verify font identity, completeness before
  normalization, missing versus invalid-layout behavior, duplicate keys, early
  iterator stop, exact limits, ownership and all three pinned font ranges.
- Parent regressions preserve candidate indexes, propagate layout limits and retain
  the existing one-loader-call tests. Full v4 module tests, v1 parent/compiler
  integration-race checks, targeted staticcheck and application build pass.
- All three v1 captures remain byte-identical to the original references: 45 draws,
  62 labels, complete fonts and unchanged geometry/textures.
- New production functions are at most 10 in complexity review. Gopls reports no
  new build errors. Repository-wide staticcheck retains only generated ST1006
  warnings; the recorded GO-2026-5024 baseline remains unresolved.

Separate v4 test binaries ran before/after/after/before with identical pinned assets,
GOMAXPROCS 16 and two-second direct-fixture samples:

| Version | Time/op | Allocated bytes/op | Allocs/op |
| --- | ---: | ---: | ---: |
| Before text preparation extraction | 36.82 / 37.82 ms | 90,782,116 / 90,781,695 | 102,315 / 102,314 |
| After text preparation extraction | 37.28 / 37.72 ms | 90,780,955 / 90,781,264 | 102,319 / 102,319 |

Timing ranges overlap and volume stays about **90.78 MB/op**. The iterator-based
boundary has roughly four to five extra allocations per fixture in these samples;
there is no large conversion buffer. No speedup is claimed. Samples exclude file
I/O, JSON and GPU work, and establish no MapLibre parity. Next is final scene
packing and headless fixture orchestration, followed by live-update/parity gates.

### Headless scene buffer and draw packing

Committed font/text preparation as `f6ad403`, then continued with kata **tsww**.
`compiler.SceneBuilder` now owns final geometry/text vertex packing, indexed
assembly, first-seen texture identities, glyph RGB-distance conversion, adjacent
draw coalescing and scene validation. The parent supplies evaluated materials,
clipping and accepted symbol passes in the original order. Expanded geometry/text
writes directly into final buffers; indexed geometry and icon topology reuse the
existing builder. No post-hashing or conversion pass is introduced.

The builder preserves one mesh with ID/revision one, texture IDs starting at one,
original float32 color conversion, `Sincos`/text transform arithmetic and borrowed
immutable sprite pixels. Glyph conversion owns its output bytes. Repeated texture
keys keep the first image. `Finish` validates then seals the output; errors return
nil and later builder mutation cannot change a published scene. Resource identities
are scene-local, not a new incremental-upload protocol.

Capacity checks precede scratch allocation and apply the existing combined
**36,780,000-element** fixture ceiling to both output modes and unique vertex
storage; callers can lower it. Expanded packing previously depended on upstream
limits. Triangle/index validation occurs before append. Texture metadata is capped
at 16,384 entries with valid scene dimensions/storage, checked without 386 integer
overflow; image-byte ownership and aggregate image memory remain caller-bounded.
Glyph conversion retains its 2048-square bound. Methods latch the first error and
skip subsequent preparation; final finite/material/clip checks reuse scene.Validate.

Verification:

- Compiler coverage remains **100%**, including new packing functions. Headless
  amd64/386 tests cover topology, draw order/coalescing/clips, transforms, texture
  identities, ownership, glyph conversion, malformed/oversized input, error latching,
  final validation and sealed publication. A 20-second fuzz run completed
  **1,626,740 executions** without failure.
- Full v4 module coverage tests, v1 parent/compiler integration-race checks,
  targeted staticcheck and application build pass; gopls reports no build errors.
  New functions are all at most 10 in complexity review. Existing generated ST1006
  and recorded GO-2026-5024 baseline findings remain unresolved.
- All three v1 captures are byte-identical to the original references: 45 draws,
  62 labels, complete fonts and unchanged geometry/textures. No GPU or generated
  binding algorithms changed.

Separate v4 binaries ran before/after/after/before, with identical pinned inputs,
GOMAXPROCS 16 and two-second direct-fixture preparation samples:

| Version | Time/op | Allocated bytes/op | Allocs/op |
| --- | ---: | ---: | ---: |
| Before shared scene packing | 29.00 / 30.63 ms | 90,780,918 / 90,781,255 | 102,319 / 102,320 |
| After shared scene packing | 29.68 / 30.80 ms | 90,780,635 / 90,781,098 | 102,318 / 102,319 |

Allocation volume stays about **90.78 MB/op** and timing ranges overlap. No speedup
is established; every sample is retained. These exclude I/O/JSON/GPU work and do
not establish MapLibre parity. Layer/material/pass selection and top-level fixture
orchestration remain parent-bound; extracting them will make the command headless.
Live update, upload scheduling and matched-quality presentation gates remain open.

### Headless material and symbol pass assembly

Committed scene packing as `3f30909`, then continued with kata **jyqg**.
`SceneBuilder.Primitive` now prepares solid/pattern materials, pattern phase and
tile clips. `SymbolLayer` emits accepted icons, then all text halos, then all text
fills using shared Symbol/PreparedLayout values. Sprite readiness/caching is a
synchronous caller resolver. The parent supplies small value adapters, selected
layer ranges and acceptance/layout pointers; there is no conversion slice.

Pattern scales, white/full-opacity sprite lookup, evaluated material opacity,
icon anchor/quads, texture key identity, angle/offset arithmetic and label counts
are preserved. Sprite ratios must be finite positive values; invalid metadata
latches a packing error. Missing sprites are skipped. Text layouts must be
atlas-complete and match the output mode, as before. The shared pass assembler
retains SceneBuilder's error latching and sealed publication.

An initial replayable-iterator API caused roughly 275 extra allocations per fixture.
The final API uses a bounded count plus indexed accessor, returning the same
immutable one-layer sequence on each of three passes. Counts above 10,000 reject
before accessor calls; negative counts and absent required accessors reject too.
This removes the iterator-frame allocation regression without a candidate slice.
Accessor/resolver work and resource state remain caller-owned.

Verification:

- Compiler remains **100% covered** headlessly. amd64/386 tests cover pattern
  phases/world wraps, pass/texture order, paint, transforms, label counts, missing
  assets/layouts, nil accessors, limits, error propagation and both topology modes.
- Full v4 module coverage tests, final v1 parent/compiler integration-race checks,
  targeted staticcheck and application build pass. An initial concurrent full-suite
  run hit the unrelated geodata install test's one-second Eventually timeout; the
  suite passed on rerun without parallel agent checks. No geodata code changed.
- All three final v1 captures are byte-identical to original references: 45 draws,
  62 labels, complete fonts and unchanged geometry/texture data.
- Complexity review retains the explicit three-pass loop (16 including input/stop
  guards); other new production functions are at most 10. Gopls reports no errors.
  Generated ST1006 and recorded GO-2026-5024 baseline findings remain unresolved.

Separate v4 binaries used identical pinned inputs, GOMAXPROCS 16 and two-second
direct-fixture samples, each sequence before/after/after/before:

| Implementation | Before ms/op | After ms/op | Before bytes/op | After bytes/op | Before / after allocs/op |
| --- | ---: | ---: | ---: | ---: | --- |
| Initial iterator passes | 32.20 / 33.96 | 34.06 / 34.00 | 90,781,351 / 90,782,246 | 90,790,409 / 90,789,962 | 102,319 / 102,322 vs 102,597 / 102,596 |
| Final indexed accessor | 32.27 / 29.62 | 31.57 / 30.33 | 90,781,366 / 90,781,335 | 90,781,855 / 90,781,598 | 102,320 / 102,320 vs 102,321 / 102,320 |

Final allocation volume/count is back to the prior range, about **90.78 MB/op**.
Every sample, including the initial regression, is retained. Variable timings do
not establish a speedup. These exclude I/O/JSON/GPU work and are not MapLibre parity
evidence. Top-level fixture orchestration and asset wiring remain the next boundary
to finish the Qt-free producer; live update/upload and presentation gates remain.

### Headless pinned Liberty assets and caches

Committed material/pass assembly as `6907f14`, then continued with kata **4vqr**.
The three pinned assets now live in `pkg/vecmap/liberty`, with exactly one embedded
copy each and unchanged SHA-256 checksums. The package exposes immutable compiled
layers, value sprite metrics and cached crop/tint images without importing Qt.
Parent style, image and collision-readiness adapters use the same shared cache.
No style map or pixel conversion copy is added. The existing fetcher now runs from
`go generate ./pkg/vecmap/liberty`; its checksums and download limits are unchanged.

Style/sprite decoding retain separate `sync.Once` gates and cached failures.
The mutex-protected 512-entry FIFO preserves four-decimal opacity keys, replacement
without promotion, concurrent duplicate-miss preparation and borrowed pixel
lifetime after eviction. Parsing and pixel algorithms remain in their existing
shared packages; there is no new dependency or native adapter change.

Verification:

- New package has **100% headless coverage**: pins, layer/pixel sharing, metadata,
  missing sprites, cached failures, invalid opacity, quantized keys, FIFO behavior,
  borrow lifetime and concurrent cold loading/cache pressure. Headless 386 tests
  also pass for liberty, style, sprite and compiler.
- Full v4 module coverage suite, v1 parent integration/liberty race tests,
  targeted staticcheck and application build pass. Gopls has no errors; all new
  functions are below the complexity threshold of 11. Repository staticcheck still
  reports only generated `internal/miqtquick` ST1006 findings. The vulnerability
  scan still reports the recorded standard-library **GO-2026-5024** baseline.
- Expanded, direct-indexed and post-indexed v1 captures are byte-identical to their
  original references: **45 draws, 62 labels, complete fonts**, unchanged buffers
  and textures. No GPU/presentation timings are inferred from this CPU extraction.
- Regeneration command wiring was checked with `go generate -n`; offline checksum
  tests verify the moved assets. No network refetch was needed.

Controlled direct-fixture samples used separate before/after v4 binaries, pinned
inputs, GOMAXPROCS 16 and two seconds per sample, in before/after/after/before order:

| Version | ms/op | bytes/op | allocs/op |
| --- | ---: | ---: | ---: |
| Before | 34.43 / 31.67 | 90,782,063 / 90,781,623 | 102,321 / 102,321 |
| After | 31.24 / 31.21 | 90,781,586 / 90,779,894 | 102,321 / 102,319 |

Allocation counts/volume remain in the prior range. Variable timings do not
establish a speedup; these exclude file I/O, JSON and GPU work. Assets/caches are
now headless, but `cmd/vecmap-fixture` still imports the parent for top-level
orchestration and legacy bucket/candidate/collision-readiness wiring. That is the
next checkpoint toward a command that builds with `CGO_ENABLED=0`.

### Top-level headless fixture orchestration

Continued with kata **2d12**, preserving the verified, uncommitted **4vqr** asset
checkpoint. `pkg/vecmap/fixture` now owns pinned input validation, source decoding,
tile/candidate compilation, one font-loader invocation, text/atlas preparation,
collision and scene publication. The CLI imports this package directly and builds
with **CGO_ENABLED=0**. `go list -deps` confirms no Qt, cgo or parent vecmap import.
Public parent APIs are thin wrappers/type aliases; the former orchestrator remains
only as a test oracle using the legacy renderer's bucket/candidate adapters.

Shared primitives and symbols are retained directly, without conversion slices.
Maps use source candidate indexes for this one fixed tile/wrap. Layer ordering,
reverse-candidate collision collection, stable ties and icon/halo/fill order are
preserved. Collision still uses metric layouts before atlas filtering. The old
native fallback eligibility predicate is now `glyph.LegacyFallbackEligible`, shared
with the parent: fallback-eligible unsupported text can reserve collision space
without being rendered by this offline SDF-only path. This deliberately preserves
the previous fixture policy rather than changing label acceptance during extraction.

The result retains packed geometry, glyph RGBA, shared sprite pixels and metadata,
not source features/candidates/layouts. Raw tile bytes are capped before checksum
work. Atlas/collision errors now propagate atomically instead of being ignored;
these guards do not change valid pinned output. Per-feature MVT resource summaries
are returned in `Result.Limits` rather than logged by shared preparation. No new
module dependency, native binding or GPU rendering algorithm is introduced.

Verification:

- Headless fixture coverage **94.6%**; assembly, request iteration, packing,
  projection and collision functions **100%**. Remaining guards are error returns
  behind pinned/validated inputs plus finish-level collision propagation. Shared
  lower-level packages cover their corresponding failures. Shared fallback predicate
  coverage is 100%; total glyph coverage remains 99.8%.
- Pinned headless amd64/386 tests cover topology, loader count/errors, font absence,
  budgets, atomic failure, fallback readiness, reverse priority and camera-only
  frames. The command's serialized output is validated headlessly too.
- Legacy/new fixture comparisons pass with full, partial and missing fonts in
  both topology modes. All three final headless v1 captures are byte-identical to
  original references: **45 draws, 62 labels, complete fonts**, unchanged geometry
  and textures. Captures are `/tmp/opencode/vecmap-{expanded,direct,post}-headless-scene.json`.
- Full v4 coverage suite, v1 parent/fixture/glyph/command integration-race checks,
  targeted staticcheck and application build pass. Gopls reports no diagnostics.
  All new production functions are at most 10 in the complexity review; the
  test-only old orchestrator is 11. Generated ST1006 and the recorded GO-2026-5024
  vulnerability baseline remain. No new GPU/presentation timing claim is made.

Controlled v4 direct-fixture samples used the previous asset checkpoint binary
(`vecmap-assets-after.test`) and the new `vecmap-headless-after.test`, identical
pinned inputs, GOMAXPROCS16 and two seconds, in before/after/after/before order:

| Version | ms/op | bytes/op | allocs/op |
| --- | ---: | ---: | ---: |
| Before | 30.84 / 31.53 | 90,781,323 / 90,781,313 | 102,319 / 102,319 |
| After | 30.23 / 30.00 | 90,714,392 / 90,713,641 | 102,318 / 102,318 |

Smaller single-tile keys reduce allocation volume by about 67 KB per fixture;
allocation counts remain essentially unchanged. Variable timings do not establish
a speedup. These samples exclude I/O/JSON/GPU work and are not MapLibre parity.

The offline CPU extraction is now complete. Next is bounded live tile/placement
updates and GPU uploads with explicit logical resource identities, ordering,
clipping, fallback coverage and world-wrap contracts. Shader-driven zoom styling,
multi-map validation, matched-quality MapLibre comparisons and real presentation
measurements remain open acceptance gates under **ngrb**.

### Bounded retained fragment updates

The headless fixture/assets work was committed as **93aba3e**. Continued with kata
**0ex5**, following the requested toolkit-neutral retained update layer first.
`pkg/vecmap/retained` introduces a single-owner `Store`, atomic `Apply([]Change)`
and `Snapshot([]Range)` over existing prepared scenes. Reusable scene validation
and Go maps/slices are sufficient; no new library or native dependency was needed.

Fragment-local mesh/texture IDs are remapped into a store-wide namespace. An
unchanged fragment retains IDs/revisions; replacement preserves surviving local
slots' IDs and increments every resource revision conservatively. Input revisions
are ignored. Removal/re-addition gets fresh IDs, including resources removed from
a still-present fragment. ID/revision overflow rejects atomically. This prevents
tile-local ID collisions and stale uploads when a packer's first-use slots change
meaning. It does not yet deduplicate shared atlases or track per-resource dirtiness.

Snapshots take explicit contiguous draw-record ranges and instance transform slots.
They preserve supplied order, material values and clips, sharing resources across
repeated instances. They do not guess style layers, tile/fallback coverage, symbol
visibility or pattern wrap phases. Metadata is owned; validated geometry/indices/
pixels are immutable borrows. Existing snapshots survive later updates, and
camera-only frames can keep the same snapshot pointer.

Default ceilings (lowerable) are 128 fragments, 4,096 meshes, 4,096 textures, 65,536
draws and 512 MiB of logical payload. Keys are capped at 256 bytes and copied.
Batches permit twice the fragment count in changes to replace a full cover
atomically. Incoming new scenes and the final retained state each obey aggregate
bounds; all removals are accounted before final insertion totals. Length/byte
preflight precedes deep input validation and remap allocation. Failed batches do
not change state or consume IDs. Old/incoming payload and caller-retained snapshots
have explicitly separate lifetime/memory obligations, documented in the package.

Verification:

- New package has **100% headless coverage**, including rollback, resource identity
  exhaustion, revision/eviction lifecycle, metadata isolation, old snapshot lifetime,
  full-capacity replacement, input/output limits and independent snapshot readers.
- Pinned fixture composition interleaves two copies of the 45-draw scene into 90
  ordered draws with independent IDs and shared buffers. It verifies material/clip
  preservation and that replacing one fragment leaves the other's revisions intact.
  This is a CPU composition test, not real multi-tile label-placement validation.
- Headless amd64/386, v1 race checks, full v4 module coverage and application build
  pass. Targeted staticcheck is clean; repository-wide staticcheck still reports
  only generated ST1006. Gopls reports no diagnostics. The vulnerability baseline
  remains **GO-2026-5024**.
- Complexity review retains Snapshot16 and preflight11: the scores reflect explicit
  bounded selection checks and transactional accounting; other new functions <=10.
  The validation-before-publication phases remain visible rather than obscured to
  lower a metric. No existing rendering algorithm or generated bindings changed.

New snapshot microbenchmark, v4/GOMAXPROCS16, two seconds per sample, two fragments
with 350,000 vertices each and three selected ranges/output draws:

| ns/op | bytes/op | allocs/op |
| ---: | ---: | ---: |
| 316.2 / 299.4 / 288.7 | 768 / 768 / 768 | 6 / 6 / 6 |

This measures metadata-only composition, excluding deep validation, tile/style
preparation, JSON and GPU work. There is no prior implementation speedup or frame
performance claim. Production and the QRhi viewer do not use the store yet.

Next is a bounded upload/admission planner over these stable identities, including
successful-upload acknowledgement and old-scene retention until replacement
resources are ready. Live tile/placement selection, coverage/clip/wrap policy and
matched-quality presentation comparisons remain separate acceptance gates.

### Acknowledged bounded upload planning

Committed retained fragment updates as **41faeaf**, then continued with kata
**09np**. `retained.Planner` now consumes immutable Store snapshots and plans exact
kind/ID/revision upload and release operations. The current scene remains unchanged
until every desired version has a successful acknowledgement. Old and replacement
revisions can coexist; obsolete allocations retire only after publication and a
separate successful release acknowledgement.

Admission rejects active-plus-target unions exceeding configured logical residency
limits (default/max 1 GiB, 16,384 resources). Each target still obeys Store's scene
limits. Native overhead/staging and caller-held CPU snapshots have separate budgets.
Per-batch positive byte/resource limits are explicit. Resources are indivisible:
oversized uploads return `ErrBudget` rather than exceeding a cap or being skipped.
The planner does not chunk uploads or promise a wall-clock render-thread budget.

One batch may be outstanding. Stale/duplicate acknowledgement tickets cannot commit
different work, and failed batches retry with fresh tickets. Public descriptor
slices are isolated from the private pending record. Supersession happens between
batches; successfully uploaded but abandoned target versions retire before new
uploads. Draw-only targets already resident publish immediately, as do empty scenes.
Nil targets clear publication, then retire resources through the same protocol.

The adapter must stage exact versions, acknowledge uploads only when usable, discard
an entire failed upload batch, and respect in-flight frames before confirming native
releases. Failed release acknowledgement means nothing was released. CPU readiness
is not a GPU fence. Device loss/reset requires a new planner and reset native
namespace. These obligations are documented in the package README; the current
ID-only QRhi residency maps have not yet been migrated to versioned staging.

Verification:

- Package remains **100% covered** headlessly. Tests exercise activation, failures,
  retries, descriptor isolation, stale tickets, revision coexistence, release
  accounting, supersession, draw-only/empty publication, input/budget rejection and
  ticket exhaustion.
- Pinned two-fragment replacement runs through 12 MiB/two-resource batches under a
  64 MiB/32-resource logical residency cap, preserving the previous scene until
  readiness and checking that release never targets active/desired versions.
- A fake-backend fuzz model checks published-resource readiness, release safety
  and peak logical residency across target changes, batches and acknowledgements:
  **431,742 executions**, 20 seconds, no failure.
- Headless amd64/386, v1 race, full v4 coverage, targeted staticcheck and application
  build pass. Gopls reports no diagnostics. Complexity review retains Next12 for
  explicit state/budget/ticket guards; other new production functions are <=10.
  Generated ST1006 and the recorded **GO-2026-5024** baseline remain.

This checkpoint changes CPU planning only. No new dependency, generated bindings,
GPU upload algorithm, production renderer or fixture geometry is changed. There
is no new GPU/presentation timing or parity claim. Next is revision-aware QRhi
staging/execution with acknowledged batches and native lifetime tests, followed by
live tile/placement wiring and matched-quality presentation validation.

### Revision-aware transactional QRhi staging

Committed acknowledged upload planning as **a252847**, then continued with kata
**bekb**. The next native checkpoint replaces ID-only GPU caches with exact
ID/revision keys, prepares selected resource keys per draw, and separates allocation,
upload recording and scene selection. Existing methods and generated Qt bindings
are reused; no C++ or generated binding file is edited.

All missing mesh/texture allocations now succeed before any resource upload is
recorded. Failed allocation destroys only fresh, unrecorded objects and leaves
prior cache entries intact. Successful stages transfer ownership on recording;
consumed stages cannot subsequently discard cached objects. Older and staged new
revisions can coexist while the old scene remains selected. Activation of an
already-staged version performs no additional upload. This is transactional
resource allocation, not full-frame rollback: automatic prepare/pipeline failures
still follow the existing renderer reset path.

Qt's documented `QRhiResource::deleteLater` guarantees wrappers referenced by the
current frame survive through `endFrame`, with underlying native GPU destruction
deferred until safe. The adapter now uses the existing generated DeleteLater for
resident retirement, including bindings, uniforms, pipelines and samplers. Fresh
unrecorded allocation failures still delete immediately. A test retires throwaway
uploads while their commands belong to the current frame, then verifies rendering.

DeleteLater scheduling is **not** completed release acknowledgement. Live cache
counters exclude deferred retirement, and upload counters count recorded commands,
not planner readiness. End-of-frame/native retirement acknowledgements and bounded
Planner batch execution remain the next integration boundary. The current viewer
still synchronizes a changed scene's missing resources as a whole.

Verification:

- Vulkan integration passes on **AMD Radeon 860M Graphics / RADV KRACKAN1**, including
  generated lifecycle/pair-bound tests and both expanded/indexed rendering paths.
- OpenGL integration passes under Xvfb/Mesa llvmpipe, including **QT_SCALE_FACTOR=2**
  and **GOAMD64=v1 race** checks. Tests cover injected mesh/white/later-texture
  allocation failure, real allocation rollback, revision coexistence, old pixels
  during staging, activation without reupload, uniform-buffer growth, consumed
  stages, current-frame retirement and zero logical cache counts at teardown.
- Native renderer coverage is **89.0%**; all new `resources.go` functions are **100%**.
  Native allocation/device/pipeline failure guards remain partly uncovered.
- Final expanded/direct full Madrid static Vulkan PNGs are byte-identical to the
  earlier reference PNGs. Each retains **45 draws, 62 labels, complete fonts**;
  uploads remain 19,045,116 expanded bytes or 11,834,328 direct bytes, with one mesh
  and five textures including white.
- Full v4 module coverage, default application build, tagged v1 viewer build and
  tagged integration staticcheck pass. Tagged staticcheck exposed an existing
  S1011 loop in the edited renderer test; it was replaced with equivalent slice
  append. Default repository staticcheck still reports generated ST1006 findings.
  Gopls lacks package metadata for the opt-in integration files, so tagged
  compilation/tests/staticcheck provide the semantic checks. GO-2026-5024 remains.
- Complexity review retains prepare15, pipeline preparation14, resource preparation11
  and render11; all new resource staging/selection functions are <=10. These explicit
  lifecycle guards and pass setup remain visible rather than split for a metric.
- One parallel Xvfb race attempt aborted in QApplication construction before any
  renderer was created. The same command passed serially; the startup cause is
  unconfirmed. An initial test tried to install a second generated prepare override,
  which is forbidden; node/lifetime setup now supports one test wrapper installation.

Static smoke runs are short correctness captures, **not controlled performance
comparisons**. To retain all observed samples, their reported p95s are below. The
final captures overlapped a staticcheck process; no timing improvement is inferred.

| Capture | Frames | Prepare µs p95 | Submit µs p95 | Previous Qt GPU frame ms p95 | Callback ms p95 |
| --- | ---: | ---: | ---: | ---: | ---: |
| Initial direct smoke | 61 | 49.002 | 113.344 | 0.963630 | 17.079643 |
| Final direct smoke | 58 | 62.598 | 111.440 | 0.956577 | 17.239544 |
| Final expanded smoke | 58 | 60.023 | 116.470 | 2.399158 | 25.415715 |

These are not actual presentation timestamps or MapLibre parity evidence. Final
artifacts: `/tmp/opencode/vecmap-rhi-staging`, `vecmap-{direct,expanded}-staging.png`
and `vecmap-native-staging-final-coverage.out`. Live map scheduling and production
renderer migration remain outside this checkpoint.

## Flatpak integration

Build the adapter against the exact Qt SDK shipped with the application, and
regenerate it as part of deliberate Qt upgrades. Keep generated adapters and
shader packages in source so ordinary Flatpak builds do not require Clang or
network access to regenerate them. The generator is a development-time tool.

The companion checkout's current manifest targets `org.kde.Platform//6.9`;
that is not compatible with these Qt 6.11.2 bindings. Before shipping the RHI
backend, select a matching SDK/runtime or bundle a controlled Qt build, then
build and verify the adapter in that environment. A runtime branch name alone
does not pin its exact Qt patch version: coordinate runtime updates or ship Qt
under `/app` if exact dependency control is required.

The production application remains on its existing renderer while these gates
are open. This prototype adds explicit opt-in build/test targets.

## Remaining migration gates

- Continue moving the CPU engine out of its Qt-bound package; camera, projection,
  tile coverage and transforms have been extracted into `pkg/vecmap/view`.
- Reuse geometry across live style-zoom changes; add shader-driven line
  extrusion/dashes where appropriate instead of repeatedly rebuilding triangles.
- Replace the fixture with incremental live tile/placement updates and bounded
  upload scheduling. Validate fallback clipping and world wraps under motion.
- Validate multiple simultaneous maps and GPU resource sharing where safe.
- Compare identical data, style, label density, resolution, and antialiasing with
  MapLibre Native across dense/rural/overzoom/HiDPI traces and tile-arrival bursts.
- Measure full GUI/render/GPU/presentation costs and memory stability. Meeting
  the display refresh rate on this one retained fixture is only an initial gate.

The SDF shader reuses the existing MapLibre-derived equations; their BSD notices
are retained in `ui/shaders/LICENSE`. Generated binding notices are in
`internal/qtrhi/LICENSE`.
