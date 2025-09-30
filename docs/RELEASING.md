# Releasing WhereAmI

This document describes how to create releases and build RPM packages for WhereAmI.

## Prerequisites

### Required Tools

1. **Go** (matching version in go.mod)
2. **GoReleaser** v2
3. **miqt-rcc** for Qt resource compilation
4. **Qt6 development files**
5. **GCC** for CGO compilation

### Installing GoReleaser

```bash
# Using Go
go install github.com/goreleaser/goreleaser/v2@latest

# Using Homebrew (macOS/Linux)
brew install goreleaser

# Using Snap
snap install goreleaser

# Using package manager (Fedora)
dnf install goreleaser
```

### Installing miqt-rcc

```bash
go install github.com/mappu/miqt/cmd/miqt-rcc@latest
```

## Quick Start

### Building a Test Release (Snapshot)

To build a snapshot release without publishing:

```bash
make release-snapshot
```

This creates builds in the `dist/` directory without requiring a git tag.

### Building RPM Packages

#### Generic RPM (works on most distributions)

```bash
make release-rpm
```

#### Fedora-specific RPM

```bash
make release-rpm-fedora
```

The RPMs will be created in `dist/` with appropriate dependencies for Qt6 runtime libraries.

## Full Release Process

### 1. Prepare the Release

1. Update version information if needed
2. Ensure all changes are committed
3. Run tests and lint checks:
   ```bash
   make lint
   make qml-test
   ```

### 2. Tag the Release

Create an annotated git tag following semantic versioning:

```bash
# For a new release
git tag -a v1.0.0 -m "Release v1.0.0: Brief description"

# For a pre-release
git tag -a v1.0.0-beta.1 -m "Pre-release v1.0.0-beta.1"

# Push the tag
git push origin v1.0.0
```

### 3. Build the Release

```bash
# This will create a GitHub release with all artifacts
make release

# Or use GoReleaser directly with specific config
goreleaser release --clean
```

### 4. Release Artifacts

The release process creates:

- **Binary archives** (tar.gz) for Linux (amd64, arm64)
- **RPM packages** with Qt6 dependencies
- **Checksums file** (SHA256)
- **Release notes** (auto-generated from commits)

## RPM Package Details

### Package Contents

The RPM includes:

- `/usr/bin/whereami` - Main executable
- `/usr/share/applications/io.github.rubiojr.whereami.desktop` - Desktop entry
- `/usr/share/icons/hicolor/scalable/apps/io.github.rubiojr.whereami.svg` - Application icon
- `/usr/share/whereami/bookmarks.gpx` - Default bookmarks (marked as config)
- `/usr/share/doc/whereami/` - Documentation
- `/usr/share/licenses/whereami/LICENSE` - License file

### Dependencies

The RPM automatically requires these runtime dependencies:

#### Core Qt6 Libraries
- qt6-qtbase
- qt6-qtbase-gui
- qt6-qtdeclarative
- qt6-qtquickcontrols2
- qt6-qtlocation
- qt6-qtpositioning
- qt6-qtsvg

#### System Libraries
- OpenGL (mesa-libGL, mesa-libEGL)
- fontconfig, freetype, harfbuzz
- libX11, libxcb, libxkbcommon
- openssl-libs
- libicu
- Various compression libraries (zlib, brotli, libzstd)

### Post-Installation

The RPM runs post-install scripts to:
- Update desktop database
- Update icon cache
- Update MIME database

## Testing Releases

### Local Installation Test

```bash
# Build snapshot
make release-snapshot

# Install the RPM (Fedora/RHEL)
sudo dnf install dist/whereami*.rpm

# Or on other RPM-based distros
sudo rpm -Uvh dist/whereami*.rpm

# Test the installation
whereami

# Check desktop integration
ls /usr/share/applications/io.github.rubiojr.whereami.desktop
```

### Dependency Verification

