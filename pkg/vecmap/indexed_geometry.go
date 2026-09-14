package vecmap

import (
	"fmt"
	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
)

func tessellateLibertyGeometry(paths [][]roadPoint, paint libertyLinePaint, maximumTriangles int, indexed bool) (geometry.Mesh, error) {
	mesh, err := geometry.TessellateLines(paths, geometry.LineStyle{Width: paint.width, Offset: paint.offset, Dashes: paint.dashes, Cap: paint.lineCap, Join: paint.lineJoin}, maximumTriangles, indexed)
	if err != nil {
		return geometry.Mesh{}, fmt.Errorf("%w: %v", errFeatureResourceLimit, err)
	}
	return mesh, nil
}
