package tiles

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOwnedBuildPreservesRenderingAndBoundsCopies(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		p, err := Prepare(preparePBF(), prepareStyle(t), PrepareOptions{Tile: testTile, Zoom: 3, Indexed: indexed})
		require.NoError(t, err)
		assets := prepareAssets()
		borrowed, err := p.Build(assets)
		require.NoError(t, err)
		owned, err := p.BuildOwned(assets, 1<<20)
		require.NoError(t, err)
		assert.Equal(t, borrowed, owned)
		// Glyph atlas is first; the other textures came from the sprite callback.
		require.Greater(t, len(owned.Fragment.Scene.Textures), 1)
		for i := 1; i < len(owned.Fragment.Scene.Textures); i++ {
			old, next := borrowed.Fragment.Scene.Textures[i].RGBA, owned.Fragment.Scene.Textures[i].RGBA
			assert.NotSame(t, &old[0], &next[0])
			assert.Equal(t, len(next), cap(next))
		}
		clear(borrowed.Fragment.Scene.Textures[1].RGBA)
		assert.NotEqual(t, borrowed.Fragment.Scene.Textures[1].RGBA, owned.Fragment.Scene.Textures[1].RGBA)
		for _, limit := range []uint64{0, 1, (1 << 30) + 1} {
			out, err := p.BuildOwned(assets, limit)
			assert.ErrorIs(t, err, ErrLimit)
			assert.Nil(t, out)
		}
	}
}
