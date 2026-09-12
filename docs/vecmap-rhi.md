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

The existing decoder/compiler is reused by the fixture producer. It still lives
in the Qt-bound `pkg/vecmap` package; extracting the rest of the engine into
headless packages remains work under the umbrella issue. The scene consumer
already builds independently of that package.

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
