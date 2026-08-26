#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
    printf 'Usage: %s PACKAGE_DIR DATA_DIR\n' "$0" >&2
    exit 2
fi

for dependency in jq sha256sum; do
    if ! command -v "$dependency" >/dev/null 2>&1; then
        printf 'Required command not found: %s\n' "$dependency" >&2
        exit 1
    fi
done

PACKAGE_DIR=$(realpath "$1")
DATA_DIR=$(realpath -m "$2")
MANIFEST="$PACKAGE_DIR/manifest.json"
[[ -f "$MANIFEST" && -f "$PACKAGE_DIR/SHA256SUMS" ]] || {
    printf 'Invalid geodata package directory: %s\n' "$PACKAGE_DIR" >&2
    exit 1
}

(
    cd "$PACKAGE_DIR"
    sha256sum --check SHA256SUMS
)

GENERATION_COUNT=$(jq '.generations | length' "$MANIFEST")
[[ "$GENERATION_COUNT" == "1" ]] || {
    printf 'Local packages must contain exactly one generation\n' >&2
    exit 1
}
GENERATION_ID=$(jq -r '.generations[0].id' "$MANIFEST")
[[ "$GENERATION_ID" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]] || {
    printf 'Unsafe generation ID: %s\n' "$GENERATION_ID" >&2
    exit 1
}

ADMIN_ROOT="$DATA_DIR/geodata/admin"
GENERATIONS="$ADMIN_ROOT/generations"
TARGET="$GENERATIONS/$GENERATION_ID"
mkdir -p "$GENERATIONS"
chmod 700 "$ADMIN_ROOT" "$GENERATIONS"

if [[ -e "$TARGET" ]]; then
    chmod -R u+w "$TARGET"
    rm -rf "$TARGET"
fi
STAGE=$(mktemp -d "$ADMIN_ROOT/.staging-local-XXXXXX")
cleanup() {
    if [[ -d "$STAGE" ]]; then
        chmod -R u+w "$STAGE" 2>/dev/null || true
        rm -rf "$STAGE"
    fi
}
trap cleanup EXIT

INDEX_FILE=$(jq -r '.generations[0].artifacts[] | select(.role == "index") | .filename' "$MANIFEST")
POLYGONS_FILE=$(jq -r '.generations[0].artifacts[] | select(.role == "polygons") | .filename' "$MANIFEST")
[[ -f "$PACKAGE_DIR/$INDEX_FILE" && -f "$PACKAGE_DIR/$POLYGONS_FILE" ]] || {
    printf 'Package is missing index or polygons artifact\n' >&2
    exit 1
}
cp --reflink=auto "$PACKAGE_DIR/$INDEX_FILE" "$STAGE/$INDEX_FILE"
cp --reflink=auto "$PACKAGE_DIR/$POLYGONS_FILE" "$STAGE/$POLYGONS_FILE"
jq -c '.generations[0]' "$MANIFEST" >"$STAGE/generation.json"
chmod 600 "$STAGE/$INDEX_FILE" "$STAGE/$POLYGONS_FILE" "$STAGE/generation.json"
mv "$STAGE" "$TARGET"
chmod 500 "$TARGET"

cp "$MANIFEST" "$ADMIN_ROOT/local-manifest.json"
printf '{"current":"%s"}\n' "$GENERATION_ID" >"$ADMIN_ROOT/current.json"
chmod 600 "$ADMIN_ROOT/local-manifest.json" "$ADMIN_ROOT/current.json"

printf 'Installed %s under %s\n' "$GENERATION_ID" "$ADMIN_ROOT"
printf 'Manifest override: %s\n' "$ADMIN_ROOT/local-manifest.json"
