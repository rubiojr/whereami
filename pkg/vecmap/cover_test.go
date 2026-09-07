package vecmap

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVisibleTileCoverIncludesCenterAndPrefetchRing(t *testing.T) {
	camera := NewCamera(
		Coordinate{Latitude: 40.4168, Longitude: -3.7038},
		9,
		0,
		256,
		256,
	)

	cover := visibleTileCover(camera)

	require.NotEmpty(t, cover)
	assert.Equal(t, pinnedTile, cover[0])
	assert.Contains(t, cover, pinnedTile)
	assert.LessOrEqual(t, len(cover), maximumCoverSide*maximumCoverSide)
	assertUniqueValidTileIDs(t, cover)
}

func TestVisibleTileCoverWrapsAtAntimeridian(t *testing.T) {
	camera := NewCamera(Coordinate{Latitude: 0, Longitude: 179.9}, 2, 0, 256, 256)

	cover := visibleTileCover(camera)

	assert.Contains(t, cover, vectorTileID{X: 0, Y: 2, Z: 2})
	assert.Contains(t, cover, vectorTileID{X: 3, Y: 2, Z: 2})
	assertUniqueValidTileIDs(t, cover)
}

func TestVisibleTileCoverBoundsLargeViewportAndSourceZoom(t *testing.T) {
	camera := NewCamera(Coordinate{}, 20, 33, 1_000_000, 1_000_000)

	cover := visibleTileCover(camera)

	require.NotEmpty(t, cover)
	assert.LessOrEqual(t, len(cover), maximumCoverSide*maximumCoverSide)
	for _, tile := range cover {
		assert.Equal(t, uint32(maximumSourceZoom), tile.Z)
	}
	assertUniqueValidTileIDs(t, cover)
}

func TestVisibleTileCoverRequiresViewport(t *testing.T) {
	assert.Empty(t, visibleTileCover(NewCamera(Coordinate{}, 9, 0, 0, 100)))
	assert.Empty(t, visibleTileCover(NewCamera(Coordinate{}, 9, 0, 100, 0)))
}

func TestTileHierarchyWrapsAtAntimeridian(t *testing.T) {
	west := vectorTileID{X: 0, Y: 2, Z: 3}
	east := vectorTileID{X: 7, Y: 2, Z: 3}

	westParent, exists := west.parent()
	assert.True(t, exists)
	assert.Equal(t, vectorTileID{X: 0, Y: 1, Z: 2}, westParent)
	eastParent, exists := east.parent()
	assert.True(t, exists)
	assert.Equal(t, vectorTileID{X: 3, Y: 1, Z: 2}, eastParent)
	assert.True(t, tileContains(westParent, west))
	assert.True(t, tileContains(eastParent, east))
	assert.False(t, tilesOverlap(westParent, eastParent))
	_, exists = (vectorTileID{}).parent()
	assert.False(t, exists)
}

func TestBoundedTileRangeCentersAndRespectsEdges(t *testing.T) {
	start, end := boundedTileRange(0, 20, 10.75)
	assert.Equal(t, int64(7), start)
	assert.Equal(t, int64(14), end)

	start, end = boundedTileRange(0, 20, 0)
	assert.Equal(t, int64(0), start)
	assert.Equal(t, int64(7), end)

	start, end = boundedTileRange(0, 20, 20)
	assert.Equal(t, int64(13), start)
	assert.Equal(t, int64(20), end)
}

func assertUniqueValidTileIDs(t *testing.T, cover []vectorTileID) {
	t.Helper()
	seen := make(map[vectorTileID]struct{}, len(cover))
	for _, tile := range cover {
		dimension := uint32(1) << tile.Z
		assert.Less(t, tile.X, dimension)
		assert.Less(t, tile.Y, dimension)
		_, duplicate := seen[tile]
		assert.False(t, duplicate, "duplicate tile %+v", tile)
		seen[tile] = struct{}{}
	}
}
