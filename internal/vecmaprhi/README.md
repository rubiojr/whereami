# vecmaprhi

Opt-in (`vecmap_rhi`) Go adapter from immutable `scene.Frame` data to Qt Quick's
QRhi context and command buffer. Native handles and render-thread lifetimes live
here; geometry, style, packing and retained upload planning live in shared Go
packages. Bindings in `internal/qtrhi` are generated for **Qt 6.11.2**.

## Revision-aware resource staging

Mesh and texture caches use exact **ID/revision pairs**, in separate namespaces.
The implicit white texture is key `{0,0}`. Per-draw keys are prepared when the
selected scene changes, so the draw loop chooses a particular revision even when
another revision of the same resource ID has already been uploaded. Camera-only
frames reuse both scene resource selection and resident allocations.

`resources.go` separates three render-thread operations:

1. **Allocate:** `allocateResources` skips already-cached exact versions, then
   creates all missing mesh buffers and textures, including white when needed.
   No upload commands are recorded until every allocation succeeds. On failure,
   freshly allocated objects are immediately destroyed and resident caches remain
   unchanged. Factories must clean up their own failed allocation. The synchronous
   factory boundary permits deterministic failure injection after real allocations.
2. **Record:** `recordResources` records uploads into one caller-owned update batch,
   transfers handles to the revision caches and consumes the stage. An already
   consumed/discarded stage owns no objects. CPU buffers are borrowed through
   recording with the original QRhi data-copy/lifetime behavior. No application
   geometry or bitmap copy is added.
3. **Select:** `selectResources` prepares ordered per-draw resource keys and retires
   unselected versions. A previously staged exact version activates without another
   upload. Staging alone does not change selected draws or retire the old version.

Inputs are validated immutable scenes prepared off-thread. These private adapter
operations do not duplicate the shared engine's input/resource-budget validation.
The ordinary `New` path synchronizes all missing resources for a changed scene.
The fixture viewer now uses `BatchRenderer` and `retained.Worker`. Resource allocation is
transactional, not the entire frame: the existing automatic prepare/pipeline error
path still resets renderer resources. The separate `BatchRenderer` below executes
worker-planned batches.

## Retirement and acknowledgements

