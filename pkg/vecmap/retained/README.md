# retained

Toolkit-neutral retained fragment updates and ordered scene composition. This is
the identity/publication layer between prepared tile scenes and a future upload
planner. It uses the existing `scene.Scene` contract without Qt, cgo or new module
dependencies. It is not yet connected to the production renderer or QRhi viewer.

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
perform uploads, track GPU acknowledgements or make native replacement atomic.

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
