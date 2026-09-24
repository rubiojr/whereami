package view

import "math"

// StyleZoom retains the production renderer's sixteenth-zoom preparation policy.
// Camera transforms still use the unquantized zoom; callers normalize cameras.
func StyleZoom(zoom float64) float64 { return math.Round(zoom*16) / 16 }
