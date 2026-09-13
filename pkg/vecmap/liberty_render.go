package vecmap

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

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
	color    mapColor
	width    float64
	offset   float64
	dashKey  string
	dashes   []float64
	lineCap  string
	lineJoin string
}

type libertyLineBatch struct {
	paint libertyLinePaint
	paths [][]roadPoint
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
	geometryScale := math.Exp2(zoom - float64(bucket.tile.Z))
	features := bucket.sourceLayers[layer.SourceLayer]
	type fillBatch struct {
		color mapColor
		mesh  geometry.Builder[roadPoint]
	}
	type patternBatch struct {
		name    string
		opacity float64
		mesh    geometry.Builder[roadPoint]
	}
	batches := make([]fillBatch, 0, 2)
	batchIndexes := make(map[mapColor]int)
	patternBatches := make([]patternBatch, 0, 2)
	patternIndexes := make(map[string]int)
	outlineBatches := make([]libertyLineBatch, 0, 2)
	outlineIndexes := make(map[string]int)
	colorProperty := "fill-color"
	if layer.Kind == "fill-extrusion" {
		colorProperty = "fill-extrusion-color"
	}
	for _, feature := range features {
		if feature.GeometryType != mvtPolygonType {
			continue
		}
		evaluation := libertyEvaluation{zoom: zoom, geometryID: feature.GeometryType, properties: feature.Properties}
		if !layer.Matches(evaluation.context()) {
			continue
		}
		opacityProperty := "fill-opacity"
		if layer.Kind == "fill-extrusion" {
			opacityProperty = "fill-extrusion-opacity"
		}
		opacity := libertyEvaluatedNumber(layer, opacityProperty, evaluation, 1)
		patternName := libertyEvaluatedString(layer, "fill-pattern", evaluation, "")
		if patternName != "" {
			patternKey := patternName + "/" + strconv.FormatFloat(opacity, 'g', -1, 64)
			patternIndex, exists := patternIndexes[patternKey]
			if !exists {
				patternIndex = len(patternBatches)
				patternIndexes[patternKey] = patternIndex
				patternBatches = append(patternBatches, patternBatch{name: patternName, opacity: opacity, mesh: geometry.NewBuilder[roadPoint](indexed, maxTileRenderedTriangles*3)})
			}
			for _, polygon := range feature.Polygons {
				if err := patternBatches[patternIndex].mesh.Append(polygon.Vertices, polygon.Indices); err != nil {
					return fmt.Errorf("%w: %v", errFeatureResourceLimit, err)
				}
			}
		} else {
			color, ok := libertyEvaluatedColor(layer, colorProperty, evaluation, mapColor{Alpha: 255})
			if !ok {
				continue
			}
			color = libertyColorWithOpacity(color, opacity)
			batchIndex, exists := batchIndexes[color]
			if !exists {
				batchIndex = len(batches)
				batchIndexes[color] = batchIndex
				batches = append(batches, fillBatch{color: color, mesh: geometry.NewBuilder[roadPoint](indexed, maxTileRenderedTriangles*3)})
			}
			for _, polygon := range feature.Polygons {
				if err := batches[batchIndex].mesh.Append(polygon.Vertices, polygon.Indices); err != nil {
					return fmt.Errorf("%w: %v", errFeatureResourceLimit, err)
				}
			}
		}

		outline, hasOutline := libertyEvaluatedColor(layer, "fill-outline-color", evaluation, mapColor{})
		if !hasOutline || outline.Alpha == 0 {
			continue
		}
		outline = libertyColorWithOpacity(outline, libertyEvaluatedNumber(layer, "fill-opacity", evaluation, 1))
		paint := libertyLinePaint{color: outline, width: 1 / geometryScale, lineCap: "butt", lineJoin: "round"}
		paintKey := libertyLinePaintKey(paint)
		outlineIndex, exists := outlineIndexes[paintKey]
		if !exists {
			outlineIndex = len(outlineBatches)
			outlineIndexes[paintKey] = outlineIndex
			outlineBatches = append(outlineBatches, libertyLineBatch{paint: paint})
		}
		for _, polygon := range feature.Polygons {
			outlineBatches[outlineIndex].paths = append(outlineBatches[outlineIndex].paths, closedLibertyRing(polygon.Exterior))
			for _, hole := range polygon.Holes {
				outlineBatches[outlineIndex].paths = append(outlineBatches[outlineIndex].paths, closedLibertyRing(hole))
			}
		}
	}
	for _, batch := range batches {
		if err := appendPrimitive(layer, geometry.Mesh{Vertices: batch.mesh.Vertices, Indices: batch.mesh.Indices}, batch.color); err != nil {
			return err
		}
	}
	for _, batch := range patternBatches {
		if err := appendPattern(layer, geometry.Mesh{Vertices: batch.mesh.Vertices, Indices: batch.mesh.Indices}, batch.name, 1/geometryScale, batch.opacity); err != nil {
			return err
		}
	}
	for _, batch := range outlineBatches {
		mesh, err := tessellateLibertyGeometry(batch.paths, batch.paint, maxTileRenderedTriangles, indexed)
		if err != nil {
			return err
		}
		if err := appendPrimitive(layer, mesh, batch.paint.color); err != nil {
			return err
		}
	}
	return nil
}

