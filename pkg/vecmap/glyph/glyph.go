// Package glyph prepares Go-owned SDF glyph ranges, atlas images, metric text
// layouts and glyph meshes without Qt or cgo. Font loading, caching, shaping and placement belong
// to callers.
package glyph

import (
	"errors"
	"fmt"
)

const (
	RangeSize         = uint32(256)
	MaxCodePoint      = uint32(0xffff)
	MaxRangeStacks    = 8
	MaxGlyphsPerRange = 256
	MaxDimension      = 255
	MaxMetric         = 127
	MinMetric         = -128
	// MaxRangeBytes matches the existing font download/cache input ceiling.
	MaxRangeBytes = 2 << 20
	// PBFBorder is the distance-field border already present in glyph bitmaps.
	PBFBorder = 3
	// AtlasGuard adds a zero-distance texel around each bitmap.
	AtlasGuard   = 1
	AtlasPadding = PBFBorder + AtlasGuard
	MinAtlasSize = 256
	MaxAtlasSize = 2048
)

// Glyph contains unscaled metrics and a row-major single-channel SDF bitmap.
// Width/Height exclude the PBF border. A zero dimension means no bitmap.
// Treat the bitmap as immutable after publication.
type Glyph struct {
	ID      uint32
	Bitmap  []byte
	Width   uint32
	Height  uint32
	Left    int32
	Top     int32
	Advance uint32
}

// Range owns decoded glyphs. FontStack is the caller's requested stack; the
// server may return a different name when expanding fallback fonts.
type Range struct {
	FontStack string
	RangeName string
	Glyphs    map[uint32]Glyph
}

// Key distinguishes the same code point in different font stacks.
type Key struct {
	FontStack string
	ID        uint32
}

// Rect includes both the PBF border and atlas guard, in atlas texels.
type Rect struct{ X, Y, Width, Height int }

// Atlas owns a row-major single-channel image and glyph rectangles. Positions
// lists only packed glyphs; callers must reject layouts missing any needed glyph.
// All data is immutable after publication and contains no native handles.
type Atlas struct {
	Pixels    []byte
	Width     int
	Height    int
	Positions map[Key]Rect
}

// RangeName formats a range label. DecodeRange additionally validates alignment
// and the supported BMP code-point interval.
func RangeName(start uint32) string {
	return fmt.Sprintf("%d-%d", start, uint64(start)+uint64(RangeSize)-1)
}

func (g Glyph) validate() error {
	if g.Width > MaxDimension || g.Height > MaxDimension || g.Advance > MaxDimension {
		return errors.New("glyph dimensions exceed metric limit")
	}
	if g.Left < MinMetric || g.Left > MaxMetric || g.Top < MinMetric || g.Top > MaxMetric {
		return errors.New("glyph bearing exceeds metric limit")
	}
	expected := 0
	if g.Width > 0 && g.Height > 0 {
		expected = int(g.Width+2*PBFBorder) * int(g.Height+2*PBFBorder)
	}
	if len(g.Bitmap) != expected {
		return fmt.Errorf("glyph %d bitmap has %d bytes, want %d", g.ID, len(g.Bitmap), expected)
	}
	return nil
}
