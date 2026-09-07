# Building WhereAmI

## Flatpak Build

The supported distribution build lives in
[whereami-flatpak](https://github.com/rubiojr/whereami-flatpak). The application
includes its default Go/public-QSG map renderer. The packaging repository may
also build MapLibre Native Qt against the pinned KDE/Qt runtime as the optional
`--legacy-map-renderer` fallback.

```bash
git clone https://github.com/rubiojr/whereami-flatpak.git
cd whereami-flatpak
./build dev ../whereami
```

If the optional fallback is packaged, its manifest pins MapLibre Native Qt
commit `c924d8f4723c51eee9fd3dadad0ac3df53441c2c`. Do not substitute a prebuilt
MapLibre artifact from another Qt distribution or runtime because that provider
uses Qt private APIs.

## Local Build

### Prerequisites

- Go 1.25 or newer, matching the minimum in `go.mod`
- Qt 6.5 or newer
- GCC and G++ for CGO and the Qt bindings
- `miqt-rcc` for embedding QML resources

The application build does not link MapLibre and does not need its QtLocation
geoservice for the default basemap. The Go renderer fetches the pinned
OpenFreeMap tile snapshot and renders it through public Qt Quick scene-graph
APIs. `--legacy-map-renderer` uses the `maplibre` QtLocation geoservice when one
is installed; otherwise QtLocation's overlay-only provider keeps the
application usable.

On Fedora, install the native dependencies with:

```bash
sudo dnf install golang gcc gcc-c++ \
  qt6-qtbase-devel qt6-qtdeclarative-devel \
  qt6-qtpositioning-devel qt6-qtlocation-devel qt6-qtsvg-devel
```

Install the resource compiler at the same MIQT version used by `go.mod`:

```bash
go install github.com/mappu/miqt/cmd/miqt-rcc@v0.14.0
```

Ensure `$GOBIN` or `$HOME/go/bin` is in `PATH`. `make build` also adds `/usr/lib64/qt6/libexec` to `PATH` for Qt tools.

Check the local toolchain:

```bash
./scripts/build.sh --check-deps
```

### Build

```bash
make build
```

The target runs `go generate` to rebuild the Qt resource bundle, then writes the executable to `bin/whereami`.

The cross-backend SDF text shader packs are committed under `ui/shaders`, so a
normal build does not require Qt Shader Tools. Regenerating those packs requires
`qsb`; use the Qt 6.5-compatible commands documented in
`ui/shaders/README.md`.

The helper script exposes the same steps separately:

```bash
./scripts/build.sh --generate
./scripts/build.sh --build
./scripts/build.sh --all
```

### Checks

```bash
go test ./...
go vet ./...
make lint-qml
make qml-test
```

`make lint-qml` skips the check when `qmllint-qt6` is unavailable. `make qml-test` requires either `qmltestrunner-qt6` or `qmltestrunner`.

### Common Failures

- `miqt-rcc` not found: install it and add its directory to `PATH`, or set `MIQT_RCC_PATH` when using `scripts/build.sh`.
- `rcc` not found: add the Qt 6 libexec directory to `PATH`.
- Qt package errors: install the Qt base, declarative, positioning, location, and SVG development packages.
- CGO compiler errors: install GCC and G++ and ensure `CGO_ENABLED=1`.
- Default basemap unavailable: check the logged Go renderer initialization or tile error, network access to `tiles.openfreemap.org`, and write access to the effective cache directory.
- Legacy basemap unavailable: install a MapLibre Native QtLocation provider built against the exact local Qt private ABI, or omit `--legacy-map-renderer`.
