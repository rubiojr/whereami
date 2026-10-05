package tiles

import (
	"math"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/placement"
	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCoverPublication(t *testing.T) {
	s := newSet(t, retained.Limits{})
	parent, _ := testTile.Parent()
	other := view.TileID{X: 3, Y: 2, Z: 3}
	targets := []view.TileID{testTile, other}
	camera := testCamera(testTile)
	require.NoError(t, s.Apply([]Change{{parent, baseFragment(0)}}))
	coarse := selectTiles(t, s, targets, nil, camera)
	assert.Equal(t, []view.TileID{parent}, coarse.Cover)
	assert.Equal(t, 1, coarse.Fallbacks)
	require.NoError(t, s.Apply([]Change{{testTile, baseFragment(0)}}))
	partial := selectTiles(t, s, targets, coarse.Cover, camera)
	assert.Equal(t, coarse.Cover, partial.Cover)
	assert.Equal(t, coarse.Scene.Meshes, partial.Scene.Meshes)
	require.NoError(t, s.Apply([]Change{{other, baseFragment(0)}}))
	refined := selectTiles(t, s, targets, coarse.Cover, camera)
	assert.Equal(t, targets, refined.Cover)
	assert.Zero(t, refined.Fallbacks)
	assert.Len(t, refined.Scene.Meshes, 2)
	// Merely generating refined does not make it eligible continuity. The caller
	// still supplies its acknowledged selection. With the parent removed, a coarse
	// request has nothing until detailed continuity is explicitly supplied.
	require.NoError(t, s.Apply([]Change{{parent, nil}}))
	without := selectTiles(t, s, []view.TileID{parent}, coarse.Cover, camera)
	assert.Empty(t, without.Cover)
	with := selectTiles(t, s, []view.TileID{parent}, refined.Cover, camera)
	assert.Equal(t, targets, with.Cover, "partial descendants avoid a completely blank target")
	assert.Equal(t, 2, with.Fallbacks)
	assert.Len(t, coarse.Scene.Draws, 1, "old snapshots survive removal")
	// A loaded blank tile is complete content, not a failed/missing response.
	require.NoError(t, s.Apply([]Change{{parent, &Fragment{Scene: &scene.Scene{}}}}))
	blank := selectTiles(t, s, []view.TileID{parent}, refined.Cover, camera)
	assert.Equal(t, []view.TileID{parent}, blank.Cover)
	assert.Empty(t, blank.Scene.Draws)
}

func TestLayerWrapComposition(t *testing.T) {
	s := newSet(t, retained.Limits{})
	root := view.TileID{}
	f := baseFragment(9, 2)
	period := [2]float64{3.1, 7.3}
	f.Scene.Draws[0].Material.Kind = scene.Pattern
	f.Scene.Draws[0].Material.PatternSize = [2]float32{float32(period[0]), float32(period[1])}
	f.Scene.Draws[0].Material.PatternPhase = [2]float32{1, 2}
	f.Draws[0].PatternPeriod = period
	require.NoError(t, s.Apply([]Change{{root, f}}))
	camera := view.NewCamera(view.Coordinate{Longitude: 179}, 0, 30, 512, 256)
	snapshot := selectTiles(t, s, []view.TileID{root}, nil, camera)
	wraps := view.WorldWraps(camera)
	require.Greater(t, len(wraps), 1)
	require.Len(t, snapshot.TileSpaces, len(wraps))
	require.Len(t, snapshot.Scene.Draws, len(wraps)*2)
	require.Len(t, snapshot.Scene.Meshes, 1, "world instances share resources")
	for i, wrap := range wraps {
		assert.Equal(t, scene.Solid, snapshot.Scene.Draws[i].Material.Kind, "lower layer across all instances first")
		draw := snapshot.Scene.Draws[i+len(wraps)]
		assert.Equal(t, i, draw.Transform)
		assert.Equal(t, [4]float32{0, 0, 256, 256}, draw.Clip)
		x, y := view.PatternPhase(root, wrap, period[0], period[1])
		assert.Equal(t, [2]float32{float32(x), float32(y)}, draw.Material.PatternPhase)
		assert.Equal(t, scene.TileSpace{Tile: root, Wrap: wrap}, snapshot.TileSpaces[i])
	}
	assert.Equal(t, [2]float32{1, 2}, f.Scene.Draws[0].Material.PatternPhase, "phase patching only changes snapshot metadata")
	moved := camera.Panned(20, -10)
	assert.Same(t, snapshot, selectTiles(t, s, []view.TileID{root}, nil, moved))
	frame := snapshot.Frame(moved)
	assert.Same(t, snapshot.Scene, frame.Scene)
	for i, space := range snapshot.TileSpaces {
		want := view.TileTransform(moved, space.Tile, space.Wrap)
		assert.Equal(t, float32(want.DX), frame.Transforms[i].DX)
		assert.Equal(t, float32(want.DY), frame.Transforms[i].DY)
	}
	assert.NotEqual(t, snapshot.Frame(camera).Transforms, frame.Transforms)
}

func TestLayerTileOrder(t *testing.T) {
	s := newSet(t, retained.Limits{})
	other := view.TileID{X: 3, Y: 2, Z: 3}
	require.NoError(t, s.Apply([]Change{{testTile, baseFragment(2, 0, 2)}, {other, baseFragment(0, 2)}}))
	snapshot := selectTiles(t, s, []view.TileID{other, testTile}, nil, testCamera(testTile))
	require.Len(t, snapshot.Scene.Draws, 5)
	var slots []int
	for _, draw := range snapshot.Scene.Draws {
		slots = append(slots, draw.Transform)
	}
	assert.Equal(t, []int{0, 1, 0, 1, 1}, slots, "layer, selected tile, wrap, original draw order")
	assert.Equal(t, snapshot.Scene.Draws[1].Mesh, snapshot.Scene.Draws[3].Mesh)
	assert.NotEqual(t, snapshot.Scene.Draws[0].Mesh, snapshot.Scene.Draws[1].Mesh)
}

func symbolFragment(anchor geometry.Point, order int, count int) *Fragment {
	f := baseFragment()
	for range count {
		f.Symbols = append(f.Symbols, Symbol{Candidate: placement.Symbol{Order: order, Anchor: anchor, Text: "label", TextSize: 16, TextColor: style.Color{Alpha: 255}, ViewportAligned: true}, TextReady: true, HasTextBounds: true, TextBounds: placement.Box{Left: -10, Top: -10, Right: 10, Bottom: 10}})
	}
	for _, kind := range []scene.Kind{scene.SDFHalo, scene.SDFFill} {
		for i := range count {
			f.Scene.Draws = append(f.Scene.Draws, scene.Draw{Mesh: 7, Count: 3, Material: scene.Material{Kind: kind, Color: [4]float32{1, 1, 1, 1}, FontScale: 1}})
			f.Draws = append(f.Draws, Draw{Layer: order, Part: Text, Candidate: i})
		}
	}
	return f
}

func TestCrossTilePlacement(t *testing.T) {
	s := newSet(t, retained.Limits{})
	other := view.TileID{X: 3, Y: 2, Z: 3}
	left := symbolFragment(geometry.Point{X: 256, Y: 128}, 5, 2)
	right := symbolFragment(geometry.Point{X: 0, Y: 128}, 5, 1)
	require.NoError(t, s.Apply([]Change{{testTile, left}, {other, right}}))
	camera := view.NewCamera(view.TileCoordinate(testTile, view.ScreenPoint{X: 256, Y: 128}), 3, 0, 512, 256)
	targets := []view.TileID{testTile, other}
	snapshot := selectTiles(t, s, targets, nil, camera)
	assert.False(t, snapshot.Accepted[SymbolKey{testTile, 0, 0}].Text)
	assert.True(t, snapshot.Accepted[SymbolKey{testTile, 0, 1}].Text, "reverse candidate order wins ties")
	assert.False(t, snapshot.Accepted[SymbolKey{other, 0, 0}].Text, "selected tile order breaks cross-tile ties")
	require.Len(t, snapshot.Scene.Draws, 2)
	assert.Equal(t, scene.SDFHalo, snapshot.Scene.Draws[0].Material.Kind)
	assert.Equal(t, scene.SDFFill, snapshot.Scene.Draws[1].Material.Kind)
	assert.Same(t, snapshot, selectTiles(t, s, targets, nil, camera.Panned(1, 0)))
	_, err := s.Select(targets, nil, camera, 1)
	assert.ErrorIs(t, err, placement.ErrCollisionLimit)
	assert.Same(t, snapshot, s.cached, "failed collision never publishes partial acceptance")
	// A metric-ready higher-priority candidate can reserve space even without
	// packed glyph draws. This preserves the existing readiness/atlas distinction.
	right.Symbols[0].Candidate.Order = 6
	right.Scene.Draws, right.Draws = nil, nil
	require.NoError(t, s.Apply([]Change{{other, right}}))
	reserved := selectTiles(t, s, targets, nil, camera)
	assert.True(t, reserved.Accepted[SymbolKey{other, 0, 0}].Text)
	assert.Empty(t, reserved.Scene.Draws)
	// Placement changes do not rebuild retained payloads. Pan both labels offscreen.
	offscreen := selectTiles(t, s, targets, nil, camera.Panned(0, 1000))
	assert.False(t, offscreen.Accepted[SymbolKey{other, 0, 0}].Text)
	assert.Empty(t, offscreen.Scene.Draws)
	assert.Len(t, snapshot.Scene.Draws, 2)
}

func TestIconReadiness(t *testing.T) {
	s := newSet(t, retained.Limits{})
	f := baseFragment()
	f.Symbols = []Symbol{{Candidate: placement.Symbol{Order: 1, Anchor: geometry.Point{X: 128, Y: 128}, IconName: "icon", IconSize: 1, IconOptional: true}, HasSprite: true, Sprite: placement.SpriteMetrics{Width: 16, Height: 16, PixelRatio: 1}}}
	f.Scene.Draws = []scene.Draw{{Mesh: 7, Count: 3, Material: scene.Material{Kind: scene.Image, Color: [4]float32{1, 1, 1, 1}}}}
	f.Draws = []Draw{{Layer: 1, Part: Icon}}
	require.NoError(t, s.Apply([]Change{{testTile, f}}))
	f.Symbols[0].Sprite.Width = 900 // Set owns the metric value
	snapshot := selectTiles(t, s, []view.TileID{testTile}, nil, testCamera(testTile))
	assert.True(t, snapshot.Accepted[SymbolKey{testTile, 0, 0}].Icon)
	assert.Len(t, snapshot.Scene.Draws, 1)
	f.Symbols[0].HasSprite = false
	require.NoError(t, s.Apply([]Change{{testTile, f}}))
	snapshot = selectTiles(t, s, []view.TileID{testTile}, nil, testCamera(testTile))
	assert.False(t, snapshot.Accepted[SymbolKey{testTile, 0, 0}].Icon)
	assert.Empty(t, snapshot.Scene.Draws)
}

func TestCompositionLimits(t *testing.T) {
	s := newSet(t, retained.Limits{})
	root := view.TileID{}
	parent, _ := testTile.Parent()
	camera := testCamera(testTile)
	for _, pair := range []struct{ targets, previous []view.TileID }{
		{targets: make([]view.TileID, MaxTiles+1)}, {previous: make([]view.TileID, MaxTiles+1)},
		{targets: []view.TileID{testTile, testTile}}, {targets: []view.TileID{testTile, parent}},
		{targets: []view.TileID{{Z: 15}}}, {previous: []view.TileID{parent, testTile}},
		{previous: []view.TileID{{Z: 15}}},
	} {
		got, err := s.Select(pair.targets, pair.previous, camera, 0)
		assert.Error(t, err)
		assert.Nil(t, got)
	}
	for _, dimension := range []float64{0, -1, math.NaN(), math.Inf(1), placement.MaxCollisionViewport + 1} {
		camera.Width = dimension
		_, err := s.Select(nil, nil, camera, 0)
		assert.ErrorIs(t, err, ErrInput)
	}
	camera = testCamera(root)
	empty := selectTiles(t, s, nil, nil, camera)
	assert.Empty(t, empty.Scene.Draws)
	assert.Empty(t, empty.Frame(camera).Transforms)
	_, err := s.Select(nil, nil, camera, -1)
	assert.ErrorIs(t, err, placement.ErrCollisionLimit)
	one := newSet(t, retained.Limits{Draws: 1})
	require.NoError(t, one.Apply([]Change{{root, baseFragment(0)}}))
	_, err = one.Select([]view.TileID{root}, nil, camera, 0)
	assert.ErrorIs(t, err, ErrLimit, "wraps expand draw count before filtering")
	f := baseFragment(0)
	f.Symbols = make([]Symbol, placement.MaxSymbols)
	require.NoError(t, s.Apply([]Change{{root, f}}))
	camera.Width, camera.Height = 4096, 4096
	_, err = s.Select([]view.TileID{root}, nil, camera, 0)
	assert.ErrorIs(t, err, ErrLimit, "reference count rejects before projection")
}
