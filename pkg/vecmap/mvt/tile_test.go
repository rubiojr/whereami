package mvt

import (
	"math"
	"os"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var triangleCommands = []uint32{9, 0, 0, 18, 200, 0, 199, 200, 15}

func TestDecodeTileOwnedOrderedFeatures(t *testing.T) {
	point := varintField(featureMessage(PointType, []uint32{9, 32, 64}), 1, 0)
	line := featureMessage(LineStringType, []uint32{9, 0, 0, 10, 32, 64})
	polygon := featureMessage(PolygonType, triangleCommands)
	data := tileMessage(layerMessage("shared", point, line), layerMessage("shared", polygon))
	expanded, err := DecodeTile(data, false)
	require.NoError(t, err)
	direct, err := DecodeTile(data, true)
	require.NoError(t, err)
	clear(data)
	require.Len(t, expanded.Layers["shared"], 3)
	features := expanded.Layers["shared"]
	assert.True(t, features[0].HasID)
	assert.Zero(t, features[0].ID)
	assert.False(t, features[1].HasID)
	assert.Equal(t, []geometry.Point{{X: 1, Y: 2}}, features[0].Points)
	assert.Equal(t, [][]geometry.Point{{{X: 0, Y: 0}, {X: 1, Y: 2}}}, features[1].Lines)
	assert.Equal(t, []int{1, 2, 3}, []int{features[0].PointCount(), features[1].PointCount(), features[2].PointCount()})
	for _, feature := range features {
		assert.Equal(t, "primary", feature.Properties["class"])
	}
	assert.Empty(t, expanded.Limits)
	assertEquivalentTile(t, expanded, direct)
}

func TestDecodeTileSkipsInvalidAndResourceLimitedFeatures(t *testing.T) {
	commands := make([]uint32, 4+2*MaxPolygonPoints)
	commands[0], commands[3] = 9, uint32(MaxPolygonPoints)<<3|2
	commands = append(commands, 15)
	data := tileMessage(layerMessage("land",
		featureMessage(PolygonType, triangleCommands),
		[]byte{0}, // malformed feature framing
		featureMessage(99, nil),
		featureMessage(PolygonType, commands),
		bytesField(featureMessage(PointType, []uint32{9, 0, 0}), 2, []byte{0}), // odd tags
		featureMessage(PolygonType, []uint32{9}),
		featureMessage(PolygonType, triangleCommands),
	))
	tile, err := DecodeTile(data, true)
	require.NoError(t, err)
	require.Len(t, tile.Layers["land"], 2)
	require.Len(t, tile.Limits, 1)
	assert.Equal(t, "land", tile.Limits[0].Name)
	assert.Equal(t, 1, tile.Limits[0].Skipped)
	assert.Equal(t, 3, tile.Limits[0].LastFeatureIndex)
	assert.ErrorIs(t, tile.Limits[0].Last, geometry.ErrResourceLimit)
}

// Budget-exhaustion regression moved from the Qt-bound decoder tests.
func TestDecodeFeaturesKeepsEarlierContentWhenBudgetIsExhausted(t *testing.T) {
	large := make([][2]int32, 20)
	for index := range large {
		angle := 2 * math.Pi * float64(index) / float64(len(large))
		large[index] = [2]int32{int32(1000 + 500*math.Cos(angle)), int32(1000 + 500*math.Sin(angle))}
	}
	for _, indexed := range []bool{false, true} {
		layers, err := DecodeLayers(tileMessage(layerMessage("landcover",
			featureMessage(PolygonType, triangleCommands), featureMessage(PolygonType, polygonCommands(large)),
		)))
		require.NoError(t, err)
		decoder := NewDecoder(indexed)
		decoder.budget.Remaining = 30
		features, limits := decoder.DecodeLayer(layers[0])
		require.Len(t, features, 1)
		assert.Equal(t, 1, limits.Skipped)
		assert.Equal(t, 1, limits.LastFeatureIndex)
		assert.ErrorIs(t, limits.Last, geometry.ErrResourceLimit)
		assert.Equal(t, 12, decoder.budget.Remaining)
	}
}

func TestAggregateBudgetsAcrossLayers(t *testing.T) {
	point := featureMessage(PointType, []uint32{9, 0, 0})
	triangle := featureMessage(PolygonType, triangleCommands)
	layers, err := DecodeLayers(tileMessage(layerMessage("first", point), layerMessage("second", point, point)))
	require.NoError(t, err)
	for _, kind := range []string{"features", "points"} {
		t.Run(kind, func(t *testing.T) {
			d := NewDecoder(false)
			if kind == "features" {
				d.features = MaxTileFeatures - 1
			} else {
				d.points = MaxGeometryPoints - 1
			}
			features, limits := d.DecodeLayer(layers[0])
			require.Len(t, features, 1)
			assert.Zero(t, limits.Skipped)
			features, limits = d.DecodeLayer(layers[1])
			assert.Empty(t, features)
			assert.ErrorIs(t, limits.Last, ErrFeatureResourceLimit)
			if kind == "features" {
				assert.Equal(t, 1, limits.Skipped)
				assert.Equal(t, 0, cap(features), "do not preallocate beyond remaining feature slots")
			} else {
				assert.Equal(t, 2, limits.Skipped)
			}
		})
	}
	quad := featureMessage(PolygonType, polygonCommands([][2]int32{{0, 0}, {100, 0}, {100, 100}, {0, 100}}))
	layers, err = DecodeLayers(tileMessage(layerMessage("first", quad, triangle), layerMessage("second", triangle)))
	require.NoError(t, err)
	for _, indexed := range []bool{false, true} {
		d := NewDecoder(indexed)
		d.triangles = MaxTileStyleTriangles - 1
		features, limits := d.DecodeLayer(layers[0])
		require.Len(t, features, 1, "rejecting the quad must leave capacity for the triangle")
		assert.Equal(t, MaxTileStyleTriangles, d.triangles)
		assert.Equal(t, 1, d.features)
		assert.Equal(t, 3, d.points)
		assert.Equal(t, 1, limits.Skipped)
		assert.Less(t, d.budget.Remaining, MaxStyleTriangulation-18, "rejected quad still spends work")
		features, limits = d.DecodeLayer(layers[1])
		assert.Empty(t, features)
		assert.ErrorIs(t, limits.Last, ErrFeatureResourceLimit)
	}
}

func TestGroupRings(t *testing.T) {
	exterior := []geometry.Point{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}}
	hole := []geometry.Point{{X: 3, Y: 3}, {X: 3, Y: 7}, {X: 7, Y: 7}, {X: 7, Y: 3}}
	polygons, err := GroupRings([][]geometry.Point{nil, exterior, hole, exterior})
	require.NoError(t, err)
	require.Len(t, polygons, 2)
	require.Len(t, polygons[0].Holes, 1)
	assert.Same(t, &exterior[0], &polygons[0].Exterior[0])
	assert.Same(t, &hole[0], &polygons[0].Holes[0][0])
	assert.Equal(t, 12, (Feature{Polygons: polygons}).PointCount())
	_, err = GroupRings([][]geometry.Point{hole})
	require.ErrorContains(t, err, "interior ring")
	_, err = GroupRings(nil)
	require.ErrorContains(t, err, "no exterior")
}

