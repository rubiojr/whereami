package mvt

import (
	"encoding/binary"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// duplicateKeyLayer has a feature naming "name" twice; the last value wins.
func duplicateKeyLayer() []byte {
	var commands []byte
	for _, command := range []uint32{9, 2, 4} {
		commands = binary.AppendUvarint(commands, uint64(command))
	}
	feature := bytesField(nil, 2, []byte{0, 0, 1, 1, 1, 2})
	feature = varintField(feature, 3, uint64(PointType))
	feature = bytesField(feature, 4, commands)
	data := bytesField(nil, 1, []byte("dups"))
	data = bytesField(data, 2, feature)
	for _, key := range []string{"class", "name"} {
		data = bytesField(data, 3, []byte(key))
	}
	for _, value := range []string{"primary", "a", "b"} {
		data = bytesField(data, 4, bytesField(nil, 1, []byte(value)))
	}
	return varintField(data, 5, 4096)
}

// A FeatureSet materializes exactly the features DecodeLayer decodes.
func TestFeatureSetMatchesDecodeLayer(t *testing.T) {
	inputs := [][]byte{
		tileMessage(layerMessage("shared",
			varintField(featureMessage(PointType, []uint32{9, 32, 64}), 1, 0),
			featureMessage(LineStringType, []uint32{9, 0, 0, 10, 32, 64}),
		), layerMessage("shared", featureMessage(PolygonType, triangleCommands)), duplicateKeyLayer()),
	}
	if path := os.Getenv("WHEREAMI_VECTOR_TILE_FIXTURE"); path != "" {
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		inputs = append(inputs, data)
	}
	for _, data := range inputs {
		for _, indexed := range []bool{false, true} {
			tile, err := DecodeTile(data, indexed)
			require.NoError(t, err)
			layers, err := DecodeLayers(data)
			require.NoError(t, err)
			decoder := NewDecoder(indexed)
			want := make(map[string][]Feature)
			for _, layer := range layers {
				features, _ := decoder.DecodeLayer(layer)
				want[layer.name] = append(want[layer.name], features...)
			}
			for name, features := range want {
				if len(features) == 0 {
					assert.Nil(t, tile.Layers[name])
					continue
				}
				require.Equal(t, features, detached(tile.Layers[name]), name)
			}
		}
	}
	tile, err := DecodeTile(tileMessage(duplicateKeyLayer()), true)
	require.NoError(t, err)
	var feature Feature
	tile.Layers["dups"].At(0, &feature)
	name, ok := feature.Properties.Get("name")
	assert.True(t, ok)
	assert.Equal(t, "b", name)
	_, ok = feature.Properties.Get("missing")
	assert.False(t, ok)
}

// At reuses only its own containers: never a FeatureSlice feature's slices.
func TestFeatureSetAtKeepsOtherFeaturesIntact(t *testing.T) {
	data := tileMessage(layerMessage("roads",
		featureMessage(LineStringType, []uint32{9, 0, 0, 10, 32, 64}),
		featureMessage(LineStringType, []uint32{9, 2, 2, 10, 8, 8}),
	))
	tile, err := DecodeTile(data, true)
	require.NoError(t, err)
	layers, err := DecodeLayers(data)
	require.NoError(t, err)
	decoder := NewDecoder(true)
	legacy, _ := decoder.DecodeLayer(layers[0])
	before := detached(FeatureSlice(legacy))
	var feature Feature
	FeatureSlice(legacy).At(0, &feature)
	for i := range tile.Layers["roads"].Len() {
		tile.Layers["roads"].At(i, &feature)
	}
	assert.Equal(t, before, detached(FeatureSlice(legacy)))
	line := feature.Lines[0]
	assert.Equal(t, len(line), cap(line), "borrowed parts are capped")
	assert.Zero(t, (*FeatureSet)(nil).Len())
	assert.NotZero(t, tile.Layers["roads"].RetainedBytes())
}

// Reset drops every reference into the source but keeps At's containers.
func TestFeatureResetDropsTheSource(t *testing.T) {
	holed := []uint32{9, 0, 0, 26, 200, 0, 0, 200, 199, 0, 15, 9, 50, 149, 26, 0, 100, 100, 0, 0, 99, 15}
	tile, err := DecodeTile(tileMessage(layerMessage("mixed",
		featureMessage(LineStringType, []uint32{9, 0, 0, 10, 32, 64, 9, 2, 2, 10, 8, 8}),
		featureMessage(PolygonType, holed),
	)), true)
	require.NoError(t, err)
	set := tile.Layers["mixed"]
	var feature Feature
	read := func() {
		for i := range set.Len() {
			set.At(i, &feature)
		}
	}
	read()
	require.Len(t, feature.Polygons, 1)
	require.Len(t, feature.Polygons[0].Holes, 1)
	feature.Reset()
	scratch := feature.scratch
	assert.Equal(t, Feature{scratch: scratch}, feature)
	assert.Zero(t, scratch.properties)
	require.Positive(t, cap(scratch.lines))
	require.Positive(t, cap(scratch.polygons))
	require.Positive(t, cap(scratch.holes))
	for _, line := range scratch.lines[:cap(scratch.lines)] {
		assert.Nil(t, line)
	}
	for _, polygon := range scratch.polygons[:cap(scratch.polygons)] {
		assert.Zero(t, polygon)
	}
	for _, hole := range scratch.holes[:cap(scratch.holes)] {
		assert.Nil(t, hole)
	}
	assert.Zero(t, testing.AllocsPerRun(10, func() { read(); feature.Reset() }), "later reads reuse the containers")
}
