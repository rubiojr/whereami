package vecmap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeRoadBucket(t *testing.T) {
	tile := vectorTileID{X: 250, Y: 193, Z: 9}
	data := syntheticRoadTile("transportation")

	bucket, err := decodeRoadBucket(data, tile)
	require.NoError(t, err)
	assert.Equal(t, tile, bucket.tile)
	assert.Equal(t, 2, bucket.featureCount)
	assert.Len(t, bucket.segments, 4)
	assert.Len(t, bucket.sourceLayers["transportation"], 3)
	assert.NotEmpty(t, bucket.liberty)
	assert.True(t, hasLibertyLayer(bucket.liberty, "road_trunk_primary"))
	expected := tileLocalPoint(4096, 100, 100)
	assert.Equal(t, expected, bucket.segments[0].Start)
}

func hasLibertyLayer(primitives []libertyRenderPrimitive, layerID string) bool {
	for _, primitive := range primitives {
		if primitive.layerID == layerID {
			return true
		}
	}
	return false
}

func TestDecodeRoadBucketRejectsInvalidTiles(t *testing.T) {
	tile := vectorTileID{X: 250, Y: 193, Z: 9}

	_, err := decodeRoadBucket([]byte{1}, tile)
	assert.ErrorContains(t, err, "decode MVT tile")

	empty, err := decodeRoadBucket(nil, tile)
	require.NoError(t, err)
	assert.Empty(t, empty.segments)
	assert.Empty(t, empty.sourceLayers)

	empty, err = decodeRoadBucket(syntheticRoadTile("water"), tile)
	require.NoError(t, err)
	assert.Empty(t, empty.segments)

	malformedGeometry := mvtTile(mvtLayerMessage(
		"transportation",
		mvtFeatureMessage(0, []uint32{9, 2, 2, 10, 2}),
		mvtFeatureMessage(0, mvtLineGeometry([][2]int32{{0, 0}, {1, 1}})),
	))
	bucket, err := decodeRoadBucket(malformedGeometry, tile)
	require.NoError(t, err)
	assert.Len(t, bucket.segments, 1)

	invalidTags := mvtTile(mvtLayerMessage(
		"transportation",
		mvtFeatureMessage(99, mvtLineGeometry([][2]int32{{0, 0}, {1, 1}})),
	))
	bucket, err = decodeRoadBucket(invalidTags, tile)
	require.NoError(t, err)
	assert.Empty(t, bucket.segments)

	fullBucket := &tileBucket{segments: make([]roadSegment, maxRoadSegments)}
	err = appendMVTLineGeometry(fullBucket, mvtLineGeometry([][2]int32{{0, 0}, {1, 1}}), 4096)
	assert.ErrorIs(t, err, errRoadResourceLimit)
}

func TestDecodePolygonFills(t *testing.T) {
	tile := vectorTileID{X: 250, Y: 193, Z: 9}
	land := mvtLayerMessage("landcover", mvtTypedFeatureMessage(0, mvtPolygonType, mvtPolygonGeometry(
		[][2]int32{{0, 0}, {100, 0}, {100, 100}, {0, 100}},
	)))
	water := mvtLayerMessage("water", mvtTypedFeatureMessage(0, mvtPolygonType, mvtPolygonGeometry(
		[][2]int32{{200, 200}, {400, 200}, {400, 400}, {200, 400}},
		[][2]int32{{250, 250}, {250, 350}, {350, 350}, {350, 250}},
	)))

	bucket, err := decodeRoadBucket(mvtTile(land, water), tile)

	require.NoError(t, err)
	assert.Equal(t, 1, bucket.land.featureCount)
	assert.Len(t, bucket.land.triangles, 6)
	assert.Empty(t, bucket.land.cutouts)
	assert.Equal(t, 1, bucket.water.featureCount)
	assert.Len(t, bucket.water.triangles, 6)
	assert.Len(t, bucket.water.cutouts, 6)
}

