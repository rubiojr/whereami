package vecmap

import (
	"fmt"
	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
)

func backgroundGeometry(indexed bool) geometry.Mesh {
	if !indexed {
		return geometry.Mesh{Vertices: backgroundTriangles()}
	}
	return geometry.Mesh{Vertices: []roadPoint{{X: 0, Y: 0}, {X: tileSize, Y: 0}, {X: tileSize, Y: tileSize}, {X: 0, Y: tileSize}}, Indices: []uint32{0, 1, 2, 0, 2, 3}}
}

func tessellateLibertyGeometry(paths [][]roadPoint, paint libertyLinePaint, maximumTriangles int, indexed bool) (geometry.Mesh, error) {
	if maximumTriangles < 0 || maximumTriangles > maxTileRenderedTriangles {
		return geometry.Mesh{}, fmt.Errorf("%w: invalid line triangle limit", errFeatureResourceLimit)
	}
	mesh := geometry.NewBuilder[roadPoint](indexed, maximumTriangles*3)
	capacity := libertyLineVertexCapacity(paths, paint, maximumTriangles)
	if indexed {
		mesh.Indices = make([]uint32, 0, capacity)
		capacity /= 2
	}
	mesh.Vertices = make([]roadPoint, 0, capacity)
	if err := appendLibertyLines(paths, paint, maximumTriangles, &mesh); err != nil {
		return geometry.Mesh{}, fmt.Errorf("%w: %v", errFeatureResourceLimit, err)
	}
	return geometry.Mesh{Vertices: mesh.Vertices, Indices: mesh.Indices}, nil
}
