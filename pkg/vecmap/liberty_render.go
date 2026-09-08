package vecmap

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	maxTileRenderedTriangles = 2_000_000
	maxDashedSegmentsPerPath = 100_000
)

type libertyRenderPrimitive struct {
	order        int
	layerID      string
	triangles    []roadPoint
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
	layers, err := compiledLibertyLayers()
	if err != nil {
		return err
	}
	bucket.compiled = false
	bucket.liberty = nil
	bucket.symbols = nil
	primitives := make([]libertyRenderPrimitive, 0, len(layers))
	totalTriangles := 0
	appendPrimitive := func(layer compiledLibertyLayer, triangles []roadPoint, color mapColor) error {
		if len(triangles) == 0 || color.alpha == 0 {
			return nil
		}
		if len(triangles)%3 != 0 {
			return errors.New("liberty compiler produced an incomplete triangle")
		}
		triangleCount := len(triangles) / 3
		if triangleCount > maxTileRenderedTriangles-totalTriangles {
			return fmt.Errorf("%w: liberty geometry exceeds %d-triangle limit", errFeatureResourceLimit, maxTileRenderedTriangles)
		}
		totalTriangles += triangleCount
		primitives = append(primitives, libertyRenderPrimitive{
			order:     layer.order,
			layerID:   layer.id,
			triangles: triangles,
			color:     color,
		})
		return nil
	}
	appendPattern := func(
		layer compiledLibertyLayer,
		triangles []roadPoint,
		patternName string,
		patternScale, opacity float64,
	) error {
		if len(triangles) == 0 || patternName == "" || opacity <= 0 {
			return nil
		}
		if len(triangles)%3 != 0 {
			return errors.New("liberty compiler produced an incomplete patterned triangle")
		}
		triangleCount := len(triangles) / 3
		if triangleCount > maxTileRenderedTriangles-totalTriangles {
			return fmt.Errorf("%w: liberty geometry exceeds %d-triangle limit", errFeatureResourceLimit, maxTileRenderedTriangles)
		}
		totalTriangles += triangleCount
		primitives = append(primitives, libertyRenderPrimitive{
			order:        layer.order,
			layerID:      layer.id,
			triangles:    triangles,
			patternName:  patternName,
			patternScale: patternScale,
			opacity:      opacity,
		})
		return nil
	}

	for _, layer := range layers {
		if !layer.visibleAt(zoom) || libertyLayerHidden(layer) {
			continue
		}
		switch layer.kind {
		case "background":
			evaluation := libertyEvaluation{zoom: zoom}
			color, ok := libertyEvaluatedColor(layer, "background-color", evaluation, mapColor{alpha: 255})
			if !ok {
				continue
			}
			opacity := libertyEvaluatedNumber(layer, "background-opacity", evaluation, 1)
			if err := appendPrimitive(layer, backgroundTriangles(), libertyColorWithOpacity(color, opacity)); err != nil {
				return err
			}
		case "fill", "fill-extrusion":
			if err := compileLibertyFillLayer(bucket, layer, zoom, appendPrimitive, appendPattern); err != nil {
				return err
			}
		case "line":
			if err := compileLibertyLineLayer(bucket, layer, zoom, appendPrimitive); err != nil {
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
	appendPrimitive func(compiledLibertyLayer, []roadPoint, mapColor) error,
	appendPattern func(compiledLibertyLayer, []roadPoint, string, float64, float64) error,
) error {
	geometryScale := math.Exp2(zoom - float64(bucket.tile.Z))
	features := bucket.sourceLayers[layer.sourceLayer]
	type fillBatch struct {
		color     mapColor
		triangles []roadPoint
	}
	type patternBatch struct {
		name      string
		opacity   float64
		triangles []roadPoint
	}
	batches := make([]fillBatch, 0, 2)
	batchIndexes := make(map[mapColor]int)
	patternBatches := make([]patternBatch, 0, 2)
	patternIndexes := make(map[string]int)
	outlineBatches := make([]libertyLineBatch, 0, 2)
	outlineIndexes := make(map[string]int)
	colorProperty := "fill-color"
	if layer.kind == "fill-extrusion" {
		colorProperty = "fill-extrusion-color"
	}
	for _, feature := range features {
		if feature.geometryID != mvtPolygonType {
			continue
		}
		evaluation := libertyEvaluation{zoom: zoom, geometryID: feature.geometryID, properties: feature.properties}
		if !layer.matches(evaluation) {
			continue
		}
		opacityProperty := "fill-opacity"
		if layer.kind == "fill-extrusion" {
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
				patternBatches = append(patternBatches, patternBatch{name: patternName, opacity: opacity})
			}
			for _, polygon := range feature.polygons {
				patternBatches[patternIndex].triangles = append(patternBatches[patternIndex].triangles, polygon.triangles...)
			}
		} else {
			color, ok := libertyEvaluatedColor(layer, colorProperty, evaluation, mapColor{alpha: 255})
			if !ok {
				continue
			}
			color = libertyColorWithOpacity(color, opacity)
			batchIndex, exists := batchIndexes[color]
			if !exists {
				batchIndex = len(batches)
				batchIndexes[color] = batchIndex
				batches = append(batches, fillBatch{color: color})
			}
			for _, polygon := range feature.polygons {
				batches[batchIndex].triangles = append(batches[batchIndex].triangles, polygon.triangles...)
			}
		}

		outline, hasOutline := libertyEvaluatedColor(layer, "fill-outline-color", evaluation, mapColor{})
		if !hasOutline || outline.alpha == 0 {
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
		for _, polygon := range feature.polygons {
			outlineBatches[outlineIndex].paths = append(outlineBatches[outlineIndex].paths, closedLibertyRing(polygon.exterior))
			for _, hole := range polygon.holes {
				outlineBatches[outlineIndex].paths = append(outlineBatches[outlineIndex].paths, closedLibertyRing(hole))
			}
		}
	}
	for _, batch := range batches {
		if err := appendPrimitive(layer, batch.triangles, batch.color); err != nil {
			return err
		}
	}
	for _, batch := range patternBatches {
		if err := appendPattern(layer, batch.triangles, batch.name, 1/geometryScale, batch.opacity); err != nil {
			return err
		}
	}
	for _, batch := range outlineBatches {
		triangles, err := tessellateLibertyLines(batch.paths, batch.paint, maxTileRenderedTriangles)
		if err != nil {
			return err
		}
		if err := appendPrimitive(layer, triangles, batch.paint.color); err != nil {
			return err
		}
	}
	return nil
}

func compileLibertyLineLayer(
	bucket *tileBucket,
	layer compiledLibertyLayer,
	zoom float64,
	appendPrimitive func(compiledLibertyLayer, []roadPoint, mapColor) error,
) error {
	features := bucket.sourceLayers[layer.sourceLayer]
	geometryScale := math.Exp2(zoom - float64(bucket.tile.Z))
	batches := make([]libertyLineBatch, 0, 4)
	indexes := make(map[string]int)
	for _, feature := range features {
		evaluation := libertyEvaluation{zoom: zoom, geometryID: feature.geometryID, properties: feature.properties}
		if !layer.matches(evaluation) {
			continue
		}
		color, ok := libertyEvaluatedColor(layer, "line-color", evaluation, mapColor{alpha: 255})
		if !ok {
			continue
		}
		color = libertyColorWithOpacity(color, libertyEvaluatedNumber(layer, "line-opacity", evaluation, 1))
		width := libertyEvaluatedNumber(layer, "line-width", evaluation, 1) / geometryScale
		gap := libertyEvaluatedNumber(layer, "line-gap-width", evaluation, 0) / geometryScale
		if width <= 0 || color.alpha == 0 {
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
			switch feature.geometryID {
			case mvtLineStringType:
				batches[batchIndex].paths = append(batches[batchIndex].paths, feature.lines...)
			case mvtPolygonType:
				for _, polygon := range feature.polygons {
					batches[batchIndex].paths = append(batches[batchIndex].paths, closedLibertyRing(polygon.exterior))
					for _, hole := range polygon.holes {
						batches[batchIndex].paths = append(batches[batchIndex].paths, closedLibertyRing(hole))
					}
				}
			}
		}
	}
	for _, batch := range batches {
		triangles, err := tessellateLibertyLines(batch.paths, batch.paint, maxTileRenderedTriangles)
		if err != nil {
			return err
		}
		if err := appendPrimitive(layer, triangles, batch.paint.color); err != nil {
			return err
		}
	}
	return nil
}

func libertyLayerHidden(layer compiledLibertyLayer) bool {
	visibility, exists := layer.layout["visibility"]
	return exists && visibility == "none"
}

func libertyEvaluatedColor(
	layer compiledLibertyLayer,
	name string,
	evaluation libertyEvaluation,
	fallback mapColor,
) (mapColor, bool) {
	value, exists := layer.value(name, evaluation)
	if !exists {
		return fallback, true
	}
	return parseLibertyColor(value)
}

func libertyEvaluatedNumber(layer compiledLibertyLayer, name string, evaluation libertyEvaluation, fallback float64) float64 {
	value, exists := layer.value(name, evaluation)
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
	value, exists := layer.value(name, evaluation)
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
	value, exists := layer.value(name, evaluation)
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
		paint.color.red,
		paint.color.green,
		paint.color.blue,
		paint.color.alpha,
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
	triangles := make([]roadPoint, 0, libertyLineVertexCapacity(paths, paint, maximumTriangles))
	if paint.width <= polygonEpsilon {
		return triangles, nil
	}
	appendTriangle := func(first, second, third roadPoint) error {
		if len(triangles)/3 >= maximumTriangles {
			return fmt.Errorf("%w: tessellated line exceeds %d-triangle limit", errFeatureResourceLimit, maximumTriangles)
		}
		triangles = append(triangles, first, second, third)
		return nil
	}
	for _, rawPath := range paths {
		path := cleanLibertyLine(rawPath)
		if len(path) < 2 {
			continue
		}
		path = offsetLibertyLine(path, paint.offset)
		segments, err := libertyDashedSegments(path, paint.dashes, paint.width, maximumTriangles)
		if err != nil {
			return nil, err
		}
		if len(segments) == 0 {
			continue
		}
		for _, segment := range segments {
			start, end := segment.Start, segment.End
			deltaX := end.X - start.X
			deltaY := end.Y - start.Y
			length := math.Hypot(deltaX, deltaY)
			if length <= polygonEpsilon {
				continue
			}
			if paint.lineCap == "square" {
				extensionX := deltaX / length * paint.width / 2
				extensionY := deltaY / length * paint.width / 2
				start.X -= extensionX
				start.Y -= extensionY
				end.X += extensionX
				end.Y += extensionY
			}
			normalX := -deltaY / length * paint.width / 2
			normalY := deltaX / length * paint.width / 2
			first := roadPoint{X: start.X + normalX, Y: start.Y + normalY}
			second := roadPoint{X: start.X - normalX, Y: start.Y - normalY}
			third := roadPoint{X: end.X + normalX, Y: end.Y + normalY}
			fourth := roadPoint{X: end.X - normalX, Y: end.Y - normalY}
			if err := appendTriangle(first, second, third); err != nil {
				return nil, err
			}
			if err := appendTriangle(third, second, fourth); err != nil {
				return nil, err
			}
			if paint.lineCap == "round" && len(paint.dashes) > 0 {
				if err := appendLibertyDisk(start, paint.width/2, appendTriangle); err != nil {
					return nil, err
				}
				if err := appendLibertyDisk(end, paint.width/2, appendTriangle); err != nil {
					return nil, err
				}
			}
		}
		if len(paint.dashes) == 0 {
			if paint.lineJoin == "round" {
				for _, point := range path[1 : len(path)-1] {
					if err := appendLibertyDisk(point, paint.width/2, appendTriangle); err != nil {
						return nil, err
					}
				}
			} else {
				for index := 1; index+1 < len(path); index++ {
					if err := appendLibertyMiterJoin(path[index-1], path[index], path[index+1], paint.width/2, appendTriangle); err != nil {
						return nil, err
					}
				}
			}
			if paint.lineCap == "round" && path[0] != path[len(path)-1] {
				if err := appendLibertyDisk(path[0], paint.width/2, appendTriangle); err != nil {
					return nil, err
				}
				if err := appendLibertyDisk(path[len(path)-1], paint.width/2, appendTriangle); err != nil {
					return nil, err
				}
			}
		}
	}
	return triangles, nil
}

func libertyLineVertexCapacity(paths [][]roadPoint, paint libertyLinePaint, maximumTriangles int) int {
	if len(paint.dashes) > 0 {
		return 0
	}
	vertices := 0
	for _, path := range paths {
		points := len(path)
		if points < 2 {
			continue
		}
		vertices += (points - 1) * 6
		if paint.lineJoin == "round" && points > 2 {
			vertices += (points - 2) * libertyDiskSections * 3
		}
		if paint.lineCap == "round" && path[0] != path[points-1] {
			vertices += 2 * libertyDiskSections * 3
		}
	}
	return min(vertices, maximumTriangles*3)
}

func appendLibertyMiterJoin(
	previous, point, next roadPoint,
	halfWidth float64,
	appendTriangle func(roadPoint, roadPoint, roadPoint) error,
) error {
	firstX, firstY := point.X-previous.X, point.Y-previous.Y
	secondX, secondY := next.X-point.X, next.Y-point.Y
	firstLength := math.Hypot(firstX, firstY)
	secondLength := math.Hypot(secondX, secondY)
	if firstLength <= polygonEpsilon || secondLength <= polygonEpsilon || halfWidth <= polygonEpsilon {
		return nil
	}
	firstX, firstY = firstX/firstLength, firstY/firstLength
	secondX, secondY = secondX/secondLength, secondY/secondLength
	cross := firstX*secondY - firstY*secondX
	if math.Abs(cross) <= polygonEpsilon {
		return nil
	}
	for _, side := range []float64{-1, 1} {
		firstCorner := roadPoint{X: point.X - firstY*halfWidth*side, Y: point.Y + firstX*halfWidth*side}
		secondCorner := roadPoint{X: point.X - secondY*halfWidth*side, Y: point.Y + secondX*halfWidth*side}
		deltaX := secondCorner.X - firstCorner.X
		deltaY := secondCorner.Y - firstCorner.Y
		factor := (deltaX*secondY - deltaY*secondX) / cross
		miter := roadPoint{X: firstCorner.X + firstX*factor, Y: firstCorner.Y + firstY*factor}
		if math.Hypot(miter.X-point.X, miter.Y-point.Y) > halfWidth*4 {
			miter = point
		}
		if err := appendTriangle(firstCorner, miter, secondCorner); err != nil {
			return err
		}
	}
	return nil
}

func libertyDashedSegments(path []roadPoint, dashes []float64, width float64, maximumTriangles int) ([]roadSegment, error) {
	if len(dashes) == 0 {
		return solidLibertySegments(path), nil
	}
	pattern := make([]float64, len(dashes))
	patternLength := 0.0
	for index, dash := range dashes {
		pattern[index] = dash * width
		if pattern[index] < 0 || math.IsNaN(pattern[index]) || math.IsInf(pattern[index], 0) {
			return nil, fmt.Errorf("%w: invalid dashed line pattern", errFeatureResourceLimit)
		}
		patternLength += pattern[index]
	}
	if patternLength <= polygonEpsilon {
		return solidLibertySegments(path), nil
	}
	if len(pattern)%2 != 0 {
		pattern = append(pattern, pattern...)
	}
	segments := make([]roadSegment, 0, len(path))
	patternIndex := 0
	remaining := pattern[0]
	drawing := true
	advancePattern := func() bool {
		for range len(pattern) {
			if remaining > polygonEpsilon {
				return true
			}
			patternIndex = (patternIndex + 1) % len(pattern)
			remaining = pattern[patternIndex]
			drawing = patternIndex%2 == 0
		}
		return remaining > polygonEpsilon
	}
	if !advancePattern() {
		return solidLibertySegments(path), nil
	}
	iterations := 0
	maximumSegments := min(maxDashedSegmentsPerPath, maximumTriangles/2)
	maximumIterations := max(1024, maximumSegments*4)
	for index := 1; index < len(path); index++ {
		start := path[index-1]
		end := path[index]
		deltaX := end.X - start.X
		deltaY := end.Y - start.Y
		length := math.Hypot(deltaX, deltaY)
		position := 0.0
		for position < length-polygonEpsilon {
			iterations++
			if iterations > maximumIterations {
				return nil, fmt.Errorf("%w: dashed line exceeds %d-iteration limit", errFeatureResourceLimit, maximumIterations)
			}
			step := min(remaining, length-position)
			if drawing {
				if len(segments) >= maximumSegments {
					return nil, fmt.Errorf("%w: dashed line exceeds %d-segment limit", errFeatureResourceLimit, maximumSegments)
				}
				firstFactor := position / length
				secondFactor := (position + step) / length
				segments = append(segments, roadSegment{
					Start: roadPoint{X: start.X + deltaX*firstFactor, Y: start.Y + deltaY*firstFactor},
					End:   roadPoint{X: start.X + deltaX*secondFactor, Y: start.Y + deltaY*secondFactor},
				})
			}
			position += step
			remaining -= step
			if !advancePattern() {
				return solidLibertySegments(path), nil
			}
		}
	}
	return segments, nil
}

func solidLibertySegments(path []roadPoint) []roadSegment {
	segments := make([]roadSegment, 0, max(0, len(path)-1))
	for index := 1; index < len(path); index++ {
		segments = append(segments, roadSegment{Start: path[index-1], End: path[index]})
	}
	return segments
}

func cleanLibertyLine(path []roadPoint) []roadPoint {
	cleaned := make([]roadPoint, 0, len(path))
	for _, point := range path {
		if len(cleaned) == 0 || cleaned[len(cleaned)-1] != point {
			cleaned = append(cleaned, point)
		}
	}
	return cleaned
}

func offsetLibertyLine(path []roadPoint, offset float64) []roadPoint {
	if offset == 0 {
		return path
	}
	offsetPath := make([]roadPoint, len(path))
	for index, point := range path {
		var normalX, normalY float64
		var referenceX, referenceY float64
		normalCount := 0
		if index > 0 {
			deltaX := point.X - path[index-1].X
			deltaY := point.Y - path[index-1].Y
			length := math.Hypot(deltaX, deltaY)
			if length > polygonEpsilon {
				referenceX, referenceY = -deltaY/length, deltaX/length
				normalX += referenceX
				normalY += referenceY
				normalCount++
			}
		}
		if index+1 < len(path) {
			deltaX := path[index+1].X - point.X
			deltaY := path[index+1].Y - point.Y
			length := math.Hypot(deltaX, deltaY)
			if length > polygonEpsilon {
				referenceX, referenceY = -deltaY/length, deltaX/length
				normalX += referenceX
				normalY += referenceY
				normalCount++
			}
		}
		length := math.Hypot(normalX, normalY)
		if length > polygonEpsilon {
			normalX /= length
			normalY /= length
		}
		distance := offset
		if normalCount == 2 {
			denominator := normalX*referenceX + normalY*referenceY
			if math.Abs(denominator) > polygonEpsilon {
				distance = offset / denominator
				distance = max(-4*math.Abs(offset), min(4*math.Abs(offset), distance))
			}
		}
		offsetPath[index] = roadPoint{X: point.X + normalX*distance, Y: point.Y + normalY*distance}
	}
	return offsetPath
}

func appendLibertyDisk(
	center roadPoint,
	radius float64,
	appendTriangle func(roadPoint, roadPoint, roadPoint) error,
) error {
	for section := range libertyDiskSections {
		firstDirection := libertyDiskDirections[section]
		secondDirection := libertyDiskDirections[section+1]
		first := roadPoint{X: center.X + firstDirection.X*radius, Y: center.Y + firstDirection.Y*radius}
		second := roadPoint{X: center.X + secondDirection.X*radius, Y: center.Y + secondDirection.Y*radius}
		if err := appendTriangle(center, first, second); err != nil {
			return err
		}
	}
	return nil
}

const libertyDiskSections = 8

var libertyDiskDirections = [...]roadPoint{
	{X: 1, Y: 0},
	{X: math.Sqrt2 / 2, Y: math.Sqrt2 / 2},
	{X: 0, Y: 1},
	{X: -math.Sqrt2 / 2, Y: math.Sqrt2 / 2},
	{X: -1, Y: 0},
	{X: -math.Sqrt2 / 2, Y: -math.Sqrt2 / 2},
	{X: 0, Y: -1},
	{X: math.Sqrt2 / 2, Y: -math.Sqrt2 / 2},
	{X: 1, Y: 0},
}
