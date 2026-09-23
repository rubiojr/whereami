package tiles

import (
	"os"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/fixture"
	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPinnedGeometryComposition(t *testing.T) {
	path := os.Getenv("WHEREAMI_VECTOR_TILE_FIXTURE")
	if path == "" {
		t.Skip("set WHEREAMI_VECTOR_TILE_FIXTURE for the pinned Madrid tile")
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	result, err := fixture.Compile(data, nil, fixture.Options{DirectIndexed: true})
	require.NoError(t, err)
	// The old packed fixture doesn't carry layer/candidate/precise pattern-period
	// provenance. Use its solid base draws with explicit source-order metadata;
	// this tests real payload sharing, not a generic tile compiler or label parity.
	source := *result.Scene
	source.Draws = nil
	f := &Fragment{Scene: &source}
	for i, draw := range result.Scene.Draws {
		if draw.Material.Kind == scene.Solid && draw.Clip != [4]float32{} {
			source.Draws = append(source.Draws, draw)
			f.Draws = append(f.Draws, Draw{Layer: i})
		}
	}
	require.NotEmpty(t, f.Draws)
	first, second := fixture.Tile(), fixture.Tile()
	second.X++
	s := newSet(t, retained.Limits{})
	require.NoError(t, s.Apply([]Change{{first, f}, {second, f}}))
	combined := selectTiles(t, s, []view.TileID{first, second}, nil, result.Camera)
	require.Len(t, combined.Scene.Meshes, 2)
	require.Len(t, combined.Scene.Draws, len(source.Draws)*2)
	for i, original := range source.Draws {
		for slot := range 2 {
			got := combined.Scene.Draws[i*2+slot]
			assert.Equal(t, slot, got.Transform)
			got.Mesh, got.Material.Texture, got.Transform = original.Mesh, original.Material.Texture, original.Transform
			assert.Equal(t, original, got)
		}
	}
	assert.Same(t, &source.Meshes[0].Vertices[0], &combined.Scene.Meshes[0].Vertices[0])
	assert.Same(t, &source.Meshes[0].Indices[0], &combined.Scene.Meshes[1].Indices[0])
	require.NoError(t, s.Apply([]Change{{second, f}}))
	updated := selectTiles(t, s, []view.TileID{first, second}, combined.Cover, result.Camera)
	assert.Equal(t, combined.Scene.Meshes[0], updated.Scene.Meshes[0])
	assert.Equal(t, combined.Scene.Meshes[1].ID, updated.Scene.Meshes[1].ID)
	assert.Equal(t, uint64(2), updated.Scene.Meshes[1].Revision)
	assert.Equal(t, uint64(1), combined.Scene.Meshes[1].Revision)
}
