package mvt

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Typed-value regression moved from the Qt-bound parent package.
func TestDecodeTypedValues(t *testing.T) {
	tests := []struct {
		name     string
		message  []byte
		expected any
	}{
		{"string", bytesField(nil, 1, []byte("road")), "road"},
		{"float", append([]byte{21}, binary.LittleEndian.AppendUint32(nil, math.Float32bits(1.5))...), float32(1.5)},
		{"double", append([]byte{25}, binary.LittleEndian.AppendUint64(nil, math.Float64bits(2.5))...), float64(2.5)},
		{"int", varintField(nil, 4, 42), int64(42)},
		{"uint", varintField(nil, 5, 43), uint64(43)},
		{"sint", varintField(nil, 6, 9), int64(-5)},
		{"bool", varintField(nil, 7, 1), true},
		{"negativeInt", varintField(nil, 4, math.MaxUint64), int64(-1)},
		{"empty", nil, nil},
		{"unknown", varintField(nil, 8, 100), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, err := decodeValue(tt.message)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, value.value)
		})
	}
	for _, data := range [][]byte{{0}, {10}, {21}, {25}, {32}, {40}, {48}, {56}, {67}} {
		_, err := decodeValue(data)
		require.Error(t, err)
	}
}

func TestParseLayers(t *testing.T) {
	feature := featureMessage(PointType, []uint32{9, 32, 64})
	data := tileMessage(layerMessage("places", feature), layerMessage("places"))
	layers, err := DecodeLayers(data)
	require.NoError(t, err)
	require.Len(t, layers, 2)
	assert.Equal(t, "places", layers[0].Name())
	assert.Equal(t, uint32(4096), layers[0].Extent())
	require.Len(t, layers[0].FeatureData(), 1)
	assert.Equal(t, feature, layers[0].FeatureData()[0])
	class, err := layers[0].FeatureClass([]uint32{0, 0})
	require.NoError(t, err)
	assert.Equal(t, "primary", class)
	class, err = layers[0].FeatureClass(nil)
	require.NoError(t, err)
	assert.Empty(t, class)
	_, err = layers[0].FeatureClass([]uint32{0})
	require.Error(t, err)
	for _, tags := range [][]uint32{{0}, {1, 0}, {0, 1}, {math.MaxUint32, math.MaxUint32}} {
		properties, err := layers[0].FeatureProperties(tags)
		require.Error(t, err)
		assert.Nil(t, properties)
	}
	// Metadata is copied, raw feature payloads intentionally borrow input.
	clear(data)
	assert.Equal(t, "places", layers[0].Name())
	assert.Equal(t, make([]byte, len(feature)), layers[0].FeatureData()[0])
}

func TestParseLayerFraming(t *testing.T) {
	for _, payload := range [][]byte{
		{0}, {10}, {18}, {26}, {34}, {34, 1, 0}, {40},
		varintField(nil, 5, 0), varintField(nil, 5, uint64(math.MaxUint32)+1),
		{67},
	} {
		layers, err := DecodeLayers(tileMessage(payload))
		require.Error(t, err)
		assert.Nil(t, layers)
	}
	for _, data := range [][]byte{{1}, {0, 0}, {26, 255}, {24, 0}, {11, 0}} {
		layers, err := DecodeLayers(data)
		require.Error(t, err)
		assert.Nil(t, layers)
	}
	// Servers answer empty tiles with zero bytes.
	for _, data := range [][]byte{nil, {}} {
		layers, err := DecodeLayers(data)
		require.NoError(t, err)
		assert.Empty(t, layers)
	}
	// Unknown fields are skipped; extent defaults to 4096 without field 5.
	layers, err := DecodeLayers(append(varintField(nil, 9, 10), tileMessage(varintField(nil, 15, 2))...))
	require.NoError(t, err)
	require.Len(t, layers, 1)
	assert.Equal(t, uint32(4096), layers[0].Extent())
}

func TestParseFeatureFields(t *testing.T) {
	data := varintField(nil, 1, 0)
	data = bytesField(data, 2, []byte{0})
	data = varintField(data, 2, 0)
	data = varintField(data, 3, PointType)
	data = bytesField(data, 4, []byte{9, 32})
	data = varintField(data, 4, 64)
	data = varintField(data, 99, 5)
	feature, err := DecodeFeature(data)
	require.NoError(t, err)
	assert.True(t, feature.HasID)
	assert.Zero(t, feature.ID)
	assert.Equal(t, []uint32{0, 0}, feature.Tags)
	assert.Equal(t, []uint32{9, 32, 64}, feature.Geometry)
	assert.Equal(t, uint32(PointType), feature.GeometryType)
	clear(data)
	assert.Equal(t, []uint32{9, 32, 64}, feature.Geometry)
	feature, err = DecodeFeature(nil)
	require.NoError(t, err)
	assert.False(t, feature.HasID)
	for _, data := range [][]byte{
		{0}, {8}, {18}, {24}, {34}, {43},
		varintField(nil, 3, uint64(math.MaxUint32)+1),
		varintField(nil, 2, uint64(math.MaxUint32)+1),
		bytesField(nil, 4, binary.AppendUvarint(nil, uint64(math.MaxUint32)+1)),
	} {
		feature, err := DecodeFeature(data)
		require.Error(t, err)
		assert.Equal(t, RawFeature{}, feature)
	}
}

func TestTileInputLimit(t *testing.T) {
	data := make([]byte, MaxTileBytes+1)
	_, err := DecodeLayers(data)
	require.ErrorIs(t, err, ErrTileResourceLimit)
	_, err = DecodeFeature(data)
	require.ErrorIs(t, err, ErrTileResourceLimit)
	tile, err := DecodeTile(data, false)
	require.ErrorIs(t, err, ErrTileResourceLimit)
	assert.Nil(t, tile)
	// An exact-size message consisting of one ignored bytes field is accepted.
	data = bytesField(nil, 9, make([]byte, MaxTileBytes-4))
	require.Len(t, data, MaxTileBytes)
	tile, err = DecodeTile(data, false)
	require.NoError(t, err)
	assert.Empty(t, tile.Layers)
}

func bytesField(data []byte, field int, payload []byte) []byte {
	data = binary.AppendUvarint(data, uint64(field<<3|2))
	data = binary.AppendUvarint(data, uint64(len(payload)))
	return append(data, payload...)
}

func varintField(data []byte, field int, value uint64) []byte {
	return binary.AppendUvarint(binary.AppendUvarint(data, uint64(field<<3)), value)
}

func tileMessage(layers ...[]byte) []byte {
	var data []byte
	for _, layer := range layers {
		data = bytesField(data, 3, layer)
	}
	return data
}

func layerMessage(name string, features ...[]byte) []byte {
	data := bytesField(nil, 1, []byte(name))
	for _, feature := range features {
		data = bytesField(data, 2, feature)
	}
	data = bytesField(data, 3, []byte("class"))
	data = bytesField(data, 4, bytesField(nil, 1, []byte("primary")))
	return varintField(data, 5, 4096)
}

func featureMessage(kind uint32, commands []uint32) []byte {
	data := bytesField(nil, 2, []byte{0, 0})
	data = varintField(data, 3, uint64(kind))
	var packed []byte
	for _, command := range commands {
		packed = binary.AppendUvarint(packed, uint64(command))
	}
	return bytesField(data, 4, packed)
}
