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

`Targets == nil` uses `view.VisibleTileCover`; an explicit empty slice clears the
cover. Explicit covers have at most 64 unique canonical tiles at one source zoom
through 14. Submit copies that small slice. Camera updates coalesce in one slot.

## Scheduling, refresh and failure

- Load immediate parents before detailed targets, preserving nearest-first order.
  Concurrency prioritizes dispatch, not completion order. `Set.Select` supplies the
  existing requested-sibling refinement and ancestor/descendant continuity policy.
- Cancel obsolete loads and loads from an obsolete style/source snapshot. Keep
  useful loads across overlapping cover updates or asset refresh. Their original
  registered job token remains valid; it is not silently retagged.
- Canceled jobs retain their worker/raw reservation until their results return.
  Even a loader ignoring cancellation cannot cause replacement goroutines to be
  spawned. Late canceled results are discarded before decoding.
- Preparation concurrency is **one**. A synchronous Prepare/Build finishes before
  its owner handles the next request. Compatible camera updates do not discard
  useful compilation; changed source/style/assets or obsolete tiles do. No decoding,
  tessellation, shaping or composition runs in Submit, Next or native callbacks.
- Raw MVT remains cached alongside reusable Prepared data. A style change reparses
  and recompiles cached bytes; an asset change calls Build without Prepare. A source
  change reloads bytes. Old immutable fragments survive through their leases.
- A published cover must have a coherent source/style/asset epoch. While a refresh
  is incomplete, the native consumer keeps its acknowledged Current. It must not
  replace Current with a partially refreshed cover or pair it with target slots.
- Drop satisfied fallback parents from desired work, but retain fragments pinned by
  Current or any outstanding lease. Only explicit native Current is continuity;
  publishing or queueing a CPU snapshot does not advance it.
- Transient transport errors get at most three attempts, separated by RetryDelay
  (default 250 ms). One ticker handles all retries. Missing/permanent responses,
  writer overflow, decode/build failures and admission errors are not retried in
  that workload. A changed cover/style/assets request resets failure state;
  camera-only updates within the same cover do not.
- Missing is not blank: `ErrMissing` retains fallback/continuity; a successfully
  decoded valid empty tile is ready coverage. A zero-byte response is malformed
  under the existing MVT decoder. MVT feature degradation and text readiness keep
  the shared compiler's behavior. Dynamic missing-asset demand reporting is later.

`Status.LastError` is the most recent bounded error text, not an event log or a
readiness flag. It remains latched after recovery. Cache/count failures are explicit;
the producer does not enlarge limits or evict pinned continuity to admit new work.
If an admission failed after work started, another workload request is needed to
retry it. Work waiting for cache capacity can resume when leases/Current release it.

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
   decoded string backing), Prepared storage, and twice Fragment storage (including
   Set metadata copies). Complete style/asset profiles referenced by cache entries
   are charged once per distinct pointer, including old epochs. This accounts for
   hidden backing behind borrowed strings or sprite subimages without duplicating
   every string/pixel buffer.
3. **Publication:** each lease charges the snapshot independently, plus each distinct
   backing asset profile in its cover. Shared geometry/pixels are deliberately
   charged again. Holding old leases therefore cannot escape the snapshot budget.
   One additional logical snapshot slot is reserved for Set's internal selection
   cache. `SelectBounded` rejects oversized logical snapshots before caching them;
   producer publication also checks borrowed asset backing.
4. **Request handoff:** reserve three ProfileBytes allowances for owner, latest and
   one in-transfer profile pair. Jobs retain a copied source name, not old profiles.
5. **Temporary work:** reserve one compiler/compositor working set and one
   compaction buffer of at most RawBytes in addition to
   these retained budgets. MVT decode, triangulation, candidates, glyph layout/atlas,
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
Old leases retain their original asset charge when a smaller replacement profile
arrives. Current lease counts remain accurate when callers release after Close.

`CacheUsage` splits raw/backing, prepared geometry, fragments/Set copies and unique
profiles. `PeakCache` records the breakdown at the largest total cache charge; its
fields are not independent maxima. PhaseTime reports count, total and maximum wall
time for completed load, Prepare, Build and Select work. Loading overlaps across
workers, so its sum is not elapsed time or CPU time. Failed/discarded compilation
attempts are included. ResponseBytes and RawCapacityBytes describe completed raw
results before admission. LastErrorStage identifies the most recent failing stage.
Requested/SelectedTiles/Fallbacks describe the latest CPU target, not native Current.

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
Likewise, observing a newer Current does not prove old Planner/native retirement
finished. The Bridge described below tracks these bounded owners through coalescing,
release packets and reset before calling Release. Copying a pointer does not acquire
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

## Serial-target viewer bridge

`NewBridge` takes a Producer with at least four lease slots, an empty geographic
bootstrap document, residency limits and an upload budget. It creates an opt-in
settling Worker and one background mailbox/document owner. The caller supplies
camera requests independently through Producer.Submit. Documents alias the leased
Scene/TileSpaces; draw counting and document preparation stay off GUI/render threads.

The renderer uses `Bridge.Worker()` and samples `Bridge.Target()`. It must report
Worker restarts with `Restarted`, consumed Current packets with `Consumed`, and
successfully enqueued native acknowledgements with `Completed`. These methods do
bounded metadata work. The viewer follows this protocol on its render owner.

Accepted targets are **serialized through retirement**. Newer CPU targets coalesce
into one pending slot while an accepted target uploads. At most Current, accepted
target, and pending document are held by the bridge, plus the producer output or
in-transfer lease. Same-snapshot camera publications update request progress without
creating another Worker target. This is a conservative latency tradeoff: obsolete
accepted uploads finish before the newest pending target starts.

`NewSettlingWorkerWithData` publishes an acknowledged nil-batch `Settled` packet
after all old resource releases have been acknowledged. Only settlement for the
serial target allows Bridge to release older leases. The default Worker's packet
cadence is unchanged. Settlement is a Planner ownership boundary, **not a GPU fence**.
Even an empty target must wait for `BatchRenderer.Initialized()` after reset, proving
both existing startup drains before Bridge releases prior-namespace leases.

The consumer must drop old packet/document borrows at these protocol boundaries.
The viewer releases on nil-packet consumption in updatePaintNode, while Qt blocks
the GUI thread, after selecting the new Current. It joins Bridge only after native
window/item use has stopped. Bridge.Close stops the mailbox owner, joins its Worker
and Producer, then releases remaining leases. It is not a native destruction API.

`Bridge.Ready(revision)` also accounts for in-transfer/pending documents. For a final
capture, require producer Pending/Failed to be zero and the render consumer to have
selected the bridge target. Timed live traces freeze their camera and allow a bounded
settlement tail; a timeout reports an error and never acknowledges GPU work.

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

The next gate is **7vqy**: inspect distinct payload ownership, discardable prepared
caches and refinement headroom under the existing limits. The moving trace also
exposes repeated Prepare/Build work at style-zoom boundaries. See
[`docs/vecmap-rhi.md`](../../../docs/vecmap-rhi.md) for full commands, corpus paths,
observed costs and the failed-settlement results. Dynamic glyph demand follows that
admission gate rather than adding more retained assets to an already full cache.
