package vecmap

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStyledDecodePreservesInputValidation(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		for _, tt := range []struct {
			name string
			data []byte
			tile vectorTileID
		}{
			{"empty", nil, pinnedTile},
			{"short", []byte{0}, pinnedTile},
			{"truncated", []byte{0xff, 0xff}, pinnedTile},
			{"unsupported-zoom", []byte{0, 0}, vectorTileID{Z: 31}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				_, legacyErr := decodeRoadBucketGeometry(tt.data, tt.tile, indexed)
				_, styledErr := decodeStyledBucketGeometry(tt.data, tt.tile, indexed)
				require.Error(t, legacyErr)
				require.EqualError(t, styledErr, legacyErr.Error())
			})
		}
	}
}

func TestStyledDecodeSkipsUnusedPreparation(t *testing.T) {
	data := benchmarkVectorTile(t)
	for _, indexed := range []bool{false, true} {
		legacy, err := decodeRoadBucketGeometry(data, pinnedTile, indexed)
		require.NoError(t, err)
		require.True(t, legacy.compiled)
		assert.Equal(t, float64(pinnedTile.Z), legacy.compiledZoom)
		assert.NotEmpty(t, legacy.segments)
		assert.Positive(t, fillTriangleCount(legacy.land)+fillTriangleCount(legacy.water))

		styled, err := decodeStyledBucketGeometry(data, pinnedTile, indexed)
		require.NoError(t, err)
		assert.False(t, styled.compiled, "decoding must not choose a style zoom")
		assert.Empty(t, styled.liberty)
		assert.Empty(t, styled.symbols)
		assert.Empty(t, styled.segments)
		assert.Zero(t, styled.featureCount)
		assert.Equal(t, fillBucket{}, styled.land)
		assert.Equal(t, fillBucket{}, styled.water)
		assert.Equal(t, legacy.sourceLayers, styled.sourceLayers)

		for _, zoom := range []float64{10, 10.5} {
			require.NoError(t, compileLibertyTileGeometry(legacy, zoom, indexed))
			require.NoError(t, compileLibertyTileGeometry(styled, zoom, indexed))
			assert.Equal(t, legacy.liberty, styled.liberty)
			assert.Equal(t, legacy.symbols, styled.symbols)
			assert.Equal(t, zoom, styled.compiledZoom)
		}
	}
}
