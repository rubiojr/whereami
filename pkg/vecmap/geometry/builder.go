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
	Indices  []uint32
	indexed  bool
	limit    int
}

func NewBuilder[T any](indexed bool, maximumElements int) Builder[T] {
	return Builder[T]{indexed: indexed, limit: maximumElements}
}

func (b *Builder[T]) Count() int {
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
	if b.indexed {
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

func (b *Builder[T]) Triangle(first, second, third T) error {
	return b.Append([]T{first, second, third}, nil)
}
