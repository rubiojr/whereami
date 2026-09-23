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
the wrappers at the frame-end boundary and defers underlying native destruction
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
4. Upload commands are explicitly submitted with a checked **in-frame QRhi::finish**
   while they still belong to the current command buffer. `afterFrameEnd`, connected
   with **Qt::DirectConnection**, then observes the frame boundary. It is not proof
   of submission: Qt also emits it on failed beginFrame/endFrame. The following
   prepare checks the same healthy context and reports the already-completed upload
   without another drain. Retirement and rolled-back allocation batches instead
   drain in that following prepare, before reporting completion. Allocation failure
   retains earlier successful batches and permits a fresh ticket; submission/drain
   failure resets the entire namespace. Current stays selected during partial uploads.
5. The callback must enqueue its result without blocking or reentering the renderer.
   The worker acknowledges the ticket and publishes the new Current frame before,
   or together with, the next retirement batch. The adapter preserves exact selected
   revisions and activates staged resources without another upload.

Before executing its first batch, each new BatchRenderer explicitly submits its
white/uniform initialization, waits for that frame to end, then drains retirement
in the next prepare. These **two startup finish calls** prove initialization and
handle replacement nodes using the same QRhi: their predecessor's deferred releases
must finish before new Planner-owned allocations are made. `Stats.PrepareTime`
covers the entire batch prepare callback. `CompletionDrains` counts explicit finish
calls, including failed attempts; `CompletionDrainTime` accumulates their render-thread
wall time, including submission and GPU waiting. The viewer prints these cumulative
values even though its warm-frame timing samples exclude the first 30 frames.

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
from `afterFrameEnd` using an out-of-frame finish would be incorrect. A later idle
drain also cannot recover uploads from an earlier failed endFrame; upload submission
must be checked in its producing frame. Qt's
[basic](https://github.com/qt/qtdeclarative/blob/v6.11.2/src/quick/scenegraph/qsgrenderloop.cpp)
and [threaded](https://github.com/qt/qtdeclarative/blob/v6.11.2/src/quick/scenegraph/qsgthreadedrenderloop.cpp)
loops emit the signal after error results too. Other source evidence:
[Vulkan](https://github.com/qt/qtbase/blob/v6.11.2/src/gui/rhi/qrhivulkan.cpp),
[OpenGL](https://github.com/qt/qtbase/blob/v6.11.2/src/gui/rhi/qrhigles2.cpp),
[QRhi contract](https://doc.qt.io/qt-6/qrhi.html#finish).

Context release/replacement, prepare failure, drain failure or node destruction emits
one `Reset` result and permanently invalidates the instance. Reset is **not** a failed
batch acknowledgement: discard the old Planner and recreate the renderer/node and
Planner together. Tag transport messages with the renderer instance/generation and
ignore old results after recreation. General native/device failures do not promise
old-frame recovery. Signal subscriptions are explicitly disconnected on node
destruction, so recreating nodes does not accumulate inert window-lifetime callbacks.

White texture, uniforms, bindings and pipelines remain backend overhead outside the
Planner's logical byte/resource limits. The executor performs no extra geometry or
bitmap conversion. The fixture viewer now supplies a `retained.Worker`, generation-
tagged packets/results, timer-driven GUI frame pumping, camera coalescing and native
node recreation. `WorkerWithData[*scene.Document]` now keeps each Current scene
paired with its own transform-slot mapping across live document replacement.
Tile coverage and placement selection still belong to the producer.
Replacement of the blocking drain remains subsequent work. The production
renderer/scheduler is unchanged.

The [Qt 6.11.2 asynchronous-completion audit](../../docs/vecmap-rhi-completion.md)
records why readback callbacks alone cannot replace checked submission: Vulkan
can invoke them without a successful producing submission, QRhi cleanup callbacks
precede backend result writes during destruction, and OpenGL readback can block.
`frameSwapped` is not a submission-success signal either. Resolve the native
submission and final-reference boundaries before introducing result ownership.

### Signal subscription ownership

The regenerated `OnAfterFrameEnd` returns a `*qtrhi.SignalConnection`. Its owner must
call `Disconnect` on the owning thread before dropping it, even if the sender has
already died. Disconnect is idempotent, can run from its own callback, and affects
only that subscription. The native connection copy uses explicit deletion, not a
Go finalizer. Do not copy the Go wrapper or use it concurrently.

The generated Qt functor owns the Go callback handle through shared native lifetime
storage. Disconnect or sender destruction releases it after any in-progress callback
returns. No separate sender-destruction subscription is needed. This is generic Qt
callback ownership in the generator; all rendering and upload policy remains Go.

### Live fixture targets

The viewer accepts an opt-in `-reload` interval:

```sh
vecmap-rhi -scene /tmp/live-scene.json -reload 250ms -duration 0 \
  -upload-resources 1
```

Publish files by writing a sibling temporary file and renaming it over the scene
path. One background producer reads at most 128 MiB per poll, compares a SHA-256
digest, then decodes, validates and remaps changed documents through one retained
Store. Identical file contents don't publish a target, even after a rename.
Invalid files report an error and retain the last valid target; changed content
can recover. Viewport size and geographic/affine coordinate mode must stay fixed.
Transform slots, tile spaces, resources and draw order may change. Mappings are
limited to 65,536 slots; Store limits apply to scenes.

Each accepted file replaces one fragment. Surviving local resource IDs retain
global IDs and get new revisions, so edited bytes cannot masquerade as already
resident data even when the file reuses its original IDs/revisions. This deliberately
reuploads changed documents conservatively. It isn't a fine-grained tile updater.

The producer publishes one latest-document slot. The worker coalesces targets while
native work is pending and carries the Current document in its packet. The render
thread projects **that document's** tile spaces with the latest common camera, or
composes the camera's affine transform with its local transforms. It never pairs
an old scene with an unfinished target's slots. The initial document defines the
camera trace; reload does not jump to a replacement document's captured camera.

The 8 ms GUI timer only samples the latest target and camera. Decoding, Store
updates and Planner validation stay off GUI/render callbacks. Resource residency
or indivisible-upload budget errors still stop the benchmark. At a timed exit,
the latest sampled target must be ready before a screenshot is accepted.

This supplies a live document-replacement harness. Live network tile loading,
cover/order/wrap selection and cross-tile placement remain the next producer work.
The checked submission and retirement drains remain in use.

## Verification

```sh
QT_RHI_INCLUDE=/path/to/QtGui/6.11.2/QtGui \
QSG_RHI_BACKEND=vulkan GOAMD64=v1 \
sh scripts/qt-rhi-env.sh go test -count=1 -tags 'vecmap_rhi integration' \
  ./internal/qtrhi ./internal/vecmaprhi ./cmd/vecmap-rhi

QT_RHI_INCLUDE=/path/to/QtGui/6.11.2/QtGui \
QT_SCALE_FACTOR=2 QSG_RHI_BACKEND=opengl GOAMD64=v1 \
xvfb-run -a sh scripts/qt-rhi-env.sh go test -race -count=1 \
  -tags 'vecmap_rhi integration' ./internal/qtrhi ./internal/vecmaprhi ./cmd/vecmap-rhi
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
The later **z3cc** checkpoint adds scoped signal disconnection and checked submission.
Native renderer coverage is **91.0%**. Tests exercise submission result failures as
namespace resets, exactly one finish per ordinary batch (two for startup), and
disconnection on node destruction. Binding tests disconnect 1,000 subscriptions
while retaining the window, preserve independent listeners, self-disconnect, destroy
the sender first, and use weak Go probes to verify callback handles release captured
objects. Submission failures are injected API results with a healthy native device,
not physical GPU-device-loss tests.
