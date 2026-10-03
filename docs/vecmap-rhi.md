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
  versions to coexist. The fixture viewer feeds its batch executor through a
  generation-aware headless Worker, with conservative native completion drains.
  Glyph offsets stay in screen pixels during camera motion.
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

Vulkan batch execution also requires the
[Qt 6.11.2 checked-finish dependency patch](../patches/README.md). The viewer
checks the loaded runtime's capability marker and reports a missing correction
before executing Planner-owned uploads.

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
the window on top, so the compositor keeps presenting it. This is an opt-in
benchmark setting, not a change to production map windows. It doesn't help if
another on-top window covers the benchmark.

`-diagnostics` reports GUI timer intervals, active/exposed tick counts, and gaps
over 100 ms with window state and Qt's queued `frameSwapped` count. It also times
the render thread's `QRhi::beginFrame` and `endFrame` calls from the window's
`beforeFrameBegin`, `beforeSynchronizing`/`beforeRendering`, `afterRendering` and
`afterFrameEnd` signals:

```text
pacing_gap frame=208 timer=1.001776436s render_age=1.001675476s swapchain_wait=996.204609ms visible=true active=false exposed=true swaps=208
pacing_gaps=4 swapchain_gaps=4
frame_begin_wait samples=354 p50=11.272107ms p95=12.219364ms p99=654.396338ms max=996.204609ms
frame_end_wait samples=353 p50=84.81µs p95=179.769µs p99=256.914µs max=302.12µs
```

Each gap carries the longest swapchain wait finished since the previous timer
tick. `swapchain_gaps` counts gaps with a wait of at least 100 ms. **A nonzero
count means the window system withheld buffers; that run doesn't measure the
renderer.** Vulkan blocks in `beginFrame` (image acquisition), OpenGL in `endFrame`
(buffer swap). An uncovered window waits up to one refresh there: 11.3 ms begin
p50 on Vulkan and 16.5 ms end p50 on OpenGL at 60 Hz.

Diagnostics retain at most 60,000 timer samples, 60,000 samples per wait and 64
gap records. The first 30 frames are excluded from gap records and wait samples,
consistently with rendering timings. The report also records the Qt platform
plugin, renderer, device and trace type.

Investigation of kata `vx93` on the XCB desktop found:

| Trace | Foreground | Frames / duration | Callback p99 |
| --- | --- | ---: | ---: |
| Geographic, Vulkan | no | 153 / 8 s | 1.001 s |
| Geographic, Vulkan | yes | 478 / 8 s | 22.66 ms |
| Affine, Vulkan | yes | 361 / 6 s | 18.40 ms |
| Geographic, OpenGL | yes | 358 / 6 s | 18.38 ms |

Both trace implementations exhibited gaps without the foreground control. The
GUI timer stalled alongside rendering, while CPU draw work and GPU time remained
small. Qt still reported `visible=true` and `exposed=true` during stalls, so those
flags can't certify a timing run.

#### Cause: Xwayland presents hidden windows once per second

