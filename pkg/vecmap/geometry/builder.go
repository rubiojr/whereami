package geometry

import (
	"errors"
	"math"
	"slices"
)

var ErrGeometryLimit = errors.New("geometry exceeds element limit")

// Builder appends triangle topology without hashing or reordering it. Indexed
// output retains source vertices; expanded output is a comparison/legacy path.
// Build off the render thread and stop mutating before publishing its slices.
type Builder[T any] struct {
	Vertices []T
	Topology
}

// Topology is the index side of a Builder, the same for every vertex type.
type Topology struct {
	Indices []uint32
	// Short and Segments replace Indices after ShortIndices: the indices from
	// a segment's First up to the next segment count from its Base vertex.
	Short    []uint16
	Segments []Segment
	indexed  bool
	short    bool
	limit    int
}

// Segment is where a run of Topology.Short starts and the vertex its indices
// count from.
type Segment struct{ First, Base int }

// MaxSegmentVertices is the number of vertices a segment's uint16 indices
// reach.
const MaxSegmentVertices = 1 << 16

func NewBuilder[T any](indexed bool, maximumElements int) Builder[T] {
	return Builder[T]{Topology: Topology{indexed: indexed, limit: maximumElements}}
}

// ShortIndices makes an empty indexed builder store uint16 indices in Short,
// in segments of at most MaxSegmentVertices vertices. Appended geometry that
// does not fit in the last segment starts a new one; geometry with more
// vertices than a segment holds is split between triangles, and each segment
// gets its own copy of the vertices it uses. Triangle order is unchanged.
// Expanded and nonempty builders ignore it.
func (b *Builder[T]) ShortIndices() {
	if b.indexed && len(b.Vertices) == 0 {
		b.short = true
	}
}

func (b *Builder[T]) Count() int {
	if b.short {
		return len(b.Short)
	}
	if b.indexed {
		return len(b.Indices)
	}
	return len(b.Vertices)
}

// Append accepts a triangle list or local indices into vertices. Validation is
// atomic: invalid topology or exhausted limits leave the builder unchanged.
func (b *Builder[T]) Append(vertices []T, indices []uint32) error {
	count, err := b.checkAppend(vertices, indices)
	if err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	if b.indexed {
		b.appendIndexed(vertices, indices)
	} else if indices == nil {
		b.Vertices = append(b.Vertices, vertices...)
	} else {
		b.Vertices = slices.Grow(b.Vertices, len(indices))
		for _, index := range indices {
			b.Vertices = append(b.Vertices, vertices[index])
		}
	}
	return nil
}

func (b *Builder[T]) checkAppend(vertices []T, indices []uint32) (int, error) {
	count := len(vertices)
	if indices != nil {
		count = len(indices)
	}
	if count%3 != 0 {
		return 0, errors.New("incomplete geometry triangle")
	}
	if b.limit < b.Count() || count > b.limit-b.Count() {
		return 0, ErrGeometryLimit
	}
	addedVertices := count
	if b.short {
		addedVertices = max(len(vertices), count) // split geometry copies shared vertices
	} else if b.indexed {
		addedVertices = len(vertices)
	}
	if uint64(b.Count())+uint64(count) > math.MaxUint32 || uint64(len(b.Vertices))+uint64(addedVertices) > math.MaxUint32 {
		return 0, ErrGeometryLimit
	}
	for _, index := range indices {
		if uint64(index) >= uint64(len(vertices)) {
			return 0, errors.New("geometry index out of range")
		}
	}
	return count, nil
}

func (b *Builder[T]) appendIndexed(vertices []T, indices []uint32) {
	if b.short {
		b.appendShort(vertices, indices)
		return
	}
	base := uint32(len(b.Vertices))
	b.Vertices = append(b.Vertices, vertices...)
	count := len(vertices)
	if indices != nil {
		count = len(indices)
	}
	b.Indices = slices.Grow(b.Indices, count)
	if indices == nil {
		for i := range vertices {
			b.Indices = append(b.Indices, base+uint32(i))
		}
	} else {
		for _, index := range indices {
			b.Indices = append(b.Indices, base+index)
		}
	}
}