func TestDecodePolygonFillsSkipsMalformedGeometry(t *testing.T) {
	tile := vectorTileID{X: 250, Y: 193, Z: 9}
	tests := []struct {
		name      string
		geometry  []uint32
		errorText string
	}{
		{name: "line before move", geometry: []uint32{1<<3 | mvtLineToCommand, 2, 2}, errorText: "before MoveTo"},
		{name: "unclosed", geometry: []uint32{1<<3 | mvtMoveToCommand, 0, 0, 2<<3 | mvtLineToCommand, 2, 0, 0, 2}, errorText: "not closed"},
		{name: "bad close count", geometry: []uint32{1<<3 | mvtMoveToCommand, 0, 0, 2<<3 | mvtLineToCommand, 2, 0, 0, 2, 2<<3 | mvtClosePathCommand}, errorText: "count is not one"},
		{name: "truncated", geometry: []uint32{1<<3 | mvtMoveToCommand, 0}, errorText: "truncated"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := decodeMVTPolygonRings(test.geometry, 4096)
			assert.ErrorContains(t, err, test.errorText)

			valid := mvtPolygonGeometry([][2]int32{{0, 0}, {100, 0}, {100, 100}, {0, 100}})
			layer := mvtLayerMessage(
				"water",
				mvtTypedFeatureMessage(0, mvtPolygonType, test.geometry),
				mvtTypedFeatureMessage(0, mvtPolygonType, valid),
			)
			bucket, err := decodeRoadBucket(mvtTile(layer), tile)
			require.NoError(t, err)
			assert.Equal(t, 1, bucket.water.featureCount)
		})
	}

	holeOnly := mvtPolygonGeometry([][2]int32{{0, 0}, {0, 100}, {100, 100}, {100, 0}})
	layer := mvtLayerMessage("water", mvtTypedFeatureMessage(0, mvtPolygonType, holeOnly))
	bucket, err := decodeRoadBucket(mvtTile(layer), tile)
	require.NoError(t, err)
	assert.Empty(t, bucket.water.triangles)
}

func TestDecodePolygonFillsEnforcesRingPointLimit(t *testing.T) {
	points := make([][2]int32, maxFillRingPoints+1)
	for index := range points {
		points[index] = [2]int32{int32(index), int32(index % 2)}
	}

	_, err := decodeMVTPolygonRings(mvtPolygonGeometry(points), 4096)
	assert.ErrorIs(t, err, errPolygonResourceLimit)
}

func TestDecodeRoadBucketSkipsResourceLimitedLandcoverFeature(t *testing.T) {
	tile := vectorTileID{X: 250, Y: 193, Z: 9}
	oversized := make([][2]int32, maxFillRingPoints+1)
	for index := range oversized {
		oversized[index] = [2]int32{int32(index % 1024), int32(index / 1024)}
	}
	valid := [][2]int32{{0, 0}, {100, 0}, {100, 100}, {0, 100}}
	layer := mvtLayerMessage(
		"landcover",
		mvtTypedFeatureMessage(0, mvtPolygonType, mvtPolygonGeometry(oversized)),
		mvtTypedFeatureMessage(0, mvtPolygonType, mvtPolygonGeometry(valid)),
	)
	messages := make(chan string, 2)
	previousReporter := reportVectorWarning
	reportVectorWarning = func(format string, args ...any) {
		messages <- fmt.Sprintf(format, args...)
	}
	t.Cleanup(func() { reportVectorWarning = previousReporter })

	bucket, err := decodeRoadBucket(mvtTile(layer), tile)

	require.NoError(t, err)
	assert.Equal(t, 1, bucket.land.featureCount)
	assert.Len(t, bucket.sourceLayers["landcover"], 1)
	require.Len(t, messages, 2)
	for range 2 {
		message := <-messages
		assert.Contains(t, message, "z=9 x=250 y=193")
		assert.Contains(t, message, "landcover")
		assert.Contains(t, message, "last feature index=0")
		assert.Contains(t, message, "polygon resource limit exceeded")
	}
}

func TestDecodePinnedRoadFixture(t *testing.T) {
	path := os.Getenv("WHEREAMI_VECTOR_TILE_FIXTURE")
	if path == "" {
		t.Skip("set WHEREAMI_VECTOR_TILE_FIXTURE to the pinned OpenFreeMap PBF")
	}

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, verifyTileChecksum(data, pinnedTileSHA256))
	bucket, err := decodeRoadBucket(data, pinnedTile)
	require.NoError(t, err)
	assert.Positive(t, bucket.featureCount)
	assert.NotEmpty(t, bucket.segments)
	assert.NotEmpty(t, bucket.land.triangles)
	assert.NotEmpty(t, bucket.water.triangles)
	assert.NotEmpty(t, bucket.sourceLayers)
	assert.NotEmpty(t, bucket.liberty)
	assert.NotEmpty(t, bucket.symbols)
	t.Logf(
		"decoded roads=%d/%d land=%d/%d water=%d/%d cutouts=%d Liberty primitives=%d symbols=%d",
		bucket.featureCount, len(bucket.segments),
		bucket.land.featureCount, len(bucket.land.triangles)/3,
		bucket.water.featureCount, len(bucket.water.triangles)/3,
		len(bucket.water.cutouts)/3,
		len(bucket.liberty),
		len(bucket.symbols),
	)
}

