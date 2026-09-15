# fixture

Qt-free orchestration for the pinned offline Madrid comparison scene. The command
and all CPU preparation now build with `CGO_ENABLED=0`:

```sh
CGO_ENABLED=0 go build ./cmd/vecmap-fixture
CGO_ENABLED=0 GOAMD64=v1 go run ./cmd/vecmap-fixture \
  -tile /path/to/openfreemap-20260823-z9-250-193.pbf \
  -glyph-dir /path/to/glyphs -direct-indexed -out /tmp/scene.json
```

`Compile(data, ranges, Options{})` uses supplied font-range bytes.
`CompileWithGlyphLoader(data, load, options)` invokes the caller's synchronous loader
exactly once after successful tile/style compilation, with sorted unique font
stacks. Nil loader means no fonts. Missing entries are reported without fetching or
substitution; a loader error aborts before layout/packing. A preparation worker owns
I/O, aggregate font count/bytes, scheduling and total concurrent memory.

## Fixed-scene contract

- Source is OpenFreeMap 20260823 z9/250/193, with `TileSHA256` checked before decode.
  Raw input above the MVT **2 MiB** ceiling rejects before hashing. `Tile()` returns
  a value, so callers cannot mutate shared source identity.
- Liberty layers come from the single shared `liberty` asset/cache package. Source
  features are decoded once, style is compiled once at zoom 10, and shared primitives
  and evaluated symbols are stored directly. No legacy bucket/candidate conversion
  slices or additional geometry pass are created.
- `CompileTile` retains document order and the aggregate geometry budget.
  `PrepareSymbols` shares the **10,000-candidate** budget across all symbol layers.
  Symbol-limit errors preserve the parent's `mvt.ErrFeatureResourceLimit` identity.
- Text requests retain original candidate indexes and font identities. The loader
  requests range 0–255. Existing SDF-only preparation, retained atlas selection,
  whole-label atlas coverage, icon/halo/fill order and packing math are reused.
- Collision uses the fixed 512x512 zoom-10 camera and nearest world copy (wrap zero),
  collecting candidates **in reverse order** before the stable layer/sort-key sort.
  Metric-ready layouts participate even if atlas coverage later omits their meshes.
- For compatibility with the old fixture, SDF-eligible text requires a metric
  layout for collision readiness. Other text follows `glyph.LegacyFallbackEligible`:
  some unsupported text can reserve collision space even though this offline path
  never draws native fallback text. Thaana remains excluded. Missing fonts, text
  optionality and icon availability retain the prior asymmetric collision behavior.
- Expanded output is the default. Direct indexed topology is opt-in; expensive
  post-deduplication remains a separate command option. `Frame(camera)` changes only
  transforms; style widths, visibility and collision stay frozen.

This is a pinned offline fixture, not a generic style/tile ingestion API, live
scheduler, incremental upload queue or shaping engine. Resource IDs are local to
one scene; multi-tile/live composition must define stable identities separately.

## Publication and failures

`Result` owns packed scene geometry/metadata and the missing-font list. Sprite
pixels are immutable borrows from `liberty`. Intermediate features, candidates,
layout maps and the single-channel glyph atlas are released after packing rather
than retained in the result. The scene owns its converted glyph RGBA texture.
All inputs must remain immutable throughout the synchronous compilation call;
published results and all reachable data must be treated as immutable.

Every returned error has nil output. Loader, layout, atlas, collision and packing
errors propagate; the headless path does not silently turn an atlas/collision
failure into a partially labeled scene. This makes previously ignored internal
failure guards explicit. Valid pinned output, including partial/missing font input,
remains equal to the old implementation. MVT per-feature resource degradation is
reported in `Result.Limits` instead of logging inside the shared package.

Parent `vecmap.CompileRenderFixture*` functions are compatibility wrappers with
aliases for options, loader and result. The former orchestration is retained only
in `render_fixture_legacy_test.go`, where it compares the new output against legacy
bucket/candidate/projection adapters with full, partial and absent fonts.

## Verification

Set both fixture variables explicitly; pinned tests/benchmarks skip without them:

```sh
WHEREAMI_VECTOR_TILE_FIXTURE=/path/to/pinned.pbf \
WHEREAMI_VECTOR_GLYPH_FIXTURE_DIR=/path/to/glyphs \
CGO_ENABLED=0 go test -cover ./pkg/vecmap/fixture ./cmd/vecmap-fixture

WHEREAMI_VECTOR_TILE_FIXTURE=/path/to/pinned.pbf \
WHEREAMI_VECTOR_GLYPH_FIXTURE_DIR=/path/to/glyphs \
CGO_ENABLED=0 GOARCH=386 go test ./pkg/vecmap/fixture ./cmd/vecmap-fixture
```

Fixture coverage is **94.6%**. All assembly, request iteration, pass packing,
projection and collision functions have 100% coverage. Uncovered statements are
error propagation guards for pinned decode/style/geometry and validated atlas/mesh
preparation, plus a finish-level collision error guard. Lower-level failure paths
are tested in their shared packages. Tests cover topology, loader count/errors,
font absence, candidate budgets, atomic failure, reverse-priority ties, fallback
readiness and camera-only frames. The command test validates its serialized scene.

All three headless v1 captures match the original reference bytes: 45 draws,
62 labels, complete fonts. Full module coverage, v1 integration/race checks,
targeted staticcheck and build pass. See kata **2d12** and
[`docs/vecmap-rhi.md`](../../../docs/vecmap-rhi.md) for controlled cost samples.
