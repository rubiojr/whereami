package tiles

import (
	"unsafe"

	"github.com/rubiojr/whereami/pkg/vecmap/placement"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
)

// RetainedBytes charges backing-array capacities, value metadata and string
// lengths, counting shared storage repeatedly. It is a logical CPU storage charge,
// not a Go heap/RSS measurement: allocator/map overhead, temporary compiler work,
// and backing storage behind borrowed substrings belong to the producer's separate
// input/scratch allowance. Call only on immutable, successfully prepared values.
func (p *Prepared) RetainedBytes() uint64 {
	if p == nil {
		return 0
	}
	n := uint64(unsafe.Sizeof(*p)) + arrayBytes(p.orders) + arrayBytes(p.primitives) + arrayBytes(p.symbols) + arrayBytes(p.limits)
	for _, v := range p.primitives {
		n += arrayBytes(v.Mesh.Vertices) + arrayBytes(v.Mesh.Indices) + uint64(len(v.LayerID)) + uint64(len(v.PatternName))
	}
	for _, v := range p.symbols {
		n += symbolStrings(v)
	}
	for _, v := range p.limits {
		n += uint64(len(v.Name))
		if v.Last != nil {
			n += uint64(len(v.Last.Error()))
		}
	}
	return n
}

// RetainedBytes uses the same conservative logical charge as Prepared.RetainedBytes.
// Borrowed sprite pixels are charged in full, independently of the asset owner.
func (f *Fragment) RetainedBytes() uint64 {
	if f == nil {
		return 0
	}
	n := uint64(unsafe.Sizeof(*f)) + sceneBytes(f.Scene) + arrayBytes(f.Draws) + arrayBytes(f.Symbols)
	for _, v := range f.Symbols {
		n += symbolStrings(v.Candidate)
	}
	return n
}

// RetainedBytes charges a snapshot independently, even when it shares every
// payload with a Set or another snapshot. Map entries use key/value logical sizes;
// Go runtime overhead is excluded, as with the other CPU storage charges.
func (s *Snapshot) RetainedBytes() uint64 {
	if s == nil {
		return 0
	}
	return uint64(unsafe.Sizeof(*s)) + sceneBytes(s.Scene) + arrayBytes(s.Cover) + arrayBytes(s.TileSpaces) +
		uint64(len(s.Accepted))*uint64(unsafe.Sizeof(SymbolKey{})+unsafe.Sizeof(placement.Accepted{}))
}

func arrayBytes[T any](v []T) uint64 {
	var element T
	return uint64(cap(v)) * uint64(unsafe.Sizeof(element))
}

func sceneBytes(s *scene.Scene) uint64 {
	if s == nil {
		return 0
	}
	n := uint64(unsafe.Sizeof(*s)) + arrayBytes(s.Meshes) + arrayBytes(s.Textures) + arrayBytes(s.Draws)
	for _, m := range s.Meshes {
		n += arrayBytes(m.Vertices) + arrayBytes(m.Indices)
	}
	for _, t := range s.Textures {
		n += arrayBytes(t.RGBA)
	}
	return n
}

func symbolStrings(s placement.Symbol) uint64 {
	return uint64(len(s.LayerID)) + uint64(len(s.Text)) + uint64(len(s.FontFamily)) + uint64(len(s.FontStack)) +
		uint64(len(s.TextAnchor)) + uint64(len(s.TextJustify)) + uint64(len(s.IconName)) + uint64(len(s.IconAnchor))
}
