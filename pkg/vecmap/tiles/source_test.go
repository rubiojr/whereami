package tiles

import (
	"os"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/fixture"
	"github.com/rubiojr/whereami/pkg/vecmap/liberty"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireSameAcrossZooms prepares one decoded source at each zoom and compares
// it with a fresh Prepare: sharing the source must not change the output.
func requireSameAcrossZooms(t *testing.T, data []byte, layers []style.CompiledLayer, options PrepareOptions, zooms []float64) {
	t.Helper()
	source, err := Decode(data, options.Indexed)
	require.NoError(t, err)
	charge := source.RetainedBytes()
	for _, zoom := range zooms {
		options.Zoom = zoom
		want, err := Prepare(data, layers, options)
		require.NoError(t, err)
		got, err := PrepareSource(source, layers, options)
		require.NoError(t, err)
		require.Equal(t, want, got, "zoom %g", zoom)
	}
	assert.Equal(t, charge, source.RetainedBytes(), "preparation leaves the source unchanged")
}

func TestPrepareSourceMatchesPrepare(t *testing.T) {
	for _, resident := range []bool{false, true} {
		for _, indexed := range []bool{false, true} {
			options := PrepareOptions{Tile: testTile, Indexed: indexed, ResidentGeometry: resident, ResidentSymbols: resident, ResidentDashes: resident, CompactVertices: resident}
			requireSameAcrossZooms(t, residentPBF(), residentStyle(t), options, []float64{3, 3.0625, 5, 3})
		}
	}
}

func TestPrepareSourceInput(t *testing.T) {
	source, err := Decode(residentPBF(), true)
	require.NoError(t, err)
	assert.NotZero(t, source.RetainedBytes())
	_, err = PrepareSource(source, residentStyle(t), PrepareOptions{Tile: testTile, Zoom: 3})
	assert.ErrorIs(t, err, ErrInput, "the source was decoded indexed")
	_, err = PrepareSource(nil, residentStyle(t), PrepareOptions{Tile: testTile, Zoom: 3, Indexed: true})
	assert.ErrorIs(t, err, ErrInput)
	_, err = PrepareSource(source, residentStyle(t), PrepareOptions{Tile: testTile, Zoom: 30, Indexed: true})
	assert.ErrorIs(t, err, ErrInput)
	_, err = Decode([]byte{0xff}, true)
	assert.Error(t, err)
	assert.Zero(t, (*Source)(nil).RetainedBytes())
}

func TestPrepareSourceMatchesPrepareOnPinnedTile(t *testing.T) {
	path := os.Getenv("WHEREAMI_VECTOR_TILE_FIXTURE")
	if path == "" {
		t.Skip("set the pinned tile fixture environment variable")
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	layers, err := liberty.Layers()
	require.NoError(t, err)
	for _, coarser := range []int{0, 1} {
		options := PrepareOptions{Tile: fixture.Tile(), Coarser: coarser, Indexed: true, ResidentGeometry: true, ResidentSymbols: true, ResidentDashes: true, CompactVertices: true}
		requireSameAcrossZooms(t, data, layers, options, []float64{9, 9.0625, 9.4375, 10, 9.0625})
	}
}
