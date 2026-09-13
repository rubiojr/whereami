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
	mesh, err := geometry.TessellateLines(paths, geometry.LineStyle{Width: paint.width, Offset: paint.offset, Dashes: paint.dashes, Cap: paint.lineCap, Join: paint.lineJoin}, maximumTriangles, indexed)
	if err != nil {
		return geometry.Mesh{}, fmt.Errorf("%w: %v", errFeatureResourceLimit, err)
	}
	return mesh, nil
}