func TestLoadRoadBucketFetchesAndCaches(t *testing.T) {
	tile := vectorTileID{X: 250, Y: 193, Z: 9}
	data := syntheticRoadTile("transportation")
	checksum := sha256.Sum256(data)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		assert.Equal(t, "/tile.pbf", request.URL.Path)
		assert.Contains(t, request.Header.Get("Accept"), "application/vnd.mapbox-vector-tile")
		writer.WriteHeader(http.StatusOK)
		_, err := writer.Write(data)
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)

	source := tileSource{
		url:       server.URL + "/tile.pbf",
		cacheName: "test.pbf",
		sha256:    hex.EncodeToString(checksum[:]),
		tile:      tile,
	}
	cacheDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, source.cacheName), []byte("corrupt"), 0o600))
	first, err := loadRoadBucket(context.Background(), server.Client(), source, cacheDir)
	require.NoError(t, err)
	second, err := loadRoadBucket(context.Background(), server.Client(), source, cacheDir)
	require.NoError(t, err)

	assert.Equal(t, int32(1), requests.Load())
	assert.Equal(t, first.featureCount, second.featureCount)
	assert.Equal(t, first.segments, second.segments)
	cached, err := os.ReadFile(filepath.Join(cacheDir, source.cacheName))
	require.NoError(t, err)
	assert.Equal(t, data, cached)
}

func TestLoadRoadBucketChecksDynamicCacheSidecar(t *testing.T) {
	data := syntheticRoadTile("transportation")
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, err := writer.Write(data)
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	source := tileSource{
		url:       server.URL,
		cacheName: filepath.Join("snapshot", "9", "251", "193.pbf"),
		tile:      vectorTileID{X: 251, Y: 193, Z: 9},
	}
	cacheDir := t.TempDir()

	_, err := loadRoadBucket(context.Background(), server.Client(), source, cacheDir)
	require.NoError(t, err)
	_, err = loadRoadBucket(context.Background(), server.Client(), source, cacheDir)
	require.NoError(t, err)
	assert.Equal(t, int32(1), requests.Load())

	cachePath := filepath.Join(cacheDir, source.cacheName)
	require.NoError(t, os.WriteFile(cachePath, []byte("corrupt"), 0o600))
	_, err = loadRoadBucket(context.Background(), server.Client(), source, cacheDir)
	require.NoError(t, err)
	assert.Equal(t, int32(2), requests.Load())
	checksum, err := os.ReadFile(cachePath + ".sha256")
	require.NoError(t, err)
	assert.Equal(t, tileChecksum(data), strings.TrimSpace(string(checksum)))
}

func TestWriteCachedTilePairSerializesConcurrentWriters(t *testing.T) {
	cacheDir := t.TempDir()
	name := filepath.Join("snapshot", "9", "251", "193.pbf")
	var wait sync.WaitGroup
	errors := make(chan error, 32)
	for index := range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			data := []byte(strings.Repeat(string(rune('a'+index%26)), 128+index))
			errors <- writeCachedTilePair(cacheDir, name, data, tileChecksum(data))
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}

	data, checksum, err := readCachedTile(filepath.Join(cacheDir, name), "")
	require.NoError(t, err)
	assert.NoError(t, verifyTileChecksum(data, checksum))
}

