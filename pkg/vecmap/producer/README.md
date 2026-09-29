# producer

Headless live tile production over `tiles.Prepare`, `Prepared.Build` and `Set`.
One owner goroutine handles preparation, cache admission, coverage and placement.
Between one and four fixed workers perform transport only. The package builds
without Qt, cgo, or the Qt-bound parent `pkg/vecmap`.

The package supplies injected loaders, an HTTP/cache adapter and already-loaded
immutable style/font/sprite snapshots. The opt-in QRhi viewer now uses its Bridge
for live targets. Dynamic asset fetching remains separate. Checked GPU submission
and retirement remain mandatory.

## Input boundary

```go
p, err := producer.New(load, producer.DefaultLimits())
// Handle err. Load writes one bounded response; it does not decode or compile.
revision, err := p.Submit(producer.Request{
    Camera: camera,
    Style: styleSnapshot,
    Assets: assetSnapshot,
})
// Handle err. Revision identifies the coalesced request, not native readiness.
```

`Loader(context.Context, Key, io.Writer) error` receives source, canonical tile,
style epoch and producer job generation. The writer owns a fixed-capacity buffer
and latches overflow even if the loader ignores its write error. The loader must
bound its own transport buffers and error evaluation, obey cancellation and return
without retaining the writer. Pending results retain at most 512 copied error
bytes, rather than arbitrary loader error objects. HTTP status, redirects, timeout,
disk cache and checksum policy belong to the transport adapter.

The existing parent implementations were inspected before defining this boundary:
tile downloads/cache reads enforce 2 MiB, HTTP uses context cancellation and a
15-second timeout, cached tiles use checksum pairs and disk eviction, and glyph
ranges use immutable merged maps with a separate retry policy. Those transport and
disk-cache mechanics now live in [`tileio`](../tileio/README.md), shared with the
parent. `HTTPLoader` uses them with immutable XYZ source templates. This package
reuses the shared decoder/compiler and extracts `view.LoadOrder` from the scheduler,
which also delegates to it. It introduces no second parser, shaper or cover policy.

`Style` and `Assets` are immutable application-owned snapshots. Supply a new pointer
and epoch for changed content, including evaluated style zoom, topology and limits.
The source string identifies an immutable source version; change it when the same
tile address can return different content. `Style.Options.Tile` is set per job.
Fonts may contain merged decoded glyph ranges. Sprite callbacks are synchronous,
bounded and I/O-free. Liberty's existing sprite lookup/cache can be supplied here.

Each profile declares `Bytes`: an upper bound on **all reachable storage**, including
callback captures and the maximum sprite-cache storage they can retain. This is a
trusted caller contract, like compiled styles themselves; a closure's memory cannot
be inferred automatically. It must remain valid as a cache fills. Underdeclaring a
profile violates the budget contract. No new untrusted style/asset ingestion API is
introduced.

`Targets == nil` uses `view.VisibleTileCoverAt` with `Style.Options.Coarser`
(zero by default, at most `view.MaxCoarser`); an explicit empty slice clears the
cover. Explicit targets must lie `Style.Options.Coarser` zoom levels below the
camera zoom, because baked widths and symbol spacing are converted at that scale. Explicit covers have at most 64 unique canonical tiles at one source zoom
through 14. Submit copies that small slice. Camera updates coalesce in one slot.

## Scheduling, refresh and failure

- Load immediate parents before detailed targets, preserving nearest-first order.
  Concurrency prioritizes dispatch, not completion order. `Set.Select` supplies the
  existing requested-sibling refinement and ancestor/descendant continuity policy.
- Cancel obsolete loads and loads from an obsolete source. Raw data belongs to the
  immutable source, so keep useful loads across style, cover and asset updates. Their original
  registered job token remains valid; it is not silently retagged.
- Canceled jobs retain their worker/raw reservation until their results return.
  Even a loader ignoring cancellation cannot cause replacement goroutines to be
  spawned. Late canceled results are discarded before decoding.
- Preparation concurrency is **one**. Observe newer requests between synchronous
  Prepare and Build, then again before installation. Obsolete preparation skips
  packing; a compatible asset refresh uses that preparation with the newest assets.
  Compatible camera updates don't discard useful compilation. Changes during Build
  still reject obsolete source/style/assets or tiles before installation. No decoding,
  tessellation, shaping or composition runs in Submit, Next or native callbacks.
