package vecmap

import (
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLibertySceneCompilerPublishesLatestRequest(t *testing.T) {
	var latest atomic.Pointer[libertySceneSnapshot]
	compiler := newLibertySceneCompiler(latest.Store)
	t.Cleanup(compiler.stop)
	bucket := &tileBucket{tile: vectorTileID{Z: 4}, sourceLayers: map[string]vectorFeatures{}}
	tiles := []loadedRoadTile{{id: bucket.tile, roads: bucket}}

	compiler.request(1, 4, tiles)
	compiler.request(2, 4.5, tiles)

	require.Eventually(t, func() bool {
		snapshot := latest.Load()
		return snapshot != nil && snapshot.tileRevision == 2 && snapshot.styleZoom == 4.5
	}, time.Second, time.Millisecond)
	assert.Empty(t, bucket.liberty)
	assert.NotEmpty(t, latest.Load().tiles[0].roads.liberty)
}

func TestCompileLibertySceneTilesReusesMatchingCompiledZoom(t *testing.T) {
	bucket := &tileBucket{tile: vectorTileID{Z: 4}, sourceLayers: map[string]vectorFeatures{}}
	require.NoError(t, compileLibertyTile(bucket, 4))

	compiled := compileLibertySceneTiles([]loadedRoadTile{{id: bucket.tile, roads: bucket}}, 4)

	require.Len(t, compiled, 1)
	assert.Same(t, bucket, compiled[0].roads)
}

func TestCompileLibertySceneTilesPreservesSourceIdentityAtFractionalZoom(t *testing.T) {
	bucket := &tileBucket{tile: vectorTileID{Z: 4}, sourceLayers: map[string]vectorFeatures{}}
	require.NoError(t, compileLibertyTile(bucket, 4))

	compiled := compileLibertySceneTiles([]loadedRoadTile{{id: bucket.tile, roads: bucket}}, 4.5)

	require.Len(t, compiled, 1)
	assert.NotSame(t, bucket, compiled[0].roads)
	assert.Same(t, bucket, compiled[0].contentIdentity())
}

func TestLibertyPrimitivesAtOrderReturnsContiguousLayer(t *testing.T) {
	primitives := []libertyRenderPrimitive{{order: 1}, {order: 3}, {order: 3}, {order: 8}}

	assert.Equal(t, primitives[1:3], libertyPrimitivesAtOrder(primitives, 3))
	assert.Empty(t, libertyPrimitivesAtOrder(primitives, 2))
	assert.Empty(t, libertyPrimitivesAtOrder(primitives, 9))
}

func TestCompiledLibertyOutputIsOrderedByLayer(t *testing.T) {
	bucket := &tileBucket{tile: vectorTileID{Z: 9}, sourceLayers: map[string]vectorFeatures{}}
	require.NoError(t, compileLibertyTile(bucket, 9))

	assert.True(t, sort.SliceIsSorted(bucket.liberty, func(first, second int) bool {
		return bucket.liberty[first].order < bucket.liberty[second].order
	}))
	assert.True(t, sort.SliceIsSorted(bucket.symbols, func(first, second int) bool {
		return bucket.symbols[first].order < bucket.symbols[second].order
	}))
}