[QRhiResource's lifetime contract](https://doc.qt.io/qt-6/qrhiresource.html#deleteLater)
requires C++ wrappers referenced by current-frame commands to survive until
`endFrame`. Resident buffers, textures, bindings, pipelines, uniforms and samplers
therefore use the already-generated `DeleteLater` method when retired. Qt deletes
the wrappers after frame submission and defers underlying native destruction
further until in-flight GPU use is safe. Fresh, unrecorded allocation failures can
be destroyed immediately.

This distinction is essential for planner execution:

- Recording `ResourceUpdate` is not a completed resource upload acknowledgement.
  Submission and ordering must guarantee use by the published frame.
- Calling `DeleteLater` is not a completed release acknowledgement and must not
  immediately reclaim the headless planner's logical residency budget. The batch
  executor waits for end-of-frame and a subsequent native drain as described below.
- `Stats.MeshUploads`, `TextureUploads` and `UploadedBytes` count recorded upload
  commands. `LiveMeshes`/`LiveTextures` count cache entries, excluding deferred
  retirements; they are not an outstanding-native-allocation or VRAM meter.
- Context recreation/release clears selected keys and caches. Planner integration
  must reset CPU readiness with the corresponding native namespace.

Generated virtual overrides are installed once. `New` uses the private node/lifetime
constructor and installs the normal prepare callback; integration tests install a
single wrapper callback around staging, rather than trying to replace an existing
generated override. Additional binding scope is regenerated through `cmd/qt-rhi-gen`;
there is no handwritten application C++.

## Bounded batch executor

`NewBatchRenderer(item, observe, notify)` creates a dedicated empty native namespace.
Its `Node` has Qt ownership, like the ordinary renderer. A preparation worker owns
one `retained.Planner`; **all Planner methods, including Current and SetTarget,
stay with that single owner**. The adapter never deep-validates payloads or invokes
the Planner in a rendering callback.

1. The worker calls `SetTarget`, then `Next(budget)` and prepares an immutable frame
   for **Current**, retaining that scene's transform-slot mapping while the target
   is incomplete. Send that frame and batch as one ordered packet.
2. In `updatePaintNode`, call `Sync(frame, batch)`. Batch metadata is copied, while
   geometry, pixels, scene and transforms remain immutable borrows. Only original
   unmodified Planner descriptors are accepted by contract; this is not a second
   untrusted-input validation API. Tickets must increase within the namespace.
3. The next prepare selects only already-resident versions. Missing resources in a
   published frame reset the namespace instead of bypassing the upload budget.
   Allocation/recording uses only the batch resources. No automatic scene-selection
   eviction occurs. Releases are preflighted in full against selected resources,
   duplicate versions and missing handles before any destruction is scheduled.
4. `afterFrameEnd`, connected with **Qt::DirectConnection**, observes frame submission
   on the render thread. It does not acknowledge residency. The following prepare,
   outside any render pass, checks the same healthy context and calls `QRhi::finish`.
   Only then does `notify(BatchResult)` report success or a rolled-back allocation
   failure. A failure result retains earlier successful batches and allows a fresh
   Planner ticket. The renderer continues drawing Current throughout partial uploads.
5. The callback must enqueue its result without blocking or reentering the renderer.
   The worker acknowledges the ticket and publishes the new Current frame before,
   or together with, the next retirement batch. The adapter preserves exact selected
   revisions and activates staged resources without another upload.

Before executing its first batch, each new BatchRenderer also waits for one prepared
frame to end and drains in the next prepare. This startup barrier handles replacement
nodes using the same QRhi: their predecessor's deferred releases must finish before
new Planner-owned allocations are made. White/uniform backend overhead may be created
during this barrier. `Stats.PrepareTime` covers the entire batch prepare callback,
including allocation/recording and blocking startup/completion drains.

**Keep requesting frames until the callback arrives**, even when Current is nil or
empty. Hidden/suspended windows may delay completion; there is no timeout-based
optimistic acknowledgement. This executor requires normal QQuickWindow frame-end
signals; `grabWindow` alone is not a completion pump. `Sync` returns `ErrBusy` for
any update while a batch is pending, so callers coalesce camera updates until the
next available synchronization. Between batches a nil batch updates only the frame.
Nil scene clears selection while release batches continue to run.

The drain is deliberately blocking, including after allocation failures, so discarded
allocations cannot accumulate outside Planner accounting. This is a correctness
baseline, **not a nonblocking upload scheduler or a performance result**. On Qt
6.11.2 Vulkan, `finish` waits for the queue and drains deferred releases. OpenGL's
implementation does nothing outside a frame: its next `beginFrame` processes deferred
deletions, then `finish` inside the frame calls `glFinish`. This is why acknowledging
from `afterFrameEnd` using an out-of-frame finish would be incorrect. Source evidence:
[Vulkan](https://github.com/qt/qtbase/blob/v6.11.2/src/gui/rhi/qrhivulkan.cpp),
[OpenGL](https://github.com/qt/qtbase/blob/v6.11.2/src/gui/rhi/qrhigles2.cpp),
[QRhi contract](https://doc.qt.io/qt-6/qrhi.html#finish).

Context release/replacement, prepare failure, drain failure or node destruction emits
one `Reset` result and permanently invalidates the instance. Reset is **not** a failed
batch acknowledgement: discard the old Planner and recreate the renderer/node and
Planner together. Tag transport messages with the renderer instance/generation and
ignore old results after recreation. General native/device failures do not promise
old-frame recovery. Signal handles are released on window destruction; destroyed
nodes sever the signal's Go reference to their renderer and borrowed payloads.
Inert per-node registrations remain until window destruction; explicit disconnect
support is a follow-up for repeated node recreation in a long-lived window.

White texture, uniforms, bindings and pipelines remain backend overhead outside the
Planner's logical byte/resource limits. The executor performs no extra geometry or
bitmap conversion. The fixture viewer now supplies a `retained.Worker`, generation-
tagged packets/results, timer-driven GUI frame pumping, camera coalescing and native
node recreation. Its fixed document shares transform slots across generations.
Changing live targets needs its own scene/frame mapping and placement handoff.
Replacement of the blocking drain remains subsequent work. The production
renderer/scheduler is unchanged.

## Verification

```sh
QT_RHI_INCLUDE=/path/to/QtGui/6.11.2/QtGui \
QSG_RHI_BACKEND=vulkan GOAMD64=v1 \
sh scripts/qt-rhi-env.sh go test -count=1 -tags 'vecmap_rhi integration' \
  ./internal/qtrhi ./internal/vecmaprhi ./cmd/vecmap-rhi

QT_RHI_INCLUDE=/path/to/QtGui/6.11.2/QtGui \
QT_SCALE_FACTOR=2 QSG_RHI_BACKEND=opengl GOAMD64=v1 \
xvfb-run -a sh scripts/qt-rhi-env.sh go test -race -count=1 \
  -tags 'vecmap_rhi integration' ./internal/vecmaprhi
```

Tests run with Qt's basic render loop on the QApplication's locked OS thread. They
cover expanded/indexed pixel equality, clipping, opacity, camera-only retention,
resize/invalidation/destruction, live uniform-buffer growth, allocation rollback,
simultaneous old/new revisions, old pixels during staging, activation without
reupload, consumed-stage ownership and retirement while upload commands still
belong to the current frame.

Verified with Vulkan on **Radeon 860M / RADV KRACKAN1** and OpenGL on Mesa llvmpipe,
including 2× scale and v1 race checks. Native renderer coverage is **89.0%**, with
all `resources.go` functions at **100%**; native creation/device/pipeline failure
guards remain partly uncovered. Expanded and direct-indexed full Madrid Vulkan
screenshots are byte-identical to their earlier reference PNGs. See kata **bekb**
and [`docs/vecmap-rhi.md`](../../docs/vecmap-rhi.md) for measurements and limitations.

The later batch checkpoint (**qfmf**) passes Vulkan and OpenGL/2×/v1 race checks on
Qt 6.11.2 / Mesa 26.2.2, with **90.1%** renderer coverage. Batch upload/release,
ticket/preflight guards, frame-end observation and invalidation have 100% coverage;
native device/drain and prepare failure guards remain partly uncovered. Tests keep
the old scene during bounded partial uploads, retry failed multi-resource allocations,
supersede partial targets, publish new transform slots, move the camera without
uploads, retire after native completion and invalidate a pending batch at teardown.
A cross-thread signal probe checks direct delivery.

The worker-viewer checkpoint (**qfkf**) additionally runs the real viewer transport
in separate basic and threaded Qt processes on Vulkan and OpenGL, including 2×/v1
race checks. Tests recreate native resources while idle and after upload recording
but before acknowledgement, ignore old-epoch callbacks, preserve camera-only pixels
without uploads, reject an undersized budget, and cancel workers at teardown. These
are explicit resource-release/reset tests, not injected physical GPU device loss.
The initial native drain protects old namespace retirement on same-context resets.
The window's inert signal registrations still persist until window destruction.
