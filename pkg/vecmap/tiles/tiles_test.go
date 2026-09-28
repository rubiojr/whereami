package tiles

import (
	"math"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/placement"
	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testTile = view.TileID{X: 2, Y: 2, Z: 3}

func newSet(t *testing.T, limits retained.Limits) *Set {
	t.Helper()
	s, err := New(limits)
	require.NoError(t, err)
	return s
}

func baseFragment(layers ...int) *Fragment {
	f := &Fragment{Scene: &scene.Scene{Meshes: []scene.Mesh{{ID: 7, Vertices: []scene.Vertex{{}, {X: 256}, {Y: 256}}}}}}
	for _, layer := range layers {
		f.Scene.Draws = append(f.Scene.Draws, scene.Draw{Mesh: 7, Count: 3, Clip: [4]float32{0, 0, 256, 256}, Material: scene.Material{Color: [4]float32{1, 1, 1, 1}}})
		f.Draws = append(f.Draws, Draw{Layer: layer})
	}
	return f
}

func testCamera(tile view.TileID) view.Camera {
	return view.NewCamera(view.TileCoordinate(tile, view.ScreenPoint{X: 128, Y: 128}), float64(tile.Z), 0, 256, 256)
}

func selectTiles(t *testing.T, s *Set, targets, previous []view.TileID, camera view.Camera) *Snapshot {
	t.Helper()
	snapshot, err := s.Select(targets, previous, camera, 0)
	require.NoError(t, err)
	require.NoError(t, snapshot.Scene.Validate())
	return snapshot
}

func TestTileUpdates(t *testing.T) {
	s := newSet(t, retained.Limits{Fragments: 1, Bytes: 72})
	f := baseFragment(1)
	require.NoError(t, s.Apply([]Change{{testTile, f}}))
	first := selectTiles(t, s, []view.TileID{testTile}, nil, testCamera(testTile))
	assert.Equal(t, uint64(1), first.Scene.Meshes[0].Revision)
	assert.Same(t, &f.Scene.Meshes[0].Vertices[0], &first.Scene.Meshes[0].Vertices[0], "geometry is borrowed")
	// Metadata is owned, unlike the payload. Altering caller metadata doesn't
	// change the retained fragment or a published snapshot.
	f.Draws[0].Layer = 99
	f.Scene.Draws[0].Material.Color = [4]float32{0, 0, 0, 1}
	assert.Equal(t, 1, s.tiles[testTile].draws[0].Layer)
	assert.Equal(t, [4]float32{1, 1, 1, 1}, first.Scene.Draws[0].Material.Color)
	require.NoError(t, s.Apply(nil))
	assert.Same(t, first, selectTiles(t, s, []view.TileID{testTile}, nil, testCamera(testTile).Panned(1, 0)))
	tooLarge := baseFragment(1)
	tooLarge.Scene.Meshes[0].Vertices = append(tooLarge.Scene.Meshes[0].Vertices, scene.Vertex{})
	assert.ErrorIs(t, s.Apply([]Change{{testTile, tooLarge}}), retained.ErrLimit)
	assert.Same(t, first, selectTiles(t, s, []view.TileID{testTile}, nil, testCamera(testTile)))
	require.NoError(t, s.Apply([]Change{{testTile, baseFragment(1)}}))
	same := selectTiles(t, s, []view.TileID{testTile}, nil, testCamera(testTile))
	assert.NotSame(t, first, same, "replacement invalidates the selection cache")
	assert.Equal(t, first.Scene.Meshes[0], same.Scene.Meshes[0], "byte-identical geometry keeps its resident version")
	assert.Equal(t, uint64(1), s.ReusedVersions())
	moved := baseFragment(1)
	moved.Scene.Meshes[0].Vertices[1].X = 128
	require.NoError(t, s.Apply([]Change{{testTile, moved}}))
	second := selectTiles(t, s, []view.TileID{testTile}, nil, testCamera(testTile))
	assert.Equal(t, first.Scene.Meshes[0].ID, second.Scene.Meshes[0].ID)
	assert.Equal(t, uint64(3), second.Scene.Meshes[0].Revision, "changed payload takes the fragment's replacement generation")
	assert.Equal(t, uint64(1), first.Scene.Meshes[0].Revision)
	other := view.TileID{X: 3, Y: 2, Z: 3}
	// Full-capacity replacement admits inserts and removals in either order.
	require.NoError(t, s.Apply([]Change{{other, baseFragment(1)}, {testTile, nil}}))
	third := selectTiles(t, s, []view.TileID{other}, nil, testCamera(other))
	assert.NotEqual(t, first.Scene.Meshes[0].ID, third.Scene.Meshes[0].ID)
	require.NoError(t, s.Apply([]Change{{testTile, nil}}))
	assert.Len(t, third.Scene.Draws, 1)
}

func TestTileInputRejection(t *testing.T) {
	var zero Set
	assert.ErrorIs(t, zero.Apply(nil), ErrInput)
	_, err := zero.Select(nil, nil, testCamera(testTile), 0)
	assert.ErrorIs(t, err, ErrInput)
	_, err = New(retained.Limits{Fragments: -1})
	assert.ErrorIs(t, err, retained.ErrLimit)
	s := newSet(t, retained.Limits{})
	for _, mutate := range []func(*Fragment){
		func(f *Fragment) { f.Scene = nil },
		func(f *Fragment) { f.Draws = nil },
		func(f *Fragment) { f.Draws[0].Layer = -1 },
		func(f *Fragment) { f.Draws[0].Part = 99 },
		func(f *Fragment) { f.Scene.Draws[0].Transform = 1 },
		func(f *Fragment) { f.Scene.Draws[0].Clip = [4]float32{} },
		func(f *Fragment) { f.Scene.Draws[0].Clip[0] = -1 },
		func(f *Fragment) { f.Draws[0].Part = Text },
		func(f *Fragment) { f.Scene.Draws[0].Material.Kind = scene.SDFFill },
		func(f *Fragment) { f.Draws[0].PatternPeriod = [2]float64{1, 1} },
		func(f *Fragment) { f.Scene.Draws[0].Material.Kind = scene.Pattern },
		func(f *Fragment) {
			f.Scene.Draws[0].Material.Kind = scene.Pattern
			f.Scene.Draws[0].Material.PatternSize = [2]float32{1, 1}
			f.Draws[0].PatternPeriod = [2]float64{math.Inf(1), 1}
		},
		func(f *Fragment) {
			f.Draws[0].Part = Icon
			f.Symbols = []Symbol{{Candidate: placement.Symbol{Order: 1}}}
		},
		func(f *Fragment) { f.Scene.Meshes[0].Vertices[0].X = float32(math.NaN()) },
	} {
		f := baseFragment(1)
		mutate(f)
		assert.Error(t, s.Apply([]Change{{testTile, f}}))
		assert.Empty(t, s.tiles)
	}
	assert.ErrorIs(t, s.Apply([]Change{{view.TileID{Z: 15}, nil}}), ErrInput)
	assert.ErrorIs(t, s.Apply([]Change{{view.TileID{X: 8, Z: 3}, nil}}), ErrInput)
	assert.ErrorIs(t, s.Apply([]Change{{testTile, nil}, {testTile, nil}}), ErrInput)
	assert.ErrorIs(t, s.Apply(make([]Change, 2*MaxTiles+1)), ErrLimit)
	one := newSet(t, retained.Limits{Draws: 1, Fragments: 1})
	assert.ErrorIs(t, one.Apply([]Change{{testTile, baseFragment(0, 1)}}), ErrLimit)
	assert.ErrorIs(t, one.Apply([]Change{{testTile, baseFragment(0)}, {view.TileID{X: 3, Y: 2, Z: 3}, baseFragment(0)}}), ErrLimit)
	f := baseFragment(1)
	f.Symbols = make([]Symbol, placement.MaxSymbols+1)
	assert.ErrorIs(t, s.Apply([]Change{{testTile, f}}), ErrLimit)
	f.Symbols = f.Symbols[:placement.MaxSymbols]
	changes := make([]Change, 11)
	for i := range changes {
		changes[i] = Change{view.TileID{X: uint32(i), Z: 4}, f}
	}
	assert.ErrorIs(t, s.Apply(changes), ErrLimit, "aggregate candidate budget rejects before metadata copies")
}
