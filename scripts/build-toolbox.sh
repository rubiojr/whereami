#!/bin/bash
#
# build-toolbox.sh - Build whereami RPMs using Fedora toolbox
#
# This script creates a Fedora toolbox container, installs all necessary
# dependencies, and runs the RPM build process inside the isolated environment.
#
# Usage:
#   ./scripts/build-toolbox.sh [OPTIONS]
#
# Options:
#   --fedora-version VERSION  Fedora version to use (default: 42)
#   --toolbox-name NAME       Name for the toolbox container (default: whereami-build)
#   --clean                   Remove existing toolbox before creating new one
#   --keep-toolbox           Don't remove toolbox after build
#   --help                   Show this help message
#
# Requirements:
#   - podman and toolbox installed
#   - Git repository with proper tags for versioning
#
# Environment variables:
#   FEDORA_VERSION - Override default Fedora version
#   TOOLBOX_NAME   - Override default toolbox name
#

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default configuration
DEFAULT_FEDORA_VERSION="42"
DEFAULT_TOOLBOX_NAME="whereami-build"

# Configuration (can be overridden by environment or command line)
FEDORA_VERSION="${FEDORA_VERSION:-$DEFAULT_FEDORA_VERSION}"
TOOLBOX_NAME="${TOOLBOX_NAME:-$DEFAULT_TOOLBOX_NAME}"
CLEAN_TOOLBOX=false

# Script directory and project root
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Function to print colored messages
print_msg() {
    local color=$1
    shift
    echo -e "${color}$*${NC}"
}

print_error() {
    print_msg "$RED" "ERROR: $*" >&2
}

print_success() {
    print_msg "$GREEN" "✓ $*"
}

print_info() {
    print_msg "$BLUE" "ℹ $*"
}

print_warning() {
    print_msg "$YELLOW" "⚠ $*"
}

# Function to show usage
show_help() {
    cat << EOF
Usage: $0 [OPTIONS]

Build whereami RPMs using Fedora toolbox container.

OPTIONS:
    --fedora-version VERSION  Fedora version to use (default: $DEFAULT_FEDORA_VERSION)
    --toolbox-name NAME       Name for toolbox container (default: $DEFAULT_TOOLBOX_NAME)
    --clean                   Remove existing toolbox before creating new one AND after build
    --help                   Show this help message

EXAMPLES:
    $0                                    # Build with defaults (keeps toolbox)
    $0 --fedora-version 39               # Use Fedora 39
    $0 --clean                           # Clean rebuild and remove after
    $0 --toolbox-name my-build           # Use custom container name

ENVIRONMENT VARIABLES:
    FEDORA_VERSION    Override default Fedora version
    TOOLBOX_NAME      Override default toolbox name

REQUIREMENTS:
    - podman and toolbox installed
    - Git repository with proper tags for versioning
    - Current directory must be the project root

EOF
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --fedora-version)
            FEDORA_VERSION="$2"
            shift 2
            ;;
        --toolbox-name)
            TOOLBOX_NAME="$2"
            shift 2
            ;;
        --clean)
            CLEAN_TOOLBOX=true
            shift
            ;;
        --help)
            show_help
            exit 0
            ;;
        *)
            print_error "Unknown option: $1"
            echo
            show_help
            exit 1
            ;;
    esac
done

# Function to check prerequisites
check_prerequisites() {
    print_info "Checking prerequisites..."

    # Check if we're in the project root
    if [[ ! -f "go.mod" ]] || [[ ! -f ".goreleaser.yml" ]]; then
        print_error "This script must be run from the project root directory"
        exit 1
    fi

    # Check if toolbox is installed
    if ! command -v toolbox >/dev/null 2>&1; then
        print_error "toolbox command not found. Please install toolbox:"
        echo "  sudo dnf install toolbox"
        exit 1
    fi

    # Check if podman is installed
    if ! command -v podman >/dev/null 2>&1; then
        print_error "podman command not found. Please install podman:"
        echo "  sudo dnf install podman"
        exit 1
    fi

    # Check git repository state
    if ! git rev-parse --git-dir >/dev/null 2>&1; then
        print_error "Not in a git repository"
        exit 1
    fi

    print_success "Prerequisites check passed"
}

# Function to manage toolbox container
manage_toolbox() {
    print_info "Managing toolbox container: $TOOLBOX_NAME"

    # Check if toolbox exists
    if toolbox list | grep -q "$TOOLBOX_NAME"; then
        if [[ "$CLEAN_TOOLBOX" == "true" ]]; then
            print_info "Removing existing toolbox: $TOOLBOX_NAME"
            toolbox stop "$TOOLBOX_NAME" 2>/dev/null || true
            toolbox rm "$TOOLBOX_NAME" || true
        else
            print_info "Using existing toolbox: $TOOLBOX_NAME"
            return 0
        fi
    fi

    # Create new toolbox
    print_info "Creating new toolbox: $TOOLBOX_NAME (Fedora $FEDORA_VERSION)"
    if ! toolbox create --distro fedora --release "$FEDORA_VERSION" "$TOOLBOX_NAME"; then
        print_error "Failed to create toolbox container"
        exit 1
    fi

    print_success "Toolbox container ready: $TOOLBOX_NAME"
}

