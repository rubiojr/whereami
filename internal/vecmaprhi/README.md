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
The current viewer still synchronizes all missing resources for a changed scene;
it does not yet execute bounded `retained.Planner` batches. Resource allocation is
transactional, not the entire frame: the existing automatic prepare/pipeline error
path still resets renderer resources. Planner-driven recovery/publication is a
separate integration step.

## Retirement and acknowledgements

[QRhiResource's lifetime contract](https://doc.qt.io/qt-6/qrhiresource.html#deleteLater)
requires C++ wrappers referenced by current-frame commands to survive until
`endFrame`. Resident buffers, textures, bindings, pipelines, uniforms and samplers
therefore use the already-generated `DeleteLater` method when retired. Qt deletes
the wrappers after frame submission and defers underlying native destruction
further until in-flight GPU use is safe. Fresh, unrecorded allocation failures can
be destroyed immediately.

This distinction is essential for future planner execution:

- Recording `ResourceUpdate` is not a completed resource upload acknowledgement.
  Submission and ordering must guarantee use by the published frame.
- Calling `DeleteLater` is not a completed release acknowledgement and must not
  immediately reclaim the headless planner's logical residency budget. An explicit
  end-of-frame/native-retirement handoff is still required.
- `Stats.MeshUploads`, `TextureUploads` and `UploadedBytes` count recorded upload
  commands. `LiveMeshes`/`LiveTextures` count cache entries, excluding deferred
  retirements; they are not an outstanding-native-allocation or VRAM meter.
- Context recreation/release clears selected keys and caches. Planner integration
  must reset CPU readiness with the corresponding native namespace.

Generated virtual overrides are installed once. `New` uses the private node/lifetime
constructor and installs the normal prepare callback; integration tests install a
single wrapper callback around staging, rather than trying to replace an existing
generated override. No generated bindings or handwritten C++ are changed.

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
