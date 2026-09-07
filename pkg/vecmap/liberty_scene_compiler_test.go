package vecmap

import (
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
	bucket := &tileBucket{tile: vectorTileID{Z: 4}, sourceLayers: map[string][]vectorFeature{}}
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
