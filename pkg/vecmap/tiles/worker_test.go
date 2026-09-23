package tiles

import (
	"testing"
	"time"

	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAcknowledgedTileHandoff(t *testing.T) {
	s := newSet(t, retained.Limits{})
	parent, _ := testTile.Parent()
	other := view.TileID{X: 3, Y: 2, Z: 3}
	targets := []view.TileID{testTile, other}
	camera := testCamera(testTile)
	require.NoError(t, s.Apply([]Change{{parent, baseFragment(1)}}))
	coarse := selectTiles(t, s, targets, nil, camera)
	worker, err := retained.NewWorkerWithData[*Snapshot](retained.ResidencyLimits{Bytes: 1024, Resources: 8}, retained.Budget{Bytes: 72, Resources: 1})
	require.NoError(t, err)
	t.Cleanup(func() { worker.Close(); <-worker.Done() })
	generation, err := worker.RestartWithData(coarse.Scene, coarse)
	require.NoError(t, err)
	native := make(map[retained.Version]bool)
	next := func() retained.PacketWithData[*Snapshot] {
		t.Helper()
		var packet retained.PacketWithData[*Snapshot]
		require.Eventually(t, func() bool {
			var ok bool
			packet, ok = worker.Next()
			return ok
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, packet.Err)
		assert.Equal(t, generation, packet.Generation)
		if packet.Current != nil {
			require.NotNil(t, packet.CurrentData)
			assert.Same(t, packet.Current, packet.CurrentData.Scene)
			frame := packet.CurrentData.Frame(camera)
			for _, mesh := range packet.Current.Meshes {
				assert.True(t, native[retained.Version{Kind: retained.MeshResource, ID: mesh.ID, Revision: mesh.Revision}], "publication must only reference acknowledged uploads")
			}
			for _, draw := range frame.Scene.Draws {
				assert.Less(t, draw.Transform, len(frame.Transforms))
			}
		}
		return packet
	}
	ack := func(packet retained.PacketWithData[*Snapshot]) {
		t.Helper()
		if batch := packet.Batch; batch != nil {
			for _, resource := range batch.Uploads {
				native[resource.Version] = true
			}
			for _, release := range batch.Releases {
				if packet.Current != nil {
					for _, mesh := range packet.Current.Meshes {
						assert.NotEqual(t, retained.Version{Kind: retained.MeshResource, ID: mesh.ID, Revision: mesh.Revision}, release)
					}
				}
				delete(native, release) // fake backend completes retirement before ack
			}
		}
		require.True(t, worker.Acknowledge(packet.Generation, packet.Sequence, true))
	}
	p := next()
	assert.Nil(t, p.CurrentData)
	ack(p)
	p = next()
	assert.Same(t, coarse, p.CurrentData)
	ack(p)
	require.NoError(t, s.Apply([]Change{{testTile, baseFragment(1)}, {other, baseFragment(1)}}))
	refined := selectTiles(t, s, targets, coarse.Cover, camera)
	require.True(t, worker.SetTargetWithData(generation, refined.Scene, refined))
	for range 2 {
		p = next()
		assert.Same(t, coarse, p.CurrentData, "partial refinement preserves the complete old cover and mapping")
		require.Len(t, p.Batch.Uploads, 1)
		ack(p)
	}
	p = next()
	assert.Same(t, refined, p.CurrentData)
	require.Len(t, p.Batch.Releases, 1)
	assert.Equal(t, targets, p.CurrentData.Cover)
	ack(p)
	assert.Len(t, native, 2)
	// Camera-only selection preserves pointer identity, so producers need not
	// submit another target or trigger the Worker's deep resource validation.
	assert.Same(t, refined, selectTiles(t, s, targets, refined.Cover, camera.Panned(5, 0)))
}

func FuzzTileComposition(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5, 6})
	f.Add([]byte{3, 3, 3, 7, 2, 6, 1, 5})
	f.Fuzz(func(t *testing.T, operations []byte) {
		if len(operations) > 128 {
			return
		}
		s := newSet(t, retained.Limits{Fragments: 8, Draws: 32})
		parent, _ := testTile.Parent()
		tiles := []view.TileID{parent, testTile, {X: 3, Y: 2, Z: 3}, {X: 2, Y: 3, Z: 3}, {X: 3, Y: 3, Z: 3}}
		var previous []view.TileID
		for _, operation := range operations {
			tile := tiles[int(operation)%len(tiles)]
			var data *Fragment
			if operation&8 == 0 {
				data = baseFragment(2, 0)
			}
			require.NoError(t, s.Apply([]Change{{tile, data}}))
			out := selectTiles(t, s, tiles[1:], previous, testCamera(testTile))
			for i, tile := range out.Cover {
				_, exists := s.tiles[tile]
				assert.True(t, exists)
				for _, other := range out.Cover[:i] {
					assert.False(t, view.TilesOverlap(tile, other))
				}
			}
			for _, draw := range out.Scene.Draws {
				assert.Less(t, draw.Transform, len(out.TileSpaces))
			}
			previous = out.Cover // this fake backend consumes each target immediately
		}
	})
}