func TestDecodePinnedTileHeadless(t *testing.T) {
	path := os.Getenv("WHEREAMI_VECTOR_TILE_FIXTURE")
	if path == "" {
		t.Skip("set WHEREAMI_VECTOR_TILE_FIXTURE for the pinned PBF")
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	expanded, err := DecodeTile(data, false)
	require.NoError(t, err)
	direct, err := DecodeTile(data, true)
	require.NoError(t, err)
	require.NotEmpty(t, expanded.Layers)
	assert.Empty(t, expanded.Limits)
	assertEquivalentTile(t, expanded, direct)
}

func assertEquivalentTile(t *testing.T, expanded, direct *Tile) {
	t.Helper()
	require.Len(t, direct.Layers, len(expanded.Layers))
	for name, features := range expanded.Layers {
		require.Len(t, direct.Layers[name], len(features))
		for index, want := range features {
			got := direct.Layers[name][index]
			assert.Equal(t, want.ID, got.ID)
			assert.Equal(t, want.HasID, got.HasID)
			assert.Equal(t, want.GeometryType, got.GeometryType)
			assert.Equal(t, want.Properties, got.Properties)
			assert.Equal(t, want.Points, got.Points)
			assert.Equal(t, want.Lines, got.Lines)
			require.Len(t, got.Polygons, len(want.Polygons))
			for i, polygon := range want.Polygons {
				indexed := got.Polygons[i]
				assert.Equal(t, polygon.Exterior, indexed.Exterior)
				assert.Equal(t, polygon.Holes, indexed.Holes)
				require.Len(t, indexed.Indices, len(polygon.Vertices))
				for j, vertex := range polygon.Vertices {
					require.Less(t, int(indexed.Indices[j]), len(indexed.Vertices))
					actual := indexed.Vertices[indexed.Indices[j]]
					assert.Equal(t, math.Float64bits(vertex.X), math.Float64bits(actual.X))
					assert.Equal(t, math.Float64bits(vertex.Y), math.Float64bits(actual.Y))
				}
			}
		}
	}
}

func polygonCommands(ring [][2]int32) []uint32 {
	commands := []uint32{9}
	var x, y int32
	for i, point := range ring {
		if i == 1 {
			commands = append(commands, uint32(len(ring)-1)<<3|2)
		}
		dx, dy := point[0]-x, point[1]-y
		commands = append(commands, uint32(dx<<1)^uint32(dx>>31), uint32(dy<<1)^uint32(dy>>31))
		x, y = point[0], point[1]
	}
	return append(commands, 15)
}

func FuzzDecodeTile(f *testing.F) {
	f.Add(tileMessage(layerMessage("land", featureMessage(PolygonType, triangleCommands))))
	f.Add([]byte{26, 0})
	f.Add([]byte{0, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > MaxTileBytes {
			return
		}
		tile, err := DecodeTile(data, true)
		if err != nil {
			require.Nil(t, tile)
			return
		}
		features, points, triangles := 0, 0, 0
		for _, layer := range tile.Layers {
			features += len(layer)
			for _, feature := range layer {
				points += feature.PointCount()
				for _, polygon := range feature.Polygons {
					triangles += len(polygon.Indices) / 3
				}
			}
		}
		assert.LessOrEqual(t, features, MaxTileFeatures)
		assert.LessOrEqual(t, points, MaxGeometryPoints)
		assert.LessOrEqual(t, triangles, MaxTileStyleTriangles)
	})
}
