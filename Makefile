# Makefile for whereami
#
# Provides convenience targets for:
#  - Local Go build / run / lint
#  - Flatpak build, install, run, clean
#  - Local desktop install (binary + .desktop + icon)
#
# Requirements (host, outside Flatpak):
#  - Go toolchain (matching go.mod version)
#  - qmllint-qt6 (for QML lint target) optional
#  - flatpak + flatpak-builder for Flatpak targets
#
# Flatpak manifest: flatpak/io.github.rubiojr.whereami.yml
#
# Common usage:
#   make build
#   make run
#   make flatpak-build
#   make flatpak-install
#   make install   (local desktop integration)
#
# Override variables if needed:
#   make FLATPAK_BUILDDIR=out/flatpak flatpak-build
#   make INSTALL_PREFIX=/custom/path install
#

APP_NAME          := whereami
APP_ID            := io.github.rubiojr.whereami
FLATPAK_MANIFEST  := flatpak/$(APP_ID).yml
FLATPAK_BUILDDIR  := build-dir
FLATPAK_EXPORTDIR := export-dir
BIN_DIR           := bin
GO                := go
QML_LINT          := qmllint-qt6

# Optional: pass ldflags to reduce binary size
LDFLAGS := -s -w

# Desktop integration install prefixes (override INSTALL_PREFIX to relocate)
INSTALL_PREFIX    ?= $(HOME)/.local
BIN_INSTALL_DIR   := $(INSTALL_PREFIX)/bin
DESKTOP_DIR       := $(INSTALL_PREFIX)/share/applications
ICON_DIR_SCALABLE := $(INSTALL_PREFIX)/share/icons/hicolor/scalable/apps
ICON_SIZES        := 16 24 32 48 64 128 256
DESKTOP_FILE_SRC  := desktop/$(APP_ID).desktop
ICON_FILE_SRC     := ui/icons/$(APP_ID).svg
DESKTOP_FILE_DEST := $(DESKTOP_DIR)/$(APP_ID).desktop
ICON_FILE_DEST    := $(ICON_DIR_SCALABLE)/$(APP_ID).svg

# Detect OS/Arch (optional for logging)
HOST_OS   := $(shell uname -s)
HOST_ARCH := $(shell uname -m)

.PHONY: all build run clean lint fmt vet tidy qml-test \
        flatpak-build flatpak-install flatpak-run flatpak-clean flatpak-rebuild \
        install uninstall print-vars help \
        release release-snapshot release-rpm release-rpm-fedora check-release-deps

all: build

print-vars:
	@echo "HOST_OS=$(HOST_OS)"
	@echo "HOST_ARCH=$(HOST_ARCH)"
	@echo "APP_ID=$(APP_ID)"
	@echo "FLATPAK_MANIFEST=$(FLATPAK_MANIFEST)"
	@echo "FLATPAK_BUILDDIR=$(FLATPAK_BUILDDIR)"
	@echo "FLATPAK_EXPORTDIR=$(FLATPAK_EXPORTDIR)"
	@echo "INSTALL_PREFIX=$(INSTALL_PREFIX)"
	@echo "BIN_INSTALL_DIR=$(BIN_INSTALL_DIR)"
	@echo "DESKTOP_FILE_DEST=$(DESKTOP_FILE_DEST)"
	@echo "ICON_FILE_DEST=$(ICON_FILE_DEST)"

########################################
# Go (local) targets
########################################

build:
	@echo "==> Building $(APP_NAME)"
	@mkdir -p $(BIN_DIR)

	PATH=$(PATH):/usr/lib64/qt6/libexec $(GO) generate
	$(GO) build -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(APP_NAME) .

run: build
	@echo "==> Running $(APP_NAME)"
	./$(BIN_DIR)/$(APP_NAME)

clean:
	@echo "==> Cleaning local build artifacts"
	rm -rf $(BIN_DIR)