func TestEnforceTileCacheLimitEvictsPairsAndPreservesPinnedTile(t *testing.T) {
	cacheDir := t.TempDir()
	oldName := filepath.Join("snapshot", "9", "250", "192.pbf")
	newName := filepath.Join("snapshot", "9", "250", "194.pbf")
	data := []byte(strings.Repeat("x", 128))
	for _, name := range []string{oldName, pinnedTileCacheName, newName} {
		require.NoError(t, writeCachedTilePair(cacheDir, name, data, tileChecksum(data)))
	}
	setCachePairTime := func(name string, modified time.Time) {
		t.Helper()
		require.NoError(t, os.Chtimes(filepath.Join(cacheDir, name), modified, modified))
		require.NoError(t, os.Chtimes(filepath.Join(cacheDir, name+".sha256"), modified, modified))
	}
	now := time.Now()
	setCachePairTime(oldName, now.Add(-3*time.Hour))
	setCachePairTime(pinnedTileCacheName, now.Add(-2*time.Hour))
	setCachePairTime(newName, now.Add(-time.Hour))
	staleTemporary := filepath.Join(cacheDir, ".tile-orphan")
	require.NoError(t, os.WriteFile(staleTemporary, []byte("stale"), 0o600))

	require.NoError(t, enforceTileCacheLimit(cacheDir, 400))

	_, err := os.Stat(filepath.Join(cacheDir, oldName))
	assert.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(filepath.Join(cacheDir, oldName+".sha256"))
	assert.ErrorIs(t, err, os.ErrNotExist)
	for _, name := range []string{pinnedTileCacheName, newName} {
		_, err = os.Stat(filepath.Join(cacheDir, name))
		assert.NoError(t, err)
		_, err = os.Stat(filepath.Join(cacheDir, name+".sha256"))
		assert.NoError(t, err)
	}
	_, err = os.Stat(staleTemporary)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestWriteTileAtomicallyRejectsPathTraversal(t *testing.T) {
	err := writeTileAtomically(t.TempDir(), filepath.Join("..", "escape"), []byte("data"))
	assert.ErrorContains(t, err, "escapes cache directory")
}

func TestOpenFreeMapRoadSource(t *testing.T) {
	tile := vectorTileID{X: 251, Y: 193, Z: 9}

	source := openFreeMapRoadSource(tile)

	assert.Equal(t, openFreeMapBaseURL+"/9/251/193.pbf", source.url)
	assert.Equal(t, filepath.Join("openfreemap-"+openFreeMapSnapshot, "9", "251", "193.pbf"), source.cacheName)
	assert.Empty(t, source.sha256)
	assert.Equal(t, pinnedTileCacheName, openFreeMapRoadSource(pinnedTile).cacheName)
	assert.Equal(t, pinnedTileSHA256, openFreeMapRoadSource(pinnedTile).sha256)
}

func TestCheckTileRedirectRequiresSameHTTPSOrigin(t *testing.T) {
	original, err := http.NewRequest(http.MethodGet, "https://tiles.openfreemap.org/start", nil)
	require.NoError(t, err)
	sameOrigin, err := http.NewRequest(http.MethodGet, "https://tiles.openfreemap.org/final", nil)
	require.NoError(t, err)
	otherOrigin, err := http.NewRequest(http.MethodGet, "https://example.com/final", nil)
	require.NoError(t, err)
	insecure, err := http.NewRequest(http.MethodGet, "http://tiles.openfreemap.org/final", nil)
	require.NoError(t, err)

	assert.NoError(t, checkTileRedirect(sameOrigin, []*http.Request{original}))
	assert.Error(t, checkTileRedirect(otherOrigin, []*http.Request{original}))
	assert.Error(t, checkTileRedirect(insecure, []*http.Request{original}))
	assert.Error(t, checkTileRedirect(sameOrigin, nil))
}

func FuzzDecodeRoadBucket(f *testing.F) {
	f.Add(syntheticRoadTile("transportation"))
	f.Add(mvtTile(mvtLayerMessage("water", mvtTypedFeatureMessage(0, mvtPolygonType, mvtPolygonGeometry(
		[][2]int32{{0, 0}, {100, 0}, {100, 100}, {0, 100}},
	)))))
	f.Add([]byte{0x1a, 0x01, 0xff})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxTileBytes {
			t.Skip()
		}
		_, _ = decodeRoadBucket(data, vectorTileID{X: 250, Y: 193, Z: 9})
	})
}

func TestLoadRoadBucketRejectsBadResponses(t *testing.T) {
	tile := vectorTileID{X: 250, Y: 193, Z: 9}
	tests := []struct {
		name      string
		status    int
		body      string
		checksum  string
		errorText string
	}{
		{name: "status", status: http.StatusBadGateway, errorText: "502 Bad Gateway"},
		{name: "checksum", status: http.StatusOK, body: "not a tile", checksum: strings.Repeat("0", 64), errorText: "checksum mismatch"},
		{name: "size", status: http.StatusOK, body: strings.Repeat("x", maxTileBytes+1), errorText: "exceeds"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(test.status)
				_, _ = writer.Write([]byte(test.body))
			}))
			t.Cleanup(server.Close)

			source := tileSource{
				url:       server.URL,
				cacheName: "test.pbf",
				sha256:    test.checksum,
				tile:      tile,
			}
			_, err := loadRoadBucket(context.Background(), server.Client(), source, "")
			assert.ErrorContains(t, err, test.errorText)
		})
	}
}

