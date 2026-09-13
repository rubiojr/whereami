package placement

import (
	"math"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func projectionContext() ProjectionContext {
	return ProjectionContext{Transform: view.Affine{M11: 1, M22: 1}, Width: 512, Height: 512, TextReady: true}
}

func TestProjectSymbolUsesGlyphBoundsAndScreenPixelOffsets(t *testing.T) {
	symbol := Symbol{Anchor: geometry.Point{X: 100, Y: 100}, Text: "label", TextColor: style.Color{Alpha: 255}, TextSize: 10,
		TextOffset: geometry.Point{X: 1, Y: 2}, TextPadding: 2, TextAllowsOverlap: true, TextOptional: true,
		HaloWidth: 999, HaloBlur: 999} // Bounds already include halo; do not add it twice.
	bounds := Box{Left: -5, Top: -6, Right: 7, Bottom: 8}
	context := projectionContext()
	context.TextBounds = &bounds
	projected := ProjectSymbol(&symbol, context)
	assert.Equal(t, CollisionPart{Box: Box{Left: 103, Top: 112, Right: 119, Bottom: 130}, Present: true, Visible: true, AllowsOverlap: true, Optional: true}, projected.Text)
	assert.Equal(t, CollisionPart{}, projected.Icon)
	context.Transform.M11, context.Transform.M22 = 2, 3
	scaled := ProjectSymbol(&symbol, context)
	assert.Equal(t, Box{Left: 203, Top: 312, Right: 219, Bottom: 330}, scaled.Text.Box)
	assert.Equal(t, projected.Text.Box.Right-projected.Text.Box.Left, scaled.Text.Box.Right-scaled.Text.Box.Left)
	bounds.Left = 999
	symbol.Anchor.X = 999
	assert.Equal(t, 103.0, projected.Text.Box.Left)
}

func TestProjectSymbolReadinessAndPresence(t *testing.T) {
	context := projectionContext()
	bounds := Box{Right: 10, Bottom: 10}
	context.TextBounds = &bounds
	context.TextReady = false
	symbol := Symbol{Text: "label", TextColor: style.Color{Alpha: 255}, TextSize: 10, TextOptional: true, TextAllowsOverlap: true}
	projected := ProjectSymbol(&symbol, context)
	assert.True(t, projected.Text.Present)
	assert.False(t, projected.Text.Visible)
	assert.Equal(t, Box{}, projected.Text.Box)
	assert.True(t, projected.Text.Optional)
	assert.True(t, projected.Text.AllowsOverlap)
	context.TextReady = true
	for _, alpha := range []int{0, -1} {
		symbol.TextColor.Alpha = alpha
		assert.False(t, ProjectSymbol(&symbol, context).Text.Present)
	}
	symbol.TextColor.Alpha = 255
	symbol.Text = ""
	assert.False(t, ProjectSymbol(&symbol, context).Text.Present)
	assert.Equal(t, ProjectedSymbol{}, ProjectSymbol(nil, context))
}

func TestProjectSymbolFallbackEstimateAndLargeWrapCount(t *testing.T) {
	symbol := Symbol{Anchor: geometry.Point{X: 100, Y: 100}, Text: "ab\ncd", TextColor: style.Color{Alpha: 255},
		TextSize: 10, LineHeight: 1.2, MaximumWidth: 2, HaloWidth: 1, HaloBlur: 2, TextAnchor: "top-left"}
	projected := ProjectSymbol(&symbol, projectionContext())
	assert.Equal(t, Box{Left: 100, Top: 100, Right: 126, Bottom: 130}, projected.Text.Box)
	assert.True(t, projected.Text.Visible)
	symbol.Text = "ab"
	symbol.MaximumWidth, symbol.HaloWidth, symbol.HaloBlur = 0, 0, 0
	symbol.TextAnchor = "center"
	projected = ProjectSymbol(&symbol, projectionContext())
	assert.InDelta(t, 94.2, projected.Text.Box.Left, 1e-12)
	assert.InDelta(t, 105.8, projected.Text.Box.Right, 1e-12)
	assert.Equal(t, 94.0, projected.Text.Box.Top)
	assert.Equal(t, 106.0, projected.Text.Box.Bottom)
	// The old float-to-int wrap count overflowed here on both 386 and amd64.
	symbol.Text, symbol.MaximumWidth, symbol.LineHeight = "ا", 1e-20, 1
	box := fallbackTextBounds(&symbol)
	require.True(t, finite(box.Top) && finite(box.Bottom))
	assert.Greater(t, box.Bottom-box.Top, 1e20)
	assert.InDelta(t, 5.8e20, box.Bottom-box.Top, 1e7)
}

func TestProjectSymbolSpriteMetricsAndAvailability(t *testing.T) {
	symbol := Symbol{Anchor: geometry.Point{X: 100, Y: 100}, IconName: "airport", IconSize: 2,
		IconAnchor: "bottom-right", IconOffset: geometry.Point{X: 3, Y: 4}, IconPadding: 1,
		IconAllowsOverlap: true, IconOptional: true, IconOpacity: 0, IconColor: style.Color{}}
	context := projectionContext()
	metrics := SpriteMetrics{Width: 20, Height: 10, PixelRatio: 2}
	context.Sprite = &metrics
	projected := ProjectSymbol(&symbol, context)
	assert.Equal(t, CollisionPart{Box: Box{Left: 85, Top: 97, Right: 107, Bottom: 109}, Present: true, Visible: true, AllowsOverlap: true, Optional: true}, projected.Icon)
	// Preserve collision presence regardless of icon opacity/color alpha.
	metrics.Width = 100
	assert.Equal(t, 85.0, projected.Icon.Box.Left)
	for _, ratio := range []float64{0, -1, math.NaN()} {
		metrics.PixelRatio = ratio
		assert.False(t, ProjectSymbol(&symbol, context).Icon.Present)
	}
	context.Sprite = nil
	assert.False(t, ProjectSymbol(&symbol, context).Icon.Present)
	context.Sprite = &SpriteMetrics{PixelRatio: 1}
	assert.True(t, ProjectSymbol(&symbol, context).Icon.Present) // Empty dimensions retain prior behavior.
	symbol.IconName = ""
	assert.False(t, ProjectSymbol(&symbol, context).Icon.Present)
}

func TestProjectionAnglesAnchorsAndPadding(t *testing.T) {
	transform := view.Affine{M12: -2, M21: 3, DX: 100, DY: 200}
	assert.Equal(t, math.Pi/2, ScreenSymbolAngle(transform, 0, 0, false))
	assert.InDelta(t, math.Atan2(3, -2), ScreenSymbolAngle(transform, math.Pi/4, 0, false), 1e-12)
	assert.Equal(t, 0.25, ScreenSymbolAngle(transform, 7, 0.25, true))
	assert.Equal(t, 0.75, RenderedSymbolAngle(0.5, 0.25, false))
	assert.Equal(t, 0.25, RenderedSymbolAngle(0.5, 0.25, true))
	box := RotatedBox(geometry.Point{X: 100, Y: 100}, Box{Left: -5, Top: -2, Right: 5, Bottom: 2}, math.Pi/2, 3)
	assert.InDelta(t, 95, box.Left, 1e-12)
	assert.InDelta(t, 92, box.Top, 1e-12)
	assert.InDelta(t, 105, box.Right, 1e-12)
	assert.InDelta(t, 108, box.Bottom, 1e-12)
	assert.Equal(t, Box{Left: 3, Top: 3, Right: 1, Bottom: 1}, RotatedBox(geometry.Point{}, Box{Right: 4, Bottom: 4}, 0, -3))
	for _, tt := range []struct {
		anchor string
		x, y   float64
	}{
		{"center", -5, -10}, {"top-left", 0, 0}, {"bottom-right", -10, -20}, {"unknown", -5, -10}, {"bottom-top-right-left", 0, 0},
	} {
		x, y := AnchoredOrigin(tt.anchor, 10, 20)
		assert.Equal(t, tt.x, x)
		assert.Equal(t, tt.y, y)
	}
	x, _ := AnchoredOrigin("center", 0, 0)
	assert.True(t, math.Signbit(x))
	x, _ = AnchoredOrigin("left", 0, 0)
	assert.False(t, math.Signbit(x))
	assert.True(t, (Box{Left: -10, Top: -10, Right: 0, Bottom: 0}).Visible(100, 100))
	assert.True(t, (Box{Left: 100, Top: 100, Right: 110, Bottom: 110}).Visible(100, 100))
	assert.False(t, (Box{Left: 101, Right: 110, Bottom: 10}).Visible(100, 100))
	assert.False(t, (Box{Top: 101, Right: 10, Bottom: 110}).Visible(100, 100))
}

func TestProjectionFeedsHeadlessCollisionReadiness(t *testing.T) {
	symbol := Symbol{Text: "waiting", TextColor: style.Color{Alpha: 255}, IconName: "airport", IconSize: 1}
	context := projectionContext()
	context.TextReady = false
	context.Sprite = &SpriteMetrics{Width: 10, Height: 10, PixelRatio: 1}
	projected := ProjectSymbol(&symbol, context)
	refs := []CollisionReference[int]{{Key: 1, Text: projected.Text, Icon: projected.Icon}}
	accepted, err := SelectSymbols(refs, CollisionOptions{Width: 512, Height: 512})
	require.NoError(t, err)
	assert.Empty(t, accepted)
	symbol.TextOptional = true
	projected = ProjectSymbol(&symbol, context)
	refs[0].Text, refs[0].Icon = projected.Text, projected.Icon
	accepted, err = SelectSymbols(refs, CollisionOptions{Width: 512, Height: 512})
	require.NoError(t, err)
	assert.Equal(t, map[int]Accepted{1: {Icon: true}}, accepted)
}

func FuzzProjectSymbol(f *testing.F) {
	f.Add(0.0, 100.0, 100.0, 16.0)
	f.Add(math.Pi/2, -50.0, 100.0, 8.0)
	f.Fuzz(func(t *testing.T, angle, x, y, size float64) {
		symbol := Symbol{Anchor: geometry.Point{X: x, Y: y}, Text: "label", TextColor: style.Color{Alpha: 255}, TextSize: size,
			LineHeight: 1.2, MaximumWidth: 10, TextRotate: angle, IconName: "icon", IconSize: size, IconRotate: angle}
		context := projectionContext()
		context.Sprite = &SpriteMetrics{Width: 20, Height: 10, PixelRatio: 2}
		projected := ProjectSymbol(&symbol, context)
		accepted, err := SelectSymbols([]CollisionReference[int]{{Key: 1, Text: projected.Text, Icon: projected.Icon}}, CollisionOptions{Width: 512, Height: 512, WorkLimit: 128})
		if err != nil {
			require.Nil(t, accepted)
			return
		}
		require.LessOrEqual(t, len(accepted), 1)
	})
}
