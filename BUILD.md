# Building WhereAmI

## Flatpak Build

The supported distribution build lives in
[whereami-flatpak](https://github.com/rubiojr/whereami-flatpak). It builds the
application and MapLibre Native Qt against the same pinned KDE/Qt runtime,
which is required because the QtLocation provider uses private Qt APIs.

```bash
git clone https://github.com/rubiojr/whereami-flatpak.git
cd whereami-flatpak
./build dev ../whereami
```

The Flatpak manifest pins MapLibre Native Qt commit
`c924d8f4723c51eee9fd3dadad0ac3df53441c2c`. Do not substitute a prebuilt
MapLibre artifact from another Qt distribution or runtime.

## Local Build

### Prerequisites

- Go 1.25 or newer, matching the minimum in `go.mod`
- Qt 6.5 or newer
- GCC and G++ for CGO and the Qt bindings
- `miqt-rcc` for embedding QML resources

The application build does not link MapLibre. For a functional basemap, the
runtime must provide the `maplibre` QtLocation geoservice built against the
same Qt version. The application falls back to QtLocation's overlay-only
provider when MapLibre is absent so source builds and QML tests still work.

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
- Basemap unavailable: use the Flatpak, or install a MapLibre Native QtLocation provider built against the exact local Qt private ABI.