```bash
# Check RPM dependencies
rpm -qpR dist/whereami*.rpm

# Check installed files
rpm -ql whereami

# Verify runtime dependencies
ldd /usr/bin/whereami
```

## Troubleshooting

### Missing miqt-rcc

If `miqt-rcc` is not found during build:

```bash
# Install it
go install github.com/mappu/miqt/cmd/miqt-rcc@latest

# Or set the path explicitly
export MIQT_RCC_PATH=$HOME/go/bin/miqt-rcc
```

### Qt6 Development Files Not Found

Install Qt6 development packages:

```bash
# Fedora
sudo dnf install qt6-qtbase-devel qt6-qtdeclarative-devel \
                 qt6-qtlocation-devel qt6-qtpositioning-devel

# Ubuntu/Debian
sudo apt install qt6-base-dev qt6-declarative-dev \
                 qt6-location-dev qt6-positioning-dev

# openSUSE
sudo zypper install qt6-base-devel qt6-declarative-devel \
                    qt6-location-devel qt6-positioning-devel
```

### Build Errors with CGO

Ensure CGO is enabled and GCC is installed:

```bash
# Install GCC
sudo dnf install gcc gcc-c++  # Fedora
sudo apt install build-essential  # Ubuntu/Debian

# Build with CGO enabled
CGO_ENABLED=1 go build
```

### RPM Build Fails

Check GoReleaser version (should be v2):

```bash
goreleaser --version
```

Clean the build directory and retry:

```bash
rm -rf dist/
make release-snapshot
```

## Configuration Files

### Main Configuration

`.goreleaser.yml` - Generic configuration for all distributions

### Distribution-Specific

`.goreleaser/fedora.yml` - Fedora/RHEL specific package names and dependencies

### Customization

You can customize the build by editing these files:

1. **Version information**: Set in git tags
2. **Dependencies**: Edit the `dependencies` section in GoReleaser configs
3. **Build flags**: Modify `ldflags` in the build section
4. **Package metadata**: Update vendor, maintainer, description fields

## CI/CD Integration

### GitHub Actions Example

```yaml
name: Release

on:
  push:
    tags:
      - 'v*'

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      
      - name: Install Qt6
        run: |
          sudo apt update
          sudo apt install -y \
            qt6-base-dev qt6-declarative-dev \
            qt6-location-dev qt6-positioning-dev
      
      - name: Install miqt-rcc
        run: go install github.com/mappu/miqt/cmd/miqt-rcc@latest
      
      - name: Run GoReleaser
        uses: goreleaser/goreleaser-action@v5
        with:
          version: latest
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

## Advanced Usage

### Building for Specific Distributions

```bash
# For Fedora 39
FEDORA_VERSION=39 goreleaser release \
  --config .goreleaser/fedora.yml \
  --snapshot --clean

# For RHEL 9
FEDORA_VERSION=el9 goreleaser release \
  --config .goreleaser/fedora.yml \
  --snapshot --clean
```

### Cross-Compilation

For ARM64 builds on x86_64:

```bash
# Install cross-compilation tools
sudo dnf install gcc-aarch64-linux-gnu

# Build
CC=aarch64-linux-gnu-gcc \
CXX=aarch64-linux-gnu-g++ \
GOARCH=arm64 \
goreleaser build --single-target --snapshot
```

### Debug Builds

To create builds with debug symbols:

```bash
# Edit .goreleaser.yml and remove -s -w from ldflags
# Then build
goreleaser release --snapshot --clean
```

## Support

For issues with:
- **Build process**: Check `scripts/build.sh --check-deps`
- **RPM packages**: Review the dependencies section in `.goreleaser*.yml`
- **Qt resources**: Ensure `go generate` runs successfully
- **Runtime issues**: Check Qt6 libraries with `ldd`

For more help, open an issue on GitHub with:
1. Output of `make check-release-deps`
2. GoReleaser version
3. Error messages
4. Operating system and version