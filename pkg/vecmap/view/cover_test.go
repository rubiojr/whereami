package view

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var pinnedTile = TileID{X: 250, Y: 193, Z: 9}

func TestVisibleTileCoverIncludesCenterAndPrefetchRing(t *testing.T) {
	camera := NewCamera(
		Coordinate{Latitude: 40.4168, Longitude: -3.7038},
		9,
		0,
		256,
		256,
	)

	cover := VisibleTileCover(camera)

	require.NotEmpty(t, cover)
	assert.Equal(t, pinnedTile, cover[0])
	assert.Contains(t, cover, pinnedTile)
	assert.LessOrEqual(t, len(cover), maximumCoverSide*maximumCoverSide)
	assertUniqueValidTileIDs(t, cover)
}

func TestVisibleTileCoverWrapsAtAntimeridian(t *testing.T) {
	camera := NewCamera(Coordinate{Latitude: 0, Longitude: 179.9}, 2, 0, 256, 256)

	cover := VisibleTileCover(camera)

	assert.Contains(t, cover, TileID{X: 0, Y: 2, Z: 2})
	assert.Contains(t, cover, TileID{X: 3, Y: 2, Z: 2})
	assertUniqueValidTileIDs(t, cover)
}

func TestVisibleTileCoverBoundsLargeViewportAndSourceZoom(t *testing.T) {
	camera := NewCamera(Coordinate{}, 20, 33, 1_000_000, 1_000_000)

	cover := VisibleTileCover(camera)

	require.NotEmpty(t, cover)
	assert.LessOrEqual(t, len(cover), maximumCoverSide*maximumCoverSide)
	for _, tile := range cover {
		assert.Equal(t, uint32(maximumSourceZoom), tile.Z)
	}
	assertUniqueValidTileIDs(t, cover)
}

func TestVisibleTileCoverRequiresViewport(t *testing.T) {
	assert.Empty(t, VisibleTileCover(NewCamera(Coordinate{}, 9, 0, 0, 100)))
	assert.Empty(t, VisibleTileCover(NewCamera(Coordinate{}, 9, 0, 100, 0)))
}

