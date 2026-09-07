# Releasing WhereAmI

The application is distributed as a Flatpak. Packaging lives in a separate
repository, [whereami-flatpak](https://github.com/rubiojr/whereami-flatpak).
WhereAmI includes the default Go/public-QSG map renderer. The packaging
repository can additionally bundle the legacy MapLibre Native Qt fallback
against its pinned KDE runtime. This repository produces no distributable
packages of its own.

## Application Release

Tag this repository first:

```bash
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0
```

Then build the exact tag from the packaging repository:

```bash
cd ../whereami-flatpak
./build release v1.0.0 ../whereami
```

Release mode requires a clean WhereAmI checkout with the requested tag at
`HEAD`. It regenerates Flatpak Go dependency sources, builds that exact local
Git commit, and writes `io.github.rubiojr.whereami.flatpak`. It does not commit,
push, create a GitHub release, or upload the bundle.

When the legacy fallback is bundled, its MapLibre commit is pinned separately
in `maplibre-native-qt.yml`. Changing it is a deliberate, separately tested
step, and the bundle's licensing notes depend on it staying accurate.

## Verification

Before tagging:

```bash
go test ./...
go vet ./...
make lint-qml
make qml-test
make build
```

Then verify the packaged application in its release runtime:

```bash
cd ../whereami-flatpak
./build dev ../whereami
flatpak install --user --reinstall io.github.rubiojr.whereami.flatpak
flatpak run --user io.github.rubiojr.whereami
```

Confirm the default Go basemap renders, the timeline map initializes on demand,
and the OpenFreeMap attribution is visible. If the package includes MapLibre,
also start it with `--legacy-map-renderer` and verify that fallback separately.

## Administrative Geodata

Administrative geodata is released separately from the application.
Before advertising a generation in `geodata_manifest.json`:

1. Pin an Overture release and Xiangshan version. Record both in `docs/GEODATA.md`.
2. Build or obtain the Xiangshan split `index` and `polygons` artifacts using the documented transformation.
3. Verify representative point-in-polygon probes with the application resolver.
4. Publish the immutable bytes under a versioned WhereAmI release path. Never replace bytes at an existing URL.
5. Record each artifact's exact byte count and SHA-256 digest in the manifest, with the attribution and `ODbL-1.0` license identifier.
6. Run `go test ./internal/geodata ./internal/admingeo/...` and install the generation from a clean data directory.

The complete ODbL 1.0 text is distributed at `licenses/ODbL-1.0.txt`. Only generations whose hosted sizes and checksums have been verified belong in the embedded manifest.

`make geodata-dist` writes the rsync-ready version directory under
`dist/geodata/`. The configured base URL is
`https://files.rbel.co/whereami/geodata`; see `docs/GEODATA.md` for build,
local-install, upload, and post-upload verification commands.
