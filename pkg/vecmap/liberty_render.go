package vecmap

import (
	"errors"
	"fmt"

	"github.com/rubiojr/whereami/pkg/vecmap/compiler"
	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
)

const maxTileRenderedTriangles = geometry.MaxLineTriangles

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
	totalTriangles := 0
	appendPrimitive := func(layer compiledLibertyLayer, mesh geometry.Mesh, color mapColor) error {
		count := len(mesh.Vertices)
		if mesh.Indices != nil {
			count = len(mesh.Indices)
		}
		if count == 0 || color.Alpha == 0 {
			return nil
		}
		if count%3 != 0 {
			return errors.New("liberty compiler produced an incomplete triangle")
		}
		triangleCount := count / 3
		if triangleCount > maxTileRenderedTriangles-totalTriangles {
			return fmt.Errorf("%w: liberty geometry exceeds %d-triangle limit", errFeatureResourceLimit, maxTileRenderedTriangles)
		}
		totalTriangles += triangleCount
		primitives = append(primitives, libertyRenderPrimitive{
			order:     layer.Order,
			layerID:   layer.ID,
			triangles: mesh.Vertices,
			indices:   mesh.Indices,
			color:     color,
		})
		return nil
	}
	appendPattern := func(
		layer compiledLibertyLayer,
		mesh geometry.Mesh,
		patternName string,
		patternScale, opacity float64,
	) error {
		count := len(mesh.Vertices)
		if mesh.Indices != nil {
			count = len(mesh.Indices)
		}
		if count == 0 || patternName == "" || opacity <= 0 {
			return nil
		}
		if count%3 != 0 {
			return errors.New("liberty compiler produced an incomplete patterned triangle")
		}
		triangleCount := count / 3
		if triangleCount > maxTileRenderedTriangles-totalTriangles {
			return fmt.Errorf("%w: liberty geometry exceeds %d-triangle limit", errFeatureResourceLimit, maxTileRenderedTriangles)
		}
		totalTriangles += triangleCount
		primitives = append(primitives, libertyRenderPrimitive{
			order:        layer.Order,
			layerID:      layer.ID,
			triangles:    mesh.Vertices,
			indices:      mesh.Indices,
			patternName:  patternName,
			patternScale: patternScale,
			opacity:      opacity,
		})
		return nil
	}

	for _, layer := range layers {
		if !layer.VisibleAt(zoom) || libertyLayerHidden(layer) {
			continue
		}
		switch layer.Kind {
		case "background":
			evaluation := libertyEvaluation{zoom: zoom}
			color, ok := libertyEvaluatedColor(layer, "background-color", evaluation, mapColor{Alpha: 255})
			if !ok {
				continue
			}
			opacity := libertyEvaluatedNumber(layer, "background-opacity", evaluation, 1)
			if err := appendPrimitive(layer, backgroundGeometry(indexed), libertyColorWithOpacity(color, opacity)); err != nil {
				return err
			}
		case "fill", "fill-extrusion":
			if err := compileLibertyFillLayer(bucket, layer, zoom, indexed, appendPrimitive, appendPattern); err != nil {
				return err
			}
		case "line":
			if err := compileLibertyLineLayer(bucket, layer, zoom, indexed, appendPrimitive); err != nil {
				return err
			}
		case "symbol":
			if err := compileLibertySymbolLayer(bucket, layer, zoom); err != nil {
				return err
			}
		}
	}
	bucket.liberty = primitives
	bucket.compiledZoom = zoom
	bucket.compiled = true
	return nil
}

func compileLibertyFillLayer(
	bucket *tileBucket,
	layer compiledLibertyLayer,
	zoom float64,
	indexed bool,
	appendPrimitive func(compiledLibertyLayer, geometry.Mesh, mapColor) error,
	appendPattern func(compiledLibertyLayer, geometry.Mesh, string, float64, float64) error,
) error {
	return compiler.CompileFill(bucket.sourceLayers[layer.SourceLayer], layer,
		compiler.LayerOptions{SourceZoom: int(bucket.tile.Z), Zoom: zoom, Indexed: indexed},
		func(mesh geometry.Mesh, color mapColor) error { return appendPrimitive(layer, mesh, color) },
		func(mesh geometry.Mesh, name string, scale, opacity float64) error {
			return appendPattern(layer, mesh, name, scale, opacity)
		})
}

func compileLibertyLineLayer(
	bucket *tileBucket,
	layer compiledLibertyLayer,
	zoom float64,
	indexed bool,
	appendPrimitive func(compiledLibertyLayer, geometry.Mesh, mapColor) error,
) error {
	return compiler.CompileLine(bucket.sourceLayers[layer.SourceLayer], layer,
		compiler.LayerOptions{SourceZoom: int(bucket.tile.Z), Zoom: zoom, Indexed: indexed},
		func(mesh geometry.Mesh, color mapColor) error { return appendPrimitive(layer, mesh, color) })
}

func libertyLayerHidden(layer compiledLibertyLayer) bool {
	return layer.Hidden()
}

func libertyEvaluatedColor(
	layer compiledLibertyLayer,
	name string,
	evaluation libertyEvaluation,
	fallback mapColor,
) (mapColor, bool) {
	return layer.ColorValue(name, evaluation.context(), fallback)
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