fmt:
	@echo "==> go fmt"
	$(GO) fmt ./...

vet:
	@echo "==> go vet"
	$(GO) vet ./...

tidy:
	@echo "==> go mod tidy"
	$(GO) mod tidy

lint-qml:
	@echo "==> QML lint (non-fatal if tool not present)"
	@if command -v $(QML_LINT) >/dev/null 2>&1; then \
		$(QML_LINT) ui/*.qml ui/components/*.qml ui/services/*.qml || true; \
	else \
		echo "Skipping QML lint: $(QML_LINT) not found"; \
	fi

lint: fmt vet lint-qml

qml-test:
	@echo "==> Running QML tests"
	@if command -v qmltestrunner-qt6 >/dev/null 2>&1; then \
		qmltestrunner-qt6 -import ui -input ui/tests ; \
	elif command -v qmltestrunner >/dev/null 2>&1; then \
		qmltestrunner -import ui -input ui/tests ; \
	else \
		echo "qmltestrunner not found (install Qt Quick Test)"; \
		exit 1; \
	fi


########################################
# Desktop install targets (local user)
########################################

install: build
	@echo "==> Installing binary to $(BIN_INSTALL_DIR)"
	@mkdir -p "$(BIN_INSTALL_DIR)"
	@mv "$(BIN_DIR)/$(APP_NAME)" "$(BIN_INSTALL_DIR)/$(APP_NAME)"
	@chmod 755 "$(BIN_INSTALL_DIR)/$(APP_NAME)"
	@echo "==> Installing desktop file to $(DESKTOP_DIR)"
	@mkdir -p "$(DESKTOP_DIR)"
	@cp "$(DESKTOP_FILE_SRC)" "$(DESKTOP_FILE_DEST)"
	@chmod 644 "$(DESKTOP_FILE_DEST)"
	@echo "==> Installing scalable icon to $(ICON_DIR_SCALABLE)"
	@mkdir -p "$(ICON_DIR_SCALABLE)"
	@cp "$(ICON_FILE_SRC)" "$(ICON_FILE_DEST)"
	@chmod 644 "$(ICON_FILE_DEST)"
	@echo "==> Generating PNG icons ($(ICON_SIZES)) if rsvg-convert or inkscape is available"
	@for sz in $(ICON_SIZES); do \
		outdir="$(INSTALL_PREFIX)/share/icons/hicolor/$${sz}x$${sz}/apps"; \
		mkdir -p "$$outdir"; \
		if command -v rsvg-convert >/dev/null 2>&1; then \
			rsvg-convert -w $$sz -h $$sz "$(ICON_FILE_SRC)" -o "$$outdir/$(APP_ID).png"; \
		elif command -v inkscape >/dev/null 2>&1; then \
			inkscape "$(ICON_FILE_SRC)" --export-type=png -w $$sz -h $$sz -o "$$outdir/$(APP_ID).png" >/dev/null 2>&1; \
		else \
			echo "   (no converter found: rsvg-convert or inkscape)"; \
			break; \
		fi; \
		chmod 644 "$$outdir/$(APP_ID).png"; \
	done
	@echo "==> Updating desktop database / icon cache if available"
	@command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database "$(INSTALL_PREFIX)/share/applications" || echo "Skipping update-desktop-database"
	@command -v gtk-update-icon-cache >/dev/null 2>&1 && gtk-update-icon-cache -q "$(INSTALL_PREFIX)/share/icons/hicolor" || echo "Skipping gtk-update-icon-cache"
	@echo "==> Install complete. You may need to restart your desktop shell or run 'xdg-desktop-menu forceupdate'"

uninstall:
	@echo "==> Removing installed assets"
	@rm -f "$(BIN_INSTALL_DIR)/$(APP_NAME)"
	@rm -f "$(DESKTOP_FILE_DEST)"
	@rm -f "$(ICON_FILE_DEST)"
	@echo "==> Running desktop/icon cache updates (if tools available)"
	@command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database "$(INSTALL_PREFIX)/share/applications" || true
	@command -v gtk-update-icon-cache >/dev/null 2>&1 && gtk-update-icon-cache -q "$(INSTALL_PREFIX)/share/icons/hicolor" || true
	@echo "==> Uninstall complete"

########################################
# Flatpak targets
########################################

# Build (no install) into $(FLATPAK_BUILDDIR)
flatpak-build:
	@echo "==> Flatpak build (no install)"
	flatpak-builder --ccache $(FLATPAK_BUILDDIR) $(FLATPAK_MANIFEST)

# Build + install into user repo
flatpak-install:
	@echo "==> Flatpak build + install (user)"
	flatpak-builder --user --install --ccache $(FLATPAK_BUILDDIR) $(FLATPAK_MANIFEST)

# Run installed Flatpak
flatpak-run:
	@echo "==> Running Flatpak $(APP_ID)"
	flatpak run $(APP_ID)

# Remove build artifacts (does not uninstall the app)
flatpak-clean:
	@echo "==> Cleaning Flatpak build dirs"
	rm -rf $(FLATPAK_BUILDDIR) $(FLATPAK_EXPORTDIR)

# Clean + build + install (forces fresh build)
flatpak-rebuild:
	@echo "==> Cleaning and rebuilding Flatpak"
	flatpak-builder --user --force-clean --ccache $(FLATPAK_BUILDDIR) $(FLATPAK_MANIFEST)
	flatpak build-bundle $(FLATPAK_EXPORTDIR) $(APP_ID).flatpak $(APP_ID)

# Export and create a distributable .flatpak bundle
flatpak-bundle:
	@echo "==> Creating Flatpak bundle"
	@if [ ! -d "$(FLATPAK_BUILDDIR)" ]; then \
		echo "Build directory not found. Run 'make flatpak-build' first."; \
		exit 1; \
	fi
	@mkdir -p $(FLATPAK_EXPORTDIR)
	flatpak-builder --repo=$(FLATPAK_EXPORTDIR) --force-clean $(FLATPAK_BUILDDIR) $(FLATPAK_MANIFEST)
	flatpak build-bundle $(FLATPAK_EXPORTDIR) $(APP_ID).flatpak $(APP_ID)
	@echo "==> Flatpak bundle created: $(APP_ID).flatpak"

# Upload flatpak bundle to latest GitHub release
flatpak-release:
	@echo "==> Uploading Flatpak bundle to latest GitHub release"
	@if [ ! -f "$(APP_ID).flatpak" ]; then \
		echo "Error: $(APP_ID).flatpak not found. Run 'make flatpak-bundle' first."; \
		exit 1; \
	fi
	@if ! command -v gh >/dev/null 2>&1; then \
		echo "Error: gh (GitHub CLI) not found!"; \
		echo "Install from: https://cli.github.com/"; \
		exit 1; \
	fi
	@echo "Finding latest release..."
	@LATEST_TAG=$$(gh release list --limit 1 --json tagName --jq '.[0].tagName'); \
	if [ -z "$$LATEST_TAG" ]; then \
		echo "Error: No releases found"; \
		exit 1; \
	fi; \
	echo ""; \
	echo "Release: $$LATEST_TAG"; \
	echo "File:    $(APP_ID).flatpak"; \
	echo ""; \
	read -p "Upload to this release? [y/N] " -n 1 -r; \
	echo; \
	if [[ ! $$REPLY =~ ^[Yy]$$ ]]; then \
		echo "Upload cancelled."; \
		exit 1; \
	fi; \
	echo "Uploading to release $$LATEST_TAG..."; \
	gh release upload "$$LATEST_TAG" "$(APP_ID).flatpak" --clobber
	@echo "==> Upload complete!"

########################################
# GoReleaser targets for releases and RPMs
########################################

# Check if goreleaser is installed
check-release-deps:
	@echo "==> Checking release dependencies"
	@if ! command -v goreleaser >/dev/null 2>&1; then \
		echo "Error: goreleaser not found!"; \
		echo "Install with one of:"; \
		echo "  go install github.com/goreleaser/goreleaser/v2@latest"; \
		echo "  brew install goreleaser"; \
		echo "  snap install goreleaser"; \
		exit 1; \
	fi
	@echo "    ✓ goreleaser found: $$(goreleaser --version)"
	@if [ -x scripts/build.sh ]; then \
		./scripts/build.sh --check-deps; \
	fi

# Build a snapshot release (without publishing)
release-snapshot: check-release-deps
	@echo "==> Building snapshot release with GoReleaser"
	goreleaser release --snapshot --clean

# Build RPMs using the generic configuration
release-rpm:
	@echo "==> Building RPMs with build-toolbox.sh"
	@if [ ! -x scripts/build-toolbox.sh ]; then \
		echo "Error: scripts/build-toolbox.sh not found or not executable"; \
		exit 1; \
	fi
	./scripts/build-toolbox.sh



# Create a full release (requires git tag)
release: check-release-deps
	@echo "==> Creating release with GoReleaser"
	@if ! git describe --exact-match --tags HEAD 2>/dev/null; then \
		echo "Error: Current commit is not tagged!"; \
		echo "Create a tag first: git tag -a v1.0.0 -m 'Release v1.0.0'"; \
		exit 1; \
	fi
	goreleaser release --clean

########################################
# Utility / Help
########################################

help:
	@echo ""
	@echo "Available targets:"
	@echo "  build             Build local Go binary"
	@echo "  run               Build and run locally"
	@echo "  clean             Remove local build artifacts"
	@echo "  fmt               Run go fmt"
	@echo "  vet               Run go vet"
	@echo "  tidy              Run go mod tidy"
	@echo "  lint-qml          Run QML linter (if available)"
	@echo "  lint              fmt + vet + QML lint"
	@echo "  qml-test          Run QML JS test suite"
	@echo "  install           Install binary, desktop file & icon to ~/.local"
	@echo "  uninstall         Remove installed binary/desktop/icon"
	@echo "  flatpak-build     Flatpak build (no install)"
	@echo "  flatpak-install   Flatpak build + install (user)"
	@echo "  flatpak-run       Run installed Flatpak"
	@echo "  flatpak-clean     Remove Flatpak build dirs"
	@echo "  flatpak-rebuild   Clean + build + install"
	@echo "  flatpak-bundle    Create distributable .flatpak file"
	@echo "  flatpak-release   Upload .flatpak to latest GitHub release"
	@echo "  release-snapshot  Build snapshot release with GoReleaser"
	@echo "  release-rpm       Build RPMs using build-toolbox.sh"
	@echo "  release           Create full release (requires git tag)"
	@echo "  print-vars        Show variable values"
	@echo "  help              This message"
	@echo ""
	@echo "Examples:"
	@echo "  make build"
	@echo "  make install"
	@echo "  make flatpak-install"
	@echo "  make flatpak-run"
	@echo ""
	@echo "Adjust FLATPAK_BUILDDIR or other vars:"
	@echo "  make FLATPAK_BUILDDIR=out/fp flatpak-build"
	@echo "Override install prefix:"
	@echo "  make INSTALL_PREFIX=/opt/local install"
	@echo ""
	@echo "Release examples:"
	@echo "  make release-snapshot    # Test build without publishing"
	@echo "  make release-rpm         # Build RPMs in Fedora toolbox"
	@echo "  make flatpak-bundle      # Create .flatpak file"
	@echo "  make flatpak-release     # Upload .flatpak to GitHub"
	@echo "  git tag -a v1.0.0 -m 'Release v1.0.0'"
	@echo "  make release             # Create GitHub release"
	@echo ""
