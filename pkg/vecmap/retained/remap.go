package retained

import (
	"math"
	"slices"

	"github.com/rubiojr/whereami/pkg/vecmap/scene"
)

func remap(input *scene.Scene, old *fragment, nextID *uint64) (*fragment, error) {
	revision := uint64(1)
	var oldMeshes, oldTextures map[uint64]uint64
	if old != nil {
		if old.revision == math.MaxUint64 {
			return nil, ErrLimit
		}
		revision = old.revision + 1
		oldMeshes, oldTextures = old.meshes, old.textures
	}
	p := &fragment{revision: revision, scene: scene.Scene{
		Meshes: slices.Clone(input.Meshes), Textures: slices.Clone(input.Textures), Draws: slices.Clone(input.Draws)},
		meshes: make(map[uint64]uint64, len(input.Meshes)), textures: make(map[uint64]uint64, len(input.Textures))}
	for i := range p.scene.Meshes {
		m := &p.scene.Meshes[i]
		id, err := identity(m.ID, oldMeshes, nextID)
		if err != nil {
			return nil, err
		}
		p.meshes[m.ID] = id
		m.ID, m.Revision = id, revision
	}
	for i := range p.scene.Textures {
		t := &p.scene.Textures[i]
		id, err := identity(t.ID, oldTextures, nextID)
		if err != nil {
			return nil, err
		}
		p.textures[t.ID] = id
		t.ID, t.Revision = id, revision
	}
	for i := range p.scene.Draws {
		d := &p.scene.Draws[i]
		d.Mesh = p.meshes[d.Mesh]
		if d.Material.Texture != 0 {
			d.Material.Texture = p.textures[d.Material.Texture]
		}
	}
	return p, nil
}

func identity(local uint64, old map[uint64]uint64, next *uint64) (uint64, error) {
	if id := old[local]; id != 0 {
		return id, nil
	}
	if *next == 0 {
		return 0, ErrLimit
	}
	id := *next
	(*next)++ // zero marks exhausted; IDs are never reused after removal.
	return id, nil
}