- Raw MVT remains cached alongside optional reusable Prepared data. A style change
  normally reparses/recompiles cached bytes; asset refresh reuses Prepared when available.
  Admission pressure evicts least-recently-built Prepared objects, or omits incoming
  preparation after packing. If the fixed raw/fragment/profile charge still cannot
  fit, cached tiles that are no longer requested are dropped from the cache and Set,
  lease-only pins before acknowledged Current continuity and oldest builds first.
  Their leases keep the snapshot payload, so Current still renders; only future
  selections lose that fallback (`ContinuityEvictions`). Desired tiles are never
  evicted for admission. A later asset refresh
  reparses retained raw bytes if needed; camera-only updates do not rebuild geometry.
  A source change reloads bytes. Old immutable fragments survive through their leases.
- A published cover must have a coherent source/style/asset epoch. While a refresh
  is incomplete, the native consumer keeps its acknowledged Current. It must not
  replace Current with a partially refreshed cover or pair it with target slots.
- Camera and targets follow the latest request immediately. Style/asset pairs are
  adopted at bounded boundaries: a working epoch with installed fragments is held
  until one coherent publication has been attempted, its remaining desired work
  can no longer arrive, or two covers' worth of Prepare attempts have been spent.
  Continuous sixteenth-zoom changes therefore publish coherent intermediate covers,
  each evaluated at one exact style zoom that may trail the camera by that bound,
  instead of publishing nothing until motion ends. Without installed progress the
  newest pair is adopted at once; coalesced intermediate pairs are skipped. A held
  pair counts as Pending and sets `StyleHeld`, so settlement still requires the
  newest paint. `StyleAdoptions`/`HeldStyles` count switches and deferred pairs.
- Refresh the currently selectable cover before unfinished refinements, preserving
  LoadOrder within each group. Once requested siblings have installed fragments,
  Set selects them even during epoch refresh. Don't rebuild their hidden parent;
  a fresh parent cannot bypass those siblings' coherence check. Drop these
  satisfied fallback parents from desired work, but retain fragments pinned by
  Current or any outstanding lease unless admission of desired work requires the
  space. Only explicit native Current is continuity;
  publishing or queueing a CPU snapshot does not advance it.
- Transient transport errors get at most three attempts, separated by RetryDelay
  (default 250 ms). One ticker handles all retries. Missing/permanent responses,
  writer overflow and decode/build failures are not retried in that workload.
  A changed cover/style/assets request resets failure state;
  camera-only updates within the same cover do not.
- Missing is not blank: `ErrMissing` retains fallback/continuity; a successfully
  decoded valid empty tile is ready coverage. A zero-byte response is malformed
  under the existing MVT decoder. MVT feature degradation and text readiness keep
  the shared compiler's behavior. Dynamic missing-asset demand reporting is later.

If a tile wasn't rebuilt during an intervening style request, returning to its exact
preparation inputs reuses the installed immutable fragment and optional Prepared.
The producer requires the same source, complete PrepareOptions, and identical
immutable compiled-layer slice storage/length. New layer storage is conservatively
treated as a new style even when its contents look equal. Epochs order requests;
they aren't shader/compiler inputs. This doesn't approximate the sixteenth-zoom
policy or retain multiple style variants. The original style profile stays charged
while its strings are borrowed. Assets remain an independent rebuild boundary.

Native sequence advancement with unchanged Current coverage updates progress but
doesn't dirty placement. Changed coverage, camera requests, builds and evictions
still do. Before placement/composition, the owner runs the shared coverage selector
and checks the selected fragments' epochs. A mixed-epoch cover waits for real work
to finish rather than composing a snapshot that would be rejected. Readiness still
includes old installed fragments: filtering them out would incorrectly manufacture
a smaller coherent target and could replace detailed continuity with a coarse parent.

