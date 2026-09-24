# retained

Toolkit-neutral retained fragment updates and ordered scene composition. This is
the identity/publication layer between prepared tile scenes and the acknowledged
upload planner described below. It uses `scene.Scene` without Qt, cgo or new module
dependencies. The QRhi fixture viewer now uses its asynchronous upload Worker;
the production renderer and live tile scheduler retain their existing path.

```go
store, err := retained.New(retained.Limits{})
// Handle err.
err = store.Apply([]retained.Change{
    {Key: "tile-a", Scene: preparedA},
    {Key: "tile-b", Scene: preparedB},
})
// Handle err before composing a snapshot.
snapshot, err := store.Snapshot([]retained.Range{
    {Key: "tile-a", First: 0, Count: 1, Transform: 0},
    {Key: "tile-b", First: 0, Count: 1, Transform: 1},
    {Key: "tile-a", First: 1, Count: 1, Transform: 0},
})
```

`Range.First/Count` address **draw records**, not vertices/indices. A range replaces
the selected draws' transform slots with the requested instance slot. Selection is
explicit and ordered: interleave tiles by style layer/pass as required. The store
does not infer layer order from a flattened scene, sort transparency, choose
fallback coverage, recompute label placement, or fix pattern world-wrap phases.
Keep those policies in the producer/selector. Material fields and clips are copied
exactly. Repeated ranges/instances share the fragment's resource identities.

The [tiles compositor](../tiles/README.md) is now one such producer: it combines
prepared per-tile scenes with explicit layer/candidate/pattern metadata, the shared
fallback selector, world instances and collision placement. Store's lower-level
contract remains independent of those map policies.

## Resource identities

Input IDs are local to each fragment. Mesh ID 1 and texture ID 1 in multiple input
scenes are valid. On insertion, the store assigns nonzero store-wide IDs in change
order, then mesh/texture slice order. Texture zero remains the backend white
texture. Local input revisions are ignored.

- A key identifies a logical retained fragment slot. Use caller-defined tile,
  style, wrap or other identity as appropriate. Keys must be nonempty and at most
  **256 bytes**; retained strings are copied into owned storage.
- Replacement preserves global IDs for local resource IDs still present in that
  same fragment. It increments **every resource's revision** using the fragment's
  replacement generation, even if source revisions/bytes did not change. This
  conservative rule also handles scene packers whose first-use local IDs can change
  meaning between compilations. Untouched fragments keep their IDs and revisions.
- This is slot identity, not content addressing or cross-tile atlas deduplication.
  Do not replace a fragment merely for camera motion: retain its snapshot and
  update the frame transforms. Fine-grained resource dirtiness is a later extension.
- Removed resources lose their identity. Removing/re-adding a fragment or one of
  its resources assigns fresh IDs; old IDs are never reused. ID/revision exhaustion
  returns `ErrLimit` without changing state or consuming IDs.
- Each store is an independent namespace. Do not merge different stores' snapshots
  or switch stores in a backend without resetting its residency cache.

QRhi already compares mesh/texture ID and revision pairs before uploading. This
package establishes those pairs for multiple retained fragments; it does not
perform uploads or make native replacement atomic. The separate `Planner` tracks
adapter acknowledgements against exact resource versions.

## Atomic updates and bounds

`Apply` takes inserts/replacements and nil-scene removals. Duplicate keys within a
batch reject; removing an absent key is a no-op. Empty scenes are valid fragments.
The zero-value store rejects operations; use `New`.

| Limit | Default and maximum |
| --- | ---: |
| Retained fragments | 128 |
| Meshes | 4,096 |
| Textures | 4,096 |
| Draw records | 65,536 |
| Logical geometry/index/RGBA bytes | 512 MiB |

Zero limit fields select defaults; positive fields may lower ceilings. A batch has
at most twice the fragment limit in changes, allowing a complete remove-and-replace
at capacity. Its new scenes' aggregate resource/draw/byte counts must fit the limits
independently, as must the final retained state. Final accounting subtracts all
replaced/removed fragments before adding new ones, irrespective of change order.

