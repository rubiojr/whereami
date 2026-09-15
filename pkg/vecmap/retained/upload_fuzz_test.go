package retained

import (
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sceneVersions(s *scene.Scene) []Version {
	if s == nil {
		return nil
	}
	versions := make([]Version, 0, len(s.Meshes)+len(s.Textures))
	for _, m := range s.Meshes {
		versions = append(versions, Version{Kind: MeshResource, ID: m.ID, Revision: m.Revision})
	}
	for _, x := range s.Textures {
		versions = append(versions, Version{Kind: TextureResource, ID: x.ID, Revision: x.Revision})
	}
	return versions
}

func FuzzUploadTransitions(f *testing.F) {
	f.Add([]byte{0, 1, 2, 1, 2, 4, 1, 2, 8, 1, 2, 1, 2, 1, 2})
	f.Add([]byte{0, 1, 6, 1, 2, 1, 2, 4, 1, 2, 0, 1, 2})
	f.Fuzz(func(t *testing.T, operations []byte) {
		if len(operations) > 256 {
			t.Skip()
		}
		store := newStore(t, Limits{})
		targets := []*scene.Scene{nil}
		for range 3 {
			require.NoError(t, store.Apply([]Change{{"tile", triangle()}}))
			targets = append(targets, snapshot(t, store, "tile"))
		}
		p := planner(t, ResidencyLimits{Bytes: 176, Resources: 4})
		backend := make(map[Version]Resource)
		var pending *Batch
		var target *scene.Scene
		for _, op := range operations {
			switch op % 4 {
			case 0:
				desired := targets[int(op/4)%len(targets)]
				if err := p.SetTarget(desired); err == nil {
					target = desired
				}
			case 1:
				batch, err := p.Next(Budget{Bytes: 84 + uint64(op%5), Resources: 1 + int(op%2)})
				if err == nil && batch != nil {
					pending = batch
					var bytes uint64
					for _, r := range backend {
						bytes += r.bytes()
					}
					assert.LessOrEqual(t, bytes+batch.Bytes, uint64(176))
					assert.LessOrEqual(t, len(backend)+len(batch.Uploads), 4)
					for _, key := range batch.Releases {
						assert.NotContains(t, sceneVersions(p.Current()), key)
						assert.NotContains(t, sceneVersions(target), key)
					}
				}
			case 2:
				if pending != nil {
					success := op&4 == 0
					if success {
						for _, key := range pending.Releases {
							delete(backend, key)
						}
						for _, r := range pending.Uploads {
							backend[r.Version] = r
						}
					}
					require.NoError(t, p.Acknowledge(pending.Ticket, success))
					pending = nil
				}
			case 3:
				assert.ErrorIs(t, p.Acknowledge(0, true), ErrTicket)
			}
			for _, key := range sceneVersions(p.Current()) {
				assert.Contains(t, backend, key)
			}
			assert.Len(t, p.resident, len(backend))
		}
	})
}