`Status.LastError` is the most recent bounded error text, not an event log or a
readiness flag. It remains latched after recovery. Cache/count failures are explicit;
the producer does not enlarge limits or evict pinned continuity to admit new work.
Cache-capacity rejection records a required headroom threshold. It retries only
after enough raw/fragment/profile storage has actually left the cache, normally
after acknowledged Current/lease retirement. Prepared storage was already excluded
from the failed minimum charge: reclaiming it alone cannot trigger a retry. There
is no polling retry loop, enlarged limit or optimistic retirement credit. Inputs
that cannot fit remain explicit failures. LastError stays latched after recovery;
use Pending/Failed and bridge readiness to judge current progress.

## Storage ledger

Limits are explicit positive values; use DefaultLimits as a starting policy:

| Budget | Default | Maximum |
| --- | ---: | ---: |
| Transport workers / pending raw result slots | 4 | 4 |
| Raw response capacity per slot | 2 MiB | 2 MiB |
| Cached tile entries | 128 | 128 |
| Cache charge | 256 MiB | 1 GiB |
| Output leases, including unconsumed output | 4 | 8 |
| Charge per output lease | 128 MiB | 1 GiB |
| One style + asset profile pair | 64 MiB | 1 GiB |

The ledger covers separate lifetimes:

1. **Transport:** `Workers * RawBytes` reserves running and queued-result buffers.
   Canceled calls retain their slots. There is no buffered job queue.
2. **Cache:** successful responses are compacted to exact-length owned buffers
   before cache admission. Full-capacity responses transfer without copying. Each
   tile charges twice its compact raw capacity (response plus possible
   decoded string backing), retained Prepared storage, Fragment storage and Set's
   copied metadata. `Fragment.SetCopyBytes`/`retained.CopyBytes` match exact-length
   metadata copies and logical map entries; borrowed payload is not charged twice
   within the cache. Complete referenced style profiles are charged once per distinct
   pointer, including old epochs, to cover candidate string backing.
3. **Publication:** each lease charges its snapshot independently. The producer uses
   `BuildOwned`: sprite pixels are copied into compact owned buffers after a bounded
   aggregate RGBA preflight; glyph atlas RGBA was already owned and is not recopied.
   Thus fragments and snapshots retain no asset maps, callbacks or hidden image
   backing. Shared geometry/pixels are still charged independently for each lease.
   Holding old leases therefore cannot escape the snapshot budget.
   One additional logical snapshot slot is reserved for Set's internal selection
   cache. `SelectBounded` rejects oversized logical snapshots before caching them.
4. **Request handoff:** reserve three ProfileBytes allowances for owner, latest and
   one in-transfer profile pair. Jobs retain a copied source name, not old profiles.
5. **Temporary work:** reserve one compiler/compositor working set and one
   compaction buffer of at most RawBytes in addition to
   these retained budgets. Owned sprite copies have an aggregate CacheBytes ceiling
   for the one in-progress Build; source assets stay charged to input profiles.
   MVT decode, triangulation, candidates, glyph layout/atlas,
   packing, projection and collision retain their existing byte/count/work ceilings.
   Lower PrepareOptions and Store limits for the application's scratch policy.
   Admission occurs after bounded preparation; CacheBytes is not a peak heap cap.

`RetainedBytes` counts backing-array capacities, value metadata and string lengths.
Map/allocator overhead, goroutine stacks, loader-private buffers and native driver
storage are outside those logical charges. Metadata cardinalities and worker counts
are separately bounded. These values are neither RSS nor VRAM measurements. Native
Store/Planner admission and indivisible upload budgets still apply independently.

Status exposes current and peak job/cache/lease counts and logical byte charges,
plus load/prepare/build/rejected-result counters. Pending includes unfinished work
and mailboxes; Failed counts terminal failures still relevant to desired coverage.
Old leases retain their own pixel storage and charge through asset refresh. Current
lease counts remain accurate when callers release after Close. PreparationEvictions
and PreparationBytesFreed count cached preparation reclaimed under pressure;
UncachedPreparations counts incoming objects omitted after Build. CapacityRetries
counts admission retries triggered by sufficient actual cache headroom.

`CacheUsage` splits raw/backing, prepared geometry, fragments/Set copies and unique
profiles. `PeakCache` records the breakdown at the largest total cache charge; its
fields are not independent maxima. PhaseTime reports count, total and maximum wall
time for completed load, Prepare, Build and Select work. Loading overlaps across
workers, so its sum is not elapsed time or CPU time. Failed/discarded compilation
attempts are included. ResponseBytes and RawCapacityBytes describe completed raw
results before admission. LastErrorStage identifies the most recent failing stage.
Requested/SelectedTiles/Fallbacks describe the latest CPU target, not native Current.