func TestTileHierarchyWrapsAtAntimeridian(t *testing.T) {
	west := TileID{X: 0, Y: 2, Z: 3}
	east := TileID{X: 7, Y: 2, Z: 3}

	westParent, exists := west.Parent()
	assert.True(t, exists)
	assert.Equal(t, TileID{X: 0, Y: 1, Z: 2}, westParent)
	eastParent, exists := east.Parent()
	assert.True(t, exists)
	assert.Equal(t, TileID{X: 3, Y: 1, Z: 2}, eastParent)
	assert.True(t, TileContains(westParent, west))
	assert.True(t, TileContains(eastParent, east))
	assert.False(t, TilesOverlap(westParent, eastParent))
	_, exists = (TileID{}).Parent()
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

func assertUniqueValidTileIDs(t *testing.T, cover []TileID) {
	t.Helper()
	seen := make(map[TileID]struct{}, len(cover))
	for _, tile := range cover {
		dimension := uint32(1) << tile.Z
		assert.Less(t, tile.X, dimension)
		assert.Less(t, tile.Y, dimension)
		_, duplicate := seen[tile]
		assert.False(t, duplicate, "duplicate tile %+v", tile)
		seen[tile] = struct{}{}
	}
}

func TestVisibleTileCoverAtDrawsCoarserTiles(t *testing.T) {
	madrid := Coordinate{Latitude: 40.4168, Longitude: -3.7038}
	camera := NewCamera(madrid, 12, 0, 800, 600)

	fine, coarse := VisibleTileCover(camera), VisibleTileCoverAt(camera, 1)

	assert.Equal(t, fine, VisibleTileCoverAt(camera, 0))
	assert.Len(t, fine, 30)
	require.Len(t, coarse, 16)
	for _, tile := range coarse {
		assert.Equal(t, uint32(11), tile.Z)
	}
	parent, _ := fine[0].Parent()
	assert.Equal(t, parent, coarse[0])
	assertUniqueValidTileIDs(t, coarse)
	// The same ground one zoom lower is half the viewport in each direction.
	assert.Equal(t, VisibleTileCover(NewCamera(madrid, 11, 0, 400, 300)), coarse)
}

func TestVisibleTileCoverAtBoundsZoomAndCoarser(t *testing.T) {
	madrid := Coordinate{Latitude: 40.4168, Longitude: -3.7038}
	for _, test := range []struct {
		name    string
		zoom    float64
		coarser int
		source  uint32
	}{
		{"below the source maximum", 14.5, 1, 13},
		{"at the source maximum", 15, 1, 14},
		{"above the source maximum", 17, 2, 14},
		{"below zoom coarser", 0.5, 1, 0},
		{"negative is zero", 12, -1, 12},
		{"beyond the maximum is the maximum", 12, MaxCoarser + 1, 12 - MaxCoarser},
	} {
		t.Run(test.name, func(t *testing.T) {
			cover := VisibleTileCoverAt(NewCamera(madrid, test.zoom, 0, 800, 600), test.coarser)
			require.NotEmpty(t, cover)
			for _, tile := range cover {
				assert.Equal(t, test.source, tile.Z)
			}
			assertUniqueValidTileIDs(t, cover)
		})
	}
	assert.Empty(t, VisibleTileCoverAt(NewCamera(Coordinate{}, 9, 0, 0, 100), 1))
}

func TestTilesNearKeepsTheViewportAndItsMargin(t *testing.T) {
	madrid := Coordinate{Latitude: 40.4168, Longitude: -3.7038}
	for _, test := range []struct {
		name   string
		zoom   float64
		margin float64
		want   int
	}{
		{"visible tiles only", 14, 0, 12},
		{"a margin of one tile is the ring at a whole zoom", 14, TileSize, 30},
		{"the same margin is fewer tiles when tiles are drawn larger", 14.9, TileSize, 9},
		{"overzoomed tiles", 16, TileSize, 6},
		{"negative is zero", 14, -1, 12},
		{"not a number is zero", 14, math.NaN(), 12},
		{"infinite is zero", 14, math.Inf(1), 12},
	} {
		t.Run(test.name, func(t *testing.T) {
			camera := NewCamera(madrid, test.zoom, 0, 800, 600)
			cover := VisibleTileCover(camera)
			near := TilesNear(camera, cover, test.margin)
			assert.Len(t, near, test.want)
			// Order and identity are those of the input.
			index := 0
			for _, tile := range near {
				for index < len(cover) && cover[index] != tile {
					index++
				}
				require.Less(t, index, len(cover))
			}
			assert.Equal(t, cover[0], near[0], "the tile under the centre is always near")
		})
	}
	camera := NewCamera(madrid, 14, 0, 800, 600)
	assert.Equal(t, VisibleTileCover(camera), TilesNear(camera, VisibleTileCover(camera), 1e6))
	assert.Empty(t, TilesNear(camera, nil, 10))
	assert.Empty(t, TilesNear(NewCamera(madrid, 14, 0, 0, 600), VisibleTileCover(camera), 10))
	assert.Empty(t, TilesNear(camera, []TileID{{X: 9, Y: 9, Z: 2}}, 10), "invalid tiles are never near")
	far := TileID{X: 0, Y: 0, Z: 14}
	assert.Empty(t, TilesNear(camera, []TileID{far}, TileSize))
}

func TestTilesNearFollowsRotationAndWorldCopies(t *testing.T) {
	madrid := Coordinate{Latitude: 40.4168, Longitude: -3.7038}
	straight := NewCamera(madrid, 14, 0, 800, 600)
	turned := NewCamera(madrid, 14, 45, 800, 600)
	cover := VisibleTileCover(turned)
	near := TilesNear(turned, cover, 0)
	assert.Greater(t, len(near), len(TilesNear(straight, VisibleTileCover(straight), 0)), "a turned tile is bounded by a larger box")
	assert.LessOrEqual(t, len(near), len(cover))

	// Both sides of the antimeridian are one step apart on screen.
	edge := NewCamera(Coordinate{Longitude: 179.99}, 3, 0, 512, 256)
	both := TilesNear(edge, []TileID{{X: 7, Y: 4, Z: 3}, {X: 0, Y: 4, Z: 3}, {X: 4, Y: 4, Z: 3}}, 0)
	assert.Equal(t, []TileID{{X: 7, Y: 4, Z: 3}, {X: 0, Y: 4, Z: 3}}, both)
}
