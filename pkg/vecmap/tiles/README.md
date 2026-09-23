# tiles

Toolkit-neutral composition of prepared tile scenes. `Set` combines retained
resource identity, fallback coverage, layer ordering, world instances and
cross-tile symbol placement. It builds without Qt or cgo and adds no dependency.

A preparation worker owns the Set. Tile decoding, style evaluation, glyph/sprite
loading and scene packing happen upstream. The compositor does no I/O and starts
no goroutines. Backend readiness still belongs to `retained.Worker` and its native
adapter.

```go
set, err := tiles.New(retained.Limits{})
// Handle err. prepared is an immutable *tiles.Fragment.
err = set.Apply([]tiles.Change{{Tile: tileID, Fragment: prepared}})

// previous is eligible continuity from the acknowledged Current snapshot.
target, err := set.Select(view.VisibleTileCover(camera), previous, camera, 0)
// Handle err before publication.
accepted := worker.SetTargetWithData(generation, target.Scene, target)
```

Use `retained.NewWorkerWithData[*tiles.Snapshot]` for this handoff. A consumed
packet's `CurrentData.Frame(latestCamera)` pairs Current with its own transform
slots. Return eligible Current coverage to the producer through its mailbox;
don't read or mutate Set from a render callback. Initial worker startup uses
`RestartWithData(target.Scene, target)` and an empty native namespace.

## Prepared fragment contract

Each tile has one `Fragment`:

- `Scene` contains immutable tile-local geometry, textures and draw records.
  Source transform slots are zero. Base draws must have a nonzero clip wholly
  inside the 256-unit tile square. Symbols may extend beyond tile boundaries.
- `Draws` has exactly one metadata entry per scene draw. `Layer` is the original
  style-layer order. `Part` identifies base geometry, an icon, or text.
- Icon/text entries refer to an index in `Symbols`, with matching layer order.
  Text entries cover SDF halos and fills; icon entries use image material.
  **A coalesced draw cannot cross a layer or candidate boundary.** The upstream
  packer must preserve or split those ranges. This metadata cannot be recovered
  reliably from an already flattened fixture.
- Pattern entries retain the **original float64 tile-local period** in
  `PatternPeriod`. Its float32 conversion must match `Material.PatternSize`.
  Composition recomputes phase for each tile/wrap with `view.PatternPhase`, then
  packs it to float32. Reconstructing the period from rounded material values
  would change phase, especially across world copies.
- `Symbols` contains evaluated `placement.Symbol` values and prepared readiness
  and metric snapshots. Text readiness is explicit. Bounds and sprite metrics
  are values, so Apply copies them with the candidate metadata.

Candidates follow compiler/placement's bounded evaluated-input contract. This
isn't another parser for arbitrary style expressions, source features or fonts.
Source strings are immutable borrows. Producers own shaping, native fallback,
asset readiness and consistent style epochs across tile updates.

`Apply` copies scene, draw and symbol metadata. It borrows vertex/index/RGBA
payloads through the lifetime of the Set **and every referencing snapshot**.
Changes are atomic: invalid metadata or a Store failure publishes nothing and
consumes no resource IDs. Surviving local IDs keep their global IDs with a new
revision on replacement. Removal and re-addition get fresh IDs. A nil Fragment
removes a tile; a nonnil empty scene means a successfully prepared blank tile.

## Coverage and native publication

`Select` reuses `view.SelectCover`, extracted from the production scheduler:

1. Fully ready **requested siblings** replace their parent together. A request
   need not include all four children.
2. Complete, equally detailed or finer previous coverage wins over a coarser
   parent. This retains children during zoom-out while a new coarse tile loads.
3. Otherwise prefer a ready immediate parent, then complete previous coverage.
4. If neither is available, use ready targets and partial previous descendants
   rather than leaving the whole region blank.

Output canonical tiles never overlap. Grouping preserves first-seen sibling-group
order and target order within each group. Continuity chooses the nearest eligible
ancestor or a complete nonoverlapping descendant cover, including mixed depths.
Fallback counts describe selected fallback tiles, not missing target count.

Set readiness means **CPU-prepared**, not GPU-resident. The Worker still keeps old
Current selected until every resource of the replacement snapshot is acknowledged.
`Select` takes previous coverage explicitly and never promotes its last generated
target into continuity. Keep required continuity fragments in the Set until the
replacement is acknowledged. Previously returned snapshots remain valid even if
their tile is removed from the Set.

## Ordering, wraps and placement

World copies come from `view.WorldWraps` for the normalized camera. Transform slots
are emitted in selected-tile order, then wrap order. Every slot retains its exact
`scene.TileSpace`; `Snapshot.Frame` uses the shared projection math.

