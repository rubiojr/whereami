#!/bin/bash
# build.sh - Build helper script for whereami
#
# This script ensures proper build environment setup for GoReleaser builds,
# particularly for Qt resource generation with miqt-rcc.
#
# Usage:
#   ./scripts/build.sh [--check-deps] [--generate-resources] [--build]
#
# Environment variables:
#   MIQT_RCC_PATH - Path to miqt-rcc binary (default: searches in PATH and common locations)
#   QT_VERSION    - Qt version to use (default: 6)
#   BUILD_DIR     - Build output directory (default: ./bin)

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
QT_VERSION="${QT_VERSION:-6}"
BUILD_DIR="${BUILD_DIR:-./bin}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Function to print colored messages
print_msg() {
    local color=$1
    shift
    echo -e "${color}$*${NC}"
}

# Function to find miqt-rcc
find_miqt_rcc() {
    local miqt_rcc=""

    # Check if MIQT_RCC_PATH is set
    if [ -n "${MIQT_RCC_PATH}" ] && [ -x "${MIQT_RCC_PATH}" ]; then
        miqt_rcc="${MIQT_RCC_PATH}"
    # Check in PATH
    elif command -v miqt-rcc >/dev/null 2>&1; then
        miqt_rcc="$(command -v miqt-rcc)"
    # Check common locations
    elif [ -x "/usr/local/bin/miqt-rcc" ]; then
        miqt_rcc="/usr/local/bin/miqt-rcc"
    elif [ -x "/usr/bin/miqt-rcc" ]; then
        miqt_rcc="/usr/bin/miqt-rcc"
    elif [ -x "${HOME}/go/bin/miqt-rcc" ]; then
        miqt_rcc="${HOME}/go/bin/miqt-rcc"
    elif [ -x "${HOME}/.local/bin/miqt-rcc" ]; then
        miqt_rcc="${HOME}/.local/bin/miqt-rcc"
    fi

    echo "${miqt_rcc}"
}

# Function to find Qt tools
find_qt_tools() {
    local qt_libexec=""

    # Common Qt6 libexec paths
    local qt_paths=(
        "/usr/lib64/qt6/libexec"
        "/usr/lib/qt6/libexec"
        "/usr/lib/x86_64-linux-gnu/qt6/libexec"
        "/usr/lib/aarch64-linux-gnu/qt6/libexec"
        "/opt/qt6/libexec"
        "/usr/local/opt/qt@6/libexec"
    )

    for path in "${qt_paths[@]}"; do
        if [ -d "${path}" ]; then
            qt_libexec="${path}"
            break
        fi
    done

    echo "${qt_libexec}"
}

