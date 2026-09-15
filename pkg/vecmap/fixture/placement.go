package fixture

import (
	"github.com/rubiojr/whereami/pkg/vecmap/glyph"
	"github.com/rubiojr/whereami/pkg/vecmap/liberty"
	"github.com/rubiojr/whereami/pkg/vecmap/placement"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
)

// The fixed camera needs only wrap zero. Keep reverse candidate collection so
// stable priority ties match the parent renderer. Layouts are metric-ready, not
// atlas-filtered: even omitted text can reserve collision space in this fixture.
func selectSymbols(camera view.Camera, symbols []placement.Symbol, layouts map[int]*glyph.PreparedLayout) (map[int]placement.Accepted, error) {
	if len(symbols) > placement.MaxSymbols {
		return nil, placement.ErrSymbolLimit
	}
	refs := make([]placement.CollisionReference[int], 0)
	transform := view.TileTransform(camera, Tile(), 0)
	for i := len(symbols) - 1; i >= 0; i-- {
		s := &symbols[i]
		projected := projectSymbol(transform, camera, s, layouts[i])
		if !projected.Text.Visible && !projected.Icon.Visible {
			continue
		}
		refs = append(refs, placement.CollisionReference[int]{Key: i, Order: s.Order, SortKey: s.SortKey, Text: projected.Text, Icon: projected.Icon})
	}
	return placement.SelectSymbols(refs, placement.CollisionOptions{Width: camera.Width, Height: camera.Height})
}

func projectSymbol(transform view.Affine, camera view.Camera, s *placement.Symbol, layout *glyph.PreparedLayout) placement.ProjectedSymbol {
	ready := layout != nil
	if !glyph.TextEligible(s.Text) {
		ready = glyph.LegacyFallbackEligible(s.Text)
	}
	context := placement.ProjectionContext{Transform: transform, Width: camera.Width, Height: camera.Height,
		TextReady: s.Text != "" && s.TextColor.Alpha > 0 && ready}
	var bounds placement.Box
	if layout != nil {
		bounds = placement.Box{Left: layout.Bounds.Left, Top: layout.Bounds.Top, Right: layout.Bounds.Right, Bottom: layout.Bounds.Bottom}
		context.TextBounds = &bounds
	}
	var metrics placement.SpriteMetrics
	if s.IconName != "" {
		if entry, ok := liberty.SpriteEntry(s.IconName); ok && entry.PixelRatio > 0 {
			metrics = placement.SpriteMetrics{Width: entry.Width, Height: entry.Height, PixelRatio: entry.PixelRatio}
			context.Sprite = &metrics
		}
	}
	return placement.ProjectSymbol(s, context)
}