`ReusedVersions` forwards the Set's count of replaced resources that kept their
resident revision because their payload was byte-identical, typically glyph atlas
and sprite textures across sixteenth-zoom recompiles. Fewer versions change per
epoch, so each intermediate cover uploads and later retires fewer resources.

`LoadStyleReuses` counts useful in-flight jobs retained at style-change boundaries
(one job can count more than once). `StyleReuses` counts installed fragments reused
under identical immutable preparation inputs. `SkippedBuilds` counts obsolete
pre-pack attempts. `DeferredSelections` counts mixed covers rejected before placement;
`UnchangedCurrent` counts native mailbox updates that needed no continuity selection.
These counters describe avoided work, not wall-time savings.

Raw-cache admission occurs after receiving the bounded response, when its compact
size is known. Cache failure preserves the previous entry, including its raw and
prepared data, atomically. The transport slot remains separately bounded; a small
cache is no longer required to reserve a maximum-size response for every tiny tile.
Compaction does not change rendering limits, prepared geometry or lease accounting.

## Leases and native acknowledgement

`Next` transfers the latest immutable `*Lease`. Unconsumed output may be coalesced;
consumed output is never reclaimed automatically. When all lease slots are held,
publication pauses while requests continue coalescing and bounded production runs.

Use `retained.WorkerWithData[*producer.Lease]` so Current carries its own snapshot:

```go
lease, available := p.Next()
if available {
    accepted := worker.SetTargetWithData(nativeGeneration, lease.Snapshot.Scene, lease)
    // If not accepted, release the lease after dropping local borrows.
    // If accepted, retain it until ALL Worker/packet/native borrows have ended.
    _ = accepted
}

// On consuming a Worker packet, before its batch:
if packet.CurrentData != nil {
    frame := packet.CurrentData.Snapshot.Frame(latestCamera)
    // Sync this Current frame with packet.Batch on the native owner.
    _ = frame
}
ok := p.Current(packet.Generation, packet.Sequence, packet.CurrentData)
// Handle false. This reports already-acknowledged Current; it does not acknowledge
// packet.Batch. Worker.Acknowledge still follows checked native completion.
_ = ok
```

Initial startup uses Worker.RestartWithData with an empty native namespace. On a
native reset, dispose of the previous namespace under its completion rules, restart
the Worker, and call `ResetCurrent(newGeneration)`. It reserves sequence zero so the
first real packet remains reportable. Old-generation Current callbacks cannot
restore continuity. Useful producer jobs may continue: their generation is separate.

**Release is an ownership obligation, not GPU acknowledgement.** Keep a lease until
no accepted/latest target, Planner state, packet, adapter or caller still borrows its
snapshot. A newer target being submitted does not prove that an older target was
discarded: it might have become Current during the final-upload acknowledgement.
Likewise, observing a newer Current alone does not prove old CPU borrows have ended.
Planner residency now stores only versions and byte charges after upload acknowledgement;
it doesn't retain retired payloads. The Bridge described below proves the remaining
target, packet and adapter CPU ownership before calling Release. Native allocations
still retire through checked release batches. Copying a pointer does not acquire
another lease. Retaining/using a snapshot after Release violates accounting.

`Current` copies bounded coverage into one mailbox; it neither releases the lease
nor acknowledges the Worker packet. Every Worker packet, including a nil-batch
publication, retains its existing acknowledgement requirement. Camera-only updates
can preserve the same Snapshot pointer and produce no geometry uploads; the adapter
can bypass target submission when its mapping is unchanged.

`Close` cancels immediately. Wait for `Done` outside GUI/render callbacks; it joins
the owner and fixed workers without waiting for native acknowledgement. A loader
that ignores cancellation delays Done until it returns. Caller-held leases remain
valid across Close and still require Release after their external borrows end.

## Verification and next checkpoint