# Function to install dependencies in toolbox
install_dependencies() {
    print_info "Installing build dependencies in toolbox..."

    # Create dependency installation script
    local dep_script=$(mktemp)
    cat > "$dep_script" << 'DEPS_EOF'
#!/bin/bash
set -e

echo "==> Updating package repositories..."
sudo dnf update -y

echo "==> Installing Go toolchain..."
sudo dnf install -y golang

echo "==> Installing Qt6 development packages..."
sudo dnf install -y \
    qt6-qtbase-devel \
    qt6-qtdeclarative-devel \
    qt6-qtpositioning-devel \
    qt6-qtlocation-devel \
    qt6-qtsvg-devel

echo "==> Installing build tools..."
sudo dnf install -y \
    gcc \
    gcc-c++ \
    make \
    git \
    rpm-build \
    rpmdevtools

echo "==> Installing GoReleaser..."
# Download and install goreleaser
GORELEASER_VERSION="2.4.0"
GORELEASER_URL="https://github.com/goreleaser/goreleaser/releases/download/v${GORELEASER_VERSION}/goreleaser_Linux_x86_64.tar.gz"
curl -sL "$GORELEASER_URL" -o /tmp/goreleaser.tar.gz
sudo tar -xzf /tmp/goreleaser.tar.gz -C /usr/local/bin goreleaser
rm -f /tmp/goreleaser.tar.gz
sudo chmod +x /usr/local/bin/goreleaser

echo "==> Installing miqt-rcc..."
go install github.com/mappu/miqt/cmd/miqt-rcc@latest

# Ensure Go bin is in PATH
echo 'export PATH=$PATH:~/go/bin' >> ~/.bashrc

echo "==> Dependency installation complete"
DEPS_EOF

    chmod +x "$dep_script"

    # Run dependency installation in toolbox
    if ! toolbox run --container "$TOOLBOX_NAME" bash "$dep_script"; then
        rm -f "$dep_script"
        print_error "Failed to install dependencies in toolbox"
        exit 1
    fi

    rm -f "$dep_script"
    print_success "Dependencies installed successfully"
}

# Function to run the build
run_build() {
    print_info "Running goreleaser in toolbox..."

    # Check if project is under home directory
    if [[ "$PROJECT_ROOT" != "$HOME"* ]]; then
        print_error "Project must be under home directory for toolbox access"
        exit 1
    fi

    # Run build in toolbox (home directory is automatically mounted)
    if ! toolbox run --container "$TOOLBOX_NAME" \
         bash -c "cd '$PROJECT_ROOT' && source ~/.bashrc && FEDORA_VERSION=\$(rpm -E %fedora) goreleaser release --config .goreleaser.yml --snapshot --clean"; then
        print_error "Build failed in toolbox"
        exit 1
    fi

    print_success "RPM build completed successfully"
}

# Function to cleanup toolbox
cleanup_toolbox() {
    if [[ "$CLEAN_TOOLBOX" == "true" ]]; then
        print_info "Cleaning up toolbox container: $TOOLBOX_NAME"
        toolbox stop "$TOOLBOX_NAME" 2>/dev/null || true
        toolbox rm "$TOOLBOX_NAME" || true
        print_success "Toolbox container removed"
    else
        print_info "Keeping toolbox container: $TOOLBOX_NAME"
        print_info "To remove later, run: toolbox rm $TOOLBOX_NAME"
    fi
}

# Function to show build results
show_results() {
    print_success "Build process completed!"
    echo
    print_info "Built RPM packages:"
    find "$PROJECT_ROOT/dist" -name "*.rpm" 2>/dev/null | while read -r rpm; do
        echo "  $(basename "$rpm")"
    done || print_warning "No RPM files found in dist/ directory"

    echo
    print_info "To install the RPM:"
    echo "  sudo dnf install dist/*.rpm"
    echo
    print_info "To test the RPM:"
    echo "  rpm -qpl dist/*.rpm  # List package contents"
    echo "  rpm -qpi dist/*.rpm  # Show package info"
}

# Main execution
main() {
    print_info "Starting whereami RPM build using Fedora toolbox"
    print_info "Fedora version: $FEDORA_VERSION"
    print_info "Toolbox name: $TOOLBOX_NAME"
    echo

    # Trap to ensure cleanup on exit if --clean specified
    trap cleanup_toolbox EXIT

    check_prerequisites
    manage_toolbox
    install_dependencies
    run_build
    show_results
}

# Run main function
main "$@"
