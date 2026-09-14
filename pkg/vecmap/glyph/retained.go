package glyph

// AtlasNeedsRebuild reports missing key coverage, not changed metrics or pixels.
// Empty requirements never need an atlas. Every supplied key is checked, including
// bitmap-free glyphs; callers normally supply drawable layout glyphs only.
func AtlasNeedsRebuild(atlas *Atlas, glyphs map[Key]Glyph) bool {
	if len(glyphs) == 0 {
		return false
	}
	if atlas == nil {
		return true
	}
	for key := range glyphs {
		if _, exists := atlas.Positions[key]; !exists {
			return true
		}
	}
	return false
}

// BuildRetainedAtlas reuses the current-only atlas size as the ceiling for keeping
// optional prior glyphs. Current values override retained values with the same
// key. If the merged atlas grows, is invalid, or misses any current key, it falls
// back to current-only packing. Current-only overflow retains BuildAtlas's partial
// prefix behavior; callers must still check complete label coverage with FitsAtlas.
//
// Invalid current data returns nil outputs and an error. Empty/bitmap-free current
// data returns nil outputs without examining retained data. Invalid optional
// retained data is discarded through current-only fallback, not returned as an
// error. Inputs are not mutated; returned atlas pixels/positions and resident map
// are owned. Resident glyph metrics are copied and immutable bitmaps are borrowed.
// Input cardinality, sorting/merge work and scheduling remain caller-bounded.
func BuildRetainedAtlas(current, retained map[Key]Glyph) (*Atlas, map[Key]Glyph, error) {
	currentAtlas, err := BuildAtlas(current)
	if err != nil || currentAtlas == nil {
		return nil, nil, err
	}
	merged := make(map[Key]Glyph, len(current)+len(retained))
	for key, glyph := range retained {
		merged[key] = glyph
	}
	for key, glyph := range current {
		merged[key] = glyph
	}
	atlas, _ := BuildAtlas(merged)
	if atlas == nil || atlas.Width > currentAtlas.Width || AtlasNeedsRebuild(atlas, current) {
		merged = current
		atlas = currentAtlas
	}
	resident := make(map[Key]Glyph, len(atlas.Positions))
	for key := range atlas.Positions {
		resident[key] = merged[key]
	}
	return atlas, resident, nil
}
