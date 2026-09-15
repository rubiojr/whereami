package retained

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/fixture"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPinnedMultiFragmentComposition(t *testing.T) {
	path, dir := os.Getenv("WHEREAMI_VECTOR_TILE_FIXTURE"), os.Getenv("WHEREAMI_VECTOR_GLYPH_FIXTURE_DIR")
	if path == "" || dir == "" {
		t.Skip("set pinned tile and glyph fixture environment variables")
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	result, err := fixture.CompileWithGlyphLoader(data, func(fonts []string) (map[string][]byte, error) {
		ranges := make(map[string][]byte)
		for _, font := range fonts {
			bytes, err := os.ReadFile(filepath.Join(dir, url.PathEscape(font)+".pbf"))
			if err != nil {
				return nil, err
			}
			ranges[font] = bytes
		}
		return ranges, nil
	}, fixture.Options{DirectIndexed: true})
	require.NoError(t, err)
	s := newStore(t, Limits{})
	require.NoError(t, s.Apply([]Change{{"tile-a", result.Scene}, {"tile-b", result.Scene}}))
	var order []Range
	// Deliberately interleave tile draw records rather than concatenating tiles.
	for i := range result.Scene.Draws {
		order = append(order, Range{Key: "tile-a", First: i, Count: 1}, Range{Key: "tile-b", First: i, Count: 1, Transform: 1})
	}
	combined, err := s.Snapshot(order)
	require.NoError(t, err)
	require.NoError(t, combined.Validate())
	require.Len(t, combined.Draws, 90)
	require.Len(t, combined.Meshes, 2)
	assert.Equal(t, result.Scene.Meshes[0].BufferBytes(), combined.Meshes[0].BufferBytes())
	assert.Same(t, &result.Scene.Meshes[0].Vertices[0], &combined.Meshes[1].Vertices[0])
	for i, original := range result.Scene.Draws {
		for instance := range 2 {
			got := combined.Draws[2*i+instance]
			assert.Equal(t, instance, got.Transform)
			got.Mesh, got.Material.Texture, got.Transform = original.Mesh, original.Material.Texture, original.Transform
			assert.Equal(t, original, got, "draw %d instance %d", i, instance)
		}
	}
	require.NoError(t, s.Apply([]Change{{"tile-b", result.Scene}}))
	updated, err := s.Snapshot(order)
	require.NoError(t, err)
	assert.Equal(t, combined.Meshes[0], updated.Meshes[0])
	assert.Equal(t, combined.Meshes[1].ID, updated.Meshes[1].ID)
	assert.Equal(t, uint64(2), updated.Meshes[1].Revision)
	assert.Equal(t, uint64(1), combined.Meshes[1].Revision)
	p := planner(t, ResidencyLimits{Bytes: 64 << 20, Resources: 32})
	budget := Budget{Bytes: 12 << 20, Resources: 2}
	require.NoError(t, p.SetTarget(combined))
	settle(t, p, budget)
	require.NoError(t, p.SetTarget(updated))
	assert.Same(t, combined, p.Current())
	for range 32 {
		batch, err := p.Next(budget)
		require.NoError(t, err)
		if batch == nil {
			break
		}
		var residentBytes uint64
		for _, resource := range p.resident {
			residentBytes += resource.bytes()
		}
		assert.LessOrEqual(t, residentBytes+batch.Bytes, p.limits.Bytes)
		assert.LessOrEqual(t, len(batch.Uploads)+len(batch.Releases), budget.Resources)
		assert.LessOrEqual(t, batch.Bytes, budget.Bytes)
		for _, version := range batch.Releases {
			assert.NotContains(t, p.activeSet, version)
			assert.NotContains(t, p.targetSet, version)
		}
		require.NoError(t, p.Acknowledge(batch.Ticket, true))
	}
	assert.Same(t, updated, p.Current())
	assert.Len(t, p.resident, len(updated.Meshes)+len(updated.Textures))
}
