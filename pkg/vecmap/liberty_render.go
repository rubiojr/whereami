package vecmap

import (
	"github.com/rubiojr/whereami/pkg/vecmap/compiler"
	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
)

type libertyRenderPrimitive struct {
	order     int
	layerID   string
	triangles []roadPoint
	// When present, indices addresses the unique vertices in triangles.
	indices      []uint32
	color        mapColor
	patternName  string
	patternScale float64
	opacity      float64
}

type libertyLinePaint struct {
	width    float64
	offset   float64
	dashes   []float64
	lineCap  string
	lineJoin string
}

func compileLibertyTile(bucket *tileBucket, zoom float64) error {
	return compileLibertyTileGeometry(bucket, zoom, false)
}

func compileLibertyTileGeometry(bucket *tileBucket, zoom float64, indexed bool) error {
	layers, err := compiledLibertyLayers()
	if err != nil {
		return err
	}
	bucket.compiled = false
	bucket.liberty = nil
	bucket.symbols = nil
	primitives := make([]libertyRenderPrimitive, 0, len(layers))
	err = compiler.CompileTile(layers, bucket.sourceLayers,
		compiler.LayerOptions{SourceZoom: int(bucket.tile.Z), Zoom: zoom, Indexed: indexed},
		func(primitive compiler.Primitive) error {
			primitives = append(primitives, libertyRenderPrimitive{
				order: primitive.Order, layerID: primitive.LayerID,
				triangles: primitive.Mesh.Vertices, indices: primitive.Mesh.Indices,
				color: primitive.Color, patternName: primitive.PatternName,
				patternScale: primitive.PatternScale, opacity: primitive.Opacity,
			})
			return nil
		}, func(layer compiledLibertyLayer) error {
			return compileLibertySymbolLayer(bucket, layer, zoom)
		})
	if err != nil {
		return err
	}
	bucket.liberty = primitives
	bucket.compiledZoom = zoom
	bucket.compiled = true
	return nil
}

func libertyLayerHidden(layer compiledLibertyLayer) bool {
	return layer.Hidden()
}

func libertyEvaluatedNumber(layer compiledLibertyLayer, name string, evaluation libertyEvaluation, fallback float64) float64 {
	return layer.NumberValue(name, evaluation.context(), fallback)
}

func tessellateLibertyLines(paths [][]roadPoint, paint libertyLinePaint, maximumTriangles int) ([]roadPoint, error) {
	mesh, err := tessellateLibertyGeometry(paths, paint, maximumTriangles, false)
	return mesh.Vertices, err
}

func offsetLibertyLine(path []roadPoint, offset float64) []roadPoint {
	return geometry.OffsetLine(path, offset)
}

func appendLibertyDisk(
	center roadPoint,
	radius float64,
	appendTriangle func(roadPoint, roadPoint, roadPoint) error,
) error {
	ring := geometry.DiskRing(center, radius)
	for section := range libertyDiskSections {
		if err := appendTriangle(center, ring[section], ring[section+1]); err != nil {
			return err
		}
	}
	return nil
}

const libertyDiskSections = geometry.DiskSections
