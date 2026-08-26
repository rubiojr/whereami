# Administrative Geodata Provenance

WhereAmI resolves recorded observations against an offline Xiangshan split
dataset. Xiangshan is code licensed under MIT. Its administrative division data
is derived from Overture Maps and is distributed under ODbL 1.0.

Attribution:

```text
© OpenStreetMap contributors, Overture Maps Foundation
```

The ODbL 1.0 text is in `licenses/ODbL-1.0.txt`; its canonical URI is
https://opendatacommons.org/licenses/odbl/1-0/.

## Current Candidate

- Xiangshan: `github.com/ringsaturn/xiangshan` v0.2.0
- Overture source release: `2026-04-15.0`
- Upstream source: https://github.com/OvertureMaps/data
- Upstream split artifact documentation: https://github.com/ringsaturn/xiangshan/tree/v0.2.0
- Distribution base URL: https://files.rbel.co/whereami/geodata
- `divisions.xs-index.gz`: 15,920,427 bytes, SHA-256 `7e5f4d337264b58402d0f5d80c8df96c1ce6845713fd20992a3266b0eefe09c7`
- `divisions.xs-poly`: 445,184,648 bytes, SHA-256 `89ef505f8160c07eb505a04110848f02395684eff41e2ce32597e28e74a018c5`

These hashes describe the locally inspected candidate and the immutable files
published at the distribution base URL. The verified generation is advertised
by `geodata_manifest.json` and can be installed explicitly from the Places
report page.

## Transformation

Xiangshan's pipeline reads Overture administrative division Parquet files,
extracts the division hierarchy and geometry, simplifies and validates the
topology, and encodes FlatBuffers data. Place the pinned release's `division`
and `division_area` Parquet files under `data/divisions/type=division/` and
`data/divisions/type=division_area/` in the Xiangshan v0.2.0 checkout, then run:

```bash
make remote-split VERSION=2026-04-15.0 SOURCE=overturemaps-2026-04-15
sha256sum build/divisions.xs-index.gz build/divisions.xs-poly
wc -c build/divisions.xs-index.gz build/divisions.xs-poly
```

The `remote-split` dependency chain runs extract, simplify, encode, compress,
and split. Record the commands, DuckDB/Go toolchain, resulting byte counts,
hashes, and point probes with the published generation.

## Build And Package

Build from a Xiangshan v0.2.0 checkout that already contains the pinned
Overture division Parquet files:

```bash
make geodata-build XIANGSHAN_DIR=/path/to/xiangshan
```

This copies the split output to `build/geodata/2026-04-15.0/`. Package it with:

```bash
make geodata-dist
```

An existing split pair can be packaged directly:

```bash
make geodata-dist GEODATA_INPUT_DIR=/path/to/split-files
```

The upload-ready directory is `dist/geodata/2026-04-15.0/`. It contains the
two runtime artifacts, a full manifest, generation metadata, checksums, the
ODbL text, and this provenance document. The packager opens the candidate and
derives a known Paris containment probe before writing the manifest.

Upload the version directory without renaming its files. For example:

```bash
rsync -av --progress dist/geodata/ \
  USER@files.rbel.co:REMOTE_DOCUMENT_ROOT/whereami/geodata/
```

After upload, verify `SHA256SUMS` against the hosted bytes. Only then replace
the empty embedded `geodata_manifest.json` with the packaged `manifest.json`
and release a new WhereAmI build. Published version directories are immutable.

## Local Installation

Install the exact packaged generation into an isolated managed data directory:

```bash
make geodata-install-local
```

The default root is `build/geodata-local/`. Run WhereAmI against it with:

```bash
make geodata-run-local
```

`geodata-run-local` passes the explicit development-only
`--geodata-manifest` override. Normal application runs continue using the
embedded manifest. The local installer verifies `SHA256SUMS`, creates the same
generation metadata and permissions as the network installer, and startup
rehashes and probes the active generation.

WhereAmI does not modify administrative names or infer visits. It performs
mandatory polygon containment and reports deterministic hierarchy counts for
imported GPX waypoint observations. The application does not distribute the
candidate bytes and never downloads Xiangshan's mutable upstream URLs.

Nearby POI enrichment is intentionally deferred. Xiangshan contains
administrative divisions, not places of interest, and no immutable local
Overture Places shard format or inference policy has been selected. Reports do
not label administrative containment as a venue visit.