Lengths and byte arithmetic are checked before payload validation and remapped
metadata allocation, including on 386. Each new scene is validated once through
`scene.Validate`; failure returns its error with fragment context. Invalid keys,
duplicate changes and invalid snapshot selections use `ErrInput`; exhausted limits
use `ErrLimit`. No error publishes partial state or advances IDs. Deep validation
work is bounded by incoming resource/byte limits and belongs on a preparation
worker. Existing scenes are not rescanned on updates to other fragments.

The byte limit counts logical payload sizes, even when input fragments physically
share a slice. It excludes metadata and caller-held old snapshots. The old store
and an incoming batch can each occupy the limit during a transaction. Scheduling,
concurrent input production and the number/lifetime of published snapshots remain
caller-owned memory budgets.

## Snapshots and ownership

`CopyBytes(key, scene)` reports the logical metadata storage created by Apply for
one validated fragment: exact-length scene metadata slices, identity-map key/value
entries, fragment metadata and the copied key. It excludes borrowed vertex/index/
pixel payload and runtime map/allocator overhead. Higher-level producers can charge
their input payload once plus these copies, rather than pretending Store duplicates
the payload. Snapshot and native residency charges still have separate lifetimes.

The store owns copied keys and mesh/texture/draw metadata. It borrows immutable
vertex, index and RGBA buffers. Keep these buffers immutable from the beginning of
`Apply` until both the store and **all** referencing snapshots are gone. Inputs and
selection ranges must remain unchanged during each synchronous call.

`Snapshot` validates selection and copies only metadata, without scanning/copying
payloads or coalescing/reordering draws. It includes every resource of each selected
fragment once, in first-selection order, even when selected ranges use only some
resources. At most 65,536 ranges and output draws are allowed (or the lower configured
draw limit). Invalid selection returns nil output. Empty selection yields an empty
scene, which can be used to clear a backend.

Published snapshots are immutable, independent metadata. They remain valid after
replacement/removal and can be read on another goroutine while the single owner
updates the store. `Store` itself must not be copied or accessed concurrently.
Transform storage, slot existence and finite frame matrices remain the caller's
responsibility. Retain the same snapshot for camera-only frames to avoid needless
backend residency reconciliation.

## Verification

```sh
WHEREAMI_VECTOR_TILE_FIXTURE=/path/to/pinned.pbf \
WHEREAMI_VECTOR_GLYPH_FIXTURE_DIR=/path/to/glyphs \
CGO_ENABLED=0 go test -cover ./pkg/vecmap/retained

CGO_ENABLED=0 GOARCH=386 go test ./pkg/vecmap/retained
GOAMD64=v1 go test -race ./pkg/vecmap/retained
```

Coverage is **100%**. Tests cover identity/revision lifecycle, input and snapshot
metadata isolation, borrowed buffers, old snapshot lifetime, atomic failures,
full-capacity cover replacement, removal/re-addition, exhaustion, all limits,
ordered ranges, repeated instances and independent snapshot readers. Optional
pinned-fixture tests interleave 90 draw records from two independently keyed copies
of the 45-draw scene and check geometry/material/clip preservation and isolated
replacement revisions. This is composition correctness, not cross-tile placement
or renderer integration evidence.

The metadata-only snapshot benchmark uses two fragments with 350,000 vertices
each (physically shared input), three selected ranges and three output draws. On
the recorded v4/GOMAXPROCS16 run: **316.2 / 299.4 / 288.7 ns/op**, **768 B/op**,
**6 allocs/op**. It excludes preparation, deep validation and GPU work; it is not
a frame-performance or MapLibre comparison. See kata **0ex5** and
[`docs/vecmap-rhi.md`](../../../docs/vecmap-rhi.md).

## Acknowledged upload planning

`Planner` consumes immutable snapshots from **one Store namespace**. It is a
single-owner state machine with no native handles. A typical owner runs off the
render thread, sends batches to the backend, then processes acknowledgement
messages. Do not call its methods concurrently, including `Current`.

```go
planner, err := retained.NewPlanner(retained.ResidencyLimits{})
// Handle err.
err = planner.SetTarget(snapshot)
// Handle err. Current() still returns the last fully ready scene.
batch, err := planner.Next(retained.Budget{Bytes: 16 << 20, Resources: 8})
// Handle err; nil batch means no queued work. Execute on the native owner.
// After the entire batch is ready (or failed and rolled back):
err = planner.Acknowledge(batch.Ticket, success)
// Handle err. Publish planner.Current() if it changed.
```

