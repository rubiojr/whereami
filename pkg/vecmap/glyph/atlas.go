package glyph

import (
	"cmp"
	"slices"
)

// BuildAtlas reuses deterministic shelf packing: descending height/width, then
// ascending font stack/code point. It tries square powers of two from 256 through
// 2048, retaining the sorted prefix that fits at the maximum size. Positions
// reports that prefix; overflow is normal degradation, not an error. Empty or
// bitmap-free input returns nil. Invalid glyph metrics/bitmaps fail atomically.
//
// Inputs are borrowed during the call; output owns its pixels and position map.
// Callers bound the number of input glyphs and schedule O(n log n) sorting away
// from rendering callbacks. Pixel output is bounded by MaxAtlasSize squared.
func BuildAtlas(glyphs map[Key]Glyph) (*Atlas, error) {
	keys := make([]Key, 0, len(glyphs))
	for key, glyph := range glyphs {
		if err := glyph.validate(); err != nil {
			return nil, err
		}
		if len(glyph.Bitmap) > 0 {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return nil, nil
	}
	slices.SortFunc(keys, func(first, second Key) int {
		firstGlyph, secondGlyph := glyphs[first], glyphs[second]
		if byHeight := cmp.Compare(secondGlyph.Height, firstGlyph.Height); byHeight != 0 {
			return byHeight
		}
		if byWidth := cmp.Compare(secondGlyph.Width, firstGlyph.Width); byWidth != 0 {
			return byWidth
		}
		if byStack := cmp.Compare(first.FontStack, second.FontStack); byStack != 0 {
			return byStack
		}
		return cmp.Compare(first.ID, second.ID)
	})
	for size := MinAtlasSize; size <= MaxAtlasSize; size *= 2 {
		positions, fits := pack(keys, glyphs, size)
		if !fits && size < MaxAtlasSize {
			continue
		}
		pixels := make([]byte, size*size)
		for key, rectangle := range positions {
			glyph := glyphs[key]
			bitmapWidth := int(glyph.Width) + 2*PBFBorder
			bitmapHeight := int(glyph.Height) + 2*PBFBorder
			for y := range bitmapHeight {
				for x := range bitmapWidth {
					distance := glyph.Bitmap[y*bitmapWidth+x]
					target := (rectangle.Y+AtlasGuard+y)*size + rectangle.X + AtlasGuard + x
					pixels[target] = distance
				}
			}
		}
		return &Atlas{Pixels: pixels, Width: size, Height: size, Positions: positions}, nil
	}
	return nil, nil
}

func pack(keys []Key, glyphs map[Key]Glyph, size int) (map[Key]Rect, bool) {
	positions := make(map[Key]Rect, len(keys))
	x, y, shelfHeight := 0, 0, 0
	for _, key := range keys {
		glyph := glyphs[key]
		width := int(glyph.Width) + 2*AtlasPadding
		height := int(glyph.Height) + 2*AtlasPadding
		if width > size || height > size {
			return nil, false
		}
		if x+width > size {
			x = 0
			y += shelfHeight
			shelfHeight = 0
		}
		if y+height > size {
			return positions, false
		}
		positions[key] = Rect{X: x, Y: y, Width: width, Height: height}
		x += width
		shelfHeight = max(shelfHeight, height)
	}
	return positions, true
}