# Function to check dependencies
check_dependencies() {
    print_msg "${GREEN}" "==> Checking build dependencies..."

    local missing_deps=()
    local warnings=()

    # Check for Go
    if ! command -v go >/dev/null 2>&1; then
        missing_deps+=("go (Go compiler)")
    else
        local go_version=$(go version | awk '{print $3}' | sed 's/go//')
        print_msg "${GREEN}" "    ✓ Go ${go_version} found"
    fi

    # Check for GCC (required for CGO)
    if ! command -v gcc >/dev/null 2>&1; then
        missing_deps+=("gcc (C compiler for CGO)")
    else
        local gcc_version=$(gcc --version | head -n1)
        print_msg "${GREEN}" "    ✓ GCC found: ${gcc_version}"
    fi

    # Check for miqt-rcc
    local miqt_rcc=$(find_miqt_rcc)
    if [ -z "${miqt_rcc}" ]; then
        missing_deps+=("miqt-rcc (Qt resource compiler for MIQT)")
        print_msg "${YELLOW}" "    ! miqt-rcc not found - install with: go install github.com/mappu/miqt/cmd/miqt-rcc@latest"
    else
        print_msg "${GREEN}" "    ✓ miqt-rcc found at: ${miqt_rcc}"
    fi

    # Check for Qt6 development files
    if ! pkg-config --exists Qt6Core 2>/dev/null; then
        warnings+=("Qt6 development files not found via pkg-config (build might still work)")
    else
        local qt_version=$(pkg-config --modversion Qt6Core)
        print_msg "${GREEN}" "    ✓ Qt6 ${qt_version} development files found"
    fi

    # Check for Qt6 libexec (for tools like rcc if needed)
    local qt_libexec=$(find_qt_tools)
    if [ -n "${qt_libexec}" ]; then
        print_msg "${GREEN}" "    ✓ Qt6 tools directory found: ${qt_libexec}"
        export PATH="${PATH}:${qt_libexec}"
    else
        warnings+=("Qt6 libexec directory not found (some Qt tools might be unavailable)")
    fi

    # Check for required Qt6 libraries
    local qt_libs=("Qt6Core" "Qt6Gui" "Qt6Widgets" "Qt6Qml" "Qt6Quick" "Qt6Network")
    for lib in "${qt_libs[@]}"; do
        if pkg-config --exists "${lib}" 2>/dev/null; then
            print_msg "${GREEN}" "    ✓ ${lib} found"
        else
            warnings+=("${lib} not found via pkg-config")
        fi
    done

    # Report missing dependencies
    if [ ${#missing_deps[@]} -gt 0 ]; then
        print_msg "${RED}" "==> Missing required dependencies:"
        for dep in "${missing_deps[@]}"; do
            print_msg "${RED}" "    ✗ ${dep}"
        done
        return 1
    fi

    # Report warnings
    if [ ${#warnings[@]} -gt 0 ]; then
        print_msg "${YELLOW}" "==> Warnings:"
        for warning in "${warnings[@]}"; do
            print_msg "${YELLOW}" "    ! ${warning}"
        done
    fi

    print_msg "${GREEN}" "==> All required dependencies found!"
    return 0
}

# Function to generate Qt resources
generate_resources() {
    print_msg "${GREEN}" "==> Generating Qt resources..."

    cd "${PROJECT_ROOT}"

    # Find miqt-rcc
    local miqt_rcc=$(find_miqt_rcc)
    if [ -z "${miqt_rcc}" ]; then
        print_msg "${RED}" "Error: miqt-rcc not found!"
        print_msg "${YELLOW}" "Install with: go install github.com/mappu/miqt/cmd/miqt-rcc@latest"
        return 1
    fi

    # Check if resources.qrc exists
    if [ ! -f "ui/resources.qrc" ]; then
        print_msg "${RED}" "Error: ui/resources.qrc not found!"
        return 1
    fi

    # Generate resources using go generate
    print_msg "${GREEN}" "    Running go generate..."
    if go generate ./...; then
        print_msg "${GREEN}" "    ✓ Resources generated successfully"

        # Verify generated files exist
        if [ -f "resources_gen.go" ] && [ -f "resources_gen.rcc" ]; then
            print_msg "${GREEN}" "    ✓ Generated files verified:"
            print_msg "${GREEN}" "        - resources_gen.go"
            print_msg "${GREEN}" "        - resources_gen.rcc"
        else
            print_msg "${YELLOW}" "    ! Warning: Expected generated files not found"
        fi
    else
        print_msg "${RED}" "    ✗ Failed to generate resources"
        return 1
    fi

    return 0
}

# Function to build the application
build_application() {
    print_msg "${GREEN}" "==> Building whereami..."

    cd "${PROJECT_ROOT}"

    # Create build directory
    mkdir -p "${BUILD_DIR}"

    # Set build flags
    local ldflags="-s -w"

    # Add version information if available
    if [ -n "${VERSION}" ]; then
        ldflags="${ldflags} -X main.version=${VERSION}"
    fi
    if [ -n "${COMMIT}" ]; then
        ldflags="${ldflags} -X main.commit=${COMMIT}"
    fi
    if [ -n "${DATE}" ]; then
        ldflags="${ldflags} -X main.date=${DATE}"
    fi

    # Build the application
    print_msg "${GREEN}" "    Building with flags: ${ldflags}"

    if CGO_ENABLED=1 go build -trimpath -ldflags "${ldflags}" -o "${BUILD_DIR}/whereami" .; then
        print_msg "${GREEN}" "    ✓ Build successful: ${BUILD_DIR}/whereami"

        # Show binary info
        if [ -f "${BUILD_DIR}/whereami" ]; then
            local size=$(du -h "${BUILD_DIR}/whereami" | cut -f1)
            print_msg "${GREEN}" "    ✓ Binary size: ${size}"

            # Check if binary is stripped
            if file "${BUILD_DIR}/whereami" | grep -q "not stripped"; then
                print_msg "${YELLOW}" "    ! Binary is not stripped (contains debug symbols)"
            else
                print_msg "${GREEN}" "    ✓ Binary is stripped"
            fi
        fi
    else
        print_msg "${RED}" "    ✗ Build failed"
        return 1
    fi

    return 0
}

# Function to show usage
show_usage() {
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Build helper script for whereami"
    echo ""
    echo "Options:"
    echo "  --check-deps        Check build dependencies"
    echo "  --generate          Generate Qt resources"
    echo "  --build             Build the application"
    echo "  --all               Run all steps (default)"
    echo "  --help              Show this help message"
    echo ""
    echo "Environment variables:"
    echo "  MIQT_RCC_PATH      Path to miqt-rcc binary"
    echo "  QT_VERSION         Qt version to use (default: 6)"
    echo "  BUILD_DIR          Build output directory (default: ./bin)"
    echo "  VERSION            Version string to embed"
    echo "  COMMIT             Git commit to embed"
    echo "  DATE               Build date to embed"
    echo ""
    echo "Examples:"
    echo "  $0 --check-deps                    # Check dependencies only"
    echo "  $0 --generate --build               # Generate resources and build"
    echo "  VERSION=1.0.0 $0 --all             # Build with version info"
}

# Main script logic
main() {
    local do_check_deps=false
    local do_generate=false
    local do_build=false
    local do_all=false

    # Parse arguments
    if [ $# -eq 0 ]; then
        do_all=true
    else
        while [ $# -gt 0 ]; do
            case "$1" in
                --check-deps)
                    do_check_deps=true
                    ;;
                --generate)
                    do_generate=true
                    ;;
                --build)
                    do_build=true
                    ;;
                --all)
                    do_all=true
                    ;;
                --help|-h)
                    show_usage
                    exit 0
                    ;;
                *)
                    print_msg "${RED}" "Unknown option: $1"
                    show_usage
                    exit 1
                    ;;
            esac
            shift
        done
    fi

    # If --all, enable all steps
    if [ "${do_all}" = true ]; then
        do_check_deps=true
        do_generate=true
        do_build=true
    fi

    # Change to project root
    cd "${PROJECT_ROOT}"

    print_msg "${GREEN}" "==> WhereAmI Build Script"
    print_msg "${GREEN}" "    Project root: ${PROJECT_ROOT}"
    echo ""

    # Run requested steps
    if [ "${do_check_deps}" = true ]; then
        if ! check_dependencies; then
            print_msg "${RED}" "==> Dependency check failed!"
            exit 1
        fi
        echo ""
    fi

    if [ "${do_generate}" = true ]; then
        if ! generate_resources; then
            print_msg "${RED}" "==> Resource generation failed!"
            exit 1
        fi
        echo ""
    fi

    if [ "${do_build}" = true ]; then
        if ! build_application; then
            print_msg "${RED}" "==> Build failed!"
            exit 1
        fi
        echo ""
    fi

    print_msg "${GREEN}" "==> All requested steps completed successfully!"
}

# Run main function
main "$@"
