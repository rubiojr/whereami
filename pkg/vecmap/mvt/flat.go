package mvt

import (
	"unsafe"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
)

// PropertySource reads a feature's properties. A duplicate key reads as its
// last value, as in Properties.
type PropertySource interface {
	Get(name string) (any, bool)
}

// Features is a source layer's features. DecodeTile stores them flat in a
// FeatureSet; FeatureSlice adapts decoded []Feature.
type Features interface {
	Len() int
	// At fills dst with feature i. Geometry and property values borrow the
	// source's storage, valid while the source lives. dst's own containers
	// (Lines, Polygons, Holes, Properties) are reused by the next At on dst.
	At(i int, dst *Feature)
}

// FeatureSlice is Features over decoded []Feature.
type FeatureSlice []Feature

func (s FeatureSlice) Len() int { return len(s) }
func (s FeatureSlice) At(i int, dst *Feature) {
	scratch := dst.scratch
	*dst = s[i]
	dst.scratch = scratch
}

// featureScratch holds the containers FeatureSet.At reuses. They are never
// a decoded Feature's own slices, which At must not overwrite.
type featureScratch struct {
	properties flatProperties
	lines      [][]geometry.Point
	polygons   []Polygon
	holes      [][]geometry.Point
}

// FeatureSet stores one source layer's decoded features in a few flat arrays:
// geometry, ring boundaries and triangulation as values, and properties as
// tag pairs into the layer's key and value tables. Retaining it costs the
// garbage collector a few objects per layer instead of several per feature.
// It is immutable after DecodeTile and may be read concurrently.
type FeatureSet struct {
	keys     []string
	values   []any
	features []flatFeature
	tags     []uint32         // key and value index pairs
	points   []geometry.Point // point features, then line and ring vertices
	parts    []span           // points of each line or ring
	polygons []flatPolygon
	vertices []geometry.Point // triangulation positions or expanded triangles
	indices  []uint32
}

type flatFeature struct {
	id                   uint64
	hasID                bool
	geometryType         uint32
	tags                 span // into tags
	points, parts, polys span // points of a point feature; lines; polygons
}

type flatPolygon struct {
	parts            span // exterior ring, then holes
	vertices, index  span
	hasIndices, used bool
}

type span struct{ start, end uint32 }

func (s *FeatureSet) Len() int {
	if s == nil {
		return 0
	}
	return len(s.features)
}

// At materializes feature i. See Features.
func (s *FeatureSet) At(i int, dst *Feature) {
	f := &s.features[i]
	scratch := dst.scratch
	scratch.lines, scratch.polygons, scratch.holes = scratch.lines[:0], scratch.polygons[:0], scratch.holes[:0]
	*dst = Feature{ID: f.id, HasID: f.hasID, GeometryType: f.geometryType}
	if f.points.end > f.points.start {
		dst.Points = s.points[f.points.start:f.points.end:f.points.end]
	}
	for part := f.parts.start; part < f.parts.end; part++ {
		scratch.lines = append(scratch.lines, s.part(part))
	}
	for index := f.polys.start; index < f.polys.end; index++ {
		p := &s.polygons[index]
		polygon := Polygon{Exterior: s.part(p.parts.start)}
		start := len(scratch.holes)
		for part := p.parts.start + 1; part < p.parts.end; part++ {
			scratch.holes = append(scratch.holes, s.part(part))
		}
		if end := len(scratch.holes); end > start {
			polygon.Holes = scratch.holes[start:end:end]
		}
		if p.used {
			polygon.Vertices = s.vertices[p.vertices.start:p.vertices.end:p.vertices.end]
			if p.hasIndices {
				polygon.Indices = s.indices[p.index.start:p.index.end:p.index.end]
			}
		}
		scratch.polygons = append(scratch.polygons, polygon)
	}
	scratch.properties = flatProperties{set: s, tags: s.tags[f.tags.start:f.tags.end:f.tags.end]}
	dst.scratch = scratch
	dst.Properties = &dst.scratch.properties
	if len(scratch.lines) > 0 {
		dst.Lines = scratch.lines[:len(scratch.lines):len(scratch.lines)]
	}
	if len(scratch.polygons) > 0 {
		dst.Polygons = scratch.polygons[:len(scratch.polygons):len(scratch.polygons)]
	}
}