### Admission and publication

- `SetTarget` bounds and validates the scene on the calling preparation worker.
  Each target obeys Store scene ceilings; resource revisions must be nonzero.
  The pointer and payload remain immutable borrows. Reusing a version with changed
  content is forbidden: Store guarantees this identity contract. Validation does
  not hash/compare payloads against earlier versions.
- Active plus desired **unique kind/ID/revision versions** must fit the configured
  residency ceilings before the target changes. Defaults/maxima are **1 GiB of
  logical payload and 16,384 resources**, sufficient for two maximum-sized Store
  snapshots. Positive values can lower limits. These are payload budgets, excluding
  driver alignment, native object/binding overhead, transfer staging and caller-held
  CPU snapshots. A replacement that cannot coexist with the active scene rejects
  with `ErrLimit`, retaining the prior target/current scene rather than stalling.
- `Current()` changes only when every target version is acknowledged resident.
  Until then it returns the previous ready scene (nil before initial readiness).
  A draw/instance-only change whose versions are already ready publishes immediately.
  An empty scene also publishes immediately. `SetTarget(nil)` clears the active
  scene; acknowledged release batches then reclaim old allocations.
- Replacement revisions must coexist with the old revision of the same resource
  ID. Backend maps must key by **kind, ID and revision**, not just ID. QRhi now has
  separate revision-keyed mesh/texture caches and transactional allocation staging.
  Its opt-in `BatchRenderer` explicitly submits uploads with a checked in-frame
  finish, then acknowledges across the frame boundary. Retirement/rollback drains
  remain separate; Qt's afterFrameEnd signal alone does not prove submission success.
  The fixture viewer
  uses the Worker transport described below. See the
  [native adapter contract](../../../internal/vecmaprhi/README.md).
- Target supersession is accepted between batches. Successfully uploaded resources
  of an abandoned target are retired before new uploads, unless needed by the
  active/new target. This maintains the residency bound even when stale partial
  targets temporarily occupy the admission window.

### Batches and acknowledgements

`Next` allows one outstanding batch. Batches contain **either uploads or releases**.
Releases take priority and follow successful admission order. Uploads follow target
mesh then texture slice order. Every batch has a fresh, nonzero ticket; ticket
exhaustion returns `ErrLimit` without issuing work. `SetTarget` and another `Next`
return `ErrBusy` while acknowledgement is pending.

`Budget.Bytes` and `Budget.Resources` must be positive and within residency limits.
Upload bytes and total resource operations are bounded per call. Mesh vertex plus
index buffers form one indivisible unit, as does each texture. An oversized missing
resource returns **ErrBudget**; it is not skipped or silently admitted over budget.
Raise the budget explicitly or prepare smaller resource units upstream. This
checkpoint does not split large uploads into chunks or enforce elapsed-time limits.

`Acknowledge(ticket, success)` affects only the matching outstanding batch. Stale
and duplicate tickets return `ErrTicket` and leave state intact. Public batch
descriptor slices are separate from the private acknowledgement record; geometry
and pixels remain immutable borrows. After failure, `Next` retries with a fresh
ticket, and pending targets remain unpublished.

The adapter's all-or-nothing batch obligations are concrete:

- Successful upload acknowledgement means **every** staged resource is usable by
  the next frame, not merely that a CPU request was queued.
- A failed upload batch must discard all allocations created by that batch before
  acknowledging failure. Earlier successful batches remain resident.
- Successful release acknowledgement means all listed allocations have completed
  their native lifetime obligations. Failed release acknowledgement means none were
  released. Residency is not reclaimed optimistically before success.
- Release batches exclude active and desired versions. Before executing retirement,
  the adapter must consume the newly published current scene and protect any earlier
  in-flight frames with its native deferred destruction/fence rules. CPU publication
  alone does not prove that the GPU has stopped using an old resource.
