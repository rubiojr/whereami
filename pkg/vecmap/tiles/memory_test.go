package tiles

import (
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCPUStorageChargeAndSelectionAdmission(t *testing.T) {
	assert.Zero(t, (*Prepared)(nil).RetainedBytes())
	assert.Zero(t, (*Fragment)(nil).RetainedBytes())
	assert.Zero(t, (*Snapshot)(nil).RetainedBytes())
	f := baseFragment(0)
	before := f.RetainedBytes()
	// A short slice does not hide a large retained backing array.
	f.Scene.Meshes[0].Vertices = make([]scene.Vertex, 3, 100)
	assert.Equal(t, before+97*24, f.RetainedBytes())
	p, err := Prepare(preparePBF(), prepareStyle(t), PrepareOptions{Tile: testTile, Zoom: 3, Indexed: true})
	require.NoError(t, err)
	charge := p.RetainedBytes()
	require.Greater(t, charge, uint64(0))
	built, err := p.Build(prepareAssets())
	require.NoError(t, err)
	assert.Equal(t, charge, p.RetainedBytes(), "Build cannot change prepared storage")
	assert.Greater(t, built.Fragment.RetainedBytes(), uint64(len(built.Fragment.Scene.Textures[0].RGBA)))
	s := newSet(t, retained.Limits{})
	require.NoError(t, s.Apply([]Change{{testTile, built.Fragment}}))
	camera := testCamera(testTile)
	blank := selectTiles(t, s, nil, nil, camera)
	_, err = s.SelectBounded([]view.TileID{testTile}, nil, camera, 0, 1)
	require.ErrorIs(t, err, ErrLimit)
	assert.Same(t, blank, s.cached, "failed admission cannot retain an oversized new snapshot")
	full := selectTiles(t, s, []view.TileID{testTile}, nil, camera)
	_, err = s.SelectBounded([]view.TileID{testTile}, nil, camera, 0, full.RetainedBytes()-1)
	require.ErrorIs(t, err, ErrLimit)
	assert.Same(t, full, s.cached)
	got, err := s.SelectBounded([]view.TileID{testTile}, nil, camera, 0, full.RetainedBytes())
	require.NoError(t, err)
	assert.Same(t, full, got)
	_, err = s.SelectBounded(nil, nil, camera, 0, 0)
	assert.ErrorIs(t, err, ErrLimit)
}