func compileLibertyLineLayer(
	bucket *tileBucket,
	layer compiledLibertyLayer,
	zoom float64,
	indexed bool,
	appendPrimitive func(compiledLibertyLayer, geometry.Mesh, mapColor) error,
) error {
	features := bucket.sourceLayers[layer.SourceLayer]
	geometryScale := math.Exp2(zoom - float64(bucket.tile.Z))
	batches := make([]libertyLineBatch, 0, 4)
	indexes := make(map[string]int)
	for _, feature := range features {
		evaluation := libertyEvaluation{zoom: zoom, geometryID: feature.GeometryType, properties: feature.Properties}
		if !layer.Matches(evaluation.context()) {
			continue
		}
		color, ok := libertyEvaluatedColor(layer, "line-color", evaluation, mapColor{Alpha: 255})
		if !ok {
			continue
		}
		color = libertyColorWithOpacity(color, libertyEvaluatedNumber(layer, "line-opacity", evaluation, 1))
		width := libertyEvaluatedNumber(layer, "line-width", evaluation, 1) / geometryScale
		gap := libertyEvaluatedNumber(layer, "line-gap-width", evaluation, 0) / geometryScale
		if width <= 0 || color.Alpha == 0 {
			continue
		}
		dashes := libertyEvaluatedNumbers(layer, "line-dasharray", evaluation)
		basePaint := libertyLinePaint{
			color:    color,
			width:    width,
			offset:   libertyEvaluatedNumber(layer, "line-offset", evaluation, 0) / geometryScale,
			dashKey:  libertyNumberListKey(dashes),
			dashes:   dashes,
			lineCap:  libertyEvaluatedString(layer, "line-cap", evaluation, "butt"),
			lineJoin: libertyEvaluatedString(layer, "line-join", evaluation, "miter"),
		}
		paints := []libertyLinePaint{basePaint}
		if gap > 0 {
			distance := (gap + width) / 2
			first := basePaint
			first.offset -= distance
			second := basePaint
			second.offset += distance
			paints = []libertyLinePaint{first, second}
		}
		for _, paint := range paints {
			paintKey := libertyLinePaintKey(paint)
			batchIndex, exists := indexes[paintKey]
			if !exists {
				batchIndex = len(batches)
				indexes[paintKey] = batchIndex
				batches = append(batches, libertyLineBatch{paint: paint})
			}
			switch feature.GeometryType {
			case mvtLineStringType:
				batches[batchIndex].paths = append(batches[batchIndex].paths, feature.Lines...)
			case mvtPolygonType:
				for _, polygon := range feature.Polygons {
					batches[batchIndex].paths = append(batches[batchIndex].paths, closedLibertyRing(polygon.Exterior))
					for _, hole := range polygon.Holes {
						batches[batchIndex].paths = append(batches[batchIndex].paths, closedLibertyRing(hole))
					}
				}
			}
		}
	}
	for _, batch := range batches {
		mesh, err := tessellateLibertyGeometry(batch.paths, batch.paint, maxTileRenderedTriangles, indexed)
		if err != nil {
			return err
		}
		if err := appendPrimitive(layer, mesh, batch.paint.color); err != nil {
			return err
		}
	}
	return nil
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
	value, exists := layer.Value(name, evaluation.context())
	if !exists {
		return fallback, true
	}
	return parseLibertyColor(value)
}

func libertyEvaluatedNumber(layer compiledLibertyLayer, name string, evaluation libertyEvaluation, fallback float64) float64 {
	value, exists := layer.Value(name, evaluation.context())
	if !exists {
		return fallback
	}
	number, ok := libertyNumber(value)
	if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
		return fallback
	}
	return number
}

func libertyEvaluatedString(layer compiledLibertyLayer, name string, evaluation libertyEvaluation, fallback string) string {
	value, exists := layer.Value(name, evaluation.context())
	if !exists {
		return fallback
	}
	text, ok := value.(string)
	if !ok {
		return fallback
	}
	return text
}

func libertyEvaluatedNumbers(layer compiledLibertyLayer, name string, evaluation libertyEvaluation) []float64 {
	value, exists := layer.Value(name, evaluation.context())
	if !exists {
		return nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	numbers := make([]float64, 0, len(items))
	for _, item := range items {
		number, ok := libertyNumber(item)
		if !ok || number < 0 {
			return nil
		}
		numbers = append(numbers, number)
	}
	return numbers
}

func libertyNumberListKey(numbers []float64) string {
	var builder strings.Builder
	for _, number := range numbers {
		builder.WriteString(strconv.FormatFloat(number, 'g', -1, 64))
		builder.WriteByte(',')
	}
	return builder.String()
}

func libertyLinePaintKey(paint libertyLinePaint) string {
	return fmt.Sprintf(
		"%d/%d/%d/%d/%.9g/%.9g/%s/%s/%s",
		paint.color.Red,
		paint.color.Green,
		paint.color.Blue,
		paint.color.Alpha,
		paint.width,
		paint.offset,
		paint.dashKey,
		paint.lineCap,
		paint.lineJoin,
	)
}

func closedLibertyRing(ring []roadPoint) []roadPoint {
	if len(ring) == 0 {
		return nil
	}
	closed := make([]roadPoint, len(ring)+1)
	copy(closed, ring)
	closed[len(ring)] = ring[0]
	return closed
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
