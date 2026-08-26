# Releasing WhereAmI

This repository has three release targets, all defined in `Makefile`.

## Snapshot Release

`make release-snapshot` runs GoReleaser v2 on the host without publishing:

```bash
make release-snapshot
```

Host requirements are Go, GoReleaser v2, GCC/G++, Qt 6 development files, and `miqt-rcc`. The target runs `scripts/build.sh --check-deps` before GoReleaser.

Install the two Go tools with:

```bash
go install github.com/goreleaser/goreleaser/v2@latest
go install github.com/mappu/miqt/cmd/miqt-rcc@v0.14.0
```

## Fedora RPM Snapshot

`make release-rpm` delegates to `scripts/build-podman`:

```bash
make release-rpm
```

This path requires Podman on the host. The script creates or reuses a Fedora 43 container, installs the build dependencies, runs a GoReleaser snapshot, and copies `dist/` back to the repository. Its supported options are:

```text
--fedora-version VERSION
--container-name NAME
--clean
--help
```

## Published Release

The current commit must have an exact Git tag before `make release` will run:

```bash
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0
make release
```

`make release` invokes `goreleaser release --clean`. The GoReleaser configuration creates a draft GitHub release.

## Current Artifacts

`.goreleaser.yml` currently builds Linux AMD64 only. It produces:

- A `tar.gz` archive containing the executable, README, MIT and ODbL licenses, desktop entry, and icons
- A Fedora-named RPM containing the executable, desktop entry, icons, README, and MIT and ODbL licenses
- `checksums.txt` using SHA-256

The RPM also creates `/usr/share/whereami/` as an empty application directory. The default `bookmarks.gpx` is embedded in the executable and is written to the user's data directory on first run; it is not installed under `/usr/share`.

## RPM Paths

- `/usr/bin/whereami`
- `/usr/share/applications/io.github.rubiojr.whereami.desktop`
- `/usr/share/icons/hicolor/.../io.github.rubiojr.whereami.{svg,png}`
- `/usr/share/doc/whereami/README.md`
- `/usr/share/doc/whereami/LICENSE`
- `/usr/share/doc/whereami/ODbL-1.0.txt`
- `/usr/share/doc/whereami/GEODATA.md`
- `/usr/share/whereami/`

The dependency list and package metadata are defined in the `nfpms` section of `.goreleaser.yml`. Post-install and post-remove scripts refresh the desktop and icon caches when the corresponding tools are available; the post-install script also refreshes the MIME database when available.

## Administrative Geodata

Administrative geodata is released separately from the application archives.
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

## Verification

Before tagging:

```bash
go test ./...
go vet ./...
make qml-test
goreleaser check
```

Inspect snapshot artifacts with:

```bash
rpm -qpl dist/*.rpm
rpm -qpi dist/*.rpm
rpm -qpR dist/*.rpm
```

Install an RPM locally on Fedora with:

```bash
sudo dnf install ./dist/*.rpm
```

## Configuration

The release process uses one configuration file: `.goreleaser.yml`. Version selection comes from Git tags. The package dependencies, metadata, files, scripts, archive contents, and build flags are all declared in that file.
