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
  It depends only on the standard library; pinned assets and caching remain
  caller-owned in vecmap.
- `pkg/vecmap/glyph`: bounded SDF glyph PBF decoding, shared metrics/bitmaps and
  deterministic atlas packing and glyph-metric text layout. Font I/O/cache/retry,
  retained-atlas policy, eligibility and placement stay caller-owned.
- `pkg/vecmap/placement`: feature-anchor selection, line interpolation/repetition,
  upright/raw angles, exterior-ring centroids and streaming evaluated text/icon
  candidates, plus stable collision/priority/optional-symbol selection. Projected
  boxes and glyph/sprite readiness remain caller-owned.
- `internal/vecmaprhi`: a Qt backend that retains buffers/textures and records
  draws inline with Qt Quick through `QSGRenderNode` and `QRhi`. It handles parent
  scissor/stencil clipping, inherited opacity, resize, resource replacement, and
  render-thread cleanup. Glyph offsets stay in screen pixels during camera motion.
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

The fixture producer reuses the existing compiler and the extracted headless MVT
preparation, style compiler, glyph/atlas preparation, text layout and symbol
candidates and collision selection. Projected box preparation and scene compilation
still live in the Qt-bound
`pkg/vecmap` package. Extracting that remaining CPU work is
tracked by the umbrella issue. The scene consumer already builds independently
of that package.

The native bindings and Go renderer are separate packages. Editing rendering
logic rebuilds the Go backend without recompiling the binding package's C++.

## Build and run

The checked-in bindings target **Qt 6.11.2**. Build and runtime version checks
reject another Qt version. Install the matching Qt Quick, QML, Qt Shader Tools,
and Qt base private development headers.

```sh
make rhi-build

# Uses the existing compiler and verifies the pinned PBF checksum.
go run ./cmd/vecmap-fixture \
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
