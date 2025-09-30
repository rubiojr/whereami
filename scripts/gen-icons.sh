#!/bin/bash
#
# gen-icons.sh - Generate all application icons from SVG source
#
# This script regenerates all PNG icons in ui/icons/hicolor/ from the SVG source
# with transparent backgrounds for proper display on various desktop themes.
#
# Usage: ./scripts/gen-icons.sh
#

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if we're in the right directory
if [[ ! -f "ui/icons/io.github.rubiojr.whereami.svg" ]]; then
    echo -e "${RED}Error: SVG source file not found. Please run from project root.${NC}"
    exit 1
fi

# Check if ImageMagick is available
if ! command -v magick &> /dev/null; then
    echo -e "${RED}Error: ImageMagick 'magick' command not found. Please install ImageMagick.${NC}"
    exit 1
fi

SVG_SOURCE="ui/icons/io.github.rubiojr.whereami.svg"
ICON_NAME="io.github.rubiojr.whereami.png"

echo -e "${GREEN}Generating icons from ${SVG_SOURCE}...${NC}"
echo

# Standard sizes (1x)
declare -a SIZES=("16" "22" "24" "32" "48" "64" "128" "256" "512")

echo -e "${YELLOW}Generating standard icons...${NC}"
for size in "${SIZES[@]}"; do
    target_dir="ui/icons/hicolor/${size}x${size}/apps"
    target_file="${target_dir}/${ICON_NAME}"

    echo "  ${size}x${size} -> ${target_file}"
    magick "${SVG_SOURCE}" -background transparent -resize "${size}x${size}" "${target_file}"
done

echo

# Retina sizes (@2)
declare -a RETINA_BASE_SIZES=("16" "24" "32" "48" "64" "128" "256")

echo -e "${YELLOW}Generating retina (@2) icons...${NC}"
for base_size in "${RETINA_BASE_SIZES[@]}"; do
    # Calculate actual pixel size (double the base size)
    actual_size=$((base_size * 2))

    target_dir="ui/icons/hicolor/${base_size}x${base_size}@2/apps"
    target_file="${target_dir}/${ICON_NAME}"

    echo "  ${base_size}x${base_size}@2 (${actual_size}x${actual_size}) -> ${target_file}"
    magick "${SVG_SOURCE}" -background transparent -resize "${actual_size}x${actual_size}" "${target_file}"
done

echo
echo -e "${GREEN}Icon generation complete!${NC}"
echo
echo "Generated icons:"
find ui/icons/hicolor -name "*.png" | sort | while read -r icon; do
    size=$(identify "$icon" | awk '{print $3}')
    file_size=$(du -h "$icon" | awk '{print $1}')
    echo "  $icon (${size}, ${file_size})"
done

echo
echo -e "${GREEN}All icons regenerated successfully with transparent backgrounds.${NC}"