func syntheticRoadTile(layerName string) []byte {
	primary := mvtFeatureMessage(0, mvtLineGeometry(
		[][2]int32{{100, 100}, {200, 180}, {300, 260}},
	))
	secondary := mvtFeatureMessage(1, mvtLineGeometry(
		[][2]int32{{400, 300}, {500, 350}},
		[][2]int32{{450, 500}, {600, 600}},
	))
	rail := mvtFeatureMessage(2, mvtLineGeometry(
		[][2]int32{{700, 700}, {800, 800}},
	))
	return mvtTile(mvtLayerMessage(layerName, primary, secondary, rail))
}

func mvtTile(layers ...[]byte) []byte {
	var tile []byte
	for _, layer := range layers {
		tile = appendBytesField(tile, 3, layer)
	}
	return tile
}

func mvtLayerMessage(name string, features ...[]byte) []byte {
	message := appendBytesField(nil, 1, []byte(name))
	for _, feature := range features {
		message = appendBytesField(message, 2, feature)
	}
	message = appendBytesField(message, 3, []byte("class"))
	for _, class := range []string{"primary", "secondary", "rail"} {
		value := appendBytesField(nil, 1, []byte(class))
		message = appendBytesField(message, 4, value)
	}
	message = appendVarintField(message, 5, 4096)
	return appendVarintField(message, 15, 2)
}

func mvtFeatureMessage(classIndex uint32, geometry []uint32) []byte {
	return mvtTypedFeatureMessage(classIndex, mvtLineStringType, geometry)
}

func mvtTypedFeatureMessage(classIndex, geometryType uint32, geometry []uint32) []byte {
	tags := appendProtoVarint(nil, 0)
	tags = appendProtoVarint(tags, uint64(classIndex))
	message := appendBytesField(nil, 2, tags)
	message = appendVarintField(message, 3, geometryType)
	packedGeometry := make([]byte, 0, len(geometry)*2)
	for _, value := range geometry {
		packedGeometry = appendProtoVarint(packedGeometry, uint64(value))
	}
	return appendBytesField(message, 4, packedGeometry)
}

func mvtPolygonGeometry(rings ...[][2]int32) []uint32 {
	var (
		geometry []uint32
		x        int32
		y        int32
	)
	for _, ring := range rings {
		if len(ring) < 3 {
			continue
		}
		geometry = append(geometry, 1<<3|mvtMoveToCommand)
		geometry = append(geometry, encodeZigZag(ring[0][0]-x), encodeZigZag(ring[0][1]-y))
		x, y = ring[0][0], ring[0][1]
		geometry = append(geometry, uint32(len(ring)-1)<<3|mvtLineToCommand)
		for _, point := range ring[1:] {
			geometry = append(geometry, encodeZigZag(point[0]-x), encodeZigZag(point[1]-y))
			x, y = point[0], point[1]
		}
		geometry = append(geometry, 1<<3|mvtClosePathCommand)
	}
	return geometry
}

func mvtLineGeometry(paths ...[][2]int32) []uint32 {
	var (
		geometry []uint32
		x        int32
		y        int32
	)
	for _, path := range paths {
		if len(path) < 2 {
			continue
		}
		geometry = append(geometry, 1<<3|mvtMoveToCommand)
		geometry = append(geometry, encodeZigZag(path[0][0]-x), encodeZigZag(path[0][1]-y))
		x, y = path[0][0], path[0][1]
		geometry = append(geometry, uint32(len(path)-1)<<3|mvtLineToCommand)
		for _, point := range path[1:] {
			geometry = append(geometry, encodeZigZag(point[0]-x), encodeZigZag(point[1]-y))
			x, y = point[0], point[1]
		}
	}
	return geometry
}

func encodeZigZag(value int32) uint32 {
	return uint32(value<<1) ^ uint32(value>>31)
}

func appendBytesField(data []byte, field int, value []byte) []byte {
	data = appendProtoVarint(data, uint64(field<<3|protobufWireBytes))
	data = appendProtoVarint(data, uint64(len(value)))
	return append(data, value...)
}

func appendVarintField(data []byte, field int, value uint32) []byte {
	data = appendProtoVarint(data, uint64(field<<3|protobufWireVarint))
	return appendProtoVarint(data, uint64(value))
}

func appendProtoVarint(data []byte, value uint64) []byte {
	for value >= 0x80 {
		data = append(data, byte(value)|0x80)
		value >>= 7
	}
	return append(data, byte(value))
}
