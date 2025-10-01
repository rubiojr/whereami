# Building WhereAmI

This document describes how to build the whereami application from source.

## The easy way

If you have [Podman](https://podman.io), simply run `make release-rpm`.

## The hard way

### Prerequisites

- Go 1.24 or higher
- Qt 6.5 or higher
- miqt bindings for Qt6
- miqt-rcc tool for resource compilation

For Fedora:

```bash
sudo dnf install golang qt6-qtbase-devel qt6-qtdeclarative-devel qt6-qtpositioning-devel qt6-qtlocation-devel
```

Use `script/build.sh --check-deps` to verify all deps are in place.

### Installing miqt-rcc

The `miqt-rcc` tool is required to compile QML resources into the binary:

```bash
go install github.com/mappu/miqt/cmd/miqt-rcc@latest
```

Make sure `$GOBIN` or `~/go/bin` is in your PATH.

### Building

#### 1. Build the Application

```bash
make build
```

#### Build Errors

If the build fails:
1. Ensure miqt-rcc is installed and in your PATH
2. Check that Qt6 development files are installed
3. Verify that the miqt Qt6 bindings are properly installed