- An initially empty backend namespace is required. Backend loss/reset requires
  disposing/resetting native state and creating a new Planner; do not carry CPU
  ready bits into an empty device. The implicit white texture (ID zero), uniforms,
  bindings and other backend-owned objects are outside this planner.

Per-call planning scans only bounded resource metadata, not vertex/pixel payloads.
Repeated small batches may rescan target resources; this is not a priority queue
or a measured render-thread time budget. Live tile loading, frame transforms,
selection/placement and native integration remain caller/backend work.

Upload planning (kata **09np**) keeps package coverage at **100%**. Tests cover
readiness, failed-batch retry, stale tickets, descriptor isolation, retirement,
supersession, draw-only/empty publication, byte/count admission, oversized units,
ticket exhaustion and pinned two-fragment replacement under a 12 MiB/two-resource
batch budget. A fake-backend state-machine fuzz run exercised **431,742 executions**
in 20 seconds without failure, checking current-scene readiness, release safety and
peak logical residency. Headless 386, v1 race and full module checks pass. This is
CPU planning evidence, not GPU latency, presentation or MapLibre parity evidence.

## Asynchronous worker and native generations

`NewWorker(limits, budget)` starts one Go goroutine that exclusively owns a Planner.
All target validation and planning run there. Construct workers with NewWorker;
the zero value is invalid and workers must not be copied. Methods are concurrent-safe.

```go
worker, err := retained.NewWorker(retained.ResidencyLimits{}, retained.Budget{
    Bytes: 32 << 20, Resources: 2,
})
// Handle err; invalid limits/budgets reject before starting the goroutine.
generation, err := worker.Restart(snapshot)
// Handle err. The matching native namespace must initially be empty.
// Later producer updates coalesce to the newest immutable snapshot:
accepted := worker.SetTarget(generation, replacement)
// False means closed or a stale/invalid generation; no target was accepted.
```

The transport has one latest-target slot, one output slot and one acknowledgement
slot. During outstanding work, same-generation targets remain in the coalescing slot;
only the newest is validated after acknowledgement. A short mutex protects input
metadata, never validation or native work. `Next()` polls without waiting. A `Packet`
contains a generation, sequence, Current scene, optional Batch and optional Err.

- Consume `Packet.Current` before executing its batch. Releases must use the new
  Current selection, with that scene's own transform-slot mapping.
- After the whole native batch completes, call
  `Acknowledge(packet.Generation, packet.Sequence, success)`. This never waits; false
  means the mailbox is full or closed. Retry or reset rather than dropping a required
  acknowledgement. Native callbacks should be the only result producer.
- Even a packet **without a batch** must be acknowledged after Current is consumed.
  This gives draw-only/empty publication the same ordering and backpressure as
  resource work. If publication cannot be consumed, reset the namespace.
- Target/budget errors arrive in `Packet.Err`, retaining the previously active scene.
  Acknowledge that error packet; planning then sleeps until another target or reset.
  Oversized units do not cause a busy loop or silently enlarge the budget.
- Failed native uploads retry with fresh sequences/tickets after all failed-batch
  allocations are discarded. Successful earlier batches remain resident.
- The worker privately retains the Planner ticket; public descriptors cannot change
  which pending batch is acknowledged. All payloads remain immutable borrows.

`Restart` is for native namespace recreation, **not target supersession**. Dispose of
the old native namespace first, then use its returned generation to reject old packets
and callbacks. The worker replaces its Planner, drops queued old-generation messages
and ignores stale acknowledgements. It never starts another goroutine. Queued resets
coalesce; a bounded validation already in progress completes before the reset is
handled. Generation exhaustion rejects; sequence exhaustion stops the worker rather
than reusing a token. The native adapter must still satisfy old in-flight lifetime
obligations before allocating against the new residency budget.

`Close()` cancels work without waiting for native acknowledgement. Wait on `Done()`
outside GUI/render callbacks to join the worker and drop queued borrows. Native-held
CPU buffers still have their independent immutable lifetime. Queue bounds count
messages and references, not total caller-owned snapshot bytes or driver overhead.
Bound concurrent scene preparation and snapshots separately.

