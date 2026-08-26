# Building WhereAmI

## Container Build

With Podman installed, the RPM build runs in a Fedora container:

```bash
make release-rpm
```

`scripts/build-podman` uses Fedora 43 by default and writes artifacts to `dist/`. Pass a different Fedora version directly to the script when needed:

```bash
./scripts/build-podman --fedora-version 44
```

## Local Build

### Prerequisites

- Go 1.24 or newer, matching the minimum in `go.mod`
- Qt 6.5 or newer
- GCC and G++ for CGO and the Qt bindings
- `miqt-rcc` for embedding QML resources

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