The desktop is GNOME (mutter 50.5) on Wayland. Qt's `xcb` platform talks to
**Xwayland 24.1.13**, not a native X server. Mutter apparently withholds Wayland
frame callbacks from a surface nobody can see. Xwayland's Present code then
completes flips from a fallback timer, `TIMER_LEN_FLIP` = 1000 ms
(`hw/xwayland/xwayland-present.c`: "the surface is not visible, in this case
update with long interval"). Swapchain buffers come back once per second.
Xwayland doesn't tell the X client its window is hidden, hence the stale
`visible`/`exposed` flags.

Stacks sampled with `eu-stack` during the stalls, six samples per backend, all
identical:

- Vulkan render thread: `QSGRenderThread::syncAndRender` → `QRhiVulkan::beginFrame`
  → `vkAcquireNextImageKHR` → Mesa `x11_acquire_next_image` →
  `wsi_drm_wait_for_explicit_sync_release` → `drmSyncobjTimelineWait`.
- OpenGL render thread: `QRhiGles2::endFrame` → `glXSwapBuffers` →
  `loader_dri3_swap_buffers_msc` → `xcb_wait_for_special_event`.
- GUI thread, both backends: `QSGThreadedRenderLoop::polishAndSync`, waiting for
  the render thread. That's why GUI timers stalled with rendering.

Controls on 2026-10-01: Radeon 860M, Mesa 26.2.3, Qt 6.11.2 (checked-finish build
for Vulkan, distribution build for OpenGL), Madrid fixture, `-foreground`. The
window was minimized with `xdotool` from 3.5 s to 7.5 s, or covered by a second,
later on-top viewer from 3 s to 8 s.

| Run | Pacing gaps | Swapchain gaps | Longest begin wait | Longest end wait |
| --- | ---: | ---: | ---: | ---: |
| Vulkan, uncovered, 8 s | 0 | 0 | 52.7 ms | 0.27 ms |
| Vulkan, minimized | 4 | 4 | 996 ms | 0.30 ms |
| Vulkan, covered | 4 | 4 | 995 ms | 0.26 ms |
| OpenGL, minimized | 3 | 3 | 0.10 ms | 1.001 s |

Covering the window is enough. Without `-foreground` a new window can also open
behind the active one: three runs gapped before any scripted change while the
desktop was in use. In all eight runs with the new diagnostics, every pacing gap
had a swapchain wait of at least 598 ms.

This is a desktop measurement artifact, not a renderer regression, so the
renderer is unchanged. A production Qt Quick window that keeps requesting frames
while hidden under Xwayland presumably blocks its GUI thread the same way.

`frameSwapped` and the swapchain waits are **not** display presentation
timestamps. Neither is Xwayland's Present completion time, which is its own clock
reading when the frame callback or timer fires. Poor cadence samples are
reported, not discarded.

### Presentation timestamps on native Wayland

`cmd/wayland-present` reports when the compositor actually showed each frame. It
runs any native Wayland client with an `LD_PRELOAD` library that requests
`wp_presentation` feedback for every `wl_surface` commit carrying a new buffer,
then summarizes the compositor's answers. The viewer and MapLibre Native can be
measured the same way, without changing either. Qt clients need
`QT_QPA_PLATFORM=wayland`; X11 clients, including Qt's xcb platform under
Xwayland, aren't supported.

```sh
QT_QPA_PLATFORM=wayland QSG_RHI_BACKEND=vulkan \
  go run ./cmd/wayland-present -- bin/vecmap-rhi \
  -scene /tmp/madrid-scene.json -duration 8s -diagnostics
```

```text
presentation surface=38 surfaces=1 presented=475 discarded=0 clock=1 refresh=16.665805ms
presentation_interval samples=444 p50=16.666ms p95=16.675ms p99=16.676ms max=33.33ms
presentation_latency samples=445 p50=20.685997ms p95=22.939509ms p99=23.297454ms max=36.691055ms
presentation_refreshes late_frames=2 missed_refreshes=2
presentation_flags vsync=445 hw_clock=445 hw_completion=445 zero_copy=0
```

- The report covers the surface with the most feedback, the one the client draws
  into. `-warmup` (default 30) presented frames are not sampled; `-log` keeps the
  raw feedback.
- `presentation_interval` is the time between consecutive displayed frames.
  `presentation_latency` runs from the client's `wl_surface.commit` to display, on
  the compositor's clock (`clock=1` is `CLOCK_MONOTONIC`).
- `late_frames` counts intervals longer than one refresh and `missed_refreshes`
  the refreshes without a new frame, from the vblank sequence of vsynced frames,
  else the refresh duration.
- `discarded` frames carried a new buffer but were never shown, for example
  because the window was covered. Commits without a new buffer are ignored; with
  Vulkan on mutter every frame's buffer commit is followed by one.
- `hw_clock` and `hw_completion` mean the timestamp came from the display
  hardware's page-flip completion.

The preload is benchmark tooling, never linked into an application. The tool
compiles it at run time from `cmd/wayland-present/preload/present.c` and the
protocol code `wayland-scanner` generates. It needs `gcc` (or `CC`), `pkg-config`,
`wayland-scanner`, and the libwayland-client and wayland-protocols development
files.

Measured on 2026-10-01: GNOME/mutter 50.5 on Wayland, Radeon 860M, Mesa 26.2.3,
Qt 6.11.2, Madrid fixture, 8 s geographic trace, 60 Hz panel. Vulkan used the
checked-finish Qt, with its `QWaylandIntegrationPlugin`,
`QWaylandXdgShellIntegrationPlugin` and `QWaylandEglClientBufferPlugin` targets
built; OpenGL used the distribution Qt.

| Backend | Frames | Presented / discarded | Interval p50 / p99 / max | Late frames | Commit to display p50 / p99 | Callback interval p99 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Vulkan | 477 | 475 / 0 | 16.666 / 16.676 / 33.33 ms | 2 | 20.7 / 23.3 ms | 17.7 ms |
| OpenGL | 381 | 377 / 3 | 16.666 / 16.675 / 16.676 ms | 0 | 21.0 / 22.9 ms | 18.1 ms |

Every sampled frame had `vsync`, `hw_clock` and `hw_completion` set. The OpenGL
window was hidden for its last 1.5 s: Qt reported `exposed=false`, rendering
stopped and the 3 frames in flight were discarded. Native Wayland can't keep a
window on top, so `-foreground` doesn't help there; check `discarded` and the
viewer's gap records instead.

Displayed frames are steadier than render callbacks: 16.676 ms against 17.7 ms at
p99 on Vulkan. Callback jitter isn't presentation jitter. A frame reaches the
display about 21 ms, a refresh and a quarter, after its commit on both backends.
These are viewer-only numbers, not a MapLibre comparison.

### Matched MapLibre Native comparison

The viewer replays its live trace in MapLibre Native as well, so both renderers
are measured with the same window, 8 ms GUI timer, `traceCamera` trace,
`-diagnostics` pacing, screenshot code and `wayland-present`, from the same
offline tiles, style, sprites and glyphs.

MapLibre is the application's legacy fallback: the QtLocation geoservice plugin
from **maplibre-native-qt v3.0.0** (`d929c783`, maplibre-native `0a4e5a44`). Build
it against the distribution Qt 6.11.2 with three build-time workarounds and no
source changes:

```sh
git clone --branch v3.0.0 --depth 1 --recurse-submodules --shallow-submodules \
  https://github.com/maplibre/maplibre-native-qt.git
# Qt 6.10+ finds private module targets only on request (fixed upstream after v3.0.0).
echo 'find_package(Qt6 COMPONENTS LocationPrivate REQUIRED)' > qt610-private.cmake
cmake -S maplibre-native-qt -B build -G Ninja -DCMAKE_BUILD_TYPE=Release \
  -DCMAKE_PROJECT_maplibre-native-qt_INCLUDE=$PWD/qt610-private.cmake \
  -DMLN_WITH_WERROR=OFF -DCMAKE_CXX_FLAGS='-include cstdint' \
  -DCMAKE_INSTALL_PREFIX=$PWD/install   # GCC 16: new -Wsfinae-incomplete, stricter libstdc++ includes
ninja -C build install
```

Run MapLibre with `QT_PLUGIN_PATH=install/plugins`,
`LD_LIBRARY_PATH=install/lib64` and `QSG_RHI_BACKEND=opengl`: the plugin renders
through OpenGL, and the viewer refuses another backend.

#### Offline data

`cmd/maplibre-offline` writes `style.json` from the pinned Liberty style
(`liberty.Files`), pointing its tiles at the viewer's `-cache-dir` through
`file://`, plus the pinned sprite atlas and the supplied glyphs. It also copies
the pinned z9/250/193 tile to its XYZ path, since vecmap reads it under its pinned
name. Glyph ranges other than 0-255 are written empty, giving MapLibre vecmap's
coverage: MapLibre requests Cyrillic, Greek, Arabic and Tifinagh ranges for the
prefetched z4/z5 tiles, and a failed range leaves its tiles loading forever, so
the map never settles. The natural-earth raster stops at zoom 7 and is unused.

With `-record ADDR` it also writes `style-record.json` and serves its tiles from
the cache, fetching misses through the producer's own `HTTPLoader`. Capture a
corpus by replaying the trace with network access in both renderers, then
replay with `-cache-only`:

```sh
go run ./cmd/maplibre-offline -cache-dir CORPUS -glyph-dir GLYPHS -out ASSETS -record 127.0.0.1:8765 &
bin/vecmap-rhi -maplibre-style ASSETS/style-record.json -duration 8s
bin/vecmap-rhi -live -cache-dir CORPUS -glyph-dir GLYPHS -coarser-tiles 0 -duration 8s   # and 1, per backend
bin/vecmap-rhi -live -cache-only -cache-dir CORPUS -glyph-dir GLYPHS -duration 8s       # must report failed=0
```

OpenFreeMap rebuilt the `20260823_080002_pt` snapshot on 2026-09-13: every tile
checked since then, not only the pinned one, differs from earlier caches. The
2026-10-01 corpus holds today's bytes for 82 tiles (z4–z10, 11.5 MB) plus the
pinned z9 tile, so neighbouring tiles come from a different build than the
pinned one, as in the 2026-09-28 corpus. Never mix it with older caches.

#### Matching the view

QtLocation's `zoomLevel` and vecmap's camera both use 256-pixel world units, and
the plugin passes `zoomLevel − 1` to MapLibre's 512-pixel zoom. Screenshots at the
end of the trace (bearing −5.4°, zoom 10.198, panned 16.5 px) put every label at
the same pixels, so center, zoom, bearing and pan agree.

The work doesn't: at camera zoom 10 MapLibre evaluates the style at zoom 9 and
draws z9 tiles 512 pixels wide, which is vecmap's `-coarser-tiles 1`. vecmap's
default draws z10 tiles at style zoom 10, with more detail (124 labels against
83 at the end of the trace). Compare MapLibre with Coarser 1 for matched work and
with Coarser 0 for the look vecmap ships. MapLibre's label selection differs
slightly from vecmap's at Coarser 1, with similar density.

`settled_after` is how long after the trace ended the final view was complete.
vecmap: the current scene is the final target and fully uploaded. MapLibre:
QtLocation's map renders only while MapLibre has work, so it's the last frame
before a second without one; that includes MapLibre's 300 ms symbol fade-in,
which vecmap doesn't have. Pacing records skip MapLibre's idle wait. Both modes
print `process` (CPU time and peak RSS from `getrusage`) and `drm` (GPU engine
time and GPU memory of the viewer's own DRM clients, from `/proc/self/fdinfo`).

#### Results

Measured on 2026-10-01: headless mutter 50.4 with a 1920×1080@60 virtual monitor,
Radeon 8060S (Strix Halo), Mesa 26.2.3, Qt 6.11.2, 800×600 at scale 1, threaded
loop unless noted, 8 s trace, five runs per row, load average 0.38–0.78.
Vulkan used the checked-finish Qt with `MESA_VK_WSI_PRESENT_MODE=mailbox` (below).
Ranges are min–max over runs. ¹ A copy of `style.json` with
`"transition": {"duration": 0, "delay": 0}`, which turns off symbol fades.

| Row | Settle after trace | CPU | Peak RSS | GPU time | GPU memory (VRAM) | Interval p99 | Labels |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| MapLibre, threaded | 810–878 ms | 1.20–1.31 s | 220–225 MiB | 187–205 ms | 86–90 MiB | 16.84–17.57 ms | – |
| MapLibre, basic loop | 815–848 ms | 1.15–1.24 s | 220–224 MiB | 180–203 ms | 90–92 MiB | 16.82–17.12 ms | – |
| MapLibre, no fade (3 runs)¹ | 517–535 ms | 1.16–1.35 s | 221–222 MiB | 183–192 ms | 90 MiB | 16.87–16.88 ms | – |
| vecmap Coarser 1, OpenGL | 81–83 ms | 7.03–7.47 s | 483–507 MiB | 173–206 ms | 104–118 MiB | 17.02–17.64 ms | 83 |
| vecmap Coarser 1, Vulkan | 77–80 ms | 6.84–7.07 s | 433–470 MiB | 211–249 ms | 141 MiB | 17.03–17.59 ms | 83 |
| vecmap Coarser 0, OpenGL | 320–363 ms | 8.36–8.57 s | 600–626 MiB | 207–223 ms | 153–168 MiB | 17.03–17.30 ms | 124 |
| vecmap Coarser 0, Vulkan | 332–403 ms | 8.29–8.51 s | 566–584 MiB | 249–267 ms | 141 MiB | 17.02–17.54 ms | 124 |

- Presentation is not where they differ. Every row showed a frame at each refresh,
  with no late, missed or discarded frames, interval p50 16.66–16.67 ms and commit
  to display p50 16.5 ms. The virtual monitor's timestamps come from mutter's
  frame clock (`vsync=0 hw_clock=0`), so they show that frames were ready in time
  but not real scanout. Pacing and swapchain gaps were zero in every run.
- GPU time is similar: about 0.37 ms a frame for MapLibre, 0.39 ms for vecmap on
  OpenGL and 0.47 ms on Vulkan at Coarser 1.
- vecmap settles sooner: 80 ms against 520 ms for MapLibre without fades, and
  still sooner at Coarser 0 with more detail.
- vecmap costs far more CPU and memory: 5–6× MapLibre's CPU time and about twice
  its RSS at matched work. At Coarser 1 it makes 67 tile loads and 291–295 tile
  preparations, about 4.4 per load; preparation (2.2–2.3 s), building (1.2–1.3 s)
  and selection (0.7–0.8 s) run on one compiler goroutine. MapLibre requests 10
  distinct tiles (six z9, two z8, and z5/z4 prefetch parents), 18 requests in a
  run. The preparations are broken down below.
- In the basic loop the GUI thread blocks in the buffer swap, so the 8 ms trace
  timer fires every 33 ms: MapLibre still renders at 60 Hz but its camera moves
  at 30 Hz. In the threaded loop the plugin warns "Threaded rendering is not
  optimal" and, while the map isn't fully loaded, also refreshes from a 250 ms
  timer; neither showed up as late frames.

#### Why vecmap prepares tiles several times

`prepare_causes` (`producer.Status.PrepareCauses`) attributes each preparation.
Three runs on each backend (OpenGL and Vulkan agree), same corpus and trace:

| Preparations | Coarser 1 | Coarser 0 |
| --- | ---: | ---: |
| Total | 289–298 | 501–511 |
| First for a cached tile | 71–72 | 129–132 |
| Again for a new style zoom | 217–227 | 370–379 |
| Other style change, repeat, eviction | 0 | 0 |
| Wasted (obsolete when finished) | 4–7 | 6–8 |
| For prefetch-ring tiles | 173–180 | 203–210 |
| For fallback parents | 41–43 | 81–90 |

Every adopted style epoch prepares every desired tile again. The trace crosses
15 sixteenth-zoom steps, and each one makes `liveSource.update` submit a new
`Style`; `reuseStyle` only helps when the exact previous inputs return. Resident
geometry, symbols and dashes keep the GPU buffers (`ReusedVersions` ~1,450 at
Coarser 1), but the CPU still decodes, evaluates and tessellates the tile again
to produce those identical bytes. Most desired tiles aren't on screen: the
prefetch ring and fallback parents take 74% of preparations at Coarser 1 and 57%
at Coarser 0. A one-off cross-tabulation (two runs each) put the style-zoom
re-preparations at 58–59 visible, 135–140 ring and 24–28 parent tiles at
Coarser 1, and 168–175, 148–150 and 54–59 at Coarser 0. A preparation and build
average about 12 ms on the one compiler goroutine, so the ring's style-zoom
re-preparations alone cost about 1.7 s of the 7 s CPU at Coarser 1.

Most of a re-preparation doesn't depend on the style zoom. A CPU profile of
`tiles.Prepare` over the 20 corpus z9 tiles at eight sixteenth-zoom steps
(Coarser 1, the viewer's resident options; 160 preparations in 1.65 s) spent 59%
in `mvt.DecodeTile` and 33% triangulating fills, on identical bytes each time;
extruded lines, width-independent with resident geometry, took most of the rest.
Style evaluation and symbol preparation took about a tenth. Go's background GC
added another 1.06 s on other cores. Build, repeated per epoch too, wasn't
profiled.

#### Reusing decoded tiles across style zooms

`tiles.Decode`/`PrepareSource` split decoding from compilation, and
`producer.Limits.ReuseDecoded` (viewer `-reuse-decoded`, on) keeps each tile's
decoded source so a new style zoom skips decoding and Earcut. Output is
unchanged: a shared source prepares the same `Prepared` as a fresh decode on the
synthetic and pinned Madrid tiles at every zoom tested.

The first version kept decoded tiles as `[]mvt.Feature`, a property map and
several slices per feature. It cut preparation wall time by 40% but made process
CPU a third higher and RSS 55–140 MiB larger: with the sources retained, GC CPU
went from 1.31 to 4.15 s in a `GODEBUG=gctrace=1` run at Coarser 1, because every
cycle scanned them. Retaining 62 corpus tiles cost 6.9 ms per forced GC cycle
(459k objects): about 3 ms for the property maps and 3 ms for the geometry slices.

`DecodeTile` now stores each source layer in an `mvt.FeatureSet`: geometry, ring
bounds and triangulation in a few flat arrays, and properties as tag pairs into
the layer's key and value tables. Compiler and placement read features through
`mvt.Features.At`; the legacy renderer keeps `[]Feature` through `FeatureSlice`.
The same 62 tiles cost 1.9 ms per forced cycle (231k objects, mostly per-layer
property values). Five runs per row, same corpus and trace:

| Row | CPU, reuse off → on | Settle, off → on | Peak RSS, off → on |
| --- | ---: | ---: | ---: |
| Coarser 1, OpenGL | 6.92–7.27 → 6.16–6.36 s | 70–83 → 75–84 ms | 479–521 → 524–549 MiB |
| Coarser 1, Vulkan | 6.74–7.17 → 6.02–6.45 s | 74–84 → 75–84 ms | 432–474 → 468–512 MiB |
| Coarser 0, OpenGL | 8.47–8.90 → 8.20–8.69 s | 319–353 → 249–304 ms | 598–687 → 635–696 MiB |
| Coarser 0, Vulkan | 8.30–8.69 → 7.87–8.23 s | 331–348 → 253–333 ms | 547–600 → 636–670 MiB |

Preparation wall time fell about 45% (2.25–2.57 s to 1.18–1.49 s); build rose
about 4% and producer selection 15–25%. With reuse, GC CPU was 1.23 s against
1.35 s without it (191 against 275 cycles). Presentation stayed at zero late
frames in every run. Without reuse the flat representation costs no more than
`[]Feature` did (Coarser 1 OpenGL 6.92–7.27 s against 7.19–7.57 s earlier). Some
Coarser 0 Vulkan runs read 0 VRAM from fdinfo while reporting GPU time; the GPU
memory column is unreliable for that row.

#### Where the CPU and memory go

Both modes also print `threads` (on-CPU time of the live threads from
`/proc/self/task/*/schedstat`, grouped by name; `main` is Qt's GUI thread and
Go's own threads carry the process name), `go` (the runtime's GC CPU estimate,
cycles, bytes allocated, resident and live heap) and `memory` (current RSS
split, and the moment of highest RSS sampled every 50 ms with the Go runtime's
share and its time, `peak_sample_at`). `-cpuprofile` and `-memprofile` write Go profiles that end where the
costs are read.

One profiled Coarser 1 OpenGL run after the decoded reuse above: of 6.75 s
process CPU, Go's threads took 6.04 s. Qt's render thread took 0.36 s, the GUI
thread 0.12 s, and Mesa, driver and Wayland threads about 0.25 s, so the Qt and
driver side costs about 0.7 s, less than MapLibre's whole process. Go allocated
10.9 GB during the 8 s trace (188 collections, about 1.2 s of GC). At the peak
RSS sample (555 MiB) the Go runtime held 396 MiB, 176 MiB of it live objects,
and everything else 159 MiB; the live heap was mostly CPU copies of fragments
(peak 173 MB) and decoded sources. Native memory is in line with MapLibre's 220
MiB whole process; Go's heap headroom and allocation churn are the difference.

The CPU profile (6.46 s of samples) spent 1.26 s (19%) validating meshes,
1.44 s preparing, 1.31 s building, 0.86 s selecting and about 0.75 s in GC mark
workers.

##### Validating each mesh version once

`scene.Mesh.Validate` checked every vertex coordinate with a float64
conversion, `IsNaN` and `IsInf`, branching per value, and the same vertices
were checked three times: when `SceneBuilder` finished a fragment (0.27 s),
when the Store applied it (0.20 s), and on every `Planner.SetTarget` for the
whole composed scene (0.79 s), including meshes already resident. The check is
now one maximum over each value's magnitude bits per section (2.1× faster on a
3.9 MB mesh), and `SetTarget` skips the vertex and index checks of mesh
versions that are resident or already targeted (`Scene.ValidateExcept`). A
Version already always refers to the same immutable payload, and a resident one
is never uploaded again; sizes, IDs and draws are still checked on every
target. Five runs each against the decoded-reuse rows above:

| Row | CPU before → after |
| --- | ---: |
| Coarser 1, OpenGL | 6.16–6.36 → 5.62–6.16 s |
| Coarser 1, Vulkan | 6.02–6.45 → 5.38–5.93 s |
| Coarser 0, OpenGL | 8.20–8.69 → 7.31–7.79 s |
| Coarser 0, Vulkan | 7.87–8.23 → 7.07–7.49 s |

##### Deferring hidden tiles at new style zooms

`producer.Limits.DeferHiddenRefresh` (viewer `-defer-hidden-refresh`) prepares
a tile again for newer paint inputs only once it is in the selected cover:
drawn, or standing in for a drawn target. Prefetch-ring tiles beyond the draw
margin and covered parents keep their older fragment until a pan selects them;
their decoded source is retained, so that costs one preparation and build
(about 8 ms) before the next coherent publication. Tiles without a fragment are
still prepared ahead, and deferred tiles don't count as `Status.Pending`.
Interleaved five-run A/B, same binary:

| Row | Preparations off → on | CPU off → on | Settle off → on |
| --- | ---: | ---: | ---: |
| Coarser 1, OpenGL | 297–302 → 216–219 | 5.55–6.24 → 4.97–5.44 s | 82–89 → 79–88 ms |
| Coarser 1, Vulkan | 298–299 → 217–218 | 5.59–5.94 → 4.93–5.15 s | 70–85 → 78–88 ms |
| Coarser 0, OpenGL | 533–547 → 457–467 | 7.28–7.65 → 6.88–7.45 s | 281–343 → 282–338 ms |
| Coarser 0, Vulkan | 538–547 → 459–464 | 7.11–7.64 → 6.69–6.98 s | 254–341 → 280–343 ms |

Final frames are pixel-identical, every run had zero late frames and no
fallbacks in Current, and Current changed as often (70–75 times at Coarser 1,
49–55 at Coarser 0). The longest hold of one drawable document reached 1.20 s
in two of five Coarser 0 runs with deferral against at most 1.00 s without.
Most ring tiles at Coarser 1 lie within the 256 px draw margin, so they are
drawn and still refresh. The viewer turns deferral on.

##### Fewer camera-only selections

The trace moves the camera every 8 ms and each request reselected the cover:
placement, collision and composition, 750–790 times at Coarser 1. The bridge
promotes a document only when a native packet progresses, so it set 91 targets;
it coalesced 265 documents in its pending slot and dropped 418 that matched the
current snapshot. `producer.Limits.CameraSelectInterval` (viewer
`-camera-select-interval`) is the least time between selections that only the
camera asks for; requests within it coalesce and a timer selects the latest
camera when it ends. Installed tiles, new targets, paint inputs and Current
coverage still select at once, and a deferred selection counts as pending, so
settlement waits for it. Labels are placed in shaders from resident anchors
every frame either way; the interval only delays which labels collision
admits. Five runs each, OpenGL, with deferral on:

| Interval | Coarser 1 CPU | Selections | Current changes | Coarser 0 CPU | Selections | Current changes |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 0 | 5.04–5.38 s | 754–787 | 70–73 | 6.65–7.46 s | 588–724 | 50–57 |
| 16 ms | 4.57–4.87 s | 409–437 | 67–71 | 6.48–6.83 s | 386–413 | 47–54 |
| 33 ms | 4.61–4.92 s | 244–258 | 61–67 | 6.19–6.70 s | 265–300 | 44–51 |

Settlement (51–85 ms at Coarser 1, 280–340 ms at Coarser 0), labels and peak
RSS didn't change, and every run had zero late frames. 16 ms, one refresh at 60
Hz, keeps the Coarser 1 saving and nearly every Current change; 33 ms saves more
at Coarser 0 but updates Current about a tenth less often. The viewer uses 16 ms.
Together with the validation change, Coarser 1 OpenGL went from 6.16–6.36 s to
4.57–4.87 s of process CPU, against MapLibre's 1.20–1.31 s.

##### Allocation churn

With those defaults Go still allocated 8.2 GB per trace at Coarser 1, and at the
peak RSS sample its heap held 331 MiB of objects of which about 170 MiB were
live: peak RSS is mostly garbage awaiting collection. Preparation allocated 4.3
GB (lines 2.5 GB), build 2.7 GB and selection 0.5 GB. Two scratch buffers were
thrown away on every use:

- Extruded and dashed line tessellation wrote a fresh array of `{Anchor,
  Direction}` vertices for every batch, which the compiler immediately copied
  into the primitive's separate anchor and direction arrays.
  `geometry.TessellateExtrudedLinesInto` and `TessellateDashedLinesInto` reuse
  the caller's storage, and compilers keep it across tiles in a `sync.Pool`;
  the primitive takes exact-length copies.
- `tiles.Set.place` rebuilt its collision reference list from nil for every
  selection. The single-owner Set now reuses it; references hold no pointers.

Allocation per trace fell from 8.2 to 6.7 GB and GC CPU from 0.96 to 0.79 s in
single profiled runs.

The viewer's `-gc-percent 50` trades CPU for heap. Three runs each on OpenGL
with the changes above:

| gc-percent | Coarser 1 CPU | Coarser 1 RSS | GC CPU | Coarser 0 CPU | Coarser 0 RSS |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 25 | 5.42–5.59 s | 444–451 MiB | 1.47–1.52 s | 7.07–7.45 s | 581–610 MiB |
| 50 | 4.51–4.83 s | 487–512 MiB | 0.73–0.81 s | 6.18–6.41 s | 664–695 MiB |
| 100 | 4.22–4.57 s | 605–613 MiB | 0.38–0.39 s | 5.57–5.95 s | 805–862 MiB |
| 200 | 3.94–4.08 s | 749–906 MiB | 0.19–0.20 s | 5.11–5.63 s | 1095–1127 MiB |

Each doubling halves GC CPU and adds 20–35% peak RSS; no setting approaches
MapLibre on both. Allocating less moves the whole curve, so the default stays.

##### Borrowing the stable mesh across style zooms

At every sixteenth step each drawn tile was still prepared and built in full,
and the Store then found its stable mesh (fills, patterns, extruded and
shader-dashed lines) byte-identical: on the corpus it was identical at all 300
steps of the 20 z9 tiles and all 630 of the 42 z10 tiles, including steps across
an integer zoom. `tiles.PrepareSourceReusing` (`producer.Limits.ReuseStable`,
viewer `-reuse-stable`) proves that before tessellating. Preparation records a
`compiler.StablePlan`: per stable batch, a seeded hash of its kind, layer, cap,
join and source feature indices, its size and whether paint emitted it, and after
a build the draw range it got. A preparation of the same decoded source at another
style zoom evaluates the style, checks each batch against the plan, and emits
placeholders instead of geometry; the build publishes the previous `StableMesh`
as is and draws the placeholders from their recorded ranges with new materials.
A different batch, a batch that paint now hides or shows, or a pattern sprite
that appears or goes missing falls back to the full path, so output is identical
either way. The Store recognizes the shared buffers, which keep their revision
with neither a byte comparison nor validation. Five interleaved runs each, same
binary:

| Row | CPU off → on | Peak RSS off → on | Allocated | Prepare + build wall |
| --- | ---: | ---: | ---: | ---: |
| Coarser 1, OpenGL | 4.38–4.66 → 3.84–3.96 s | 500–546 → 426–514 MiB | 6.7 → 4.4 GB | 2.05–2.18 → 1.61–1.65 s |
| Coarser 1, Vulkan | 4.38–4.62 → 3.59–3.89 s | 463–512 → 449–474 MiB | 6.7 → 4.3 GB | 2.07–2.23 → 1.53–1.67 s |
| Coarser 0, OpenGL | 5.88–6.48 → 5.37–5.72 s | 671–705 → 542–564 MiB | 9.3 → 6.3 GB | 2.55–2.89 → 2.05–2.24 s |
| Coarser 0, Vulkan | 5.64–6.36 → 5.07–5.31 s | 617–658 → 497–512 MiB | 9.2 → 6.3 GB | 2.57–2.93 → 2.04–2.16 s |

Every style-zoom preparation borrowed (147–148 at Coarser 1, 320–335 at Coarser
0). Final frames are pixel-identical, every run had zero late frames, and
settlement was 49–99 ms at Coarser 1 and 288–355 ms at Coarser 0 in both modes.
GC CPU fell about a fifth. Peak RSS falls because less garbage is in flight, not
because anything is retained less: a plan is about 32 bytes per stable batch.
Producer selection took 10–20% longer in total with borrowing; it wasn't
investigated, but its largest share turned out to be a sort (below). The viewer
turns it on.

##### Selection sort, text reservation and feature scratch

One profiled Coarser 1 OpenGL run at 0c7e961 (3.84 s process CPU, 3.54 s
sampled): preparation 0.87 s (decoding 0.49 s, most of it Earcut; compiling
layers 0.38 s), build 0.66 s, selection 0.52 s, GC mark workers 0.39 s and Qt's
calls into Go 0.25 s. Go allocated 4.3 GB. Three of those costs had a plain
cause:

- `placement.SelectSymbols` sorted its 128-byte references in place with
  `sort.SliceStable`, which moves elements through reflection: 0.23 s of
  selection. It now orders small ranks (order, sort key, input index): a
  counting sort by layer order, then a comparison sort only within layers whose
  sort keys vary. Tiles already hand references over in descending layer order,
  which a comparison sort gains little from; a rank pdqsort alone saved only 0.07
  s. The caller's slice is no longer reordered. `tiles.Set.compose` sorts draws
  with the generic stable sort.
- `SceneBuilder.IndexedText` transformed each label into a fresh slice and
  appended it to a symbol mesh that nothing had reserved, so every build grew it
  by doubling: 0.75 GB per trace. `FragmentBuilder.ReserveSymbols` sizes it once
  per build, and one buffer holds each label's transformed vertices.
- Fill and line compilers declared a new `mvt.Feature` per layer, so
  `FeatureSet.At` grew its containers again for every layer, and OpenFreeMap
  merges a road class into features with thousands of parts: 0.35 GB per trace.
  A tile's pooled compiler scratch now holds one Feature for all its layers;
  `Feature.Reset` drops its references into the decoded set before the pool
  keeps it.

Five interleaved runs each of 0c7e961, the sort alone, and both:

| Row | CPU before → sort → both | Selection wall before → sort | Allocated before → both | GC CPU before → both |
| --- | ---: | ---: | ---: | ---: |
| Coarser 1, OpenGL | 3.80–4.05 → 3.72–3.89 → 3.57–3.69 s | 467–486 → 344–373 ms | 4.4 → 3.3 GB | 0.60–0.67 → 0.49–0.54 s |
| Coarser 1, Vulkan | 3.64–3.85 → 3.63–3.73 → 3.28–3.51 s | 464–504 → 343–397 ms | 4.3 → 3.3 GB | 0.57–0.66 → 0.48–0.52 s |
| Coarser 0, OpenGL | 5.26–5.83 → 5.28–5.52 → 4.95–5.22 s | 827–915 → 580–639 ms | 6.3 → 4.3 GB | 0.82–0.88 → 0.62–0.65 s |
| Coarser 0, Vulkan | 5.08–5.40 → 4.96–5.25 → 4.54–4.78 s | 823–907 → 582–654 ms | 6.3 → 4.3 GB | 0.82–0.89 → 0.59–0.65 s |

Selection is a quarter to 30% faster; on its own that is mostly within the
spread of process CPU. Allocating a quarter to a third less cuts GC and build
time, and together the two take 7–11% of process CPU. Peak RSS didn't move
(Coarser 1 OpenGL 432–514 → 484–493 MiB, Coarser 0 OpenGL 538–548 → 525–555
MiB): less garbage, but not a lower peak. Final frames are byte-identical, every
run had zero late frames, labels and settlement didn't change. Since ev17 began,
Coarser 1 OpenGL went from 6.16–6.36 s to 3.57–3.69 s, against MapLibre's
1.20–1.31 s.

##### Earcut allocation and when RSS peaks

Decoding was then the largest allocator (0.9 GB per Coarser 1 trace) and
Earcut most of its CPU. The port allocated every node of a polygon separately,
and a second object for each node's z-order hash. Each ring's nodes now share
one allocation, the hash is a plain field, and the triangle list is sized once
(a polygon of n vertices and h holes has at most n+2h-2 triangles). Decoding the
83 corpus tiles takes 250–257 ms against 275–279 ms (interleaved, best of 7) and
allocates 1.65 million objects instead of 3.61 million. Every one of the 77,192
corpus polygons triangulates to the same indices, indexed and expanded. Upstream
earcut's per-point bbox test before `pointInTriangle` made no measurable
difference here: the ear tests are bound by walking the node lists.

`peak_sample_at` shows that peak RSS is not the load burst: it was sampled 3.5 s
into the Coarser 1 trace and at its end at Coarser 0. At that moment Go held 330
MiB of about 480 MiB, and 116 MiB was file-backed libraries, so the RSS gap is Go
memory that stays resident, mostly fragments kept on the CPU (150 MB at their
peak), not decoding garbage.

##### Text layouts within a build

A build lays out every text candidate (0.21 s of the profile: metric layout, glyph
atlas and unit meshes). Line labels repeat their text at each anchor, but only 11%
of a preparation's requests repeat on the corpus, so identical requests now share
one layout and one mesh, and unit-mesh builders reserve room for every glyph
instead of growing. On the corpus's text pipeline output is identical, time falls
4–6% and allocation 26%. The rest is map hashing of glyph keys that carry the
font-stack string.

Reusing layouts across style zooms would save about 0.12 s per Coarser 1 trace,
but each cached tile would keep about 540 KB of glyph positions, unit meshes and
atlas (12% on top of its fragment), roughly 13–27 MB more RSS; that trade was
declined.

Five interleaved runs each of 9c264f2 and both steps above (Earcut and text):

| Row | CPU before → after | Prepare wall before → after | Allocated before → after |
| --- | ---: | ---: | ---: |
| Coarser 1, OpenGL | 3.59–3.77 → 3.53–3.78 s | 908–936 → 872–903 ms | 3.3 → 3.2 GB |
| Coarser 1, Vulkan | 3.33–3.58 → 3.30–3.44 s | 861–923 → 871–896 ms | 3.3 → 3.1 GB |
| Coarser 0, OpenGL | 4.92–5.06 → 4.80–5.01 s | 1044–1069 → 998–1079 ms | 4.4 → 4.2 GB |
| Coarser 0, Vulkan | 4.54–4.98 → 4.58–4.72 s | 1008–1073 → 975–1035 ms | 4.3 → 4.1 GB |

Process CPU moved within its spread; preparation and allocation fell a few
percent. Final frames are byte-identical, every run had zero late frames, and
labels, settlement and peak RSS didn't change.

##### What holds the live heap

Go's resident memory follows its heap goal, the live heap times 1.5 at
`-gc-percent 50`, so peak RSS is set by the largest live heap, not by garbage.
The `memory` line now also prints `peak_live_kib` and `peak_live_at` (the
runtime's live heap after each collection, sampled every 50 ms), and
`-memprofile-peak` writes a heap profile whenever that peak grows 2%, so the
last one shows what was live at the worst moment. Coarser 1 peaked at 233 MiB
live 3.55 s into the trace (Go resident 358 MiB), Coarser 0 at 272 MiB.

At the Coarser 1 peak about 137 MB were fragment meshes (mostly extruded-line
vertices of four float32 values, 16 bytes each, and uint32 indices), 60 MB
decoded sources, 8 MB raw responses and 10 MB metadata. The Store and leases
share the fragments' buffers rather than copying them.

The producer had kept every tile it loaded: nothing was evicted under the
viewer's 384 MiB `-cpu-cache-bytes`. Three OpenGL runs per budget:

| Budget | Coarser 1 CPU | Coarser 1 RSS | Coarser 1 peak live | Coarser 0 CPU | Coarser 0 RSS | Coarser 0 evictions |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 96 MiB | 5.62–5.74 s | 427–453 MiB | 174–185 MiB | does not settle | 463–470 MiB | 2 |
| 128 MiB | 4.17–4.30 s | 434–453 MiB | 177–190 MiB | 6.53–6.60 s | 576–590 MiB | 40–44 |
| 192 MiB | 3.48–3.54 s | 424–498 MiB | 167–223 MiB | 5.38–5.51 s | 562–586 MiB | 1–7 |
| 256 MiB | 3.34–3.57 s | 417–488 MiB | 171–212 MiB | 4.80–5.05 s | 525–541 MiB | 0 |
| 384 MiB | 3.40–3.51 s | 429–494 MiB | 169–215 MiB | 4.85–5.05 s | 527–538 MiB | 0 |

A smaller budget doesn't lower RSS: the live peak is what the requested tiles
need while their replacements build, and evicting tiles that come back costs
preparation, allocation and, at Coarser 0, more garbage than it frees. Every
run had zero late frames. The levers are smaller data per tile (vertex and
index formats, decoded triangulation) and not keeping CPU copies of resident
geometry, not the budget.

#### Vulkan FIFO stalls in live mode on native Wayland

In live mode on Vulkan under headless mutter, the default FIFO present mode (and
IMMEDIATE) stalls after 16–157 frames. Qt's Wayland plugin holds update requests
until the frame callback it requested before presenting arrives. The callback
never comes, the render thread idles in
`QSGRenderThread::processEventsAndWaitForMore`, and after 100 ms Qt reports
"Didn't receive frame callback in time, window should now be inexposed"
(`exposed=false` in the viewer's counters). With
`QT_WAYLAND_FRAME_CALLBACK_TIMEOUT=0` the window stays exposed but still stops
rendering. OpenGL live replays and an 8 s Vulkan fixture replay didn't stall.
mutter advertises `wp_fifo_v1` and `wp_commit_timing_v1`, which Mesa's WSI uses
for FIFO. `MESA_VK_WSI_PRESENT_MODE=mailbox` avoids it and matches how Qt
throttles OpenGL on Wayland: frame callbacks with swap interval 0. Not yet
reproduced on a desktop session.

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

Collision errors return no partial acceptance map; the parent reports a warning and
skips acceptance for that job. (The selector sorted its input in place until ev17;
it now sorts ranks and leaves the slice as passed.)
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

### Bounded native batch execution and completion drains

Committed revision-aware resource staging as **bff8a97**, then continued with kata
**qfmf**. `NewBatchRenderer` now provides a separate native execution path for
`retained.Planner` batches. `Sync` consumes a packet containing a validated frame
for Planner.Current and at most one batch. The worker owns the Planner and deep
validation; native callbacks never call it or rescan geometry/pixels. The existing
viewer still uses the ordinary renderer. Worker mailbox/wakeup integration is next.

Selection is separate from retirement. The batch renderer requires selected versions
to be resident, uploads only the resources in its accepted batch and retires only
explicit release descriptors. It validates the whole release list before touching
handles. Old pixels and their transform slots remain selected during partial uploads;
activation uses the new Current frame and transforms without another geometry upload.
Allocation failure rolls back all fresh objects in that batch while preserving prior
successful batches and the selected frame. New tickets permit retry and supersession.

The completion sequence is intentionally conservative:

1. Prepare records a bounded upload batch, or schedules explicit `DeleteLater`
   retirements. Neither operation acknowledges the Planner.
2. A generated **direct** `QQuickWindow::afterFrameEnd` connection observes submission
   on the emitting render thread. It still does not reclaim residency.
3. The following prepare checks the same healthy native context and calls
   `QRhi::finish`, outside a render pass. Only then is the result queued to the worker.
   Failed allocations also drain before reporting failure so retries cannot accumulate
   discarded native allocations outside Planner accounting.

Qt 6.11.2 source inspection matters here. Vulkan finish waits for the graphics queue
and executes deferred releases. **OpenGL finish outside a frame does nothing**; its
next beginFrame processes deferred deletions, then an in-frame finish calls glFinish.
An afterFrameEnd-only drain would therefore be insufficient. This implementation
blocks and may submit other already-recorded preparation commands on the same QRhi.
It is a lifetime-correct baseline, not evidence of bounded elapsed frame time or
MapLibre-class performance. A nonblocking completion strategy remains future work.

Callers must keep requesting frames while a batch is outstanding. Hidden/suspended
windows delay completion; grabWindow alone does not pump afterFrameEnd. Sync rejects
updates while busy, so camera changes must currently be coalesced between batches.
Reset signals invalidate the entire renderer/Planner namespace and require new
instances; they must not be interpreted as ordinary failed-ticket acknowledgements.
Signal handles live until window destruction, while node destruction severs the Go
reference to scene payloads. Inert per-node signal registrations can persist until
the window dies; explicit disconnection is a follow-up for repeated node recreation.
White texture and uniform/binding/pipeline allocations remain outside Planner budgets.

Bindings for afterFrameEnd, finish, isRecordingFrame and isDeviceLost were regenerated
with `cmd/qt-rhi-gen`. The generic signal adapter uses DirectConnection and frees its
Go callback handle when the sender dies. No generated files were hand-edited, no
rendering algorithm was added to generator templates and no module dependency changed.

Verification on Qt 6.11.2 / Mesa **26.2.2**:

- Desktop Vulkan on Radeon 860M / RADV KRACKAN1 passes expanded/indexed native tests.
- Xvfb OpenGL/llvmpipe passes, including 2× scale and GOAMD64=v1 race. Tests exercise
  per-batch byte/count bounds, no early acknowledgement, old pixels during partial
  uploads, multi-allocation rollback, retries, supersession, new transform-slot
  publication, camera-only retention, activation without reupload, empty selection,
  retirement and teardown with a pending batch. A cross-thread signal probe verifies
  direct delivery on the emitting OS thread; full threaded-render-loop integration
  remains a following transport test.
- Native renderer coverage is **90.1%**; upload/release execution, ticket guards,
  selected-resource checks, frame-end observation and invalidation are 100% covered.
  Completion/device-loss and native initialization/pipeline failure guards remain
  partially uncovered. Final profile: `/tmp/opencode/vecmap-batch-final-coverage.out`.
- Full v4 module coverage with pinned fixtures and binding reproduction passes, as
  do generated lifecycle/pair tests, viewer tests, tagged targeted staticcheck, default
  application build, tagged v1 viewer build and CGO_ENABLED=0 fixture build.
- Default staticcheck retains only the pre-existing generated ST1006 findings.
  Gopls cannot obtain metadata for the opt-in integration file; tagged checks provide
  validation. Vulnerability baseline remains GO-2026-5024. Complexity review leaves
  explicit native prepare/pipeline/resource/render guards at 15/14/14/11; new batch
  implementation functions are all <=10.
- The first desktop Vulkan run had black-pixel failures in an existing lifetime
  case; subsequent runs passed. Cause remains unconfirmed. The initial new test
  incorrectly assumed grabWindow would emit frame-end; it now pumps real window
  frames. A race build exceeded its initial 120-second timeout compiling Qt and
  passed with a longer timeout. Existing Qt/GCC QChar warnings remain environmental.

No new full-fixture screenshot or presentation/performance comparison is claimed in
this checkpoint. The native adapter README specifies transport/lifetime obligations
for the next worker-mailbox integration.

### Worker-owned planning in the fixture viewer

Committed the bounded native executor as **229451b**, then continued with kata
**qfkf**. The opt-in QRhi viewer now uses `retained.Worker` and `BatchRenderer` by
default. Its new flags are `-upload-bytes` (default **32 MiB**) and
`-upload-resources` (default **2**). Budgets are positive and bounded by Planner
residency limits; indivisible meshes/textures still produce ErrBudget when too large.
The 32 MiB default admits the existing expanded fixture's mesh without chunking.

`retained.Worker` owns one Planner on one goroutine, with a latest-target slot and
single-slot packet/acknowledgement queues. Same-generation targets coalesce until
the outstanding packet completes. Packets couple Current selection to native work;
even draw-only/empty publication requires consumption acknowledgement. Validation
and planning stay off the GUI/render thread. Target/budget errors report once and
pause until a new target/reset rather than spinning or bypassing the budget.

Native generations are explicit. Restart replaces the Planner and drops old queued
messages without spawning another goroutine. Consumers reject old packets and
callbacks by generation, and stale sequence/generation acknowledgements cannot
commit new residency. Closing the worker cancels queued/outstanding CPU work without
waiting for a GPU result; shutdown joins it after native teardown. This preserves
toolkit-neutral queue/planning logic and adds no module dependency.

The viewer's render-thread facade consumes packets only when its native executor is
available. Its existing **8 ms GUI timer** polls/pumps frames through startup and
partial uploads, even when Current is empty. Camera updates coalesce while native
work is pending. A fixed document retains the same transform slots across generations;
live target changes will need explicit scene-associated frame/placement metadata.
The source document is still loaded/validated before opening the window; this is
not a live tile ingestion path or a replacement for the production map scheduler.

Resource release, node recreation and reset-before-acknowledgement now drive a new
generation automatically. Each BatchRenderer has an initial prepared-frame/endFrame/
next-prepare drain **before its first Planner-owned allocation**, so replacing a node
on the same QRhi cannot overlook its predecessor's deferred native releases. Subsequent
batch completion retains the previous conservative drain. `Stats.PrepareTime` now
includes the whole batch callback, including upload allocation/recording and drains.
These drains remain blocking and can stall unrelated work on the same graphics queue.
Inert per-node frame-end signal registrations still live until window destruction.

The CLI reports readiness, generation, batch failures and configured limits. It
returns an error if uploads have not become ready before exit, and does not save a
requested screenshot at the duration cutoff while still loading. Upload/live counters
describe the latest native renderer namespace; native recreation resets those counters.
Timing sample arrays retain warm samples across generations, but callback cadence
does not bridge a reset. The existing first-30-frame sampling filter is unchanged.

Verification:

- Headless Worker tests cover readiness, acknowledgement ordering, failure/retry,
  supersession, old/new namespaces, stale tokens, queue bounds, target/budget errors,
  concurrent producers, cancellation and exhaustion guards. Retained coverage is
  **99.7%**, with Store/Planner still at 100%; the worker-loop return forwarding
  sequence exhaustion is not exercised with 2^64 real packets. 386 and v1 race pass.
- Real viewer transport tests run in separate **basic and threaded Qt processes**
  on desktop Vulkan/RADV and Xvfb OpenGL/llvmpipe. The threaded test verifies rendering
  runs on a different OS thread. OpenGL also passes **2× scale with v1 race**.
- Native tests check initial publication, camera-only pixels without uploads, idle
  recreation, explicit release after upload recording but before acknowledgement,
  stale-epoch reset rejection, fresh-namespace reupload and cancellation at teardown.
  This exercises native release/recreation, not physical GPU-device-loss injection.
  An oversized-budget-unit test rejects before any geometry upload; the CLI Vulkan
  smoke with `-upload-bytes 1` reports ErrBudget and only four white-texture bytes.
- Full v4 module coverage with pinned fixtures, targeted tagged staticcheck, default
  app build, tagged v1 viewer build and headless fixture build pass. Default staticcheck
  still reports generated ST1006 findings. Gopls resolves the headless changes but
  reports build-tag/package-metadata errors for the opt-in native files; tagged tests
  and staticcheck provide validation. GO-2026-5024 remains the vulnerability baseline.
- Complexity review retains explicit state-machine/transport guards: Worker.run13,
  workerState.advance13, viewerStream.sync13 and the existing display orchestration19.
  They keep cancellation, publication and acknowledgement order visible.
- The initial basic-loop reset test queued invalidation for a later GUI tick; the
  upload could complete first. The test now invalidates directly from the render
  observer after recording and before endFrame, making the intended lifetime case
  deterministic. Both loops then pass, including the race checks.

Expanded/direct full-fixture static Vulkan PNGs are **byte-identical** to their old
references. Both use the actual threaded viewer with a **one-resource batch budget**,
45 draws, 62 labels and complete fonts. Total uploads remain 19,045,116 expanded bytes
or 11,834,328 direct bytes, one mesh and five textures including white; logical caches
are empty at teardown. Artifacts: `/tmp/opencode/vecmap-rhi-worker`,
`vecmap-{direct,expanded}-worker.png`, `vecmap-worker-coverage.out` and
`vecmap-viewer-stream-coverage.out`.

Two-second static smoke samples are retained below, not treated as controlled
performance comparisons. The direct capture preceded the change to include the
entire batch callback in prepare timing. Both samples apply the existing warm-frame
filter; they do not measure cold preparation/upload latency or actual presentation.

| Capture | Frames | Prepare µs p95 | Submit µs p95 | Previous Qt GPU frame ms p95 | Callback ms p95 |
| --- | ---: | ---: | ---: | ---: | ---: |
| Direct worker smoke | 119 | 56.406 | 121.879 | 0.866446 | 18.249444 |
| Expanded worker smoke | 114 | 72.406 | 149.471 | 0.883198 | 17.789049 |

Next: remove/replace or measure the blocking native drains, add explicit signal
disconnection for long-lived repeated recreation, then integrate live Store targets
with scene-associated transforms, coverage/order/wrap and placement policy. No
MapLibre parity or production migration claim follows from this checkpoint.

### Checked submission and scoped frame-end subscriptions

Committed worker/viewer integration as **82729a3**, then continued with kata **z3cc**.
Source inspection found a gap in the preceding native completion proof: Qt 6.11.2's
[basic](https://github.com/qt/qtdeclarative/blob/v6.11.2/src/quick/scenegraph/qsgrenderloop.cpp)
and [threaded](https://github.com/qt/qtdeclarative/blob/v6.11.2/src/quick/scenegraph/qsgthreadedrenderloop.cpp)
render loops emit `afterFrameEnd` even after failed beginFrame/endFrame operations.
Checking only IsDeviceLost does not distinguish a general FrameOpError. A later
successful idle drain cannot prove that an earlier frame's uploads were submitted.
This supersedes the upload-submission assumption in the qfmf/qfkf chronology above.

The batch executor now calls and checks **QRhi::finish in the producing prepare**,
after recording resource updates and before Qt closes that command buffer. Initial
white/uniform uploads get the same explicit submission check. Submission failures
reset the entire native/Planner namespace rather than acknowledging an uncertain
batch as an ordinary allocation failure. This uses QRhi's supported in-frame,
outside-a-pass API and keeps all rendering policy in Go.

Frame-end observation remains a lifetime boundary. In the following prepare, a
successful upload can be acknowledged without a duplicate finish, because its
submission/completion was already checked. Releases and allocation rollback still
drain after the boundary before reclaiming Planner capacity. Startup now has two
finish calls: initialization submission, then previous-namespace retirement cleanup.
This remains a blocking correctness baseline. A future nonblocking path needs an
asynchronous completion mechanism with safe result ownership through cancellation
and context reset; frame-end signals alone cannot replace the proof.

`Stats.CompletionDrains` and `CompletionDrainTime` count native finish calls (including
failed attempts) and accumulate their render-thread wall time, including GPU waiting.
The CLI prints both. They cover startup/upload stalls even when the existing first-
30-frame filter omits those frames from prepare/submit percentile samples. Stats are
still scoped to the latest native renderer instance, not cumulative across resets.

Generated direct-signal subscriptions now return an explicitly owned
`*qtrhi.SignalConnection`. Disconnect removes only that subscriber and deletes its
native QMetaObject::Connection copy. Qt's functor owns the Go callback handle through
shared native lifetime storage, including a local reference across an active callback.
Explicit disconnect, self-disconnect and sender destruction therefore release the
callback safely; there is no separate sender-lifetime cleanup connection to accumulate.
BatchRenderer disconnects on node destruction, eliminating the inert per-node callbacks
left on long-lived windows by the previous checkpoints. The connection copy must still
be disposed after sender destruction. No cleanup depends on a Go finalizer.

The signal adapter is generic generated binding/lifetime code, not a C++ rendering
algorithm. Only the QQuickWindow binding triplet was regenerated; generator inputs
and reproduction tests are checked in. `internal/qtrhi/connection.go` uses the existing
MIQT QObject disconnect API. No dependency, shader or asset changed.

Verification on Qt 6.11.2 / Mesa 26.2.2:

- Binding tests create/disconnect **1,000 subscriptions on a live window**, preserve
  independent listeners, self-disconnect, destroy the sender before disposing the
  connection copy, and verify captured Go objects become collectible using weak
  probes. This checks callback-handle release before window destruction as well as
  signal delivery suppression.
- Native tests inject FrameOpError/FrameOpDeviceLost results after real submissions
  and require namespace reset with no successful/ordinary failed-ticket ack. They
  check exactly one finish per ordinary batch and two during startup, disconnection
  on node destruction, allocation rollback, old pixels during partial uploads,
  activation without reupload and retirement. Physical GPU loss is not injected.
- Desktop Vulkan tests and basic/threaded viewer process tests pass. OpenGL under
  Xvfb passes at 2× scale with v1 race, including the binding/lifetime tests.
- Native renderer coverage is **91.0%**. SignalConnection helpers and the checked
  drain helper have 100% coverage; native device/context and creation guards remain
  partly uncovered. Profile: `/tmp/opencode/vecmap-submission-coverage.out`.
- Full v4 module coverage with pinned fixtures, binding reproduction, tagged
  staticcheck and builds pass. Default staticcheck retains generated ST1006 findings;
  gopls still lacks native build-tag metadata. GO-2026-5024 remains the baseline.
  Targeted staticcheck caught an unused test-probe padding field; an equivalent
  fixed array keeps it outside Go's tiny allocator without an unused field.
- Complexity review retains the explicit prepare/complete lifecycle guards at 12
  each and the generator rewrite at 12. No map algorithm was moved into bindings.

Both expanded/direct full-fixture threaded Vulkan PNGs remain byte-identical to old
references, using one-resource batches, 45 draws, 62 labels and complete fonts.
Uploads remain 19,045,116 expanded bytes or 11,834,328 direct bytes. Each reports
**seven finish calls**: two startup calls and five planned upload batches. Live caches
are empty at teardown. Artifacts: `/tmp/opencode/vecmap-rhi-submission` and
`vecmap-{direct,expanded}-submission.png`.

The two-second static smokes below are not controlled performance comparisons. Drain
time is a cumulative synchronous native-call measurement; warm callback percentiles
are not presentation timestamps, nor a cold preparation/upload latency measurement.

| Capture | Frames | Finish calls | Total finish wall ms | Prepare µs p95 | Submit µs p95 | Previous Qt GPU ms p95 | Callback ms p95 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Direct checked submission | 121 | 7 | 2.915842 | 56.396 | 129.584 | 0.866806 | 17.105972 |
| Expanded checked submission | 123 | 7 | 2.920187 | 56.527 | 129.204 | 0.871896 | 17.093158 |

Next is asynchronous native completion with explicit failure/result lifetimes, then
live retained targets and scene-associated transform/coverage/order/wrap/placement
handoff. This checkpoint makes no frame-time or MapLibre parity claim.

### Asynchronous completion audit

Committed checked submission and scoped signals as **07301d5**. Follow-up **w47r**
audits the prospective readback-based replacement against Qt 6.11.2 source.
The findings and implementation gates are in
[Asynchronous QRhi completion](vecmap-rhi-completion.md).

Vulkan records pending readbacks before submission and can later invoke their
callbacks on a matching frame slot without a successful producing submission.
QRhi cleanup registrations are invoked and cleared before backend destruction,
which can still write readback result storage. OpenGL performs synchronous driver
readback calls and can abandon commands on context failure. The threaded loop also
fires `frameSwapped` after failed endFrame results.

These findings block a callback-only replacement, not every possible asynchronous
design. The checked executor remains in place. Proceeding requires selecting a
backend-specific completion integration or a Qt/render-loop integration with
explicit submission and final-reference boundaries; live target work can proceed
independently with the checked baseline. No native readback failure injection or
new performance measurement was performed in this audit.

The **2026-10-01** review resolves **w47r** with the decision to retain stock Qt
and checked submission. It also identifies a limit in that baseline:
`QRhiVulkan::finish` discards idle-wait and command-buffer restart errors before
returning success. **1aaq** tracks exposing those failures; checking QRhi's public
return value does not prove native completion when Qt hides an error. The audit
now includes the live-target replay evidence and explicit submission, completion,
retirement and final-native-access requirements for a future integration.

**1aaq** implements that native hardening with a version-pinned Qt patch and a
generated runtime-symbol check. Failed producing submission, native idle wait or
command-buffer restart prevents acknowledgement and poisons the Qt context until
destruction. The viewer stops on completion errors instead of recreating on that
context. Eighteen native fault subprocesses cover general/device-loss results
for wait, pool reset and restart; the basic-loop retirement cases keep the Planner
busy without reclaiming capacity. Both the corrected Vulkan and distribution
OpenGL 2× race suites pass. See the [completion checkpoint](vecmap-rhi-completion.md#checked-vulkan-completion-1aaq)
for the tested dependency boundary and limitations.

### Live target data and fixture replacement

After the asynchronous-completion audit, the user asked which direction best serves
the original performance goal. The recommendation was to retain stock Qt and its
checked drains while building realistic target transitions, then use measured
workloads to decide whether backend-specific completion is warranted. A Qt fork is
not a prerequisite for this checkpoint (**w8sf**).

The toolkit-neutral `WorkerWithData[T]` carries immutable producer data with the
Current scene. Existing Worker/Packet APIs remain aliases with empty data. Mapping
promotion occurs at the same acknowledgement that makes a scene ready, including
when another target is already queued. Superseded data has bounded lifetime; no
scene-pointer history map is needed. Same-scene data changes publish without
resource uploads. Producers validate and bound their own data.

The viewer uses documents as associated data. Camera state is independent of target
slot numbering. Partial uploads keep the old mapping; publication switches scene
and mapping together. An optional `-reload` interval runs a single background file
producer with a latest-document slot and a retained Store namespace. It detects
content changes, rejects invalid documents, and gives replacements fresh resource
revisions. See the [native contract](../internal/vecmaprhi/README.md#live-fixture-targets)
for usage and bounds. This is live fixture replacement, not network tile loading
or a cover/placement policy.

Verification:

- Headless Worker tests cover partial/retried uploads, final-ack supersession,
  rejected targets, same-scene data changes, clear/reset, stale generations and
  collection of abandoned mapping data. 386 and v1 race checks pass. Retained
  coverage remains **99.7%**, with new association helpers at 100%.
- Basic/threaded Vulkan and OpenGL viewer processes replace a one-slot scene with
  a two-slot scene, check old pixels during partial upload, then check the new
  mapping and a mapping-only publication without uploads. Clear/reselect and reset
  retain the correct association. OpenGL also passes at 2× scale with v1 race.
- Real viewer reload processes atomically replace a scene file while running,
  then verify the final screenshot uses the replacement's transform slot. Feed
  tests check unchanged-content suppression, fresh revisions, old snapshot
  immutability, invalid-input recovery, coordinate-mode/viewport guards and the
  file-size bound. Frame/camera helpers have 100% coverage. Native subprocess
  coverage is separate from the parent test profile; don't treat the parent's
  viewer percentage as integrated renderer coverage.
- Full v4 module coverage with pinned assets, tagged targeted staticcheck, default
  application, tagged viewer and headless fixture builds pass. Existing generated
  ST1006, gopls build-tag metadata and GO-2026-5024 baselines remain.
- Complexity review: worker advance 14, viewer sync 15, feed replacement 13, display
  24. These retain explicit protocol/validation guards. Display and the process
  test remain candidates for a focused harness refactor as more scenarios arrive.

Static expanded/direct threaded Vulkan PNGs are byte-identical to the previous
checked-submission references. Both retain 45 draws, 62 labels and seven drains.
Uploads are still 19,045,116 expanded bytes or 11,834,328 direct bytes. Artifacts:
`/tmp/opencode/vecmap-rhi-live-targets` and
`/tmp/opencode/vecmap-{direct,expanded}-live-targets.png`.

These two-second static smokes are correctness checks, not controlled performance
comparisons. Their full warm samples are recorded here to preserve the observations:

| Capture | Frames | Finish wall ms | Prepare µs p95 | Submit µs p95 | Previous Qt GPU ms p95 | Callback ms p95 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Direct live-target harness | 121 | 2.700751 | 55.725 | 126.168 | 0.864642 | 17.290775 |
| Expanded live-target harness | 121 | 3.251635 | 67.597 | 122.421 | 0.872618 | 18.903448 |

Next: feed retained tile fragments into this association boundary with explicit
cover/order/wrap and placement policy, then measure tile-arrival bursts alongside
camera traces. The production renderer remains separate. Neither asynchronous GPU
completion nor MapLibre parity is claimed.

### Retained tile coverage, ordering, wraps and placement

Committed live scene-associated targets as **03c33d5**, then continued with **9g4n**.
The new [`tiles`](../pkg/vecmap/tiles/README.md) package composes prepared tile scenes
over retained.Store. It is single-owner, toolkit-neutral and has no I/O or goroutines.
The original scheduler's sibling/parent/continuity selection moved into shared
`view.GroupTiles` / `SelectCover`; the production scheduler delegates to those helpers
while retaining its own loading, cancellation, budgeting and rendering paths.

The compositor adds the producer policy that Store intentionally lacks:

- Atomic bounded tile admission with copied layer/candidate/metric metadata and
  borrowed immutable geometry/pixels. Store still owns IDs and revisions.
- Explicit eligible continuity from acknowledged Current. A generated/queued
  snapshot never becomes continuity implicitly. A ready parent survives partial
  requested-sibling preparation; the Worker then preserves old Current through
  partial GPU uploads of the replacement cover.
- Stable layer-major composition with selected-tile/wrap/source-draw order inside
  each layer. World instances share resource versions and have scene-associated
  TileSpaces. Pattern phase uses original float64 periods, not reconstructed
  float32 material values.
- Cross-tile placement using the existing ProjectSymbol/SelectSymbols behavior,
  including reverse-candidate tie order, optional/overlap flags and metric-versus-
  atlas readiness. Per-candidate draw metadata filters icons and text passes.
- Identical snapshot pointers for camera-only changes that preserve coverage,
  wraps and acceptance. Placement still performs bounded CPU work; this is not a
  claim of zero-cost camera motion.

Bounds include the existing Store envelope, 10,000 candidates per tile, 100,000
incoming/stored/instanced candidate limits, pre-filter instanced draw limits and
the shared collision-work ceiling. Select failures return no partial snapshot.
Producer-owned strings, concurrent preparation and caller-held old snapshots retain
their separate lifetime/memory obligations.

Prepared input needs one metadata record per draw, with no coalescing across layer
or candidate boundaries. The flattened fixture cannot recover that provenance.
Its pinned test therefore uses real solid base draws with explicit source-order
metadata, checking payload sharing and revision isolation without claiming generic
compilation or multi-tile label parity. A generic compiler emitting this metadata
is the next step before bounded network loading and producer scheduling.

Verification:

- Tiles package coverage **98.8%**; only defensive Store.Snapshot error propagation
  remains uncovered after input preflight. New shared cover-selection helpers are
  **100%** covered; the broader view package is **89.2%**.
- Tests cover requested-sibling refinement, deep/mixed-depth continuity, overlap
  exclusion, draw ordering, exact wrap phases, shared payloads, cross-tile priority,
  icon/text readiness, no-draw collision reservations, atomic rejection, old snapshot
  lifetime, resource revisions and the Worker/Planner/native-ack contract.
- Headless 386 and v1 race pass. Composition fuzzing completed **68,283 executions**
  in the configured 20-second run with no failure.
- Basic/threaded Vulkan and OpenGL viewer processes pass. New pixel tests preserve
  a red parent through single-child preparation and the first child upload, then
  display the green/blue children together. OpenGL passes at 2× scale with v1 race.
- Full v4 module coverage with pinned fixtures, targeted staticcheck and default,
  headless and tagged viewer builds pass. Shared Go diagnostics are clean; native
  test metadata remains unavailable in untagged gopls. Default staticcheck retains
  generated ST1006 findings; GO-2026-5024 remains the earlier vulnerability baseline.
- Complexity review retains explicit metadata/admission guards (maximum 16 in
  preflight); the extracted cover branches are 13 each instead of the old combined
  selection function's 27. No rendering logic moved into C++ or binding templates.

Coverage: `/tmp/opencode/vecmap-tile-composition-coverage.out`. Build artifacts:
`/tmp/opencode/{whereami,vecmap-fixture,vecmap-rhi}-tile-composition`. No new full-map
screenshot or performance measurement was made. The checked GPU drains and
asynchronous-completion audit remain in force. Production renderer migration and
MapLibre parity still require realistic compiled-tile workloads and comparison gates.

### Generic prepared-tile compilation

Committed tile composition as **3b77d8e**, then continued with **re90**.
`tiles.Prepare` now decodes an arbitrary bounded MVT tile with application-supplied
compiled style layers. It owns reusable geometry and evaluated symbol candidates,
without retaining source feature/property maps. `Prepared.Build` consumes loaded
font/sprite snapshots, reuses the shared layout/atlas/packing algorithms and returns
an immutable `tiles.Fragment` plus missing-font and MVT-degradation diagnostics.
Asset refresh doesn't decode or tessellate the tile again. These methods do no I/O
and start no workers; bounded live loading/scheduling is still the next boundary.

The new `compiler.FragmentBuilder` preserves metadata while packing, rather than
trying to reconstruct it from flattened output. Every draw carries its layer,
candidate and base/icon/text part. Patterns retain their exact float64 period.
Draw boundaries prevent cross-candidate/layer coalescing; icons, all halos and all
fills retain their original pass order. Missing assets produce no orphan records.
Empty fragments are valid ready tiles. Ordinary SceneBuilder coalescing and its
existing fixture path remain intact. `tiles.Draw`/`Part` alias the compiler types,
and both producers share the candidate-to-text-request iterator.

Build packs every potentially drawable candidate before camera-dependent placement.
The compositor can change label acceptance without repacking geometry. This costs
extra retained geometry and draw records; selected-run coalescing and shared atlases
remain future optimizations. The default fallback predicate is nil; callers can
explicitly supply `glyph.LegacyFallbackEligible` to preserve the fixture's non-SDF
collision-readiness behavior. Metrics still reserve space when atlas coverage is
incomplete, without inventing drawable fallback text.

Input bounds retain the MVT 2 MiB ceiling and compiler geometry/text budgets, plus
at most 1,024 style layers in increasing nonnegative order and evaluated zoom 0–20.
PrepareOptions can lower aggregate triangles, candidates, packed elements and draws.
Successful compilation does not guarantee Store/Planner residency or indivisible
upload-budget admission; those remain separate checks. Style expression work,
asset storage, concurrent jobs and old snapshot memory remain producer-owned.

`vecmap-fixture -retained` exposes a reproducible comparison path through the new
compiler and compositor. The command still enforces the pinned tile checksum and
uses the same range-0 font files. The generic library supports merged decoded ranges,
including non-Latin glyphs. Default capture behavior is unchanged.

Verification:

- Expanded/indexed selected-rendering comparisons match the original fixture with
  complete, partial and missing fonts: exact ordered float32 vertex bits, material
  and clip values, texture contents, label counts and diagnostics. The comparison
  removes only local resource identity, unused packed data and source-boundary draw
  splits. It doesn't claim raw scene-buffer equality.
- Tests cover layer/candidate boundaries, exact periods, both topologies, empty
  tiles, sealed publication, source-input release, asset refresh, Unicode font maps,
  partial atlas coverage, explicit fallback readiness, malformed assets, limits,
  float32 geometry overflow, MVT degradation and concurrent immutable Builds.
- Compiler coverage remains **100%**. Tiles is **98.8%**: the text-request-limit
  forwarding guard is unreachable after Prepare's candidate cap; existing defensive
  Store.Snapshot error forwarding is also uncovered. Fixture is **94.4%** after the
  iterator extraction, with the same lower-level failure guards outstanding.
- Prepared-tile fuzzing completed **622,712 executions** in the configured 20 seconds.
  Pinned 386 and v1 race checks pass. Full pinned v4 module coverage, default/tagged/
  headless builds and targeted staticcheck pass. Shared diagnostics are clean;
  generated ST1006, native gopls tag metadata and the earlier GO-2026-5024 baseline
  remain. No dependency, binding, C++ or shader changes.
- Basic/threaded native renderer/viewer tests pass on Vulkan and OpenGL, including
  2× OpenGL/v1 race. Actual new-path expanded/direct threaded Vulkan screenshots
  equal the old reference PNGs byte-for-byte. Direct OpenGL at 2× also matches its
  ordinary-fixture capture. These are fixed-scene checks, not multi-tile style/label
  or MapLibre parity evidence.
- Complexity review keeps the explicit bounds/forwarding guards: options validation
  13, metric readiness 11, retained CLI orchestration 14. Shared pass traversal avoids
  a second rendering algorithm in the provenance path.

The new full-font captures still draw **782,409 selected elements and 62 labels**.
Their retained buffers include rejected candidates, and source boundaries expand
the draw list to **150**. Each has five scene textures plus backend white, and
reports eight drains with one-resource batches:

| Retained capture | Packed vertices | Packed indices | Recorded upload bytes including white |
| --- | ---: | ---: | ---: |
| Direct indexed | 376,786 | 820,251 | 12,592,324 |
| Expanded | 820,251 | 0 | 19,954,480 |

Artifacts: `/tmp/opencode/vecmap-fixture-prepared`, `vecmap-rhi-prepared`,
`vecmap-prepared-{direct,expanded}-scene.json`, `vecmap-{direct,expanded}-prepared.png`,
and `vecmap-direct-{reference,prepared}-gl2.png`. Coverage profile:
`/tmp/opencode/vecmap-prepared-compiler-coverage.out`.

The short smokes below are correctness runs, not controlled performance comparisons.
OpenGL uses software llvmpipe. Warm samples omit the first 30 frames; callback
intervals are not presentation timestamps. Preserve the observations without
interpreting the different frame/sample counts as a speedup:

| Capture | Frames | Finish wall ms | Prepare µs p95 | Submit µs p95 | Previous Qt GPU ms p95 | Callback ms p95 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Retained direct Vulkan, 2 s | 122 | 3.890623 | 57.248 | 207.431 | 0.868329 | 16.959896 |
| Retained expanded Vulkan, 2 s | 121 | 5.111566 | 62.208 | 200.028 | 0.872297 | 17.149935 |
| Ordinary direct OpenGL 2×, 1 s | 45 | 2.332262 | 41.107 | 80.291 | 7.168368 | 29.176470 |
| Retained direct OpenGL 2×, 1 s | 49 | 2.490301 | 65.975 | 115.297 | 8.585442 | 30.403556 |

Next: bounded tile/asset loading, cancellation and style-generation-aware producer
scheduling feeding prepared fragments into Set and WorkerWithData. Use those live
workloads to measure draw/upload/placement costs before changing the checked native
completion baseline. The production renderer remains separate.

### Bounded headless live tile producer

Following committed compiler checkpoint **959ff8e**, checkpoint **c801** adds
[`producer`](../pkg/vecmap/producer/README.md). One goroutine owns Set, raw/prepared/
fragment caches and composition; up to four fixed workers perform injected transport.
Preparation concurrency is one. The loader writes into a fixed response buffer and
has no compilation or native ownership. Supplied immutable style/font/sprite profiles
keep provider transport and dynamic asset fetching outside this first checkpoint.

`view.LoadOrder` now shares the existing scheduler's immediate-parent-first priority.
Useful loads survive overlapping cover updates and asset refresh. Obsolete loads
are canceled but remain charged until they actually return; an ignored cancellation
cannot spawn replacement workers. Source/style changes invalidate old jobs, and late
results cannot restore obsolete state. A single ticker handles bounded retries.

The producer reuses cached bytes on style-zoom changes and Prepared geometry on
asset refresh. A replacement cover must have coherent source/style/asset epochs;
partial refresh keeps the native consumer's old Current. Requested siblings refine
together through the shared selector. Acknowledged Current and outstanding target
leases pin continuity fragments until their owners release them.

CPU storage has separate raw-job, cache, profile and snapshot budgets. Cache charges
include complete borrowed profiles once per distinct pointer, so old epoch backing
cannot disappear from accounting while fragments retain it. Each output lease keeps
its own snapshot and asset-backing charge until explicit Release. Producer mailboxes
coalesce requests and native Current reports without retaining a scene history.
`tiles.RetainedBytes` and `Set.SelectBounded` add logical CPU storage admission;
compiler scratch, runtime overhead and transport-private buffers retain separate
bounded-input/owner obligations. These charges are not a peak RSS or VRAM meter.

Lease ownership is deliberately explicit. A bridge cannot release a superseded
target merely because a newer target was submitted: final-upload acknowledgement
may have promoted it to Current, and Planner/native retirement may still borrow old
resources. The next viewer checkpoint must track those bounded owners. Native reset
uses `ResetCurrent` with a fresh native generation while useful producer jobs can
continue. Close/Done joins producer workers without waiting for native acknowledgement.

Verification:

- Controlled loaders and small valid MVT exercise parent-first dispatch, partial
  refinement, zoom-out continuity, pan supersession, useful-job survival, ignored
  cancellation, retries/missing/invalid/oversized input, source/style/asset epochs,
  coherent refresh, immutable old snapshots, byte/count pressure and joined shutdown.
- A headless real Worker test preserves scene-associated transforms and old Current
  through partial uploads, then closes the producer with a Worker packet outstanding.
- The supplied pinned Liberty/font fixture retains **62 labels / 782,409 selected
  elements**. This is compiler/producer correctness evidence, not a live GPU capture
  or performance comparison. Fixture inputs were explicitly supplied, not skipped.
- Full pinned v4 module coverage, headless 386, pinned v1 race and shared diagnostics
  pass. Targeted staticcheck passes; default staticcheck retains only the existing
  generated ST1006 findings. The vulnerability baseline remains **GO-2026-5024**.
- Producer coverage is **97.3%**, tiles **98.6%**, and the extracted LoadOrder is
  **100%** covered. Remaining branches include defensive token/removal guards and
  scheduler interleavings, plus the prior defensive tile error forwarding. Coverage
  profile: `/tmp/opencode/vecmap-producer-coverage.out`.
- Complexity review retains explicit input/protocol guards: validation 24, request
  handling 21, owner loop 19, publication 15. The parent setCover drops from its prior
  duplicate parent-order loop to the shared helper. No dependency or binding changes.

The tests also exposed a pre-existing shared transform issue (**p5nh**): for a
root tile at camera longitude 0 / zoom 2 / 256-square viewport, local center projects
to x=1152 rather than x=128, and WorldWraps selects only zero. A standalone headless
probe confirms it; the producer asset-refresh test uses longitude -1 to isolate its
own behavior. Coarse fallback/world-copy correctness needs that separate fix before
the live viewer's coverage can be treated as verified.

Next: connect leased targets and acknowledged Current to the opt-in viewer, reuse/
extract provider transport/cache, and verify live arrival/camera/replacement traces
on basic/threaded Vulkan and OpenGL. This checkpoint does not run new native
transitions or change the checked GPU completion baseline. Production migration,
dynamic assets, pacing evidence and MapLibre parity remain later gates.

### Live viewer, leased targets and shared transport

Checkpoint **hxzf** connects headless producer **c801** to the opt-in viewer.
Live mode uses the pinned
Liberty style/sprites and supplied Noto Sans Regular/Bold/Italic range-0 files:

```sh
QT_RHI_INCLUDE=/tmp/opencode/qt-rhi-6.11.2/usr/include/qt6/QtGui/6.11.2/QtGui \
QSG_RHI_BACKEND=vulkan GOAMD64=v1 \
sh scripts/qt-rhi-env.sh go run -tags vecmap_rhi ./cmd/vecmap-rhi \
  -live -glyph-dir /tmp/opencode/vecmap-glyphs -duration 5s -animate=false
```

`-tile-url` selects an immutable XYZ template; the default is the existing pinned
OpenFreeMap snapshot. `-cache-dir` enables its checksum-pair disk cache. Latitude,
longitude and zoom select the initial 800×600 camera. `-cpu-cache-bytes` sets the
producer cache budget explicitly; existing upload byte/resource flags still apply.
Live mode cannot be combined with scene-file/reload input. All fonts are loaded
before QApplication starts; tile I/O and compilation run on bounded background
owners. This first live mode does not fetch additional glyph ranges or sprites,
compose Natural Earth rasters, or replace the application's production renderer.

The camera trace freezes when duration elapses. Live mode then allows up to ten
seconds for actual producer publication, upload and retirement to settle before
accepting a screenshot. A failure/timeout returns an error; it never raises budgets
or acknowledges work optimistically. GUI timer sampling and finish policy have
separate helpers, keeping producer I/O/compilation outside GUI/render callbacks.

`producer.Bridge` holds Current, the accepted target and one coalesced pending
document, with a fourth producer output/in-transfer lease. It **serializes accepted
targets through retirement**, trading supersession latency for an explicit bounded
ownership proof. A new opt-in Worker constructor publishes a final acknowledged
nil-batch Settled packet after resource retirement. Only settlement of the serial
target permits old lease release. Ordinary Worker cadence remains unchanged.

Native initialization remains separate from Planner settlement. In particular,
an empty target after reset can settle in the Planner before Qt has drained the
previous namespace. The viewer now waits for `BatchRenderer.Initialized()` before
acknowledging nil-batch publications. Both existing startup drains, checked in-frame
upload finish, retirement/rollback drains and generation checks remain intact.

The new [`tileio`](../pkg/vecmap/tileio/README.md) package extracts the parent's
bounded HTTP/file reads, redirect policy, checksums, pair writes, eviction and pinned
source paths. Parent tile/raster/glyph loaders delegate to it. Cache scans gain a
16,384-path cap; failed cache admission rolls back its new pair. HTTP producer loads
retain source identity and cancellation and classify missing/permanent/retryable
responses explicitly. Transport-private buffers/filesystem work remain separate
from the producer's logical scene/cache/lease charges.

The **p5nh** coarse-tile correction is also complete. Wrap zero chooses the nearest
tile center, and narrow antimeridian views include the adjacent copy needed by a
root fallback. Headless sampling covers multiple longitudes, zooms, rotations and
viewport sizes. Native tests verify visible root coverage at the half-world boundary
and across a rotated seam without geometry reupload. This conservative shared wrap
policy can add off-screen instances near the seam; per-tile culling is later work.

Verification:

- Headless tests exercise settlement only after acknowledged retirement, failed
  release retry, queued camera updates, leased-target coalescing, reset and joined
  shutdown. HTTP tests use local servers for status/error classification, bounded
  responses, cancellation, corruption recovery and source-separated cache paths.
- Basic/threaded Vulkan and OpenGL live tests load small valid MVTs through HTTP.
  Parent pixels survive partial CPU preparation and partial GPU upload; child
  coverage, camera-only motion, upload-time reset, root seam coverage and empty
  reset all pass. OpenGL also passes at 2× under the race detector.
- Separate basic/threaded processes exercise the actual live command with supplied
  fonts and a local tile server. The controlled background-only scene reports
  **20 loads/prepares/builds, 16 uploaded meshes, 80 instanced draws, zero labels,
  1,924 upload bytes including white, and three checked drains**. Observed peaks
  are four loader jobs and two leases. These tiny synthetic scenes are correctness
  evidence, not a real-provider performance measurement or MapLibre comparison.
- The full pinned v4 suite, headless 386 and pinned v1 race checks pass. Targeted
  shared/native-tag staticcheck passes; default staticcheck retains the existing
  generated ST1006 baseline. GO-2026-5024 remains the vulnerability baseline.
- The Vulkan adapter suite and viewer scenarios pass. Its initial full run exposed
  the existing reload test's one-second timing assumption: only five frames had
  occurred and the replacement was not uploaded. That test now allows five seconds,
  retaining strict readiness and replacement-pixel assertions; both Vulkan retries
  pass. The complete OpenGL 2× race suite passes. No new presentation-pacing claim
  is made; **vx93** remains open.
- Producer coverage is about **95%**, retained **99.7%**, tiles **98.6%**, tileio
  **85.7%**, and view **92.0%**. Remaining transport branches largely forward
  filesystem failures; several bridge protocol guards and owner interleavings
  remain uncovered. Profile: `/tmp/opencode/vecmap-live-producer-coverage.out`.

Next: measure real multi-tile arrival/camera traces and the serial-target latency
tradeoff under fixed budgets, then add bounded dynamic glyph-range demand through
the asset snapshot boundary. Improve accepted-target supersession only with an
equally explicit CPU/native lifetime proof. The checked GPU completion baseline
and exact Qt SDK/runtime packaging gate remain in force.

### Real multi-tile admission and raw-cache compaction

Checkpoint **4s9y** starts real-provider workload measurements after the controlled
viewer tests. **The default Madrid workload does not settle within the existing CPU
cache budget.** Native uploads complete correctly, but a ready native fallback is
not evidence that the requested CPU cover was admitted.

Inputs and controls:

- Go v1 capture, Qt 6.11.2, the pinned OpenFreeMap `20260823_080002_pt` source,
  supplied range-0 Noto Sans Regular/Bold/Italic, Liberty style/sprites.
- Initial camera 40.4168, -3.7038, zoom 10, 800×600, scale factor 1. Five-second
  static or built-in animated trace; failed settlement exits after the ten-second
  tail. All rows below exited **1**, explicitly reporting incomplete production.
- Unchanged **268,435,456-byte CPU cache**, **33,554,432-byte / two-resource upload
  batches**, default residency/snapshot limits and rendering/preparation limits.
- Static cover: **42 requested z10 tiles + 16 coarse parents**, 6,543,357 response
  bytes. The union of the static cover and five-second trace sampled every 8 ms
  contains **71 tiles / 8,599,046 bytes**. Cache pairs and a JSON manifest preserve
  each tile's SHA-256. This is input capture, not a GPU-ready scene capture.

Artifacts under `/tmp/opencode`:

- `vecmap-real-workload-20260924/` — verified raw cache corpus.
- `vecmap-real-workload-20260924-manifest.json` — camera, static targets and hashes.
- `whereami-capture-cover.go` — bounded four-worker corpus capture script.
- `vecmap-rhi-live` — pre-instrumentation viewer from the preceding checkpoint.
- `vecmap-rhi-workload-baseline` — instrumented viewer before compaction.
- `vecmap-rhi-workload` — compacting viewer with cache-only replay.

The first network/cold OpenGL run stopped at 12 loads / 9 preparations with a
265,834,412-byte cache peak, 349 draws / 93 labels in partial fallback, four active
loaders, and no native batch failures. Its process RSS peak was 511,604 KiB.

The ordered warm baseline used one transport worker to preserve load order. It
completed nine responses totaling **1,693,316 bytes**, but cached their full
**18,874,368-byte buffer capacity**. With the decoded-string backing allowance this
alone charged **37,748,736 bytes**. The fixed transport reservation was being retained
as cache storage even for small responses.

The producer now admits responses by compact size and copies partial buffers into
exact-length owned storage. Full-capacity responses transfer directly. One bounded
compaction scratch buffer is reserved separately; running/canceled job slots remain
charged until consumption. Failed replacement admission preserves the original
raw/prepared entry atomically. Budgets, MVT limits, geometry, text and GPU completion
are unchanged. Cache accounting still conservatively charges twice Fragment storage;
this checkpoint does not relax that model.

New bounded diagnostics split current/peak cache charges and report completed
load/Prepare/Build/Select counts, aggregate and maximum wall times, raw response
bytes/capacity, latest CPU selection and last failure stage. Parallel loading sums
are not CPU time or elapsed runtime. Counters are sampled before shutdown and are
not a complete history of every failure. `-tile-workers 1` supports ordered replay;
`-cache-only` refuses missing/corrupt inputs instead of fetching replacements.
`-tile-compilers` (default 1) prepares and builds that many tiles at once; with
more than one, Prepare/Build wall-time sums overlap and exceed elapsed time.

#### Ordered static replay

| Metric | Warm baseline, OpenGL | Compacted, OpenGL | Compacted, Vulkan |
| --- | ---: | ---: | ---: |
| Completed loads | 9 | 58 | 58 |
| Prepare / Build attempts | 9 / 9 | 20 / 20 | 20 / 20 |
| Selected coarse tiles | 8 | 11 | 11 |
| Raw cache charge | 37,748,736 | 6,363,438 | 6,363,438 |
| Prepared charge | 38,674,918 | 46,112,100 | 46,112,100 |
| Fragment/Set charge | 122,301,894 | 148,819,586 | 148,819,586 |
| Profile charge | 67,108,864 | 67,108,864 | 67,108,864 |
| Total cache peak | 265,834,412 | 268,403,988 | 268,403,988 |
| Pending / failed desired work at exit | 49 / 1 | 0 / 47 | 0 / 47 |
| Recorded upload bytes | 52,507,080 | 63,891,864 | 63,891,864 |
| Process RSS peak, KiB | 494,868 | 577,008 | 382,836 |

Compaction admits more work under the same limit, so these are **not equal-work
RSS or rendering speed comparisons**. The final scene remains incomplete: 398 draws
and 93 labels, with 11 coarse fallback tiles rather than the 42 detailed targets.

Vulkan's compacted static run spent 209.33 ms in Prepare, 191.83 ms in Build and
50.30 ms in Select across the whole run. It recorded 31 drains totaling 16.09 ms,
with no batch failures. Previous-frame GPU p95 was 2.27 ms and render-callback p95
17.44 ms; the timer observed 715 active and 942 exposed ticks out of 943. This run
had normal callback cadence, but callback intervals still are not presentation
timestamps and the requested map cover was incomplete.

#### Moving replay

Four-worker replay of the existing five-second pan/zoom/bearing trace gives:

| Metric | OpenGL / llvmpipe | Vulkan / Radeon 860M |
| --- | ---: | ---: |
| Loads / rejected results | 69 / 9 | 71 / 9 |
| Prepare / Build attempts | 229 / 229 | 228 / 228 |
| Prepare wall total | 2.847 s | 2.610 s |
| Build wall total | 2.027 s | 1.948 s |
| Select wall total | 339.32 ms | 366.68 ms |
| Peak cache charge | 268,400,381 | 268,419,197 |
| Peak held leases | 4 | 4 |
| Pending / failed desired work at exit | 0 / 29 | 0 / 29 |
| Recorded upload bytes | 174,730,024 | 103,349,320 |
| Drains / total drain wall time | 127 / 44.06 ms | 60 / 32.53 ms |
| Process RSS peak, KiB | 926,240 | 530,904 |

Different callback cadence and serial-target coalescing select different intermediate
targets, so these rows are not a matched backend speed comparison. The Vulkan run
ended with 20 requested tiles and an older incomplete current scene (392 draws,
90 labels). Its previous-frame GPU p95 was 4.85 ms and callback p95 17.64 ms, despite
only 123 active ticks out of 938; 937 ticks were exposed. The dominant observed
correctness gate is CPU admission; style-zoom preparation churn is also measurable.
These data do not justify removing checked GPU drains.

Example replay, using the captured corpus:

```sh
QSG_RHI_BACKEND=vulkan /usr/bin/time -v /tmp/opencode/vecmap-rhi-workload \
  -live -glyph-dir /tmp/opencode/vecmap-glyphs \
  -cache-dir /tmp/opencode/vecmap-real-workload-20260924 -cache-only \
  -duration 5s -animate=false -foreground -diagnostics -tile-workers 1 \
  -upload-bytes 33554432 -upload-resources 2 -cpu-cache-bytes 268435456
```

Use `-animate=true -tile-workers 4` for the moving trace. Expected outcome at this
checkpoint is a nonzero exit for incomplete CPU production, not a successful map
benchmark. The input manifest covers this trace/camera; other locations or longer
traces require their own captured corpus.

Tests verify exact compaction ownership, small-cache admission despite a large
transport reservation, failure rollback/source reversion, cache accounting and
phase counters. Offline tests verify missing/corrupt entries never fall back to
HTTP. Pinned headless 386/v1 race and native controlled live transitions pass.
The full pinned v4 suite and tagged targeted staticcheck pass; shared diagnostics
are clean. Producer coverage is **95.3%**, with compaction, cache breakdown helpers
and phase counters at **100%**. The existing generated ST1006 and GO-2026-5024
baselines remain. Coverage profile: `/tmp/opencode/vecmap-workload-coverage.out`.
Complexity review retains explicit admission/identity guards (loaded 12, build 13)
and transport branches (source loader 20); no new scheduler or rendering algorithm
was introduced for this measurement checkpoint.

Next gate: **7vqy**, complete refinement within the fixed CPU budgets. Inspect
distinct retained payloads versus metadata copies, discardable preparation state
and parent/refinement headroom while preserving all acknowledged/leased fragments.
Do not increase limits or reduce label/geometry quality to hide the failure. Bounded
dynamic glyph demand follows this admission work.

### Complete real-cover admission at fixed budgets

Committed the preceding producer/viewer/workload checkpoints as **d29f523**,
`feat: add bounded live tile production and QRhi viewer`, then continued with
**7vqy**. This checkpoint completes the captured Madrid CPU cover under the same
limits and quality settings. It changes storage ownership and reclaim policy,
not the style, geometry, labels, resource upload budget or GPU completion proof.

Three ownership changes are material:

1. **Charge actual metadata copies.** Store/Set borrow immutable geometry, indices
   and RGBA. `retained.CopyBytes` and `Fragment.SetCopyBytes` count their additional
   owned metadata and logical map entries. Metadata copies use exact-length slice
   capacity. The cache charges the fragment payload once plus those copies instead
   of charging the entire fragment twice. Runtime/allocator overhead remains outside
   this logical ledger, with metadata cardinalities separately bounded.
2. **Own sprite backing.** `Prepared.BuildOwned` preserves ordinary Build output but
   copies sprite pixels into compact owned buffers after aggregate RGBA preflight.
   Glyph atlas RGBA was already owned and is not recopied. Fragments and snapshots
   therefore retain neither asset maps/callbacks nor hidden sprite subimage backing.
   Asset profiles remain charged to the existing owner/latest/in-transfer input
   allowances, rather than being borrowed again by every cache entry and lease.
   Style profiles still cover retained candidate strings. Ordinary Build retains
   its original immutable-borrow contract.
3. **Reclaim optional preparation.** Cache pressure can evict least-recently-built
   Prepared objects or omit an incoming one after Build. Raw responses remain for
   reconstruction. Fragments, Set identity and all acknowledged/leased snapshots
   remain intact. Camera-only updates still reuse geometry; a later asset update
   reparses raw data only if its preparation was evicted. This trades preparation
   reuse for space and is reported by explicit counters.

An intermediate experiment with metadata accounting and Prepared reclaim alone
still failed publication at `snapshot/backing`: whole asset-profile charges pinned
the old native cover. Owned sprite pixels remove that actual lifetime dependency;
the asset allowance is not silently ignored while borrowed pixels survive.

A stricter captured-corpus regression also found sticky capacity failures. Settling
the full first view and then zooming out left rejected tiles marked failed even
after old content retired and the cache fell to about 138 MiB. Cache failures now
record the required **non-reconstructible headroom** and retry only after that much
raw/fragment/profile storage actually leaves the cache. Prepared was already excluded
from the failed minimum charge, so dropping it cannot trigger a retry. There is no
polling loop, speculative retirement credit or automatic limit increase. Historical
LastError remains latched after successful recovery; Pending/Failed report current
work. Indivisible or unrecoverable failures remain explicit errors.

#### Verification

- Headless tests prove Set/Store payload sharing versus exact metadata copies,
  optional preparation eviction with pinned Current, camera-only reuse, asset
  reconstruction without another load, and headroom-gated retries without spinning.
- A weak-reference test uses a four-byte sprite slice backed by a 1 MiB allocation.
  Its original backing is collected after asset refresh while an old lease still
  renders the copied red pixel. New tests also cover aggregate copy limits and
  atomic failure.
- Pinned expanded/indexed rendering comparisons with full/partial/no fonts match
  borrowed and owned builds exactly: ordered vertex bits, materials, pixels,
  provenance and candidate metrics. No rendering-quality limit changed.
- `TestCapturedMadridAdmission`, enabled with `WHEREAMI_VECTOR_WORKLOAD_CACHE`,
  uses the 71-tile checksummed cache and a checked fake native backend. It settles
  the **42-tile / 130-label** initial view, then the **20-tile / 95-label** final
  camera of the five-second trace, retaining old Current until replacement and
  retirement. Upload batches remain 32 MiB / two resources; all logical cache and
  lease bounds are asserted.
- Full pinned v4 tests, captured-corpus headless 386/v1 race, and the complete
  OpenGL 2× race adapter/viewer suite pass. Real Vulkan static/moving replays below
  use the checked native adapter. No native completion mechanism changed.
- Final pinned race coverage is producer **96.7%**, tiles **98.7%**, retained
  **99.8%**; profile `/tmp/opencode/vecmap-admission-coverage.out`. Tagged targeted
  staticcheck and shared diagnostics pass. Default staticcheck retains generated
  ST1006 findings and the vulnerability baseline remains GO-2026-5024. Complexity
  review keeps reclaim admission at 11, producer Build at 14 and owned packing at
  13; the captured-corpus test's explicit fake-backend protocol is 21.

#### Final real replay observations

Commands retain the preceding camera/corpus, 800×600 logical viewport, scale 1,
256 MiB cache, 32 MiB / two-resource native batches, five-second trace and ten-second
settlement tail. Static uses one loader; moving uses four. The binary is
`/tmp/opencode/vecmap-rhi-admission`.

| Metric | Vulkan static | Vulkan moving | OpenGL/llvmpipe static | OpenGL/llvmpipe moving |
| --- | ---: | ---: | ---: | ---: |
| Final CPU requested / selected tiles | 42 / 42 | 20 / 20 | 42 / 42 | 20 / 20 |
| Final CPU pending / failed | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 |
| Final CPU fallbacks | 0 | 0 | 0 | 0 |
| Final native target ready | yes | yes | yes | **no** |
| Displayed draws / labels | 978 / 130 | 600 / 95 | 978 / 130 | 748 / 126 (older Current) |
| Peak cache charge | 268,397,027 | 268,427,931 | 268,397,027 | 268,404,248 |
| Peak leased snapshots | 4 | 4 | 4 | 4 |
| Prepare / Build attempts | 58 / 58 | 257 / 257 | 58 / 58 | 189 / 189 |
| Cached preparation evictions | 40 | 133 | 26 | 118 |
| Incoming preparations omitted | 0 | 3 | 0 | 0 |
| Capacity retries | 0 | 29 | 0 | 0 |
| Recorded upload bytes | 186,976,560 | 389,167,804 | 158,344,292 | 231,730,952 |
| Drains / drain wall ms | 167 / 139.08 | 400 / 381.81 | 132 / 30.65 | 127 / 41.71 |
| Elapsed seconds | 5.98 | 14.36 | 13.97 | 15.29 |
| Process RSS peak, KiB | 659,300 | 1,006,604 | 921,548 | 1,221,068 |
| Exit status | 0 | 0 | 0 | **1** |

The final static Vulkan cache held 7,777,322 raw/backing bytes, 17,212,076 prepared
bytes, 134,163,374 fragment/Set bytes and 4,194,304 style-profile bytes. Prepare/Build/
Select totals were 311.69 / 295.90 / 661.07 ms. Moving Vulkan totals were 2.149 /
1.533 / 1.294 seconds. Reclamation and additional complete covers can increase
rebuilds, uploads and RSS; these are not equal-work memory or speed comparisons
against the earlier incomplete scenes.

Vulkan previous-frame GPU p95 was 3.90 ms static and 3.62 ms moving; callback p95
was 18.78 and 18.62 ms. Moving llvmpipe GPU p95 was 74.35 ms, and its serial native
queue missed the unchanged settlement deadline even though CPU admission completed.
All runs had zero native batch failures. Different callback cadence, arrivals and
coalescing select different intermediate targets; these observations do not establish
MapLibre parity or presentation pacing.

Next: **3a38**, bound accepted-target latency while retaining explicit lease and
native retirement proofs. The moving Vulkan case is close to the settlement deadline,
and the software OpenGL case exposes the next queue/throughput limit. Do not extend
deadlines, raise upload limits or remove checked drains to hide that cost. Dynamic
glyph-range demand remains later work through the supplied asset boundary.

### Bounded live-target supersession

Checkpoint **3a38** replaces the bridge's upload-and-retirement serialization with
an acknowledged target handoff. Four producer lease slots and all CPU/native budgets
remain unchanged. Planner, Worker and bridge changes are toolkit-neutral Go. Native
completion still uses the checked producing-frame finish and separate retirement,
rollback and startup drains.

The ownership proof has three parts:

- Acknowledged Planner residency retains only version IDs and logical byte charges.
  It no longer holds geometry or pixel slices from abandoned targets. Actual native
  capacity is still reclaimed only after successful release acknowledgement.
- Packets carry accepted `TargetData` alongside `CurrentData`. The bridge exposes
  one unconfirmed target. Once a checked packet identifies that target, it can release
  leases outside Current/accepted/pending and expose the newest pending document.
  During handoff, an old target can occupy the fourth lease slot and pause producer
  publication until ownership is confirmed. No historical target list is added.
- The adapter must have dropped its previous CPU scene/batch borrows before bridge
  completion. Batch completion already follows prepare. Nil-batch acknowledgements
  now wait for `FrameSelected()`, including `Initialized()`: Sync only replaces the
  frame header, while prepare still borrows the previous resident scene. Empty reset
  targets continue waiting for both startup drains.

The first cover must become Current before supersession starts, including after a
reset. An initial experiment without this rule settled moving Vulkan in 6.70 seconds
but could keep abandoning the initial cover throughout motion. That is not the
accepted result. The final implementation establishes drawable continuity first.

After the render owner submits a replacement, the Worker processes it after the
one outstanding packet. It doesn't finish the obsolete target or all its retirement
first. Needed resident versions survive supersession; obsolete ones still retire in
bounded checked batches before further uploads. Hidden windows or stalled native
callbacks can still delay progress: this is a bounded-work protocol, not a hard
real-time guarantee from Qt.

#### Replay evidence

Replays use the same 71-tile captured corpus, supplied range-0 fonts, Liberty style,
800×600 logical viewport, antialiasing, five-second trace and ten-second settlement
tail. These runs explicitly set scale 1 and the threaded render loop. Static uses
one transport worker, moving uses four. Go is pinned to v1; Qt is 6.11.2. The new
binary is `/tmp/opencode/vecmap-rhi-latency`; the previous committed binary remains
`/tmp/opencode/vecmap-rhi-admission`.

```sh
QT_RHI_INCLUDE=/tmp/opencode/qt-rhi-6.11.2/usr/include/qt6/QtGui/6.11.2/QtGui \
GOAMD64=v1 sh scripts/qt-rhi-env.sh go build -tags vecmap_rhi \
  -o /tmp/opencode/vecmap-rhi-latency ./cmd/vecmap-rhi

QT_SCALE_FACTOR=1 QSG_RENDER_LOOP=threaded QSG_RHI_BACKEND=vulkan \
/usr/bin/time -v /tmp/opencode/vecmap-rhi-latency \
  -live -glyph-dir /tmp/opencode/vecmap-glyphs \
  -cache-dir /tmp/opencode/vecmap-real-workload-20260924 -cache-only \
  -duration 5s -animate=true -foreground -tile-workers 4 \
  -upload-bytes 33554432 -upload-resources 2 -cpu-cache-bytes 268435456
```

OpenGL uses `QSG_RHI_BACKEND=opengl xvfb-run -a /usr/bin/time -v ...` with the same
arguments. Static uses `-animate=false -tile-workers 1`. Fresh baseline moving runs
use the admission binary under these exact command settings.

| Replay | Elapsed seconds | First drawable Current, ms | Recorded upload bytes | Drains | Exit |
| --- | ---: | ---: | ---: | ---: | ---: |
| Vulkan moving, baseline | 14.55 | not instrumented | 403,791,588 | 407 | 0 |
| Vulkan moving, new 1 | 6.96 | 330 | 353,648,696 | 178 | 0 |
| Vulkan moving, new 2 | 7.30 | 367 | 311,348,736 | 194 | 0 |
| Vulkan moving, new 3 | 7.37 | 354 | 302,758,432 | 196 | 0 |
| llvmpipe moving, baseline | 15.28 | not instrumented | 209,767,840 | 143 | **1** |
| llvmpipe moving, new 1 | 10.91 | 323 | 331,202,364 | 128 | 0 |
| llvmpipe moving, new 2 | 8.77 | 230 | 352,154,020 | 152 | 0 |
| llvmpipe moving, new 3 | 8.62 | 206 | 275,992,284 | 152 | 0 |
| Vulkan static, new | 5.14 | 311 | 183,505,224 | 116 | 0 |
| llvmpipe static, new | 9.44 | 303 | 154,261,424 | 112 | 0 |

All new moving runs settle at **20/20 tiles, 600 draws and 95 labels**; static settles
at **42/42 tiles, 978 draws and 130 labels**. Pending, failures and fallbacks finish
at zero. Peak leases remain four, peak cache charge stays below 256 MiB, and every
run has zero native batch failures. First drawable Current is bridge wall time to
packet consumption, not a presentation timestamp or complete-cover readiness.

The new moving Vulkan runs expose 37–38 targets, supersede 35–36 before settlement,
and use 116–125 successful upload batches plus 60–69 release batches. Their drain
wall totals are 141.63–160.43 ms, versus 340.43 ms in the fresh baseline. Moving
llvmpipe exposes 24–32 targets and uses 91–103 upload plus 35–47 release batches;
drain totals are 42.12–52.03 ms. Fewer serial packet/frame round trips matter more
here than removing drain wall time. No drain was removed.

The llvmpipe baseline timed out with `pending=1` in the deadline error. Its later
teardown report already showed final Current ready and pending zero. That is still
a failed settlement deadline, not a successful run. Status snapshots at different
times must not be conflated.

These are matched **input/settings** replays, not matched intermediate frame work.
Coalescing and callback cadence select different intermediate covers. In particular,
new llvmpipe can upload more bytes than the serial baseline while settling sooner.
GPU p95 and RSS aren't normalized comparisons across those different scenes. No
MapLibre parity, equal-frame speedup or presentation-pacing result is claimed.

#### Verification and remaining work

- Headless tests drive partial supersession, a queued C during B's final upload,
  failed upload, remaining retirement after CPU lease release, and startup continuity.
  A weak-reference test collects abandoned uploaded geometry while native residency
  is still charged, including a failed retirement retry.
- Basic/threaded Vulkan and OpenGL viewer tests retain old pixels during partial
  uploads, supersede a style replacement, preserve draw-only world-wrap mapping,
  reset during upload and reset to an empty target. OpenGL also passes at 2× with
  the race detector. The nil-packet selection assertion excludes destruction reports
  from an already-dead old epoch; those precede installation of the new generation.
- Full pinned v4 tests, captured-corpus headless 386 and pinned v1 race pass. Final
  race coverage is producer **96.8%**, tiles **98.7%**, retained **99.8%**; profile
  `/tmp/opencode/vecmap-latency-coverage.out`. Bridge completion, handoff and progress
  counters have 100% statement coverage. Existing constructor/mailbox guard gaps remain.
- Shared gopls diagnostics and tagged targeted staticcheck pass. Default staticcheck
  retains generated ST1006 findings. Tagged files still hit gopls package-metadata
  limitations; native builds/tests provide that check. Vulnerability baseline remains
  GO-2026-5024. Complexity review retains the explicit protocol branches in bridge
  completion (15), Worker advance (17) and viewer Sync (21); no broad refactor was needed.

CPU churn remains visible: the final moving replays perform **195–237 Prepare/Build
attempts**, versus 58 in static. Bridge stats separate coalesced CPU publications,
same-snapshot updates, target handoffs and successful batch progress. The new
`overtaken_upload_bytes` counter marks uploads completed while a newer CPU document
was waiting or exposed; it doesn't classify shared versions as wasted. Preparation
reuse and publication demand are the next optimization boundary, tracked by **y9r4**.
That work should measure visible Current age/detail during motion as well as final
settlement. Dynamic glyph-range
demand, asynchronous completion (**w47r**) and pacing (**vx93**) remain separate.

### Bounded producer demand and exact-input reuse

Checkpoint **y9r4** removes redundant producer work and instruments drawable Current
progress. The preceding **3a38** changes remain in the same uncommitted working tree.
No cache, lease, resource, upload, quality or deadline limit changes here.

The owner now makes five demand decisions before doing expensive work:

1. Native packet sequences always update progress, but only changed Current coverage
   dirties continuity selection. Repeated upload/retirement acknowledgements no longer
   create an identical CPU publication each time.
2. The shared `view.SelectCover` policy checks selected fragment epochs before
   projection, collision and composition. Mixed covers wait for work to finish.
   Old installed fragments still count as ready; filtering them out would change
   continuity and could manufacture a coarse publishable cover.
3. Preparation observes newer input before packing. An obsolete style skips Build;
   a compatible asset update can use the just-completed preparation with its latest
   callbacks. Installation still rejects changes arriving during Build.
4. Same-source raw loads survive style changes. Their original job tokens remain
   registered, and compilation uses the newest style. Source changes and obsolete
   tiles still cancel; ignored late results still consume only their fixed raw slot.
5. Refresh currently selectable tiles before unfinished refinements. Within each
   class, preserve LoadOrder. Installed siblings hide their fallback parent even
   during an epoch refresh, so don't rebuild that unselectable parent first. Pinned
   parent fragments remain intact for old Current and outstanding leases.

There is also conservative exact-input reuse: if a tile wasn't rebuilt during an
intervening style, returning to the same source, full PrepareOptions and immutable
compiled-layer slice identity reuses its fragment and optional Prepared. No second
style variant is retained. Original borrowed style backing remains charged. Reuse
avoids Set.Apply, preserving resource IDs/revisions and the cached selection. Changed
layer storage, zoom, topology or limits rejects reuse. Assets remain independent.
This does not round or approximate the production sixteenth-zoom paint policy.

#### Proof and verification

Deterministic owner tests verify 98 unchanged-coverage Current acknowledgements do
no new selection, mixed siblings reject before placement, detailed continuity is
not replaced by a fresh coarse parent, selected refresh precedes unfinished siblings,
hidden pinned parents survive, and queued style/source/clear changes skip packing.
Asset changes between phases use only the new callbacks. Controlled loaders prove
same-source style refresh paints the new color with one load, while source replacement
still rejects ignored-cancel data. Returning-style and failed-source-rollback tests
reuse the exact snapshot and original resource identities without Prepare/Build;
subsequent asset refresh rebuilds from the reusable preparation.

Full pinned v4 tests, headless 386 and pinned v1 race pass. Final race profile
`/tmp/opencode/vecmap-churn-coverage.out` reports producer **97.3%**, tiles **98.7%**,
retained **99.5%**. New coherence/identity/reuse helpers, compilation, phase boundaries
and Current observation have 100% statement coverage. The retained variation from
99.8% is worker-loop scheduling coverage; no retained code changed in this checkpoint.
Basic/threaded Vulkan and OpenGL adapter/viewer suites pass, including supplied-font
live commands, interrupted uploads, empty resets and OpenGL 2× race.

Shared gopls diagnostics and tagged targeted staticcheck pass. An unused pre-phase
assetEpoch initializer found by staticcheck was removed; affected tests were rerun.
Default staticcheck retains generated ST1006 findings. The vulnerability baseline is
still GO-2026-5024; there are no dependency changes. Complexity review keeps the owner
transitions explicit: inputs 25, Build 17, compile 11 and publication 12.

#### Replay observations

Build `/tmp/opencode/vecmap-rhi-churn` with the preceding v1/Qt command and use the
same replay arguments, corpus, five-second trace, ten-second tail, scale 1 and
threaded loop. Static uses one loader, moving uses four. All listed runs exit 0
with zero batch failures and zero final pending/failed/fallback counts. Moving ends
at **20 tiles / 600 draws / 95 labels**; static at **42 / 978 / 130**. Peak leases
remain four and logical cache stays within 256 MiB.

| Replay | Elapsed s | Prepare / Build | Select calls / wall ms | First Current tiles / labels | Longest Current hold s |
| --- | ---: | ---: | ---: | ---: | ---: |
| Vulkan moving 1 | 7.36 | 209 / 205 | 57 / 132.98 | 3 / 88 | 6.33 |
| Vulkan moving 2 | 7.28 | 216 / 211 | 62 / 154.08 | 3 / 88 | 6.23 |
| Vulkan moving 3 | 7.18 | 200 / 194 | 54 / 131.89 | 2 / 84 | 6.48 |
| llvmpipe moving 1 | 11.22 | 196 / 192 | 33 / 77.14 | 2 / 80 | 9.64 |
| llvmpipe moving 2 | 13.09 | 164 / 158 | 30 / 113.10 | 1 / 74 | 11.63 |
| llvmpipe moving 3 | 14.98 | 160 / 156 | 24 / 72.63 | 2 / 80 | 12.03 |
| Vulkan static | 5.15 | 58 / 58 | 24 / 59.87 | 1 / 74 | 3.89 |
| llvmpipe static | 9.98 | 58 / 58 | 33 / 81.25 | 3 / 89 | 8.02 |

The preceding 3a38 Vulkan moving series made 225–285 Select calls, totaling
596–689 ms; static Vulkan made 124 calls / 373 ms and static llvmpipe 129 / 404 ms.
Avoided work also has direct counters: final moving runs skip **4–6 Builds**, defer
**71–121 mixed-epoch selections** before placement, and observe **103–187 unchanged
Current updates** without reselecting. Exact-style reuse is proven by controlled
tests, but its counter was **zero in these captured runs**. Do not attribute their
results to that fast path or claim general cross-zoom geometry reuse.

Timing conditions changed during the last llvmpipe series. Immediately afterward,
host load average was **18.98**, with unrelated headless-browser and test processes
consuming several cores. Those processes were left alone. These are loaded-host
observations, not a matched latency improvement over 3a38; the 14.98-second pass has
almost no deadline margin. Different intermediate scenes and scheduling also prevent
normalizing Prepare/Build counts or GPU/RSS measurements as equal-work comparisons.

The new bridge counters reveal a remaining visible-progress problem. All six moving
runs consumed only **two drawable documents**: the first partial cover and final
cover. First drawable Current arrived at 267–557 ms, but its document stayed selected
throughout motion. LongestCurrentHold measures that unchanged-document interval,
including any ongoing interval at observation. It is neither content age nor a
presentation timestamp: camera reprojection continues, and a static complete scene
can legitimately stay unchanged indefinitely. CurrentChanges counts document switches,
not distinct rendered images. The first/final tile, fallback and label counts supply
the corresponding detail context without retaining a history of scenes.

**twd2** tracks coherent intermediate progress during continuous style changes and
quiet-host replay confirmation. Final settlement alone is not that goal. Dynamic
glyph demand, asynchronous native completion and MapLibre comparison remain separate.

### Coherent intermediate covers during motion

Checkpoint **twd2** makes native Current advance during continuous sixteenth-zoom
motion instead of only at its end. Cache, lease, resource, upload, quality and
deadline limits are unchanged; the checked in-frame finish and separate retirement
remain. Three interacting causes were found on a recaptured corpus, and each is
addressed at its own layer with an explicit bound.

1. **Producer epoch adoption.** The owner switched compilation to every new style
   pair at once, so under motion no selected cover was ever coherent (71–121
   deferred selections per run) and nothing published until the camera froze. The
   owner now adopts style/asset pairs at bounded boundaries: a working epoch with
   installed fragments is held until one coherent publication has been attempted,
   its remaining desired work can no longer arrive, or two covers' worth of Prepare
   attempts have been spent. Camera and targets always follow the latest request.
   Each intermediate cover is still evaluated at exactly one sixteenth style zoom;
   it may trail the camera by that bound. A held pair counts as Pending, so
   settlement still requires the newest paint. Mixed epochs are never published.
2. **Bridge supersession.** The bridge superseded any exposed target at each packet
   boundary, so intermediate covers were abandoned half-uploaded. It now mirrors
   acknowledged residency from successful batches and lets a pending document
   replace the exposed target only when it needs no more uploads than the target
   still lacks. Camera-only documents share the target's fragments and replace it
   for free; a new epoch's cover waits until the target it would abandon is Current.
   Progress is guaranteed either way, and a slow backend never spends frames on a
   cover a cheaper document could replace. A target whose packet reports an error
   no longer pins the bridge. An intermediate rule that committed a target after
   its first landed upload made llvmpipe miss the settlement deadline in loaded-host
   runs; a fixed half-of-uploads bound restored the deadline but starved progress on
   Vulkan, because camera-only documents arrive about twelve times per second.
   Neither is the accepted result.
3. **Store revisions.** Every Set replacement bumped every resource revision, so
   byte-identical glyph atlas and sprite textures were re-uploaded and later retired
   two per frame. Measured on the final Madrid cover, one adjacent sixteenth changes
   all 20 tile meshes (line widths are baked in tile units) but none of the 91
   textures, which are 82% of the resource count. A replaced resource now keeps its
   revision when its payload is byte-identical under the same store ID; changed and
   new resources take the replacement generation. `ReusedVersions` counts them.

Progressive Current pins larger covers. One run deadlocked: a committed 39-tile
detailed Current filled the 256 MiB cache, the 21 coarser tiles that had to replace
it failed admission, and no headroom could ever appear. Capacity failures now drop
non-desired cached tiles from the cache and Set (lease-only pins before Current
continuity, oldest builds first) until the fixed charge fits. Leases keep their own
payload, so Current still renders; only future selections lose that fallback.
Desired tiles are never evicted, and indivisible oversized inputs stay explicit
failures. `ContinuityEvictions` reports it; it engaged in two of the final runs.

New measurements: producer `StyleAdoptions`, `HeldStyles`, `StyleHeld` and
`ReusedVersions`; bridge `CurrentTargets`, `TargetErrors`, `LongestCurrentAge` and
`TotalCurrentAge` (receipt of a lease to its consumption as Current). None are
presentation timestamps.

#### Environment and corpus

The earlier `/tmp/opencode` corpus, glyphs and Qt private headers were gone. The
headers came from the Fedora `qt6-qtbase-private-devel-6.11.2-2.fc44` RPM extracted
with `rpm2cpio`; range-0 Noto Sans Regular/Bold/Italic glyphs were downloaded from
OpenFreeMap; the 71-tile trace union was recaptured over HTTP with
`producer.HTTPLoader` on 2026-09-28 (8,399,303 bytes plus the pinned z9 tile, whose
served copy no longer matches its pinned checksum and was taken from the
application cache). The recaptured tiles place **94 labels / 598 draws** at the
final moving camera instead of 95 / 600; `TestCapturedMadridAdmission` accepts
both. Compare runs only within one corpus.

#### Replay observations

Same arguments as the preceding checkpoints: 800×600, scale 1, threaded loop, 256 MiB
cache, 32 MiB / two-resource batches, five-second trace, ten-second tail; static uses
one loader, moving four. Baseline is the committed `2e1d4fa` tree built the same
way and run on the same corpus and host. All rows exit 0 with zero pending, failed,
fallback and batch-failure counts. Moving ends at 20 tiles / 598 draws / 94 labels,
static at 42 / 978 / 130. Host load stayed between 1.9 and 3.2 during the final
series; the interim series that motivated the supersession rule ran alongside test
suites and is not reported as timing evidence.

| Replay | Elapsed s | Drawable Current changes | Longest hold s | Mean Current age s | Targets / superseded | Upload / release batches |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Vulkan moving, baseline 1 | 7.31 | 2 | 5.48 | n/a | 35 / 33 | 120 / 64 |
| Vulkan moving, baseline 2 | 7.13 | 2 | 6.49 | n/a | 45 / 43 | 119 / 64 |
| Vulkan moving, baseline 3 | 6.98 | 2 | 6.30 | n/a | 46 / 44 | 117 / 61 |
| Vulkan moving, new 1 | 8.15 | 6 | 3.05 | 1.24 | 11 / 5 | 142 / 86 |
| Vulkan moving, new 2 | 9.53 | 7 | 2.44 | 1.16 | 8 / 1 | 162 / 107 |
| Vulkan moving, new 3 | 7.65 | 6 | 2.48 | 0.79 | 15 / 9 | 133 / 78 |
| Vulkan static, new | 5.93 | 4 | 2.97 | 1.09 | 5 / 1 | 125 / 39 |
| llvmpipe moving, new 1 | 11.94 | 5 | 5.06 | 1.83 | 7 / 2 | 86 / 30 |
| llvmpipe moving, new 2 | 13.45 | 4 | 4.99 | 2.43 | 8 / 4 | 92 / 36 |
| llvmpipe moving, new 3 | 14.45 | 6 | 6.43 | 2.52 | 9 / 3 | 97 / 40 |
| llvmpipe static, new | 14.81 | 4 | 8.70 | 2.70 | 4 / 0 | 116 / 30 |

Baseline runs held their first two-to-seven-tile Current through the whole motion;
the new runs advance Current every one to three seconds on Vulkan with 700–758
reused texture versions per run and 0–3 continuity evictions. Mean age divides
`TotalCurrentAge` by drawable changes and includes the first partial cover.

The cost is settlement time: completed intermediate covers must be retired two
resources per frame, and llvmpipe frames slow down once the fuller scene is drawn
(previous-frame GPU p50 36–42 ms versus 28–32 ms while the sparse first cover was
displayed). Vulkan moving settles 0.6–2.4 s later than baseline; llvmpipe moving
settled in 8.62–10.91 s at **3a38** and now takes 11.94–14.45 s, and llvmpipe static
14.81 s against 9.44–9.98 s, all within the unchanged ten-second tail but with
little margin on the software rasterizer. Prepare/Build attempts are unchanged in
range (257–287 moving). These are matched input replays, not matched frame work;
no presentation pacing or MapLibre claim follows.

#### Verification

- Deterministic owner tests: a held epoch publishes a coherent intermediate cover
  at its own sixteenth and then adopts the newest pair, skipping coalesced ones;
  terminal failures and the Prepare bound release a hold; a held pair is Pending.
- Synthetic bridge tests: a document needing more uploads than the target has left
  waits, an equally close camera-only document replaces it without abandoning
  staged versions, retirement-only progress permits replacement, a Current target
  releases the next pending document, and an unplannable target is replaced.
  The coalescing/reset test now changes geometry rather than only zoom, since a
  zoom-only recompile keeps every resident version.
- Store/Set tests: identical replacement keeps versions and counts them; changed
  payload takes the replacement generation; worker, planner and settlement tests
  use distinct payloads where they previously relied on the unconditional bump.
- Capacity eviction test: lease-only pins yield before Current continuity, desired
  tiles are never evicted, oversized input remains an explicit failure.
- Pinned v1 race coverage: producer **96.7%**, retained **99.8%**, tiles **98.7%**;
  new adoption, hold, eviction, residency-mirror and remap code is 100% covered
  except error branches. GOARCH=386 headless suites, the full pinned pkg/vecmap
  suite, staticcheck and the captured-corpus admission test pass. The tagged
  viewer/adapter integration suites pass on Vulkan and on OpenGL under the race
  detector; the live viewer test now interrupts geometry-changing replacements
  and asserts that the newer cover waits until the interrupted one is Current.

Remaining levers for the llvmpipe margin are outside this checkpoint: upload
before retirement in the Planner (addressed by **vtgd** below), asynchronous
completion (**w47r**) and pacing (**vx93**). Dynamic glyph demand and MapLibre
comparison remain separate.

### Upload before retirement and bounded release batches

Checkpoint **vtgd** removes the two ways retirement delayed progress after **twd2**.
The Planner always issued release batches before any upload, so a new target waited
behind the previous cover's retirement, and every release batch retired at most the
upload resource count, so each completed intermediate cover cost dozens of
frame-gated retirement drains. Both were bounded by design but paid on the software
rasterizer, where every frame is expensive.

1. **Ordering.** `Planner.Next` now issues an upload batch while acknowledged
   residency (including stale versions) plus that batch fits the residency limits.
   Stale versions retire when the next upload cannot fit, when no upload remains, or
   before an unplannable target's error. Residency never exceeds the limits: once no
   stale version remains, residency is a subset of the active and target sets that
   `SetTarget` proved to fit. A running resident-byte counter backs the check. The
   bridge's no-regret supersession rule is unchanged: it counts uploads a document
   still lacks, and retirement never changes that count.
2. **Release budget.** `Budget.Releases` bounds one retirement batch separately
   from `Resources`; zero keeps the upload count. Retiring a version moves no payload
   and costs the same checked drain per batch, so larger release batches reduce the
   number of drains without admitting more upload work per batch. The viewer's
   `-release-resources` flag defaults to **8**.

#### Replay observations

Same corpus, arguments and host as **twd2**; the `-release-resources` value is the
only added argument. Baseline is the committed `22fe450` tree; the *ablation* rows
come from a build that keeps the old release-first order with the new release
budget. All rows end at 20 tiles / 598 draws / 94 labels (static 42 / 978 / 130)
with zero pending, failed and batch failures unless marked. The llvmpipe series ran
under Xvfb at host load 1.0–4.2. The desktop session was locked during that series:
on-screen Vulkan runs then presented nineteen frames in fifteen seconds for every
binary, including the baseline, and Vulkan does not start under Xvfb. Those runs are
discarded; the Vulkan series below was captured afterwards on the unlocked desktop
at host load 0.5–2.0. A locked session is another instance of the **vx93** pacing
sensitivity: check `loginctl show-session <id> -p LockedHint` before on-screen runs.

| Vulkan replay | Order | Releases | Elapsed s | Current changes | Longest hold s | Mean age s | Upload / release batches | Frames |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| moving, baseline 1 | old | 2 | 8.63 | 5 | 3.82 | 1.31 | 146 / 91 | 500 |
| moving, baseline 2 | old | 2 | 8.69 | 6 | 3.79 | 1.19 | 148 / 91 | 505 |
| moving, baseline 3 | old | 2 | 8.41 | 6 | 3.75 | 1.09 | 144 / 89 | 494 |
| static, baseline | old | 2 | 5.12 | 4 | 2.95 | 1.00 | 112 / 26 | 299 |
| moving, ablation 1 | old | 2 | 9.42 | 6 | 2.98 | 1.18 | 160 / 105 | 554 |
| moving, ablation 2 | old | 2 | 7.61 | 6 | 3.37 | 0.83 | 134 / 78 | 450 |
| moving, ablation 3 | old | 2 | 8.38 | 6 | 2.63 | 0.85 | 144 / 88 | 495 |
| static, ablation | old | 2 | 5.89 | 4 | 2.95 | 1.29 | 125 / 39 | 345 |
| moving, new 1 | new | 2 | 9.95 | 7 | 3.52 | 0.79 | 169 / 113 | 585 |
| moving, new 2 | new | 2 | 10.44 | 6 | 3.82 | 1.02 | 176 / 120 | 616 |
| moving, new 3 | new | 2 | 9.13 | 7 | 3.19 | 0.62 | 157 / 101 | 539 |
| static, new | new | 2 | 5.88 | 4 | 2.93 | 1.29 | 125 / 39 | 345 |
| moving, ablation 1 | old | 8 | 6.35 | 6 | 2.39 | 0.72 | 148 / 25 | 372 |
| moving, ablation 2 | old | 8 | 7.28 | 7 | 1.83 | 0.91 | 173 / 31 | 430 |
| moving, ablation 3 | old | 8 | 7.38 | 7 | 1.77 | 0.91 | 173 / 33 | 433 |
| static, ablation | old | 8 | 5.16 | 4 | 2.94 | 1.27 | 125 / 10 | 301 |
| moving, new 1 | new | 8 | 6.80 | 7 | 1.65 | 0.66 | 161 / 27 | 400 |
| moving, new 2 | new | 8 | 7.56 | 6 | 1.78 | 1.07 | 180 / 32 | 445 |
| moving, new 3 | new | 8 | 7.46 | 6 | 1.85 | 1.00 | 177 / 31 | 438 |
| static, new | new | 8 | 5.13 | 4 | 2.95 | 1.26 | 123 / 10 | 299 |


| llvmpipe replay | Order | Releases | Elapsed s | Current changes | Longest hold s | Mean age s | Upload / release batches | Frames |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| moving, baseline 1 | old | 2 | 12.85 | 5 | 6.86 | 1.91 | 101 / 45 | 306 |
| moving, baseline 2 | old | 2 | 12.64 | 4 | 6.89 | 2.35 | 99 / 43 | 299 |
| moving, baseline 3 | old | 2 | 14.10 | 5 | 5.07 | 2.54 | 103 / 47 | 315 |
| static, baseline | old | 2 | 12.28 | 5 | 7.07 | 2.18 | 114 / 28 | 299 |
| moving, ablation 1 | old | 2 | 12.88 | 4 | 6.87 | 2.45 | 98 / 41 | 293 |
| moving, ablation 2 | old | 2 | 14.16 | 6 | 5.15 | 2.16 | 104 / 49 | 321 |
| moving, ablation 3 | old | 2 | 13.26 | 5 | 6.96 | 2.64 | 99 / 44 | 301 |
| static, ablation (missed deadline) | old | 2 | 15.25 | 5 | 6.49 | 2.48 | 122 / 31 | 319 |
| moving, new 1 (missed deadline) | new | 2 | 15.26 | 6 | 4.63 | 1.73 | 110 / 50 | 332 |
| moving, new 2 | new | 2 | 13.41 | 6 | 4.67 | 1.48 | 102 / 45 | 306 |
| moving, new 3 | new | 2 | 14.80 | 5 | 4.98 | 1.99 | 103 / 47 | 312 |
| static, new | new | 2 | 13.16 | 4 | 7.76 | 2.38 | 116 / 30 | 307 |
| moving, ablation 1 | old | 8 | 13.34 | 5 | 5.62 | 2.38 | 120 / 16 | 286 |
| moving, ablation 2 | old | 8 | 9.48 | 5 | 4.90 | 1.65 | 99 / 11 | 235 |
| moving, ablation 3 | old | 8 | 9.86 | 5 | 5.16 | 1.44 | 101 / 13 | 241 |
| static, ablation | old | 8 | 12.21 | 5 | 5.30 | 2.22 | 122 / 10 | 278 |
| moving, new 1 | new | 8 | 12.11 | 5 | 5.33 | 2.15 | 109 / 14 | 260 |
| moving, new 2 | new | 8 | 12.19 | 5 | 5.39 | 2.21 | 109 / 14 | 261 |
| moving, new 3 | new | 8 | 9.99 | 5 | 4.47 | 1.55 | 100 / 11 | 236 |
| moving, new 4 | new | 8 | 10.00 | 4 | 4.45 | 1.66 | 98 / 11 | 234 |
| moving, new 5 | new | 8 | 9.83 | 5 | 4.18 | 1.31 | 99 / 11 | 235 |
| moving, new 6 | new | 8 | 11.51 | 5 | 5.41 | 2.02 | 96 / 11 | 226 |
| static, new 1 | new | 8 | 10.54 | 5 | 6.82 | 1.88 | 117 / 8 | 265 |
| static, new 2 | new | 8 | 10.78 | 4 | 7.88 | 2.41 | 116 / 8 | 263 |

The release budget is the main lever: retirement batches fall from 41–50 to 11–16
per moving run, and settlement on llvmpipe moves from 12.6–14.2 s (six old-order
runs at two releases, one static miss) to 9.8–12.2 s across six runs of the
committed configuration, with static at 10.5–10.8 s instead of 12.3–15.3 s. Ordering
alone does not shorten settlement on llvmpipe because it reorders rather than
removes batches, and one of its three runs missed the deadline at 110 upload plus 50
release batches; it does lower the longest hold (4.6–5.0 s against 5.1–7.0 s) and the
mean Current age (1.5–2.0 s against 1.9–2.6 s) because a new cover's uploads no longer
queue behind the previous cover's retirement. Combined, the six moving runs hold
Current for at most 4.2–5.4 s with a mean age of 1.3–2.2 s. Run-to-run variance on
the shared host remains large (about ±1.5 s), so these are ranges, not point
estimates.

Vulkan shows the same attribution. With eight releases per batch the committed
configuration settles in 6.8–7.6 s against 8.4–8.7 s for the baseline, which is also
at or below the 7.0–7.3 s that **2e1d4fa** needed before Current advanced during
motion at all, and the longest hold falls from 3.8 s to 1.7–1.9 s with six to seven
drawable Current changes. Ordering alone again does not shorten settlement: at two
releases it completes more intermediate covers (157–176 upload batches against
134–160) and settles in 9.1–10.4 s, with a lower mean age (0.6–1.0 s against
0.8–1.3 s). At eight releases the two orders are within run-to-run variance on
every metric. The ordering change is kept because it makes the residency bound
hold per batch and removes the wait behind retirement; the settlement gain belongs
to the release budget. No presentation pacing or MapLibre claim follows.

#### Verification

- Planner tests: uploads precede retirement of an abandoned revision under
  residency headroom, with stale versions then retiring in admission order and the
  resident-byte counter matching the ledger; a failed retirement keeps its charge so
  the upload that needs the capacity still waits; the existing tight-limit
  supersession test now exercises the capacity-driven release path; the release
  budget bounds retirement separately and rejects negative or over-limit values
  without issuing work.
- Worker, worker-data and synthetic bridge tests updated to the new order: the
  newest target uploads before the abandoned partial revision retires; a landed
  upload commits the target under the unchanged no-regret rule; A's lease is still
  released only when C's handoff is proven.
- The residency fuzz run exercised **634,587 executions** in 20 seconds with the
  peak-residency and release-safety assertions unchanged. Race, GOARCH=386, vet and
  staticcheck pass on retained, producer and tiles; the full pinned pkg/vecmap
  suite and the tagged OpenGL adapter/viewer suites, including the race detector,
  are recorded in the kata issue.

### Resident line and fill geometry across style zooms

Checkpoint **yfq2** stops re-uploading geometry that a style-zoom change does not
alter. It is opt-in (`tiles.PrepareOptions.ResidentGeometry`, viewer
`-resident-geometry`, default off); with the option off every package produces its
previous output byte for byte.

Measured first, on the final moving Madrid cover (20 tiles, 89 MB of packed mesh
per style epoch): lines are 81% of base geometry bytes and none survived a 1/16
step, because widths were baked in tile units; fill polygons were already
byte-identical but shared each tile's single mesh; undashed, unoffset lines are 94%
of line bytes; symbols are about 15% of elements and change at every step.

1. **Width-independent lines.** Every vertex the line engine emits is
   `anchor + halfWidth * direction` with a direction that does not depend on the
   width: segment normals, square-cap diagonals, disk ring directions, miter
   vectors and the four-half-width miter fallback. `geometry.TessellateExtrudedLines`
   emits the same triangles in the same order with anchors and directions kept
   apart. The existing arithmetic is untouched.
2. **Compiler and packing.** With `LayerOptions.ExtrudeLines`, undashed, unoffset
   lines and fill outlines become extruded primitives carrying their half width in
   logical pixels; dashed and offset lines keep baked geometry and are marked
   dynamic. A split fragment has a stable mesh (fills, patterns, extruded lines;
   local ID 1) and a dynamic mesh (baked lines, symbols; local ID 2). Draw order,
   provenance, materials and counts equal the single-mesh output.
3. **Rendering.** `Material.OffsetScale` reaches the vertex shader in the reserved
   third component of the `view` uniform and multiplies map-aligned vertex offsets;
   zero keeps label behaviour. A width change is a uniform update on a resident
   mesh. Tile clipping evaluates the anchor, so a line is cut perpendicular to its
   direction where its centerline crosses the tile edge and neighbouring tiles
   complement each other. No handwritten C++ was added; shader packages were
   regenerated with `make rhi-shaders`.
4. **Residency.** The Store's byte-identical comparison (**twd2**) keeps the stable
   mesh's revision, so the Planner uploads only the dynamic mesh and the scene's
   new draws.

On the same 20 tiles the stable mesh is 69.4 MB of 89.4 MB (77.6%) and identical in
every tile across steps of 1, 2, 4 and 8 sixteenths, including the step across
zoom 10. Bytes to upload for a style-zoom change fall to 22.4–23.5% of the
single-mesh output. Prepare plus Build wall time is unchanged (16 ms per tile in
both forms). A static 800×600 Vulkan screenshot differs from the single-mesh
rendering in 21 of 1,920,000 pixels.

#### Replay observations

Same corpus and arguments as **vtgd** (two-resource upload batches, eight-resource
release batches, 32 MiB per batch); `-resident-geometry` is the only difference
between control and resident rows, which come from one binary. Each mode ran two
passes of three moving runs, the second pass in reverse order, on an unlocked
desktop at host load 0.8–5.3. All rows exit 0 with zero pending, failed and batch
failures and end at 20 tiles / 598 draws / 94 labels (static 42 / 978 / 130).
Ranges cover the six moving runs or two static runs of a mode.

| Replay | Mode | Elapsed s | Current changes | Longest hold s | Mean age s | Upload batches | Mesh uploads | Uploaded MB |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Vulkan moving | control | 6.47–7.45 | 6–8 | 1.28–1.97 | 0.60–1.05 | 150–177 | 96–124 | 356–515 |
| Vulkan moving | resident | 6.34–7.51 | 5–7 | 1.40–3.15 | 0.67–0.99 | 148–181 | 118–169 | 222–282 |
| Vulkan static | control | 5.14–5.16 | 4 | 2.30–2.92 | 1.05–1.09 | 112–126 | 52–58 | 166–187 |
| Vulkan static | resident | 5.26–6.00 | 4 | 3.63–3.64 | 1.30–1.34 | 138–154 | 104–116 | 166–187 |
| llvmpipe moving | control | 11.19–14.10 | 5–6 | 3.91–5.87 | 1.38–2.48 | 105–121 | 49–89 | 230–364 |
| llvmpipe moving | resident | 10.42–13.47 | 5 | 4.15–5.95 | 1.61–2.54 | 103–123 | 83–103 | 165–187 |
| llvmpipe static | control | 12.50–12.97 | 5 | 5.42–5.62 | 2.22–2.31 | 122 | 56 | 179–180 |
| llvmpipe static | resident | 10.73–11.31 | 4 | 8.94–9.44 | 2.47–2.60 | 131 | 98 | 155 |

Resident geometry removes 40–45% of uploaded bytes under motion on both backends.
The remainder is first uploads of tiles entering the cover, textures and the
dynamic meshes. It does not make motion settle sooner on Vulkan, and five of six
llvmpipe runs settle in 10.4–10.9 s against 11.2–14.1 s for control. It has a
cost: every tile now contributes two mesh resources, and batches are bounded at two
resources, so a cover of new tiles needs more batches. Static loads need 8–25% more
upload batches, Vulkan static settles up to 0.9 s later, and the longest hold is
worse in static runs on both backends and in five of six Vulkan moving runs. The
count of uploads per style epoch is unchanged at about one mesh per tile, because
symbols still change at every step.

A supplementary series raised the upload count to four resources per batch under
the same 32 MiB bound, which admits the bytes that two single-mesh tiles needed
before. The desktop was locked during three of the four Vulkan control runs, which
are discarded, so Vulkan control has one valid moving run here. Checkpoint **c6jy**
below repeats the series completely.

| Replay, four resources | Mode | Elapsed s | Current changes | Longest hold s | Mean age s | Uploaded MB |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| Vulkan moving | control (one run) | 7.36 | 11 | 1.46 | 0.52 | 671 |
| Vulkan moving | resident | 7.06–7.11 | 9–10 | 1.43–1.45 | 0.47–0.50 | 351–353 |
| Vulkan static | resident | 5.14 | 5 | 2.01 | 0.57 | 187 |
| llvmpipe moving | control | 9.39–10.15 | 7 | 2.65–3.39 | 0.94–1.00 | 426–444 |
| llvmpipe moving | resident | 9.47–10.03 | 6 | 2.69–3.38 | 1.07–1.25 | 218–227 |
| llvmpipe static | control | 7.19 | 5 | 4.32 | 1.32 | 187 |
| llvmpipe static | resident | 8.76 | 5 | 5.46 | 1.45 | 187 |

At four resources both modes progress alike and resident uploads half the bytes.
The batch count is a separate policy decision, taken in **c6jy**; no time claim for
this checkpoint depends on it.
These are matched input replays, not matched frame work; no presentation pacing or
MapLibre claim follows.

#### Verification

- Geometry: extruded output equals the baked tessellation within `1e-9` relative
  tolerance for every cap and join, widths from 1/32 to 40, expanded and indexed
  topology, near-duplicate points and 200 random path sets; one mesh is
  byte-identical for every width. A 20-second fuzz run made **1,735,484
  executions**. New code is fully covered; the package is at **97.8%**.
- Compiler: extruded primitives match the baked positions, order, colors and
  indices; dashed, offset and gap lines stay baked and dynamic; another style zoom
  changes only half widths; split packing preserves draws and provenance, keeps
  fixed mesh IDs when one mesh is empty, bounds both meshes by one element limit
  and rejects late `Split`, mismatched directions and invalid half widths. Coverage
  stays at **100%**.
- Tiles and residency: a tile prepared at two style zooms keeps its stable mesh
  byte-identical, the Store keeps that revision, and the Planner uploads exactly
  the dynamic mesh before publishing the new draws. A 20-second fuzz run over
  arbitrary MVT input made **777,930 executions** comparing split and single-mesh
  fragments. Tiles coverage is **98.7%**.
- Native: the adapter test renders one path baked and extruded under a rotated,
  scaled transform, requires agreement within edge rounding, and requires a
  doubled width to reuse the resident mesh; it passes on Vulkan and OpenGL. The
  viewer's live command runs in both modes on basic and threaded loops.
- Race, GOARCH=386, vet and staticcheck pass on geometry, compiler, scene, tiles,
  retained and producer; the full pinned pkg/vecmap suite and the tagged OpenGL
  adapter and viewer suites pass with and without the race detector.

Follow-ups: pack glyph quads at a base size so text size becomes a per-draw scale
and symbols join the stable mesh; move dashes into the fragment shader; reuse
decoded and tessellated primitives across style zooms so a sixteenth change costs
paint evaluation only. With symbols stable, most style-zoom changes would publish
as draw-only updates without any upload.

### Four resources per upload batch

Checkpoint **c6jy** raises the viewer's `-upload-resources` default from 2 to **4**,
an owner decision recorded on 2026-09-29. The 32 MiB byte bound per batch, the
eight-resource release batch, residency limits, library defaults and the
`-resident-geometry` default (off) are unchanged. The count was the binding limit:
a Madrid tile mesh is about 4.5 MB, so two resources admitted roughly a quarter of
the byte budget.

Same corpus, host and arguments as **yfq2**, all rows from one binary on an unlocked
desktop at host load 0.7–2.2; the two-resource rows are the yfq2 control series.
Every row exits 0 with zero pending, failed and batch failures. Four-resource
moving ranges cover three runs, plus the earlier yfq2 runs where they were valid
(six llvmpipe control, six resident on each backend, four Vulkan control).

| Replay | Mode | Resources | Elapsed s | Current changes | Longest hold s | Mean age s | Upload batches | Uploaded MB |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Vulkan moving | single mesh | 2 | 6.47–7.45 | 6–8 | 1.28–1.97 | 0.60–1.05 | 150–177 | 356–515 |
| Vulkan moving | single mesh | 4 | 6.56–7.36 | 11–12 | 1.14–1.46 | 0.41–0.52 | 125–130 | 663–671 |
| Vulkan moving | resident | 4 | 7.06–7.26 | 9–10 | 1.43–1.47 | 0.47–0.55 | 137–142 | 351–370 |
| Vulkan static | single mesh | 2 | 5.14–5.16 | 4 | 2.30–2.92 | 1.05–1.09 | 112–126 | 166–187 |
| Vulkan static | single mesh | 4 | 5.15 | 6 | 2.44 | 0.41 | 64 | 187 |
| Vulkan static | resident | 4 | 5.14 | 4–5 | 2.01–2.29 | 0.57–0.67 | 72–79 | 170–187 |
| llvmpipe moving | single mesh | 2 | 11.19–14.10 | 5–6 | 3.91–5.87 | 1.38–2.48 | 105–121 | 230–364 |
| llvmpipe moving | single mesh | 4 | 9.39–10.32 | 7 | 2.64–3.39 | 0.92–1.16 | 67–75 | 386–444 |
| llvmpipe moving | resident | 4 | 8.93–10.03 | 5–6 | 2.46–4.02 | 1.06–1.25 | 67–76 | 211–230 |
| llvmpipe static | single mesh | 2 | 12.50–12.97 | 5 | 5.42–5.62 | 2.22–2.31 | 122 | 179–180 |
| llvmpipe static | single mesh | 4 | 6.12–7.19 | 5 | 3.69–4.32 | 1.00–1.32 | 58–63 | 168–187 |
| llvmpipe static | resident | 4 | 8.76–8.95 | 5 | 5.46–5.54 | 1.45–1.48 | 78 | 187 |

In the default single-mesh mode, four resources nearly double the drawable Current
changes on Vulkan under motion and halve the mean Current age, while settlement
stays within the two-resource range. On llvmpipe, moving settles about 2.5 s sooner
and static in half the time, which restores the margin against the fifteen-second
deadline that **twd2** had narrowed. More covers complete under motion, so uploaded
bytes rise by about half in that mode.

The cost is per-frame work. With four resources the previous-frame GPU p95 is
3.9–4.7 ms against 3.6 ms on Vulkan, and 60–74 ms against 53 ms on llvmpipe, where
one run reached a 122 ms p99. Render-callback interval p95 is 20.4–22.2 ms against
19.4 ms on Vulkan. Callback intervals are not presentation timestamps (**vx93**).

Resident geometry remains opt-in. At four resources it uploads about half the bytes
of the single-mesh mode, but it completes fewer covers on Vulkan (9–10 against
11–12 Current changes) and llvmpipe static settles later (8.8–9.0 s against
6.1–7.2 s), because each tile is still two mesh resources and symbols still change
at every step. Making symbols resident (**s834**) is what would turn the byte saving
into time.

Verification: the viewer reports `upload_budget_resources=4` without the flag; the
tagged adapter and viewer suites pass on Vulkan and OpenGL, and staticcheck passes.
Only a flag default changed, so headless packages are unaffected. No presentation
pacing or MapLibre claim follows.

### Resident symbol geometry across style zooms

Checkpoint **s834** stops re-uploading icon and glyph quads when a style-zoom
change alters only their size. It is opt-in (`tiles.PrepareOptions.ResidentSymbols`,
viewer `-resident-symbols`, default off) and independent of `-resident-geometry`;
with the option off every package produces its previous output byte for byte.

1. **Size-independent quads.** A glyph quad is its em-unit position times
   `text-size / 24`, and `text-offset` is in ems. An icon quad, its anchored origin
   and `icon-offset` are multiples of `icon-size`. `glyph.BuildUnitLayoutMesh`
   builds text at 24 pixels per em and the compiler packs icons at size one; the
   evaluated size travels as `Material.OffsetScale`. Anchors, rotation, atlas
   coordinates, `FontScale` and halo paint are unchanged.
2. **A third mesh.** Symbols go to `compiler.SymbolMesh` (local ID 3), apart from
   the stable mesh and from dashed or offset lines. Draw order, provenance,
   materials other than the scale, and counts equal the single-mesh output.
3. **Collision.** Candidates, text bounds and sprite metrics are those of the
   default output. Placement is unchanged and stays on the CPU.
4. **Rendering.** The vertex shader already multiplies offsets by `OffsetScale`
   after map alignment, for both alignments. No shader or binding changed.

Measured on the final moving Madrid cover (20 tiles, 85.1 MiB of packed mesh):
stable 66.1 MiB, symbols 15.8 MiB (18.6%), dashed and offset lines 3.2 MiB (3.8%).
Per sixteenth step between style zooms 9.75 and 10.25, with resident geometry on
in both columns:

| Step | Meshes uploaded, symbols baked | MiB | Meshes uploaded, symbols resident | MiB | Symbol meshes identical | Textures identical |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 9.7500 to 9.8125 | 20 | 19.0 | 20 | 3.2 | 20 of 20 | 91 of 91 |
| 9.8125 to 9.8750 | 20 | 19.1 | 20 | 3.3 | 20 of 20 | 91 of 91 |
| 9.8750 to 9.9375 | 20 | 19.2 | 20 | 3.3 | 20 of 20 | 91 of 91 |
| 9.9375 to 10.0000 | 20 | 19.7 | 40 | 19.7 | 0 of 20 | 51 of 75 |
| 10.0000 to 10.0625 | 20 | 19.8 | 23 | 6.1 | 17 of 20 | 75 of 75 |
| 10.0625 to 10.1250 | 20 | 19.9 | 23 | 6.3 | 17 of 20 | 75 of 75 |
| 10.1250 to 10.1875 | 20 | 20.0 | 21 | 4.6 | 19 of 20 | 75 of 75 |
| 10.1875 to 10.2500 | 20 | 20.1 | 21 | 4.7 | 19 of 20 | 75 of 75 |

Symbol layout, compared candidate by candidate, differs only in `TextSize` in 17
to 20 tiles per step. The others gain or lose a repeated line-label anchor, because
symbol spacing in tile units follows the style zoom. At zoom 10 layers appear and
every tile changes. Bytes to upload for a step fall from 19 to 20 MiB to 3.2 to
6.3 MiB, except across zoom 10. Prepare plus Build wall time is unchanged
(5.6 s against 6.1 s for 320 builds).

**The upload count does not fall.** Every one of the 20 tiles has dashed or offset
lines, so every step still uploads one dynamic mesh per tile, now about 160 KiB
each. The issue's goal, a style-zoom change published as draws only, is reached
for tiles without such lines (proven headlessly below) and for no tile of this
corpus. Dashed lines are the remaining blocker (**yj2k**): the dynamic mesh of
this cover is `park_outline` (64.8% of its elements) and `boundary_3` (34.5%),
both in every tile, plus tunnel casings and disputed boundaries. It has no offset
lines.

Merging symbols into the stable mesh, the plan recorded under **yfq2**, was
rejected on this evidence: a layout change would re-upload the whole stable mesh,
6.6 to 15.1 MiB per step instead of 4.6 to 6.3 MiB, and 85.8 MiB across zoom 10.

#### Replay observations

Same corpus, host and arguments as **c6jy** (four-resource upload batches,
eight-resource release batches, 32 MiB per batch). All rows come from one binary;
the flags `-resident-geometry` and `-resident-symbols` are the only difference.
Each mode ran two passes of three moving runs and one static run per backend, the
second pass in reverse mode order, on an unlocked desktop at host load 0.8–3.2.
All 48 runs exit 0 with zero pending, failed and batch failures and end at 20
tiles / 94 labels (static 42 / 130). Ranges cover the six moving or two static
runs of a mode.

| Replay | Mode | Elapsed s | Current changes | Longest hold s | Mean age s | Upload batches | Mesh uploads | Uploaded MB |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Vulkan moving | single mesh | 6.45–6.99 | 10–13 | 1.00–1.27 | 0.37–0.48 | 123–130 | 164–194 | 622–673 |
| Vulkan moving | resident geometry | 6.80–7.18 | 9–10 | 1.41–1.45 | 0.46–0.54 | 133–139 | 224–233 | 344–352 |
| Vulkan moving | geometry and symbols | 6.90–7.48 | 9–10 | 1.53–1.62 | 0.49–0.63 | 137–149 | 266–300 | 278–302 |
| Vulkan static | single mesh | 5.14–5.17 | 5 | 2.46–2.51 | 0.46 | 63 | 58 | 187 |
| Vulkan static | resident geometry | 5.14–5.15 | 5 | 1.89–2.02 | 0.58 | 78–79 | 116 | 187 |
| Vulkan static | geometry and symbols | 5.15–5.19 | 5 | 1.73–2.17 | 0.66–0.67 | 91 | 168 | 187 |
| llvmpipe moving | single mesh | 9.37–10.01 | 6–7 | 2.55–3.78 | 0.95–1.03 | 69–71 | 89–106 | 391–443 |
| llvmpipe moving | resident geometry | 8.56–10.34 | 5–7 | 2.39–3.98 | 0.83–1.31 | 66–75 | 107–145 | 201–246 |
| llvmpipe moving | geometry and symbols | 9.19–9.82 | 5 | 2.87–4.49 | 1.03–1.33 | 74–78 | 180–198 | 187–196 |
| llvmpipe static | single mesh | 7.27–7.32 | 5 | 4.31–4.36 | 1.32 | 63 | 58 | 187 |
| llvmpipe static | resident geometry | 8.93 | 5 | 5.38–5.43 | 1.47 | 78–79 | 116 | 187 |
| llvmpipe static | geometry and symbols | 10.17–10.70 | 5 | 6.28–6.40 | 1.90–2.00 | 91–92 | 168 | 187 |

Resident symbols remove a further 15% or so of uploaded bytes under motion on
Vulkan and about 10% on llvmpipe. They do not make anything sooner. Each tile is
now three mesh resources, the trace crosses zoom 10 where every symbol mesh
changes, and every style epoch still uploads the dashed lines of every tile, so
mesh uploads rise by a fifth to a quarter on Vulkan and by a third to two thirds
on llvmpipe. Vulkan moving settles up to 0.3 s after resident geometry with a
longest hold 0.1–0.2 s worse; llvmpipe moving completes five covers where the
other modes complete five to seven; llvmpipe static settles 1.2–1.8 s after
resident geometry and about 3 s after the single mesh. Previous-frame GPU p95 is
unchanged (Vulkan 4.6–5.0 ms, llvmpipe 56–63 ms).

The option therefore stays off, and `-resident-geometry` with it. It is a
prerequisite, not a win: with dashes moved to the shader (**yj2k**) a tile is two
mesh resources again and a step with unchanged layout uploads nothing.
These are matched input replays, not matched frame work; no presentation pacing or
MapLibre claim follows.

#### Verification

- Glyph: a unit mesh equals the baked mesh of scale one, is identical for scales
  from 0.5 to 40, leaves metrics and bounds alone, and falls back to the baked
  form or its errors for scales float32 cannot carry. Coverage stays at **99.8%**.
- Compiler: for three size pairs, expanded and indexed, resident output keeps
  provenance, order, indices, textures, anchors and atlas coordinates; materials
  differ only in the scale; scaled offsets equal baked offsets within `1e-4`
  pixels; one mesh is byte-identical for every size. Routing is proven for the
  four combinations with `Split`, as are fixed IDs with empty meshes, baked
  fallback for icon sizes of zero, below zero and `1e-50`, rejection of late calls
  and unusable unit scales, and one element limit over three meshes. Coverage
  stays at **100%**.
- Tiles and residency: a tile without dashed lines prepared at two style zooms
  keeps both meshes and its textures byte-identical, the Store reuses every
  version, and the Planner publishes the new draws with **no upload**. With a
  dashed line it uploads exactly the dynamic mesh. Draws, placement metrics and
  bytes equal the default output, and an em-relative layout change replaces the
  symbol mesh. A 20-second fuzz run over arbitrary MVT input made **693,009
  executions**. Coverage is **98.8%**.
- Native: the adapter test renders a viewport-aligned and a map-aligned quad baked
  and scaled under a rotated, scaled transform, requires agreement within edge
  rounding, and requires a doubled size to reuse the resident mesh; it passes on
  Vulkan and OpenGL. Static screenshots of the Madrid cover (42 tiles, 130 labels)
  with baked and resident symbols are identical on Vulkan and differ by at most
  one level per channel on OpenGL. The viewer's live command runs with both
  options on basic and threaded loops.
- Race, GOARCH=386, vet and staticcheck pass on glyph, compiler, tiles, retained
  and producer; the full pinned pkg/vecmap suite, the tagged Vulkan and OpenGL
  adapter and viewer suites, the OpenGL suites under the race detector and tagged
  staticcheck pass.

### Resident dashed lines

Checkpoint **yj2k** moves dashes into the fragment shader. It is opt-in
(`tiles.PrepareOptions.ResidentDashes`, viewer `-resident-dashes`, default off) and
independent of the other two resident options; with the option off every package
produces its previous output byte for byte.

1. **One quad per segment.** `geometry.TessellateDashedLines` emits a butt-capped
   quad per path segment with anchors, unit normals and the distance of each
   anchor along its path. The baked walk emits one quad per dash, in tile units
   that follow the evaluated width.
2. **Pattern as draw state.** `scene.Dashed` materials carry the half width in
   logical pixels (`OffsetScale`), the width in tile units (`DashUnit`) and up to
   four dash lengths in multiples of it (`Dashes`). A style-zoom change alters
   those values and no vertex.
3. **Rendering.** The fragment shader takes the interpolated distance modulo the
   pattern length and discards gaps. The uniform block is unchanged: the dash
   unit and the pattern use fields that solid geometry left unused. Only the
   fragment shader package was regenerated; no handwritten C++ was added.
4. **Scope.** Round and square dash caps, path offsets and patterns of more than
   four entries keep their baked geometry. All 17 dashed layers of the Liberty
   style have two entries, butt caps and no offset, so none is left baked.

Decisions that the issue left open: dashes have butt caps only; no join is drawn
inside a dash, as in the baked output; the dash phase restarts at the first point
of every path, as in the baked output, so it is not continuous across tile
boundaries in either form. Dash ends are cut per fragment and would not gain
multisample edges on a multisampled target. The baked walk fails beyond 100,000
dashes per path; one quad per segment has no such limit, so a tile that exceeded
it now succeeds.

Measured on the final moving Madrid cover with the three resident options on (20
tiles, 84.3 MiB of packed mesh, 40 mesh resources, 45 dashed draws), per sixteenth
step between style zooms 9.75 and 10.25:

| Step | Meshes uploaded, dashes baked | MiB | Meshes uploaded, dashes in shader | MiB | Tiles without upload |
| --- | ---: | ---: | ---: | ---: | ---: |
| 9.7500 to 9.8125 | 20 | 3.2 | 0 | 0.0 | 20 of 20 |
| 9.8125 to 9.8750 | 20 | 3.3 | 0 | 0.0 | 20 of 20 |
| 9.8750 to 9.9375 | 20 | 3.3 | 0 | 0.0 | 20 of 20 |
| 9.9375 to 10.0000 | 40 | 19.7 | 20 | 16.2 | 0 of 20 |
| 10.0000 to 10.0625 | 23 | 6.1 | 3 | 2.6 | 17 of 20 |
| 10.0625 to 10.1250 | 23 | 6.3 | 3 | 2.6 | 17 of 20 |
| 10.1250 to 10.1875 | 21 | 4.6 | 1 | 0.9 | 19 of 20 |
| 10.1875 to 10.2500 | 21 | 4.7 | 1 | 0.8 | 19 of 20 |

A step with unchanged symbol layout is now a draw-only update. What remains is
the symbol mesh of the tiles whose line-label anchors change, and every symbol
mesh at zoom 10, where layers appear. The dynamic mesh is gone from this cover, so
a tile is two mesh resources, as under **yfq2**.

#### Replay observations

Same corpus, host and arguments as **s834** (four-resource upload batches,
eight-resource release batches, 32 MiB per batch). All rows come from one binary
and differ only in the three resident flags. Each mode ran two passes of three
moving runs and one static run per backend, the second pass in reverse mode
order, on an unlocked desktop. Every series started at a one-minute host load of
1.19–1.48. An earlier complete series is discarded because another job held the
load at 2.4–6.4. All 64 runs exit 0 with zero pending, failed and batch failures
and end at 20 tiles / 94 labels (static 42 / 130). Ranges cover the six moving or
two static runs of a mode.

| Replay | Mode | Elapsed s | Current changes | Longest hold s | Mean age s | Upload batches | Mesh uploads | Uploaded MB |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Vulkan moving | single mesh | 6.47–6.79 | 10–13 | 1.02–1.25 | 0.38–0.49 | 118–129 | 159–190 | 636–678 |
| Vulkan moving | geometry | 6.64–7.18 | 9–10 | 1.25–1.48 | 0.45–0.54 | 131–138 | 228–234 | 350–358 |
| Vulkan moving | geometry, symbols | 6.98–7.50 | 9–10 | 1.52–1.85 | 0.62–0.64 | 138–150 | 282–297 | 279–300 |
| Vulkan moving | geometry, symbols, dashes | 6.57–6.72 | 10–11 | 1.22–1.38 | 0.29–0.36 | 121–125 | 167–174 | 300–307 |
| Vulkan static | single mesh | 5.12–5.15 | 5 | 2.48–2.53 | 0.47 | 64 | 58 | 187 |
| Vulkan static | geometry | 5.13–5.16 | 5 | 2.01–2.03 | 0.57 | 78–79 | 116 | 187 |
| Vulkan static | geometry, symbols | 5.13–5.15 | 4–5 | 1.57–2.24 | 0.65–0.77 | 84–91 | 156–168 | 173–187 |
| Vulkan static | geometry, symbols, dashes | 5.13–5.14 | 4–5 | 2.00–2.27 | 0.57–0.66 | 73–79 | 108–116 | 172–186 |
| llvmpipe moving | single mesh | 8.57–10.03 | 5–7 | 2.65–4.11 | 0.90–1.08 | 64–71 | 92–108 | 378–454 |
| llvmpipe moving | geometry | 8.95–11.30 | 6–7 | 2.12–3.68 | 0.89–1.49 | 67–78 | 105–157 | 215–246 |
| llvmpipe moving | geometry, symbols | 9.21–11.58 | 5–7 | 2.63–4.62 | 0.95–1.22 | 71–84 | 160–200 | 180–210 |
| llvmpipe moving | geometry, symbols, dashes | 7.86–9.73 | 6–7 | 2.98–4.10 | 0.65–0.93 | 64–72 | 124–133 | 196–225 |
| llvmpipe static | single mesh | 7.64–8.13 | 5 | 4.52–4.77 | 1.24–1.32 | 64 | 58 | 187 |
| llvmpipe static | geometry | 8.97–10.65 | 5 | 5.56–5.89 | 1.66–1.76 | 78–79 | 116 | 187 |
| llvmpipe static | geometry, symbols | 10.83–12.07 | 5 | 6.01–6.60 | 1.79–1.99 | 91 | 168 | 187 |
| llvmpipe static | geometry, symbols, dashes | 7.07–8.78 | 4–5 | 4.86–5.39 | 1.44–1.48 | 69–78 | 102–116 | 161–186 |

With dashes in the shader the three options together are the first resident mode
that does not cost time. Against geometry and symbols alone, Vulkan moving
settles 0.3–0.9 s sooner with half the mean Current age and 40% fewer mesh
uploads, and llvmpipe static settles 2–5 s sooner.

Against the default single mesh under motion, uploaded bytes fall by about half
on both backends and the mean Current age falls by about a quarter (Vulkan
0.29–0.36 s against 0.38–0.49 s, llvmpipe 0.65–0.93 s against 0.90–1.08 s).
Settlement is within the single-mesh range on Vulkan and at or below it on
llvmpipe. Completed covers are equal on llvmpipe and 10–11 against 10–13 on
Vulkan, where the longest hold is 0.1–0.2 s worse.

Static loads still pay for two mesh resources per tile: 69–79 upload batches
against 64, a mean Current age 0.1–0.2 s worse on both backends, and a longest
hold 0.1–0.9 s worse on llvmpipe. Vulkan static settles alike; llvmpipe static
ranges from 0.6 s sooner to 0.6 s later. Previous-frame GPU p95 under motion is
unchanged (Vulkan 4.0–4.1 ms, llvmpipe 53–61 ms).

All three options stay off at this checkpoint. Whether they become the default is
an owner decision, taken in **kykh** below: the evidence is half the uploaded
bytes and fresher covers under motion against a slower first load of a static
view.
These are matched input replays, not matched frame work; no presentation pacing or
MapLibre claim follows.

#### Verification

- Geometry: for eight patterns, four widths from 1/32 to 40, six paths and 200
  random paths, expanded and indexed, every baked dash lies on one segment quad
  with the same lateral extent, and 512 sampled distances per segment are covered
  exactly where a baked dash is. One mesh is byte-identical for every width and
  pattern. Pattern acceptance is tested for 23 arrays. A 40-second fuzz run made
  **429,950 executions**; it found that the proof must follow dash direction on
  paths that fold back, and the baked 100,000-dash limit. The package is at
  **98.0%**.
- Scene: `Dashed` materials validate their unit and pattern, split batches per
  draw, and leave existing scene files unchanged when absent.
- Compiler: eligible dashed layers emit the dashed mesh, half width, dash unit
  and pattern; round and square caps, offsets, six-entry patterns and a zero
  first dash stay baked; another style zoom changes draw values only; packing
  puts distances in `U`, routes to the stable mesh and rejects mismatched or
  invalid input. Coverage stays at **100%**.
- Tiles and residency: a tile with fills, plain lines, a dashed line and a label,
  prepared at two style zooms with the three options, keeps both meshes and its
  textures byte-identical, and the Planner publishes the new draws with **no
  upload**. Provenance and placement metrics equal the default output. A
  20-second fuzz run over arbitrary MVT input made **770,859 executions**.
  Coverage is **98.8%**.
- Native: the adapter test renders two paths with a four-entry pattern baked and
  in the shader under a rotated, scaled transform. One of 2,870 covered pixels
  differs on Vulkan and none on OpenGL; a pattern change reuses the mesh. Static
  screenshots of the Madrid cover (42 tiles, 978 draws, 130 labels) in the default
  mode and with the three options differ in 43 of 1,920,000 pixels on Vulkan and
  30 on OpenGL. The viewer's live command runs with the three options on basic and
  threaded loops.
- Race, GOARCH=386, vet and staticcheck pass on geometry, scene, compiler, tiles,
  retained and producer; the full pinned pkg/vecmap suite, the tagged Vulkan and
  OpenGL adapter and viewer suites, the OpenGL suites under the race detector and
  tagged staticcheck pass.

### Resident options on by default

Checkpoint **kykh** changes the viewer defaults of `-resident-geometry`,
`-resident-symbols` and `-resident-dashes` to **true**, an owner decision recorded
on 2026-09-29: the main use of the map will be to search, zoom and pan. The
library defaults (`tiles.PrepareOptions` zero value), the upload and release
budgets and the residency limits are unchanged. `=false` on the three flags
selects the single-mesh mode.

Same corpus, host and arguments as **yj2k**; both modes from the new binary, two
passes of three moving runs and one static run per backend, the second pass in
reverse order, every series started at a one-minute host load of 0.84–1.47 on an
unlocked desktop. All 32 runs exit 0 with zero pending, failed and batch failures
and end at 20 tiles / 94 labels (static 42 / 130).

| Replay | Mode | Elapsed s | Current changes | Longest hold s | Mean age s | Upload batches | Mesh uploads | Uploaded MB |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Vulkan moving | options off | 6.39–6.80 | 10–12 | 1.13–1.29 | 0.40–0.47 | 121–131 | 162–204 | 629–722 |
| Vulkan moving | default | 6.43–7.09 | 10–14 | 1.20–1.45 | 0.30–0.37 | 113–131 | 155–180 | 286–311 |
| Vulkan static | options off | 5.13–5.14 | 5 | 2.49–2.51 | 0.46–0.47 | 63–64 | 58 | 187 |
| Vulkan static | default | 5.13–5.16 | 5 | 1.96–1.99 | 0.58 | 79 | 116 | 186 |
| llvmpipe moving | options off | 8.89–10.77 | 5–7 | 2.68–4.00 | 0.95–1.27 | 62–73 | 86–106 | 377–446 |
| llvmpipe moving | default | 8.81–10.14 | 6–7 | 2.36–3.15 | 0.98–1.12 | 65–75 | 91–121 | 192–230 |
| llvmpipe static | options off | 6.48–8.00 | 5 | 4.08–4.68 | 1.07–1.31 | 57–64 | 52–58 | 165–187 |
| llvmpipe static | default | 9.48–9.97 | 5 | 5.62–5.91 | 1.55–1.64 | 79 | 116 | 186 |

The series confirms **yj2k** on Vulkan: under motion the default uploads less
than half the bytes and the mean Current age is about a quarter lower, settlement
is within 0.3 s, and the longest hold is 0.1–0.2 s worse. A static view settles
alike, with a shorter longest hold and a mean age 0.1 s worse.

On llvmpipe it confirms the bytes and a shorter longest hold under motion, but
not the lower mean age, which here is within the control range. The static cost
is larger than in **yj2k**: the first load settles in 9.5–10.0 s against
6.5–8.0 s, where **yj2k** measured 7.1–8.8 s against 7.6–8.1 s. Over both
series the default takes 7.1–10.0 s and the single mesh 6.5–8.1 s. Software
rendering pays most for the second mesh resource per tile; this is the known
cost of the decision and the reason the flags remain.

Peak process memory is lower under motion (Vulkan 1,050–1,088 MiB against
1,108–1,207 MiB) and higher on a static load (718–724 MiB against 637 MiB).
Previous-frame GPU p95 is unchanged. These are matched input replays, not matched
frame work; no presentation pacing or MapLibre claim follows.

Verification: the viewer reports the three options as true without flags and as
false with `=false`; the tagged adapter and viewer suites pass on Vulkan and
OpenGL, and tagged staticcheck passes. Only flag defaults changed, so headless
packages are unaffected.

### Coarser tiles

Checkpoint **xcmf** adds an option to draw tiles from below the camera zoom
(`tiles.PrepareOptions.Coarser`, `view.VisibleTileCoverAt`, `view.StyleZoomAt`,
viewer `-coarser-tiles`, 0 to 2, default 0). vecmap draws a tile 256 units wide at
its own zoom. MapLibre draws the same tile 512 units wide, so for one view vecmap
loads more tiles and evaluates the style one zoom higher. With `Coarser` set to
one, tiles and style zoom lie one level below the camera zoom.

The option has to reach the compiler. Baked line widths, pattern sizes and symbol
spacing are converted from pixels to tile units, which before this checkpoint
used the style zoom. Lowering only the style zoom on coarser tiles draws baked
lines twice as wide and spaces line labels at twice the distance. `Zoom` still
evaluates the style; the conversion uses `Zoom` plus `Coarser`, the zoom the tile
is drawn at. Geometry with pixel offsets (the three resident options, labels) was
already independent of it. The producer selects its default cover from
`Style.Options.Coarser`, so cover and preparation cannot disagree; explicit
targets remain the caller's responsibility.

Limits of the option: from camera zoom 14 plus `Coarser` up the tiles are the
same and only the style zoom differs. Below camera zoom `Coarser` the style zoom
stays at zero and baked widths are those of camera zoom `Coarser`. The prefetch
ring stays at one tile, which covers twice the ground with `Coarser` one.

Tiles per view at Madrid, bearing 0, zoom 10 to 16 in quarter steps, `Coarser` one
as a share of zero: 0.49 of the visible tiles and 0.68 with the ring at 800x600,
0.42 and 0.59 at 1280x800, 0.50 and 0.71 at 372x695.

#### Replay observations

Same host, glyphs and arguments as **kykh** (resident options on). The corpus is
the **kykh** corpus plus the tiles `view.LoadOrder(view.VisibleTileCoverAt(trace,
1))` needs: 38 tiles against 71, captured from the same pinned snapshot. Both
modes come from one binary, in two passes of three moving runs and one static
run per backend, the second pass in reverse order, on an unlocked desktop. Every
series started at a one-minute host load of 0.77–1.49. All 32 runs exit 0 with
zero pending, failed and batch failures. The two modes do not draw the same
scene: moving runs end at 20 tiles / 94 labels against 12 / 40, static runs at
42 / 130 against 20 / 90.

| Replay | Coarser | Elapsed s | Current changes | Longest hold s | Mean age s | Loads | Upload batches | Mesh uploads | Uploaded MB | Prepare + Build ms | Peak RSS MiB | GPU p95 ms |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Vulkan moving | 0 | 6.63–6.69 | 8–12 | 1.11–1.42 | 0.31–0.38 | 87 | 121–127 | 167–176 | 300–308 | 2,890–2,980 | 1,001–1,057 | 4.3–4.6 |
| Vulkan moving | 1 | 5.40–5.49 | 17–20 | 0.75–1.10 | 0.13–0.21 | 47 | 84–86 | 98 | 293 | 2,605–2,711 | 998–1,045 | 3.7–4.1 |
| Vulkan static | 0 | 5.13–5.14 | 5 | 2.04–2.07 | 0.55–0.56 | 58 | 77 | 114 | 182 | 574–605 | 695–713 | 3.8–4.0 |
| Vulkan static | 1 | 5.15 | 4 | 3.13–3.14 | 0.50 | 29 | 49 | 58 | 138 | 455–473 | 594–597 | 3.0–3.5 |
| llvmpipe moving | 0 | 9.42–10.72 | 6–9 | 2.87–3.62 | 0.94–1.13 | 60–74 | 70–78 | 100–121 | 207–234 | 3,215–3,522 | 1,304–1,467 | 60.8–67.1 |
| llvmpipe moving | 1 | 6.09–6.85 | 7–9 | 1.80–2.44 | 0.28–0.59 | 36–39 | 55–70 | 71–87 | 187–253 | 2,701–3,009 | 1,358–1,485 | 51.7–57.0 |
| llvmpipe static | 0 | 8.67–8.79 | 5 | 5.39–5.43 | 1.44 | 58 | 78 | 116 | 186 | 650–679 | 988–999 | 57.6–59.1 |
| llvmpipe static | 1 | 5.20–5.22 | 4 | 2.49–2.63 | 0.59–0.60 | 29 | 49 | 56–58 | 134–138 | 484–501 | 827–836 | 47.9–49.1 |

Under motion `Coarser` one settles 1.1–1.3 s sooner on Vulkan and 2.6–4.6 s sooner
on llvmpipe, with a mean Current age of about half or less. Vulkan completes 17–20
covers against 8–12. A static view on llvmpipe settles 3.5 s sooner, within 0.2 s
of the five-second run length.

The saving is in counts, not in bytes. Loads fall by about half, upload batches by
about a third and mesh uploads by 40–50% (less on llvmpipe under motion: 55–70
batches against 70–78). A coarser tile is heavier: uploaded bytes under motion
are unchanged and fall by a quarter on a static load, and Prepare plus Build time
falls by 9–15% under motion and 21–26% on a static load. This agrees with
**s834**: the upload count is the binding limit.

Both static Vulkan modes end at the five-second run length, so their elapsed time
says nothing. The longer hold with `Coarser` one (3.1 s against 2.1 s) is
consistent with the final cover arriving earlier and then staying; the replay
does not record that time.

In the replay, peak process memory is 100–170 MiB lower on a static load and
unchanged under motion, where both modes fill the 256 MiB CPU cache. `Coarser` one evicts far
fewer preparations to stay within it (static 1 against 35–40, moving 10–52 against
101–193).

The replay covers camera zoom 9.8–10.2 only. Static loads of the same place at
eight camera zooms (800x600, llvmpipe, CPU cache and scene budgets raised to 1 GiB
so both modes draw complete covers, one run each) show that the saving depends on
the source zoom, because tiles of zoom 8 and zoom 13 are heavy:

| Camera zoom | Source tiles | Tiles | Labels | Uploaded MB | Peak cache MiB | Peak RSS MiB | Complete at default budgets |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | --- |
| 9 | z9 → z8 | 36 → 25 | 141 → 73 | 191 → 262 | 365 → 490 | 1,054 → 1,342 | no → no |
| 11 | z11 → z10 | 30 → 20 | 132 → 85 | 120 → 110 | 221 → 209 | 747 → 729 | yes → yes |
| 12 | z12 → z11 | 30 → 16 | 90 → 86 | 127 → 88 | 235 → 164 | 778 → 588 | yes → yes |
| 13 | z13 → z12 | 36 → 20 | 54 → 28 | 326 → 92 | 577 → 173 | 1,634 → 592 | no → yes |
| 14 | z14 → z13 | 30 → 20 | 110 → 26 | 194 → 242 | 369 → 428 | 1,087 → 1,224 | no → no |
| 14.5 | z14 → z13 | 25 → 16 | 81 → 14 | 181 → 204 | 345 → 360 | 1,012 → 1,055 | no → no |
| 15 | z14 → z14 | 20 → 20 | 89 → 76 | 184 → 139 | 337 → 269 | 1,021 → 810 | no → yes |
| 16 | z14 → z14 | 12 → 12 | 75 → 64 | 164 → 123 | 326 → 231 | 953 → 707 | no → yes |

Each cell reads `Coarser` zero → one. The zoom 13 row for zero needed upload
batches of 32 resources to settle in time, so its peak RSS is not strictly
comparable. With the viewer's default budgets (256 MiB cache, 128 MiB per scene)
six of the eight views stay incomplete with `Coarser` zero and three with one:
the scene budget is exceeded and parent tiles stand in.

The look changes most at camera zoom 13 to 14.5: buildings appear at 14 instead
of 13, and street names, transit stations and the minor streets of the old centre
appear at 15 instead of 14. Zoom 12, 15 and 16 are close to identical.

The option stays off in the viewer: an owner decision recorded on 2026-09-29
after comparing the eight views. The look with `Coarser` one at street level was
judged not acceptable, and the look with zero good. Turning the option on changes
the look at every camera zoom below 15 (thinner roads, fewer labels, full detail
one zoom later).

The missing names come from the tile data, not from the style. Coarser tiles
prepared with the style zoom of the camera (explicit targets one zoom lower,
`Coarser` zero, an experiment build) bring back the width of minor streets but
place the same 26 labels at camera zoom 14 and 14 at zoom 14.5: tiles of zoom 13
carry no minor street names or transit stations. No style setting on coarser
tiles recovers today's detail. These are matched input replays, not
matched frame work; no presentation pacing or MapLibre claim follows.

#### Verification

- View: the cover with `Coarser` one equals the default cover of a camera one zoom
  lower with half the viewport; source zoom is bounded at 0 and 14 and the option
  at `MaxCoarser`. `StyleZoomAt` keeps the sixteenth steps and stays at zero below
  camera zoom `Coarser`.
- Compiler and placement: a zoom-dependent line width takes the value of the
  style zoom and the tile units of the drawn zoom; pattern scale and symbol
  spacing follow the drawn zoom; values outside the range are rejected. Coverage
  stays at **100%**.
- Tiles: a tile prepared at style zoom z with `Coarser` one equals the same tile
  prepared at z+1 without it when the style does not depend on zoom, in
  primitives and symbol candidates, and differs from z without it.
- Producer: a request without targets loads and publishes only tiles one zoom
  below the camera; values outside the range are rejected by `Submit`.
- Native: static screenshots of the Madrid view render both modes on OpenGL. The
  tagged viewer and adapter suites pass on OpenGL, and tagged staticcheck passes.
  The tagged suites were not rerun on Vulkan.

### Compiled tile memory

Checkpoint **g1av** measures where the memory of a live view goes and removes two
costs that bought nothing. It began with the static views of **xcmf**: with the
viewer's default budgets only two of eight Madrid views drew their complete cover.

#### Where it goes

A headless probe submits one 800x600 view to the producer (resident options on,
budgets of 1 GiB, one lease held) and reads the Go heap after a collection. For
camera zoom 14, 30 tiles, 11 MB of responses:

| Live heap, 304 MB | MB | What it is |
| --- | ---: | --- |
| Packed vertices | 107 | `scene.Vertex`, 24 bytes each, 4.07 million in the scene |
| Packed indices | 46 | `uint32`, 8.04 million in the scene |
| Prepared primitives | 110 | float64 geometry kept to rebuild a tile for new assets |
| Glyph atlases, responses, metadata | 41 | |

The scene itself needs 93 MiB of vertices, 31 MiB of indices and 9 MiB of
textures. Mesh buffers were published with the capacity their growth left behind:
8–18 MiB unused per view, which was also charged to the cache and to every
lease. Of the vertices, 22% carry neither offset nor texture coordinate, 40% an
offset only, and 38% both. Building the view allocates 2.1 GiB in total.

#### Changes

- Mesh buffers are reserved from the prepared primitives before packing and
  published at their exact length (`geometry.Builder.Reserve` and `Compact`,
  `compiler.FragmentBuilder.Reserve`). Output is unchanged.
- `producer.Limits.DiscardPreparation` drops prepared primitives once a tile is
  built. A later asset change prepares the tile again from its raw response; a
  style-zoom change prepared it again already. The library default keeps them.
  The viewer discards them, because its assets never change
  (`-keep-preparation` restores the old behaviour).
- The viewer takes the scene budget as `-cpu-scene-bytes`. Its default is 256
  MiB and the default of `-cpu-cache-bytes` 384 MiB, an owner decision recorded
  on 2026-09-29 so that every view draws its complete cover. They were 128 MiB
  and 256 MiB. `producer.DefaultLimits` is unchanged.

#### Results

Headless probe, before → after, MiB:

| Camera zoom | Tiles | Live heap | Scene charge | Peak RSS |
| ---: | ---: | ---: | ---: | ---: |
| 9 | 36 | 325 → 172 | 158 → 140 | 579 → 338 |
| 11 | 30 | 176 → 91 | 86 → 78 | 370 → 205 |
| 12 | 30 | 161 → 79 | 78 → 70 | 407 → 212 |
| 13 | 36 | 532 → 243 | 248 → 234 | 924 → 516 |
| 14 | 30 | 295 → 165 | 148 → 133 | 612 → 356 |
| 14.5 | 25 | 265 → 149 | 134 → 120 | 564 → 321 |
| 15 | 20 | 287 → 183 | 156 → 140 | 569 → 369 |
| 16 | 12 | 263 → 179 | 140 → 125 | 556 → 396 |

Live heap falls by 32–54% and peak memory by 29–48%. Exact buffers alone account
for 7–18 MiB of the live heap; the rest is the preparation. Allocation while
building falls by 6–14% and build time is unchanged.

Viewer on llvmpipe with the previous budgets (256 MiB cache, 128 MiB per scene),
where the uploaded copies live in the same process:

| Camera zoom | Peak RSS MiB | Complete before | Complete after | With `-cpu-scene-bytes` 256 MiB |
| ---: | ---: | --- | --- | --- |
| 9 | 972 → 776 | no | no | yes |
| 11 | 773 → 576 | yes | yes | yes |
| 12 | 770 → 601 | yes | yes | yes |
| 13 | 945 → 857 | no | no | no |
| 14 | 961 → 794 | no | no | yes |
| 14.5 | 944 → 773 | no | yes | yes |
| 15 | 890 → 740 | no | no | yes |
| 16 | 869 → 763 | no | yes | yes |

Peak memory falls by 9–25%. Complete views go from two of eight to four, and to
seven with a scene budget of 256 MiB. Zoom 13 also needs a cache budget of 320
MiB or more (peak 1,095 MiB). Static screenshots at zoom 11 and 12 are identical
to the previous binary in all 480,000 pixels.

With the new default budgets all eight views are complete, at a peak of
578–819 MiB and 1,067 MiB for zoom 13. A scene shares its buffers with the cache,
so the larger scene budget costs little while both hold the same tiles, but the
documented worst case of cache plus four leases rises from 768 MiB to 1,408 MiB.

#### Verification

- Geometry: reserved builders produce the same buffers without reallocating;
  reservation is bounded by the element limit and never raises it; compacted
  buffers keep their contents and an exact buffer is not copied again. Coverage
  is **98.1%**.
- Compiler: fragments packed with and without reservation are equal, split and
  unsplit, indexed and expanded, with a present and a missing sprite, and every
  mesh has capacity equal to length. Coverage stays at **100%**.
- Tiles: fragments built with and without the resident options hold no unused
  capacity. The tests fail without the change.
- Producer: with `DiscardPreparation` the cache holds no preparation after a
  build, and new assets prepare the tile again without loading it; without it
  the preparation serves the new assets.
- Race, vet and staticcheck pass on geometry, compiler, tiles and producer; the
  full pkg/vecmap suite, the tagged viewer and adapter suites on OpenGL and
  tagged staticcheck pass. The tagged suites were not rerun on Vulkan.

### Draw margin and collector target

This continues **g1av** with two of its open items.

The prefetch ring was composed and uploaded with the view: 30 tiles at camera
zoom 14 where 12 are visible, and 12 at zoom 16 where two are. The ring is one
tile wide, so its width on screen grows from 256 pixels at a whole zoom to 512
just below the next one, and further when tiles are overzoomed.
`producer.Limits.DrawMargin` composes only the targets within that many logical
pixels of the viewport (`view.TilesNear`). The others are still loaded and
compiled, so a pan that brings one closer composes and uploads it without
loading or compiling. A target of the acknowledged Current stays until it is twice
as far, so a view resting at the margin does not upload and retire one tile
repeatedly. Zero, the library default, composes every target. The viewer uses 256
pixels (`-draw-margin`): the pan headroom the ring gives at a whole zoom, which
was the least it ever gave.

Tiles composed with a margin of 256 pixels, as a share of the cover, over camera
zoom 9 to 18 in quarter steps at Madrid: 0.76 at 800x600, 0.79 at 1280x800 and
0.72 at 372x695. Nothing changes at whole zooms up to 14. At 800x600 zoom 14.5
composes 20 of 25 tiles, zoom 15 nine of 20 and zoom 16 six of 12.

The viewer also sets the collector target to 50% (`-gc-percent`, zero keeps the
runtime's setting). The Go heap may then grow to one and a half times the live
heap between collections instead of twice.

#### Static views

Same eight views as above, viewer on llvmpipe, new default budgets, one run
each, without and with both changes:

| Camera zoom | Tiles composed | Uploaded MB | Peak RSS MiB |
| ---: | ---: | ---: | ---: |
| 9 | 36 → 36 | 191 → 191 | 826 → 735 |
| 11 | 30 → 30 | 120 → 120 | 566 → 539 |
| 12 | 30 → 30 | 126 → 126 | 606 → 558 |
| 13 | 36 → 36 | 313 → 308 | 1,098 → 1,002 |
| 14 | 30 → 30 | 194 → 194 | 808 → 720 |
| 14.5 | 25 → 20 | 180 → 139 | 792 → 658 |
| 15 | 20 → 9 | 183 → 98 | 793 → 615 |
| 16 | 12 → 6 | 164 → 98 | 792 → 610 |

Zoom 16 settles in 4.2 s against 7.8 s. Screenshots at zoom 12, 14, 14.5, 15 and
16 are identical in all 480,000 pixels. Zoom 13 takes 10–14 s to settle on
llvmpipe in either mode, close to the 15-second limit, and one run with the
margin missed it; the margin does not change that view.

#### Replay observations

Same corpus, host and arguments as **xcmf**, with the new default budgets. Four
modes from one binary, in two passes of three moving runs and one static run per
backend, the second pass in reverse order, on an unlocked desktop. Every series
started at a one-minute host load of 0.95–1.48. All 64 runs exit 0 with zero
pending, failed and batch failures. The trace stays within camera zoom 9.8–10.2,
where the margin composes 16 of 20 tiles at its end and changes nothing in the
static view.

| Replay | Mode | Elapsed s | Current changes | Longest hold s | Mean age s | Upload batches | Uploaded MB | Peak RSS MiB |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Vulkan moving | neither | 6.48–6.70 | 9–11 | 1.13–1.42 | 0.32–0.39 | 121–124 | 299–306 | 845–998 |
| Vulkan moving | margin | 5.82–6.14 | 14–18 | 0.93–1.27 | 0.19–0.27 | 97–106 | 250–267 | 857–952 |
| Vulkan moving | collector | 6.28–6.70 | 9–11 | 1.10–1.42 | 0.32–0.40 | 114–128 | 290–308 | 690–799 |
| Vulkan moving | both | 5.77–6.10 | 13–17 | 0.82–1.10 | 0.19–0.27 | 99–105 | 252–268 | 673–743 |
| Vulkan static | neither | 5.11–5.14 | 4 | 2.27–2.31 | 0.66 | 73 | 172 | 506–510 |
| Vulkan static | both | 5.11–5.12 | 4–5 | 1.99–2.24 | 0.57–0.67 | 74–78 | 176–186 | 424–427 |
| llvmpipe moving | neither | 9.16–10.35 | 6–8 | 2.90–3.97 | 0.73–1.06 | 69–78 | 206–240 | 1,236–1,308 |
| llvmpipe moving | margin | 7.33–11.08 | 5–7 | 2.72–3.54 | 0.61–1.23 | 54–78 | 152–212 | 1,053–1,168 |
| llvmpipe moving | collector | 8.67–9.90 | 5–7 | 2.35–4.58 | 0.86–1.12 | 66–75 | 190–232 | 950–1,087 |
| llvmpipe moving | both | 7.85–11.35 | 7–8 | 2.08–3.33 | 0.55–1.30 | 62–80 | 193–221 | 900–973 |
| llvmpipe static | neither | 7.11–7.40 | 4 | 4.89–5.12 | 1.49–1.57 | 70 | 165 | 765–773 |
| llvmpipe static | both | 8.84–8.86 | 5 | 5.43–5.51 | 1.45–1.64 | 78 | 186 | 708–710 |

On Vulkan under motion the margin settles 0.3–0.9 s sooner, completes 13–18
covers against 9–11 and brings the mean Current age from 0.32–0.39 s to
0.19–0.27 s. The collector target lowers peak memory by about a fifth and changes
no time. Together: the margin's times and 673–743 MiB against 845–998 MiB.

On llvmpipe under motion peak memory falls by about a quarter. Times overlap and
spread more widely with the margin (7.3–11.4 s against 9.2–10.4 s).

The static llvmpipe rows differ by which intermediate covers a run uploads
(104 or 116 meshes), not by mode: five further static runs per collector target
with the margin settle in 8.8–10.3 s at 100% and 8.8–12.4 s at 50%, with equal
Prepare and Build time and a peak of 757–786 MiB against 693–722 MiB. That series
ran at a host load of 4.

The pan headroom is not measured. The replay pans 20 pixels, and the viewer takes
no input. With the margin a pan finds at least 256 pixels of composed map in
every direction, as it did at a whole zoom before, where it used to find up to
512 at other zooms.

#### Verification

- View: `TilesNear` keeps input order, follows a rotated camera by the bounds of
  the turned tile and both sides of the antimeridian, treats a negative or
  non-finite margin as zero and rejects invalid tiles.
- Producer: with a margin the scene is composed from the near targets while all
  targets are loaded once; a pan composes a loaded target without loading it
  again; a target of the acknowledged Current stays between one and two margins
  and leaves without it; a margin of zero composes every target; margins that are
  negative, not finite or above 2^20 are rejected. The tests ran 200 times.
- Race, vet and staticcheck pass on view and producer; the full pkg/vecmap suite
  and the tagged viewer suite on OpenGL pass.

### Compact vertices

This closes the last open item of **g1av**. A `scene.Vertex` is 24 bytes:
position, pixel offset and texture coordinate. Fills and patterns use the
position only and extruded lines no texture coordinate, which is 62% of the
vertices of a dense view.

`tiles.PrepareOptions.CompactVertices` (viewer `-compact-vertices`, default on;
library default off) packs fills and patterns as `scene.PositionVertex` (8
bytes) and extruded lines as `scene.OffsetVertex` (16 bytes). Dashed lines,
icons and text keep every attribute. A mesh holds the three kinds as sections
(`Vertices`, `Offsets`, `Positions`) and a draw names its section in
`Draw.Layout`; indices and unindexed ranges count from the start of the section.
A tile still has the same meshes, so nothing is added to the count of uploads,
which is the binding limit (**s834**). Scene files without sections are
unchanged.

The adapter packs the sections into one vertex buffer and keeps one input layout
per section. A section without an attribute reads the position in its place, and
the vertex shader, told the layout in `view.w`, uses zero instead. Only
`map.vert.qsb` is regenerated. Pipelines go from two to six.

No value is rounded or quantized, so the output is the same: a quantized 12-byte
vertex would save half instead of a quarter of the vertex bytes, at a loss of
precision when tiles are overzoomed.

#### Static views

Viewer on llvmpipe, default flags, without and with compact vertices, one run
each:

| Camera zoom | Uploaded MB | Peak cache MiB | Peak RSS MiB | Pixels that differ |
| ---: | ---: | ---: | ---: | ---: |
| 9 | 191 → 152 | 224 → 187 | 743 → 659 | 0 |
| 11 | 120 → 96 | 136 → 113 | 540 → 495 | 0 |
| 12 | 126 → 101 | 140 → 115 | 553 → 496 | 0 |
| 13 | 324 → 248 | 328 → 255 | 969 → 817 | 0 |
| 14 | 194 → 155 | 229 → 191 | 739 → 645 | 0 |
| 14.5 | 139 → 110 | 204 → 169 | 653 → 578 | 0 |
| 15 | 98 → 82 | 223 → 192 | 606 → 563 | 0 |
| 16 | 98 → 84 | 229 → 209 | 604 → 579 | 0 |

Uploads fall by 14–23%, the compiled tile cache by 9–22% and peak memory by
4–16%. In the headless probe the zoom 14 view keeps a live heap of 140 MiB
against 165 MiB and peaks at 309 MiB against 343–350 MiB.

A first version of this change saved nothing in the heap, although every charge
fell. A scene points into the builder that packed it, so the builder stays
allocated with it, and it still held the index buffer of each section next to the
shared one it had published: 25 MiB in that view. The builder now lets go of
its sections on publication. `Reserve` also allocates exact capacities, so a
buffer reserved at its final length is not copied again by `Compact`; building
the view allocates 1.7 GiB against 1.9 GiB. The numbers in this section are from
the corrected build; the zoom 13 pixel comparison is from the first. On Vulkan the zoom
13 view uploads 249 MB against 326 MB and settles in 4.3 s in both modes, with
no differing pixel at 1600x1200. On llvmpipe that view settles in 11–14 s and
misses the 15-second limit in some runs of either mode.

#### Replay observations

Same corpus, host and arguments as the draw margin series. Both modes from one
binary, two passes of three moving runs and one static run per backend, the
second pass in reverse order, on an unlocked desktop. Every series started at a
one-minute host load of 1.33–1.44. All 32 runs exit 0 with zero pending, failed
and batch failures and end at 16 tiles / 94 labels (static 42 / 130).

| Replay | Vertices | Elapsed s | Current changes | Longest hold s | Mean age s | Upload batches | Uploaded MB | Peak RSS MiB | GPU p95 ms |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Vulkan moving | full | 5.87–6.15 | 14–16 | 0.88–1.28 | 0.21–0.26 | 98–103 | 253–259 | 642–778 | 3.6–4.0 |
| Vulkan moving | compact | 5.83–6.07 | 14–16 | 0.88–1.20 | 0.21–0.26 | 102–108 | 206–218 | 581–657 | 3.9–4.2 |
| Vulkan static | full | 5.11–5.12 | 5 | 1.99–2.04 | 0.55–0.57 | 78–79 | 186 | 423–433 | 3.6–4.1 |
| Vulkan static | compact | 5.12–5.13 | 4–5 | 1.96–2.24 | 0.57–0.67 | 74–78 | 143–150 | 383–390 | 3.4–3.9 |
| llvmpipe moving | full | 8.41–10.06 | 6–7 | 2.68–3.34 | 0.74–1.13 | 65–73 | 204–208 | 946–989 | 58.4–61.3 |
| llvmpipe moving | compact | 7.72–8.99 | 6–8 | 2.10–3.21 | 0.67–0.95 | 58–67 | 152–170 | 764–846 | 58.3–63.7 |
| llvmpipe static | full | 7.10–8.69 | 4–5 | 4.93–5.35 | 1.49–1.61 | 69–78 | 161–186 | 696–731 | 59.2–59.4 |
| llvmpipe static | compact | 6.83–6.92 | 4 | 4.68–4.81 | 1.42–1.51 | 69 | 131 | 620–622 | 56.5–57.2 |

Under motion about a fifth fewer bytes are uploaded on both backends and peak
memory is about a tenth lower on Vulkan and a sixth on llvmpipe. Times are
within the range of full vertices on Vulkan and at or below it on llvmpipe, and
the six pipelines cost no GPU time.

Since the start of **g1av** the viewer's peak memory for the zoom 14 view on
llvmpipe went from 961 MiB, with an incomplete cover, to 645 MiB with a complete
one, and its uploads for the complete cover from 194 MB to 155 MB.

#### Verification

- Scene: sections validate their vertices and bound the indices of each draw by
  its own section, indexed and unindexed; draws of different sections never
  merge; scenes without sections serialize as before and sectioned scenes
  survive a round trip. `IndexMesh` leaves sectioned meshes alone.
- Compiler: for indexed and expanded output, split and unsplit, with and without
  resident symbols and reservation, every draw of a compact fragment expands to
  exactly the vertices of the full fragment, with equal draw sources, textures
  and mesh count and buffers of exact length. A finished builder holds no
  buffer of its own. Coverage stays at **100%**.
- Tiles: a tile with fills, outlines, plain and dashed lines and a label draws
  the same vertices in both modes and keeps its stable mesh across a style-zoom
  change.
- Retained: a changed section is a new revision, identical sections keep the
  resident one, and the byte limit counts every section.
- Native: fills, an extruded line and a dashed line drawn from sections, with
  draws alternating between them, match the same geometry with full vertices in
  every pixel, as one mesh upload of fewer bytes, indexed and unindexed. The
  existing adapter tests pass unchanged on the six pipelines.
- Race, vet and staticcheck pass on scene, compiler, retained, tiles and
  producer; the full pkg/vecmap suite, the tagged adapter and viewer suites on
  Vulkan and OpenGL and tagged staticcheck pass.

### Shared halo quads (nfzk)

Peak RSS follows the live heap (see "What holds the live heap"), and fragment
meshes are most of it. On the 82 corpus tiles at the trace's Coarser 1 zoom
(z+1.25) fragments held 325 MB: extruded-line vertices 127 MB (four float32),
stable-mesh indices 84 MB, symbol vertices 39 MB (six float32), dashed-line
vertices 14 MB and fill positions 8 MB.

**Halo and fill share their quads.** A haloed label was packed twice, once for
the halo pass and once for the fill, with identical vertices: halos were 48% of
the symbol mesh's index elements. The fill now draws the halo's range with its
own material, and text without a halo packs its quads in the fill pass. The
symbol mesh falls from 48.9 to 25.3 MB on the corpus. Five interleaved runs each
of e9a9243 and this change:

| Row | Peak live before → after | Peak RSS before → after | Build wall before → after |
| --- | ---: | ---: | ---: |
| Coarser 1, OpenGL | 178–228 → 171–209 MiB | 439–509 → 426–511 MiB | 608–624 → 528–586 ms |
| Coarser 1, Vulkan | 171–237 → 158–223 MiB | 385–476 → 366–456 MiB | 595–630 → 560–594 ms |
| Coarser 0, OpenGL | 245–268 → 233–253 MiB | 536–551 → 503–540 MiB | 885–951 → 852–909 ms |
| Coarser 0, Vulkan | 240–260 → 224–250 MiB | 485–515 → 464–498 MiB | 875–995 → 829–916 ms |

Final frames are byte-identical, every run had zero late frames and the same
labels; process CPU stayed within its spread.

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

Include `patches/qt-6.11.2-checked-vulkan-finish.patch` in that controlled Qt build.
The Vulkan batch renderer requires its runtime marker as well as Qt version
6.11.2; using patched headers with an unpatched QtGui library is insufficient.

The production application remains on its existing renderer while these gates
are open. This prototype adds explicit opt-in build/test targets.

## Remaining migration gates

- Continue moving the CPU engine out of its Qt-bound package; camera, projection,
  tile coverage and transforms have been extracted into `pkg/vecmap/view`.
- Reuse geometry across live style-zoom changes. Fills and shader-extruded lines
  (**yfq2**), symbols (**s834**) and dashed lines (**yj2k**) now stay resident,
  by default in the viewer (**kykh**); CPU-side reuse of tessellation remains.
- Replace the fixture with incremental live tile/placement updates and bounded
  upload scheduling. Validate fallback clipping and world wraps under motion.
- Validate multiple simultaneous maps and GPU resource sharing where safe.
- Compare identical data, style, label density, resolution, and antialiasing with
  MapLibre Native across dense/rural/overzoom/HiDPI traces and tile-arrival bursts.
  The Madrid trace is done, under headless mutter ("Matched MapLibre Native
  comparison"); the other traces and a real display remain.
- Measure full GUI/render/GPU/presentation costs and memory stability. Meeting
  the display refresh rate on this one retained fixture is only an initial gate.

The SDF shader reuses the existing MapLibre-derived equations; their BSD notices
are retained in `ui/shaders/LICENSE`. Generated binding notices are in
`internal/qtrhi/LICENSE`.