// appendShort appends to the last segment, or to a new one when the vertices
// don't fit.
func (b *Builder[T]) appendShort(vertices []T, indices []uint32) {
	if len(vertices) > MaxSegmentVertices {
		b.appendSplit(vertices, indices)
		return
	}
	start := len(b.Vertices)
	if len(b.Segments) == 0 || start+len(vertices)-b.Segments[len(b.Segments)-1].Base > MaxSegmentVertices {
		b.Segments = append(b.Segments, Segment{First: len(b.Short), Base: start})
	}
	offset := start - b.Segments[len(b.Segments)-1].Base
	b.Vertices = append(b.Vertices, vertices...)
	if indices == nil {
		b.Short = slices.Grow(b.Short, len(vertices))
		for i := range vertices {
			b.Short = append(b.Short, uint16(offset+i))
		}
		return
	}
	b.Short = slices.Grow(b.Short, len(indices))
	for _, index := range indices {
		b.Short = append(b.Short, uint16(offset+int(index)))
	}
}

// appendSplit appends geometry larger than a segment to new segments, in
// triangle order, copying each vertex into every segment whose triangles use
// it.
func (b *Builder[T]) appendSplit(vertices []T, indices []uint32) {
	count := len(vertices)
	if indices != nil {
		count = len(indices)
	}
	// segment is the segment holding a vertex's copy, counting from one, and
	// local the copy's index there.
	segment, local := make([]int32, len(vertices)), make([]uint16, len(vertices))
	current, used := int32(0), MaxSegmentVertices
	b.Short = slices.Grow(b.Short, count)
	for first := 0; first < count; first += 3 {
		if used > MaxSegmentVertices-3 {
			current, used = current+1, 0
			b.Segments = append(b.Segments, Segment{First: len(b.Short), Base: len(b.Vertices)})
		}
		for corner := first; corner < first+3; corner++ {
			vertex := corner
			if indices != nil {
				vertex = int(indices[corner])
			}
			if segment[vertex] != current {
				segment[vertex], local[vertex] = current, uint16(used)
				b.Vertices = append(b.Vertices, vertices[vertex])
				used++
			}
			b.Short = append(b.Short, local[vertex])
		}
	}
}

// Reserve makes room for that many further vertices and indices, so appending
// them does not reallocate. It is a hint: counts are clamped to the element
// limit, and later appends still grow as needed. An expanded builder has no
// index buffer; pass the expanded vertex count.
func (b *Builder[T]) Reserve(vertices, indices int) {
	room := max(b.limit-b.Count(), 0)
	b.Vertices = reserve(b.Vertices, max(min(vertices, room), 0))
	if b.short {
		b.Short = reserve(b.Short, max(min(indices, room), 0))
	} else if b.indexed {
		b.Indices = reserve(b.Indices, max(min(indices, room), 0))
	}
}

// reserve makes room for exactly n further values. Growing by append would
// round the capacity up, and Compact would then copy a buffer that was reserved
// at its final length.
func reserve[S ~[]E, E any](values S, n int) S {
	if cap(values)-len(values) >= n {
		return values
	}
	exact := make(S, len(values), len(values)+n)
	copy(exact, values)
	return exact
}

// Compact replaces buffers that hold unused capacity with copies of their exact
// length. Contents are unchanged. Call once, before publishing the slices.
func (b *Builder[T]) Compact() {
	b.Vertices, b.Indices, b.Short = compact(b.Vertices), compact(b.Indices), compact(b.Short)
}

func compact[S ~[]E, E any](values S) S {
	if cap(values) == len(values) {
		return values
	}
	if len(values) == 0 {
		return nil
	}
	exact := make(S, len(values))
	copy(exact, values)
	return exact
}

func (b *Builder[T]) Triangle(first, second, third T) error {
	return b.Append([]T{first, second, third}, nil)
}
