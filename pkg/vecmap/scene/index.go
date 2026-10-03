package scene

import "math"

// IndexMesh replaces repeated, bit-identical vertices with uint32 indices when
// this reduces total buffer bytes. Triangle order and all draw ranges stay valid.
// The input is never modified; already indexed meshes and meshes with more than
// the Vertices section are returned unchanged.
// Use before publication, or advance Revision before publishing a replacement.
func IndexMesh(mesh Mesh) (Mesh, error) {
	if err := mesh.Validate(); err != nil {
		return Mesh{}, err
	}
	if len(mesh.Indices) > 0 || len(mesh.Vertices) < 6 || mesh.VertexBytes() != uint64(len(mesh.Vertices))*24 {
		return mesh, nil
	}
	unique := make([]Vertex, 0, min(len(mesh.Vertices), 4096))
	indices := make([]uint32, len(mesh.Vertices))
	// Basemap vertices usually contain only XY. A uint64 key avoids hashing
	// four zero attributes for the overwhelmingly common case. Attribute-bearing
	// vertices still compare every bit, including signed zero, in a separate map.
	positions := make(map[uint64]uint32, len(mesh.Vertices)/2)
	attributes := make(map[[6]uint32]uint32)
	for i, vertex := range mesh.Vertices {
		key := vertexBits(vertex)
		var index uint32
		var exists bool
		if key[2]|key[3]|key[4]|key[5] == 0 {
			position := uint64(key[0])<<32 | uint64(key[1])
			index, exists = positions[position]
			if !exists {
				index = uint32(len(unique))
				positions[position] = index
			}
		} else {
			index, exists = attributes[key]
			if !exists {
				index = uint32(len(unique))
				attributes[key] = index
			}
		}
		if !exists {
			unique = append(unique, vertex)
		}
		indices[i] = index
	}
	if uint64(len(unique))*24+uint64(len(indices))*4 >= mesh.BufferBytes() {
		return mesh, nil
	}
	mesh.Vertices = unique
	mesh.Indices = indices
	return mesh, nil
}

func vertexBits(v Vertex) [6]uint32 {
	return [6]uint32{math.Float32bits(v.X), math.Float32bits(v.Y), math.Float32bits(v.OffsetX), math.Float32bits(v.OffsetY), math.Float32bits(v.U), math.Float32bits(v.V)}
}