Draw order is **ascending layer, selected tile, wrap, source draw**. Sorting is
stable. Prepared per-tile icon/halo/fill pass order is preserved, not reconstructed.
Clips and materials survive composition except for the explicitly instanced pattern
phase. Geometry and pixels aren't copied. Wraps share the tile's retained resource
versions. Store snapshots include all resources of a fragment when at least one
of its draw records is selected, even if some symbol draws are rejected.

Placement reuses `placement.ProjectSymbol` and `SelectSymbols`. Collection is
tile/wrap/**reverse-candidate** order; priority is descending layer and ascending
sort key, with stable ties. A `SymbolKey` identifies canonical tile, wrap and
candidate index. Accepted text filters both halo and fill records; accepted icons
filter icon records independently, following the existing optional/overlap rules.

Metric-ready text may reserve collision space without drawable glyph records.
This preserves the existing distinction between metric and atlas readiness. The
compositor does not silently invent a new text-fallback or eligibility policy.

When coverage, wraps, fallback count and acceptance are unchanged since the last
Apply, Select returns the **same snapshot pointer**. The consumer can update only
frame transforms without re-running upload planning. Select still performs bounded
placement work; it is not a zero-work camera tick. Frame alone does not recompute
placement. The producer decides when placement needs refreshing.

Any accepted nonempty Apply invalidates the latest selection cache, including an
update to an unselected tile. Unchanged resource versions remain reusable by the
Planner. Fine-grained dirty-region caching and shared cross-tile atlases are later
optimizations.

## Bounds and errors

- The retained Store's default/max limits apply: 128 tiles, 4,096 meshes, 4,096
  textures, 65,536 draws and 512 MiB of logical resource payload. `New` accepts
  lower `retained.Limits` values. Apply permits twice the tile limit in changes,
  allowing full-capacity remove-and-replace transactions.
- Canonical tile source zoom is 0–14, matching the current map policy. Target and
  previous lists each contain at most 128 tiles. Targets are unique at one source
  zoom; previous tiles must not overlap. `VisibleTileCover` normally produces at
  most 64 requested tiles.
- Each fragment has at most 10,000 candidates. Incoming updates and final retained
  storage each have at most 100,000 candidates. Incoming draw metadata is bounded
  before validation/copying; Store separately bounds resources and final storage.
- Instanced source draws must fit the Store draw limit **before** collision
  filtering. Instanced candidate references must fit 100,000 before projection.
  World copies remain capped by the shared wrap policy.
- Viewport dimensions must be finite and positive, at most 1,048,576 logical pixels
  per axis. Collision work retains its 1,000,000-operation default ceiling; the
  final Select argument may lower it. Camera position/zoom/bearing are normalized
  with the existing view policy.
- Errors return nil selection and preserve the current cache. Store and placement
  errors propagate. Old caller-held snapshots survive every failure and update.

Count limits bound metadata and work, not arbitrary caller-held snapshots, string
payloads, native driver overhead or total concurrently prepared input. Resource
byte limits retain Store/Planner's logical accounting. Bound producer jobs and old
snapshot lifetimes separately. Neither Set nor Store may be copied or used
concurrently; published snapshots and everything reachable from them are immutable.

## Verification and scope

```sh
WHEREAMI_VECTOR_TILE_FIXTURE=/path/to/pinned.pbf \
  CGO_ENABLED=0 go test -cover ./pkg/vecmap/tiles ./pkg/vecmap/view
CGO_ENABLED=0 GOARCH=386 go test ./pkg/vecmap/tiles ./pkg/vecmap/view
GOAMD64=v1 go test -race ./pkg/vecmap/tiles ./pkg/vecmap/view
CGO_ENABLED=0 go test ./pkg/vecmap/tiles -run '^$' \
  -fuzz '^FuzzTileComposition$' -fuzztime=20s
```

Checkpoint **9g4n**: tiles coverage is **98.8%**. The uncovered statements propagate
defensive Store.Snapshot failures after successful metadata validation. New shared
coverage-selection helpers have **100%** coverage. Tests cover fallback refinement,
explicit continuity, ordering, wrap phase, cross-tile priority, text/icon readiness,
immutable borrows, rollback, identity, camera-only reuse and Worker publication.
The fuzz run completed **68,283 executions** in its configured 20 seconds.

The pinned test composes the real fixture's solid base draws using explicit
source-order metadata. It checks payload sharing and revision isolation, not
generic compilation or label parity: the old fixture lacks the required provenance.

Basic/threaded native viewer tests on Vulkan and OpenGL verify a red parent stays
visible while only one child is prepared and while the first of two child uploads
is pending. Both children then appear together with their own transforms and no
batch failure. OpenGL also passes at 2× scale under the race detector. Production
scheduler regressions pass through the extracted shared selection policy.

Next is a generic prepared-tile compiler that emits this metadata, then bounded
loading and live producer scheduling. The command's file reload path still consumes
whole scene documents. This checkpoint adds neither network tile loading nor a new
GPU completion mechanism, and makes no MapLibre performance or visual-parity claim.