Controlled loaders and a small valid MVT cover parent-first dispatch, partial
refinement, acknowledged zoom-out continuity, pan supersession, useful-job survival,
late ignored-cancel success, retries/missing/malformed/oversized input, style/asset
refresh, coherent epochs, immutable old snapshots, byte/count admission, lease
backpressure, native Current generations and joined shutdown. A real headless Worker
test checks scene-associated transforms and old Current during partial uploads.
The optional pinned Liberty test retains 62 labels and 782,409 selected elements.

Testing exposed coarse-tile wrap issue **p5nh**, now fixed in `view`: wrap zero
chooses the nearest tile center, and narrow antimeridian views retain the adjacent
copy needed by root fallback. The longitude workaround has been removed. Headless
coverage sampling and basic/threaded native pixel tests verify the corrected path.

```sh
WHEREAMI_VECTOR_TILE_FIXTURE=/path/to/pinned.pbf \
WHEREAMI_VECTOR_GLYPH_FIXTURE_DIR=/path/to/glyphs \
CGO_ENABLED=0 go test -cover ./pkg/vecmap/producer ./pkg/vecmap/tiles ./pkg/vecmap/view

CGO_ENABLED=0 GOARCH=386 go test ./pkg/vecmap/producer ./pkg/vecmap/tiles ./pkg/vecmap/view
GOAMD64=v1 go test -race ./pkg/vecmap/producer ./pkg/vecmap/tiles ./pkg/vecmap/view
```

## Bounded superseding viewer bridge

`NewBridge` takes a Producer with at least four lease slots, an empty geographic
bootstrap document, residency limits and an upload budget. It creates an opt-in
settling Worker and one background mailbox/document owner. The caller supplies
camera requests independently through Producer.Submit. Documents alias the leased
Scene/TileSpaces; draw counting and document preparation stay off GUI/render threads.

The renderer uses `Bridge.Worker()` and samples `Bridge.Target()`. It must report
Worker restarts with `Restarted`, consumed Current packets with `Consumed`, and
successfully enqueued native acknowledgements with `Completed`. These methods do
bounded metadata work. The viewer follows this protocol on its render owner.

The bridge exposes **one unconfirmed target at a time**. A successful packet whose
`TargetData` matches that target proves the Worker has processed the handoff. While
only retirement, or nothing, has happened for the exposed target, the bridge may
then expose the latest pending CPU document, **provided that supersession does not
increase the uploads still needed before something becomes Current**. The bridge
mirrors acknowledged residency from successful upload/release batches and compares
how many versions the pending document and the exposed target still lack. A
camera-only document shares the target's fragments, so it replaces the target for
free and keeps placement fresh while the shared uploads keep counting. A new style
epoch's cover needs its own meshes, so it waits until the target it would abandon
is Current; the pending slot still coalesces to the newest document, so that wait is
bounded by one target's remaining uploads. Progress is guaranteed: the exposed target
either finishes or is replaced by a document at least as close to finishing, and a
backend whose uploads are slower than publication never spends frames on a cover
that a cheaper document could replace. A target whose packet reports an error
never pins the bridge; the next pending document replaces it. The first cover must
become Current before any supersession starts; otherwise continuous camera motion
could keep abandoning the initial cover and leave the map blank. Reset restores this
startup rule. Same-snapshot camera publications update request progress without
creating another Worker target.

Ownership at a checked packet boundary is explicit:

1. Planner borrows payloads only from Current, accepted target and the outstanding
   batch. Its resident map retains version/byte metadata, including unreleased native
   capacity, but no geometry or pixel slices.
2. Each packet identifies both `CurrentData` and accepted `TargetData`. Matching the
   bridge's exposed target also proves no earlier bridge target remains in the input
   slot. The bridge doesn't expose another target until this match is acknowledged.
3. The adapter must have selected Current and dropped previous CPU scene/batch borrows
   before `Completed`. Upload/release completion provides that boundary. A nil-batch
   packet waits for `BatchRenderer.FrameSelected()`: Sync alone only replaces the
   frame header, while prepare still holds the old resident scene. This gate includes
   `Initialized()`, preserving both startup drains even for an empty reset target.
4. Only then may the bridge release documents outside Current, accepted target and
   pending. A final-upload ack can make B Current while C is queued; C's packet
   reports B, so B's lease remains held. Failed/stale callbacks grant no release.

