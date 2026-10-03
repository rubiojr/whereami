package glyph

import "github.com/rubiojr/whereami/pkg/vecmap/geometry"

const MaxPreparedLayouts = 10_000

// PreparedLayout combines metric layout with its atlas-dependent mesh. Build it
// privately, then treat it and all reachable buffers as immutable after publication.
// Metric glyph bitmaps remain borrowed, as in TextLayout.
type PreparedLayout struct {
	TextLayout
	LayoutMesh
}

// PrepareLayouts attaches new meshes in place and returns an owned map containing
// only complete, drawable layouts, preserving caller-defined keys and pointers.
// The input map itself is not modified. Missing atlas coverage and nil layouts are
// skipped without changing their previous meshes. Successful preparation replaces
// the entire LayoutMesh, including clearing the other representation's buffers.
//
// Empty input or nil atlas returns nil. Otherwise the map is capped at
// MaxPreparedLayouts before allocation. Any mesh error returns nil output, but
// earlier successful attachments remain: map iteration order is unspecified.
// A layout shared by several keys is prepared once and returned for each.
// Callers must provide unpublished layouts with exclusive access during the call.
// An errored layout retains its prior mesh. This matches the original scene
// preparation's mutation policy, not an atomic transaction on input values.
func PrepareLayouts[K comparable](layouts map[K]*PreparedLayout, atlas *Atlas, indexed bool) (map[K]*PreparedLayout, error) {
	return prepareLayouts(layouts, atlas, indexed, BuildLayoutMesh)
}

// PrepareUnitLayouts is PrepareLayouts with BuildUnitLayoutMesh: meshes are
// independent of the evaluated text size and marked Unit. Metric layout, bounds
// and Scale are untouched, so collision input is the same as with PrepareLayouts.
func PrepareUnitLayouts[K comparable](layouts map[K]*PreparedLayout, atlas *Atlas, indexed bool) (map[K]*PreparedLayout, error) {
	return prepareLayouts(layouts, atlas, indexed, BuildUnitLayoutMesh)
}

func prepareLayouts[K comparable](layouts map[K]*PreparedLayout, atlas *Atlas, indexed bool, build func(*TextLayout, *Atlas, bool) (LayoutMesh, error)) (map[K]*PreparedLayout, error) {
	if len(layouts) == 0 || atlas == nil {
		return nil, nil
	}
	if len(layouts) > MaxPreparedLayouts {
		return nil, geometry.ErrGeometryLimit
	}
	renderable := make(map[K]*PreparedLayout, len(layouts))
	// Keys can share a layout, as identical text requests do; prepare it once.
	ready := make(map[*PreparedLayout]bool, len(layouts))
	for key, layout := range layouts {
		if layout == nil {
			continue
		}
		drawable, done := ready[layout]
		if !done {
			if drawable = FitsAtlas(&layout.TextLayout, atlas); drawable {
				mesh, err := build(&layout.TextLayout, atlas, indexed)
				if err != nil {
					return nil, err
				}
				layout.LayoutMesh = mesh
				drawable = len(mesh.Expanded) != 0 || len(mesh.Indices) != 0
			}
			ready[layout] = drawable
		}
		if drawable {
			renderable[key] = layout
		}
	}
	if len(renderable) == 0 {
		return nil, nil
	}
	return renderable, nil
}

// LayoutGlyphs collects drawable glyphs into an owned map, borrowing immutable
// bitmaps and copying metrics. Nil layouts and bitmap-free glyphs are omitted.
// Repeated keys must describe the same glyph; otherwise the selected value follows
// unspecified map traversal order, as in the original collector. Inputs are neither
// mutated nor retained as maps. Input cardinality/work remains caller-bounded.
func LayoutGlyphs[K comparable](layouts map[K]*PreparedLayout) map[Key]Glyph {
	glyphs := make(map[Key]Glyph)
	for _, layout := range layouts {
		if layout == nil {
			continue
		}
		for _, positioned := range layout.Glyphs {
			if len(positioned.Glyph.Bitmap) > 0 {
				glyphs[positioned.Key] = positioned.Glyph
			}
		}
	}
	return glyphs
}
