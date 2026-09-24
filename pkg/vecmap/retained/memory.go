package retained

import (
	"unsafe"

	"github.com/rubiojr/whereami/pkg/vecmap/scene"
)

// CopyBytes is the logical storage copied by Apply for one validated fragment.
// Geometry/index/RGBA buffers are borrowed and excluded. Map entries count their
// key/value sizes; allocator and bucket overhead remain separately count-bounded.
// Metadata slices use exact-length owned copies, matching remap.
func CopyBytes(key string, input *scene.Scene) uint64 {
	if input == nil {
		return 0
	}
	return uint64(unsafe.Sizeof(fragment{})+unsafe.Sizeof(key)+unsafe.Sizeof((*fragment)(nil))) + uint64(len(key)) +
		uint64(len(input.Meshes))*uint64(unsafe.Sizeof(scene.Mesh{})+16) +
		uint64(len(input.Textures))*uint64(unsafe.Sizeof(scene.Texture{})+16) +
		uint64(len(input.Draws))*uint64(unsafe.Sizeof(scene.Draw{}))
}

func copyMetadata[T any](values []T) []T {
	if values == nil {
		return nil
	}
	result := make([]T, len(values))
	copy(result, values)
	return result
}
