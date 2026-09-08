package vecmap

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func BenchmarkDecodeRoadBucket(b *testing.B) {
	data := benchmarkVectorTile(b)
	b.ReportAllocs()
	for b.Loop() {
		bucket, err := decodeRoadBucket(data, pinnedTile)
		require.NoError(b, err)
		require.NotEmpty(b, bucket.liberty)
	}
}

func BenchmarkCompileLibertyTile(b *testing.B) {
	data := benchmarkVectorTile(b)
	bucket, err := decodeRoadBucket(data, pinnedTile)
	require.NoError(b, err)
	b.ReportMetric(float64(len(bucket.sourceLayers)), "source_layers")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		compiled := *bucket
		require.NoError(b, compileLibertyTile(&compiled, 9.5))
	}
}

func BenchmarkCompileLibertySceneTilesSourceZoom(b *testing.B) {
	data := benchmarkVectorTile(b)
	bucket, err := decodeRoadBucket(data, pinnedTile)
	require.NoError(b, err)
	tiles := []loadedRoadTile{{id: pinnedTile, roads: bucket}}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		compiled := compileLibertySceneTiles(tiles, float64(pinnedTile.Z))
		require.Len(b, compiled, 1)
	}
}

func benchmarkVectorTile(tb testing.TB) []byte {
	tb.Helper()
	path := os.Getenv("WHEREAMI_VECTOR_TILE_FIXTURE")
	if path == "" {
		tb.Skip("set WHEREAMI_VECTOR_TILE_FIXTURE to the pinned OpenFreeMap PBF")
	}
	data, err := os.ReadFile(path)
	require.NoError(tb, err)
	require.NoError(tb, verifyTileChecksum(data, pinnedTileSHA256))
	return data
}
