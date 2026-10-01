package tiles

import (
	"unsafe"

	"github.com/rubiojr/whereami/pkg/vecmap/placement"
	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
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

// RetainedBytes charges a decoded source like Prepared.RetainedBytes: feature
// arrays, geometry and triangulation backing, and property keys and string
// values. String backing borrowed from the response is charged here as well.
func (s *Source) RetainedBytes() uint64 {
	if s == nil {
		return 0
	}
	n := uint64(unsafe.Sizeof(*s)+unsafe.Sizeof(*s.tile)) + arrayBytes(s.tile.Limits)
	for name, features := range s.tile.Layers {
		n += uint64(len(name)) + uint64(unsafe.Sizeof(features)) + arrayBytes(features)
		for _, f := range features {
			n += arrayBytes(f.Points) + arrayBytes(f.Lines) + arrayBytes(f.Polygons)
			for _, line := range f.Lines {
				n += arrayBytes(line)
			}
			for _, polygon := range f.Polygons {
				n += arrayBytes(polygon.Exterior) + arrayBytes(polygon.Holes) + arrayBytes(polygon.Vertices) + arrayBytes(polygon.Indices)
				for _, hole := range polygon.Holes {
					n += arrayBytes(hole)
				}
			}
			for key, value := range f.Properties {
				n += uint64(len(key)) + uint64(unsafe.Sizeof(key)+unsafe.Sizeof(value))
				if text, ok := value.(string); ok {
					n += uint64(len(text))
				}
			}
		}
	}
	for _, v := range s.tile.Limits {
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
		n += arrayBytes(m.Vertices) + arrayBytes(m.Offsets) + arrayBytes(m.Positions) + arrayBytes(m.Indices)
	}
	for _, t := range s.Textures {
		n += arrayBytes(t.RGBA)
	}
	return n
}

// SetCopyBytes charges only the additional owned metadata created by Set.Apply.
// Payload buffers and candidate string backing remain borrowed from the fragment.
// Like RetainedBytes, this excludes runtime/allocator overhead and assumes valid
// bounded input. Lease snapshots are charged independently by their owner.
func (f *Fragment) SetCopyBytes(tile view.TileID) uint64 {
	if f == nil {
		return 0
	}
	key := tileKey(tile)
	return retained.CopyBytes(key, f.Scene) + uint64(unsafe.Sizeof(fragment{})+unsafe.Sizeof(tile)) + uint64(len(key)) +
		uint64(len(f.Draws))*uint64(unsafe.Sizeof(Draw{})) + uint64(len(f.Symbols))*uint64(unsafe.Sizeof(Symbol{}))
}

func copyValues[T any](values []T) []T {
	if values == nil {
		return nil
	}
	result := make([]T, len(values))
	copy(result, values)
	return result
}

func symbolStrings(s placement.Symbol) uint64 {
	return uint64(len(s.LayerID)) + uint64(len(s.Text)) + uint64(len(s.FontFamily)) + uint64(len(s.FontStack)) +
		uint64(len(s.TextAnchor)) + uint64(len(s.TextJustify)) + uint64(len(s.IconName)) + uint64(len(s.IconAnchor))
}