The existing four-lease pool bounds all these owners. Normally it holds Current,
target, pending and output/in-transfer work. During handoff, the previous target may
still be held alongside its replacement and Current. Receiving a pending document
can fill the fourth slot, pausing producer publication until the next ownership
boundary. There is no historical target list or extra lease allowance.

`NewSettlingWorkerWithData` still publishes an acknowledged nil-batch `Settled` packet
after retirement. It establishes final readiness, not permission to skip a native
drain. Default Worker cadence is unchanged. Native capacity remains charged until
release acknowledgement even when its original CPU lease is already gone.

The consumer must drop old packet/document borrows at these protocol boundaries.
The viewer keeps GUI target sampling and render-owner callbacks ordered by Qt's
synchronization protocol. It joins Bridge only after native
window/item use has stopped. Bridge.Close stops the mailbox owner, joins its Worker
and Producer, then releases remaining leases. It is not a native destruction API.

`Bridge.Ready(revision)` also accounts for in-transfer/pending documents. For a final
capture, require producer Pending/Failed to be zero and the render consumer to have
selected the bridge target. Timed live traces freeze their camera and allow a bounded
settlement tail; a timeout reports an error and never acknowledges GPU work.

`Bridge.Stats` reports received/coalesced/same-snapshot documents, exposed/superseded
targets, successful upload/release batches and settlements. Overtaken upload bytes
completed while a newer CPU document was pending or exposed; shared versions can
still be useful, so this isn't a wasted-byte counter. FirstVisibleCurrent is elapsed
wall time from bridge construction to consumption of the first drawable Current,
not a presentation timestamp or proof of full coverage.

CurrentChanges counts drawable document switches. FirstCurrentTiles/Labels and
CurrentTiles/Fallbacks describe the first drawable and current selections' coverage
and detail. LongestCurrentHold measures how long a drawable document stays unchanged,
including the ongoing interval at Stats. It isn't content age: a static camera can
legitimately hold a complete scene indefinitely, and camera reprojection still runs.
CurrentTargets counts exposed targets that became Current; with Superseded and
TargetErrors it accounts for every exposed target. LongestCurrentAge and
TotalCurrentAge measure, for documents that became Current, the time from bridge
receipt of their lease to consumption as Current: how far native continuity trails
CPU publication. Divide TotalCurrentAge by CurrentChanges for the mean. None of
these are presentation timestamps.
Reset ends the old interval; these metrics retain no historical documents or leases.

Checkpoint **hxzf** tests local HTTP/MVT arrivals, partial refinement and uploads,
camera-only movement, rotated antimeridian root coverage, upload-time reset and
empty-target reset on basic/threaded Vulkan and OpenGL, including OpenGL 2× race.
The actual live command runs against a controlled tile server with supplied fonts.
Dynamic font/sprite fetching and measured throughput/cancellation refinements remain
next steps. No GPU pacing or MapLibre parity claim is made.

### Real workload findings

Checkpoint **4s9y** adds verified `CacheLoader` replay (`-cache-only` in the viewer),
`-tile-workers` for ordered or concurrent replay, and the metrics above. A captured
Madrid trace contains 71 immutable source tiles. Its static 800×600/z10 cover requests
42 tiles plus 16 coarse parents. The default 256 MiB cache does **not** admit that
complete cover, even after compaction. The same budget now admits more work, but
native batch success must not be mistaken for complete CPU coverage.

Checkpoint **7vqy** resolves that CPU admission gate with the ownership and reclaim
rules above. The same static cover reaches all 42 tiles, and the moved cover reaches
all 20, without changing budgets or labels. A captured-corpus regression also settles
the full first cover before zooming out, covering capacity rejection followed by
retirement-driven retry. The moving trace still exposes repeated Prepare/Build work
and serial native publication latency. See
[`docs/vecmap-rhi.md`](../../../docs/vecmap-rhi.md) for full commands, corpus paths,
observed costs and both earlier failures and current results. **3a38** tracks native
target latency. Its superseding ownership protocol now settles the moving corpus in
three repeated Vulkan and llvmpipe runs at unchanged limits, while first establishing
drawable continuity. See the next section in that document for measurements and
the remaining preparation/publication churn. Dynamic glyph demand stays separate.