The viewer uses generation checks, initial native completion drains on recreation,
and its existing 8 ms GUI timer for frame pumping. Camera changes coalesce while a
native batch is busy. Live document replacement uses the associated-data transport
below; tile cover and placement selection remain producer work.

Worker verification (kata **qfkf**): headless transition, rollback/retry, supersession,
reset, stale sequence/generation, mailbox bounds, errors, concurrent producers and
shutdown tests pass, including 386 and v1 race. Retained package coverage is **99.7%**;
the state-level exhaustion guard is covered, while the worker-loop return forwarding
that guard is not driven through 2^64 actual packets. Store and Planner coverage
remain 100%. Qt integration tests exercise basic/threaded Vulkan and OpenGL paths.

### Scene-associated producer data

`NewWorkerWithData[T]` pairs each target with immutable producer data. This lets a
consumer retain the right transform-slot mapping while a replacement uploads,
without keeping a lookup table of every historical scene pointer. The ordinary
`Worker` and `Packet` types are aliases with empty data; their API is unchanged.

```go
worker, err := retained.NewWorkerWithData[*scene.Document](limits, budget)
// Validate and freeze the document on the producer before handing it off.
generation, err := worker.RestartWithData(&document.Scene, document)
accepted := worker.SetTargetWithData(generation, &replacement.Scene, replacement)
packet, available := worker.Next()
// packet.CurrentData belongs to packet.Current, not necessarily replacement.
// Reproject packet.CurrentData with the latest camera before consuming Batch.
```

- `PacketWithData[T].CurrentData` switches with Current. Partial uploads, failed
  batches and rejected targets keep the old association. Initial/reset state and
  explicit nil targets have zero data, even if the caller supplies data for nil.
- Promotion is captured during acknowledgement, before applying a coalesced target.
  This matters when the final upload of B completes while C is already queued:
  B's mapping must survive even if no standalone B publication packet was sent.
- A new data value for the same scene still produces an acknowledged publication
  packet. Resident versions don't upload again. Camera-only changes should bypass
  target planning and update frame transforms on the consumer.
- The worker doesn't inspect, validate or copy `T`'s referenced storage. Producers
  own metadata validation, including slot bounds and finite transforms. Both scene
  and data remain immutable through every worker, packet and native borrow.
- Data follows the bounded current/accepted/latest-target and packet lifetimes.
  Superseded partial-target data is dropped; there is no historical association
  map. Byte limits still cover resource payloads, not arbitrary producer data or
  caller-retained snapshots. Bound those separately.
- `Restart` and `SetTarget` on a data-bearing worker supply zero data. Consumers
  needing associations should consistently use the `WithData` methods.

Checkpoint **w8sf** verifies the final-ack/coalesced-target race, retry, rejection,
same-scene data publication, clear/reset and weak-reference collection of abandoned
mapping data. Headless 386 and v1 race tests pass. Package coverage remains **99.7%**;
the new data association helpers have **100%** coverage.

### Opt-in settlement publication

`NewSettlingWorkerWithData[T]` adds a final nil-batch publication when all obsolete
resource retirement has been acknowledged. Its `Packet.Settled` is true only when
Next has no batch/error: accepted target is Current and no known retirement remains.
That packet still requires consumption and acknowledgement. Ordinary Worker
constructors retain their previous packet cadence; consumers may ignore the field.

Settlement is a CPU Planner ownership boundary, not a new GPU-completion mechanism.
The native acknowledgements must already satisfy the upload/retirement contract.
After a namespace reset, an empty target can settle immediately in the Planner;
the adapter must still finish old-namespace lifetime obligations before old CPU
leases are reclaimed. The QRhi viewer waits for BatchRenderer.Initialized even for
nil-batch packets, preserving both checked startup drains.

The headless producer Bridge uses this boundary with **serial accepted targets**:
new CPU targets coalesce outside the Worker until the current accepted target
settles. It can then drop old leases without an unbounded scene history or guessing
which target won a final-ack race. A generic superseding caller cannot infer that
all arbitrary old external references are gone merely from a Settled flag. See
[`producer`](../producer/README.md#serial-target-viewer-bridge) for ownership and
the latency tradeoff. Failed retirement never produces settlement; tests cover
retry and resetting with the final settlement packet still outstanding.
