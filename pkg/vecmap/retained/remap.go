package retained

import (
	"bytes"
	"math"
	"slices"

	"github.com/rubiojr/whereami/pkg/vecmap/scene"
)

// remap assigns store-wide identities to one replacement. A resource whose payload
// is byte-identical to the previous revision of the same store ID keeps that
// revision: the version still names exactly those immutable bytes, so an adapter
// holding it resident needs no upload or retirement. Changed or new resources take
// the fragment's next revision. The second result counts retained revisions.
func remap(input *scene.Scene, old *fragment, nextID *uint64) (*fragment, int, error) {
	revision := uint64(1)
	var oldMeshes, oldTextures map[uint64]uint64
	var previousMeshes map[uint64]*scene.Mesh
	var previousTextures map[uint64]*scene.Texture
	if old != nil {
		if old.revision == math.MaxUint64 {
			return nil, 0, ErrLimit
		}
		revision = old.revision + 1
		oldMeshes, oldTextures = old.meshes, old.textures
		previousMeshes = make(map[uint64]*scene.Mesh, len(old.scene.Meshes))
		for i := range old.scene.Meshes {
			previousMeshes[old.scene.Meshes[i].ID] = &old.scene.Meshes[i]
		}
		previousTextures = make(map[uint64]*scene.Texture, len(old.scene.Textures))
		for i := range old.scene.Textures {
			previousTextures[old.scene.Textures[i].ID] = &old.scene.Textures[i]
		}
	}
	p := &fragment{revision: revision, scene: scene.Scene{
		Meshes: copyMetadata(input.Meshes), Textures: copyMetadata(input.Textures), Draws: copyMetadata(input.Draws)},
		meshes: make(map[uint64]uint64, len(input.Meshes)), textures: make(map[uint64]uint64, len(input.Textures))}
	reused := 0
	for i := range p.scene.Meshes {
		m := &p.scene.Meshes[i]
		id, err := identity(m.ID, oldMeshes, nextID)
		if err != nil {
			return nil, 0, err
		}
		p.meshes[m.ID] = id
		m.ID, m.Revision = id, revision
		if previous := previousMeshes[id]; previous != nil && sameMesh(previous, m) {
			m.Revision = previous.Revision
			reused++
		}
	}
	for i := range p.scene.Textures {
		t := &p.scene.Textures[i]
		id, err := identity(t.ID, oldTextures, nextID)
		if err != nil {
			return nil, 0, err
		}
		p.textures[t.ID] = id
		t.ID, t.Revision = id, revision
		if previous := previousTextures[id]; previous != nil && sameTexture(previous, t) {
			t.Revision = previous.Revision
			reused++
		}
	}
	for i := range p.scene.Draws {
		d := &p.scene.Draws[i]
		d.Mesh = p.meshes[d.Mesh]
		if d.Material.Texture != 0 {
			d.Material.Texture = p.textures[d.Material.Texture]
		}
	}
	return p, reused, nil
}

// Payload equality is exact and validated input has no NaN, so float comparison
// is total. Indices compare by length and content: an unindexed mesh never equals
// an indexed one. Every vertex section must match.
func sameMesh(a, b *scene.Mesh) bool {
	return sharedMesh(a, b) || len(a.Indices) == len(b.Indices) && slices.Equal(a.Vertices, b.Vertices) && slices.Equal(a.Offsets, b.Offsets) &&
		slices.Equal(a.Positions, b.Positions) && slices.Equal(a.PackedPositions, b.PackedPositions) && slices.Equal(a.PackedOffsets, b.PackedOffsets) &&
		slices.Equal(a.PackedDashed, b.PackedDashed) && slices.Equal(a.Indices, b.Indices)
}

// sharedMesh reports whether two meshes are the same immutable buffers, as when
// a replacement borrows its predecessor's geometry, which makes them equal
// without comparing their content.
func sharedMesh(a, b *scene.Mesh) bool {
	return sameBuffer(a.Vertices, b.Vertices) && sameBuffer(a.Offsets, b.Offsets) && sameBuffer(a.Positions, b.Positions) &&
		sameBuffer(a.PackedPositions, b.PackedPositions) && sameBuffer(a.PackedOffsets, b.PackedOffsets) && sameBuffer(a.PackedDashed, b.PackedDashed) &&
		sameBuffer(a.Indices, b.Indices) && (a.Indices == nil) == (b.Indices == nil)
}

func sameBuffer[T any](a, b []T) bool {
	return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
}

func sameTexture(a, b *scene.Texture) bool {
	return a.Width == b.Width && a.Height == b.Height && bytes.Equal(a.RGBA, b.RGBA)
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
