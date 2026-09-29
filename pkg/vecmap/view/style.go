package view

import "math"

// StyleZoom retains the production renderer's sixteenth-zoom preparation policy.
// Camera transforms still use the unquantized zoom; callers normalize cameras.
func StyleZoom(zoom float64) float64 { return math.Round(zoom*16) / 16 }

// StyleZoomAt is the style zoom for tiles drawn coarser zoom levels below the
// camera zoom, as VisibleTileCoverAt selects them. It stays at zero below camera
// zoom coarser, where baked widths are those of camera zoom coarser.
func StyleZoomAt(zoom float64, coarser int) float64 {
	return StyleZoom(math.Max(0, zoom-float64(max(0, min(MaxCoarser, coarser)))))
}