// part returns line or ring i, capped so an append never writes into the next.
func (s *FeatureSet) part(i uint32) []geometry.Point {
	p := s.parts[i]
	return s.points[p.start:p.end:p.end]
}

// addTables appends a raw layer's keys and values and returns their bases.
func (s *FeatureSet) addTables(l *Layer) (keyBase, valueBase uint32) {
	keyBase, valueBase = uint32(len(s.keys)), uint32(len(s.values))
	s.keys = append(s.keys, l.keys...)
	for _, value := range l.values {
		s.values = append(s.values, value.value)
	}
	return keyBase, valueBase
}

// add stores a decoded feature whose tags were validated against the raw
// layer whose tables start at the bases.
func (s *FeatureSet) add(feature Feature, tags []uint32, keyBase, valueBase uint32) {
	f := flatFeature{id: feature.ID, hasID: feature.HasID, geometryType: feature.GeometryType}
	f.tags.start = uint32(len(s.tags))
	for i := 0; i < len(tags); i += 2 {
		s.tags = append(s.tags, keyBase+tags[i], valueBase+tags[i+1])
	}
	f.tags.end = uint32(len(s.tags))
	f.points.start = uint32(len(s.points))
	s.points = append(s.points, feature.Points...)
	f.points.end = uint32(len(s.points))
	f.parts.start = uint32(len(s.parts))
	for _, line := range feature.Lines {
		s.addPart(line)
	}
	f.parts.end = uint32(len(s.parts))
	f.polys.start = uint32(len(s.polygons))
	for _, polygon := range feature.Polygons {
		p := flatPolygon{used: polygon.Vertices != nil || polygon.Indices != nil, hasIndices: polygon.Indices != nil}
		p.parts.start = uint32(len(s.parts))
		s.addPart(polygon.Exterior)
		for _, hole := range polygon.Holes {
			s.addPart(hole)
		}
		p.parts.end = uint32(len(s.parts))
		p.vertices.start = uint32(len(s.vertices))
		s.vertices = append(s.vertices, polygon.Vertices...)
		p.vertices.end = uint32(len(s.vertices))
		p.index.start = uint32(len(s.indices))
		s.indices = append(s.indices, polygon.Indices...)
		p.index.end = uint32(len(s.indices))
		s.polygons = append(s.polygons, p)
	}
	f.polys.end = uint32(len(s.polygons))
	s.features = append(s.features, f)
}

func (s *FeatureSet) addPart(points []geometry.Point) {
	start := uint32(len(s.points))
	s.points = append(s.points, points...)
	s.parts = append(s.parts, span{start, uint32(len(s.points))})
}

// RetainedBytes charges the flat arrays, key strings and string values, like
// the other logical CPU storage charges in this module.
func (s *FeatureSet) RetainedBytes() uint64 {
	if s == nil {
		return 0
	}
	n := arrayBytes(s.keys) + arrayBytes(s.values) + arrayBytes(s.features) + arrayBytes(s.tags) + arrayBytes(s.points) +
		arrayBytes(s.parts) + arrayBytes(s.polygons) + arrayBytes(s.vertices) + arrayBytes(s.indices)
	for _, key := range s.keys {
		n += uint64(len(key))
	}
	for _, value := range s.values {
		if text, ok := value.(string); ok {
			n += uint64(len(text))
		}
	}
	return n
}

func arrayBytes[T any](v []T) uint64 {
	var element T
	return uint64(cap(v)) * uint64(unsafe.Sizeof(element))
}

// flatProperties reads a FeatureSet feature's tag pairs.
type flatProperties struct {
	set  *FeatureSet
	tags []uint32
}

func (p *flatProperties) Get(name string) (any, bool) {
	for i := len(p.tags) - 2; i >= 0; i -= 2 {
		if p.set.keys[p.tags[i]] == name {
			return p.set.values[p.tags[i+1]], true
		}
	}
	return nil, false
}
