// Package compiler prepares toolkit-neutral, styled map geometry from MVT features.
package compiler

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/mvt"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
)

// MaxTriangles is the existing per-batch tessellation/builder ceiling. Callers
// must additionally account for emitted triangles across batches and tile layers.
const MaxTriangles = geometry.MaxLineTriangles

var ErrOptions = errors.New("invalid layer compiler options or sink")

// LayerOptions converts evaluated screen-pixel paint to source-tile units.
// TriangleLimit can lower the per-batch ceiling; zero selects MaxTriangles.
type LayerOptions struct {
	SourceZoom    int
	Zoom          float64
	Indexed       bool
	TriangleLimit int
	// Coarser is the number of zoom levels the style zoom lies below the zoom
	// the tile is drawn at. Zoom evaluates the style; Zoom plus Coarser converts
	// pixels to source-tile units. At most view.MaxCoarser.
	Coarser int
	// ExtrudeLines makes CompileTile emit undashed, unoffset lines and fill
	// outlines as width-independent extruded primitives, and marks the remaining
	// zoom-baked line geometry Dynamic. False preserves the existing output.
	ExtrudeLines bool
	// ShaderDashes makes CompileTile emit butt-capped, unoffset dashed lines
	// whose pattern fits geometry.DashPattern as width-independent primitives
	// that leave dashing to the consumer. Other dashed lines keep their baked
	// geometry. False preserves the existing output.
	ShaderDashes bool
}

func (o LayerOptions) validated() (float64, int, error) {
	scale := math.Exp2(o.Zoom + float64(o.Coarser) - float64(o.SourceZoom))
	if scale <= 0 || math.IsNaN(scale) || math.IsInf(scale, 0) || o.TriangleLimit < 0 || o.TriangleLimit > MaxTriangles ||
		o.Coarser < 0 || o.Coarser > view.MaxCoarser {
		return 0, 0, ErrOptions
	}
	limit := o.TriangleLimit
	if limit == 0 {
		limit = MaxTriangles
	}
	return scale, limit, nil
}

// SolidSink consumes an owned mesh and its evaluated color synchronously.
type SolidSink func(geometry.Mesh, style.Color) error

// ExtrudedSink consumes an owned width-independent line mesh, its evaluated color
// and its half width in logical pixels.
type ExtrudedSink func(geometry.ExtrudedMesh, style.Color, float64) error

// DashedSink consumes an owned width-independent dashed line mesh, its evaluated
// color, its width in logical pixels and in tile units, and its dash pattern.
type DashedSink func(mesh geometry.DashedMesh, color style.Color, pixels, width float64, pattern geometry.DashPattern) error

// lineSinks routes tessellated lines. A nil extruded or dashed sink keeps those
// lines baked. With scratch, extruded and dashed meshes borrow its storage
// until the sink returns, and the sinks must copy what they keep. plan follows
// the stable batches, extruded and dashed, of a planned compilation.
type lineSinks struct {
	baked    SolidSink
	extruded ExtrudedSink
	dashed   DashedSink
	scratch  *lineScratch
	plan     *StablePlanner
}

// lineRoute is how emitLines tessellates a batch.
type lineRoute uint8

const (
	bakedRoute   lineRoute = iota
	skippedRoute           // extruded, and too thin to emit anything
	extrudedRoute
	dashedRoute
)

// route depends only on the batch's paint, so a batch knows it when created.
func (s lineSinks) route(paint linePaint) (lineRoute, geometry.DashPattern) {
	// Dash lengths and path offsets depend on the evaluated width or offset,
	// so only plain lines have a width-independent form. The width guard
	// matches the baked tessellator, which emits nothing at or below Epsilon.
	if s.extruded != nil && len(paint.dashes) == 0 && paint.offset == 0 {
		if paint.width <= geometry.Epsilon {
			return skippedRoute, geometry.DashPattern{}
		}
		return extrudedRoute, geometry.DashPattern{}
	}
	// Dash lengths are multiples of the width, so a pattern and the distance
	// along each path replace baked dashes. Dash caps and joins, path offsets
	// and longer patterns have no such form here.
	if pattern, ok := geometry.NewDashPattern(paint.dashes, paint.width); ok && s.dashed != nil && paint.offset == 0 && paint.lineCap != "round" && paint.lineCap != "square" {
		return dashedRoute, pattern
	}
	return bakedRoute, geometry.DashPattern{}
}

// lineScratch is storage reused by every layer of a tile: line tessellation, and
// the decoded feature that FeatureSet.At fills. A merged road feature can have
// thousands of parts, so starting that feature over at each layer allocates.
type lineScratch struct {
	extruded geometry.ExtrudedMesh
	dashed   geometry.DashedMesh
	feature  mvt.Feature
}

// feature returns the tile's reusable feature, or a fresh one without scratch.
func (s lineSinks) feature() *mvt.Feature {
	if s.scratch == nil {
		return new(mvt.Feature)
	}
	return &s.scratch.feature
}

// PatternSink consumes an owned mesh, logical sprite name, tile-unit scale and
// raw evaluated opacity. Sprite lookup and opacity filtering belong to the caller.
type PatternSink func(geometry.Mesh, string, float64, float64) error

type linePaint struct {
	color style.Color
	width float64
	// pixels is the evaluated width before tile-unit scaling. It follows width,
	// which is part of the batch key, so it never splits or merges batches.
	pixels   float64
	offset   float64
	dashKey  string
	dashes   []float64
	lineCap  string
	lineJoin string
}

type lineBatch struct {
	paint    linePaint
	paths    [][]geometry.Point
	features featureHash
	// borrowed batches are stable and need no paths: the borrowed StableMesh
	// holds their geometry.
	borrowed bool
}

// newLineBatch starts a batch, which needs paths unless it is borrowed.
func (s lineSinks) newLineBatch(paint linePaint) lineBatch {
	route, _ := s.route(paint)
	return lineBatch{paint: paint, borrowed: s.plan.borrowing() && (route == extrudedRoute || route == dashedRoute)}
}

// CompileFill emits solid batches, then patterns, then outlines, preserving the
// existing first-seen ordering within each group. It also handles fill-extrusion
// as flat geometry. Callers select layer kind, visibility and source features.
// Earlier sink calls remain accepted on a later error. Inputs are not mutated or
// retained; emitted meshes own their buffers, while names can borrow style data.
func CompileFill(features mvt.Features, layer style.CompiledLayer, options LayerOptions, solid SolidSink, pattern PatternSink) error {
	return compileFill(features, layer, options, solid, pattern, lineSinks{baked: solid})
}

func compileFill(features mvt.Features, layer style.CompiledLayer, options LayerOptions, solid SolidSink, pattern PatternSink, outlines lineSinks) error {
	geometryScale, limit, err := options.validated()
	if err != nil || solid == nil || pattern == nil || outlines.baked == nil {
		return ErrOptions
	}
	type fillBatch struct {
		color    style.Color
		mesh     geometry.Builder[geometry.Point]
		features featureHash
	}
	type patternBatch struct {
		name     string
		opacity  float64
		mesh     geometry.Builder[geometry.Point]
		features featureHash
	}
	plan := outlines.plan
	borrowing := plan.borrowing()
	batches := make([]fillBatch, 0, 2)
	batchIndexes := make(map[style.Color]int)
	patternBatches := make([]patternBatch, 0, 2)
	patternIndexes := make(map[string]int)
	outlineBatches := make([]lineBatch, 0, 2)
	outlineIndexes := make(map[string]int)
	colorProperty := "fill-color"
	if layer.Kind == "fill-extrusion" {
		colorProperty = "fill-extrusion-color"
	}
	feature := outlines.feature()
	for i := range featureCount(features) {
		features.At(i, feature)
		if feature.GeometryType != mvt.PolygonType {
			continue
		}
		evaluation := style.Context{Zoom: options.Zoom, GeometryType: feature.GeometryType, Properties: feature.Properties}
		if !layer.Matches(evaluation) {
			continue
		}
		opacityProperty := "fill-opacity"
		if layer.Kind == "fill-extrusion" {
			opacityProperty = "fill-extrusion-opacity"
		}
		opacity := layer.NumberValue(opacityProperty, evaluation, 1)
		patternName := layer.StringValue("fill-pattern", evaluation, "")
		if patternName != "" {
			patternKey := patternName + "/" + strconv.FormatFloat(opacity, 'g', -1, 64)
			patternIndex, exists := patternIndexes[patternKey]
			if !exists {
				patternIndex = len(patternBatches)
				patternIndexes[patternKey] = patternIndex
				patternBatches = append(patternBatches, patternBatch{name: patternName, opacity: opacity, mesh: geometry.NewBuilder[geometry.Point](options.Indexed, limit*3)})
			}
			plan.add(&patternBatches[patternIndex].features, i)
			for _, polygon := range feature.Polygons {
				if borrowing {
					break
				}
				if err := patternBatches[patternIndex].mesh.Append(polygon.Vertices, polygon.Indices); err != nil {
					return resourceError(err)
				}
			}
		} else {
			color, ok := layer.ColorValue(colorProperty, evaluation, style.Color{Alpha: 255})
			if !ok {
				continue
			}
			color = style.ColorWithOpacity(color, opacity)
			batchIndex, exists := batchIndexes[color]
			if !exists {
				batchIndex = len(batches)
				batchIndexes[color] = batchIndex
				batches = append(batches, fillBatch{color: color, mesh: geometry.NewBuilder[geometry.Point](options.Indexed, limit*3)})
			}
			plan.add(&batches[batchIndex].features, i)
			for _, polygon := range feature.Polygons {
				if borrowing {
					break
				}
				if err := batches[batchIndex].mesh.Append(polygon.Vertices, polygon.Indices); err != nil {
					return resourceError(err)
				}
			}
		}

		outline, hasOutline := layer.ColorValue("fill-outline-color", evaluation, style.Color{})
		if !hasOutline || outline.Alpha == 0 {
			continue
		}
		outline = style.ColorWithOpacity(outline, layer.NumberValue("fill-opacity", evaluation, 1))
		paint := linePaint{color: outline, width: 1 / geometryScale, pixels: 1, lineCap: "butt", lineJoin: "round"}
		paintKey := linePaintKey(paint)
		outlineIndex, exists := outlineIndexes[paintKey]
		if !exists {
			outlineIndex = len(outlineBatches)
			outlineIndexes[paintKey] = outlineIndex
			outlineBatches = append(outlineBatches, outlines.newLineBatch(paint))
		}
		batch := &outlineBatches[outlineIndex]
		plan.add(&batch.features, i)
		if !batch.borrowed {
			batch.paths = appendPolygonPaths(batch.paths, feature.Polygons)
		}
	}
	for i := range batches {
		batch := &batches[i]
		if err := plan.beginRun(stableFill, layer.Order, "", "", &batch.features); err != nil {
			return err
		}
		err := solid(geometry.Mesh{Vertices: batch.mesh.Vertices, Indices: batch.mesh.Indices}, batch.color)
		plan.end()
		if err != nil {
			return err
		}
	}
	for i := range patternBatches {
		batch := &patternBatches[i]
		if err := plan.beginRun(stablePattern, layer.Order, "", "", &batch.features); err != nil {
			return err
		}
		err := pattern(geometry.Mesh{Vertices: batch.mesh.Vertices, Indices: batch.mesh.Indices}, batch.name, 1/geometryScale, batch.opacity)
		plan.end()
		if err != nil {
			return err
		}
	}
	return emitLines(outlineBatches, layer.Order, options.Indexed, limit, outlines)
}

// CompileLine evaluates and batches line/polygon features in first-seen paint
// order, then tessellates with the existing geometry engine. Gap lines emit the
// negative-offset side before the positive side. Caller visibility/publication
// and streaming ownership/error rules match CompileFill.
func CompileLine(features mvt.Features, layer style.CompiledLayer, options LayerOptions, solid SolidSink) error {
	return compileLine(features, layer, options, lineSinks{baked: solid})
}

func compileLine(features mvt.Features, layer style.CompiledLayer, options LayerOptions, sinks lineSinks) error {
	geometryScale, limit, err := options.validated()
	if err != nil || sinks.baked == nil {
		return ErrOptions
	}
	batches := make([]lineBatch, 0, 4)
	indexes := make(map[string]int)
	feature := sinks.feature()
	for i := range featureCount(features) {
		features.At(i, feature)
		evaluation := style.Context{Zoom: options.Zoom, GeometryType: feature.GeometryType, Properties: feature.Properties}
		if !layer.Matches(evaluation) {
			continue
		}
		color, ok := layer.ColorValue("line-color", evaluation, style.Color{Alpha: 255})
		if !ok {
			continue
		}
		color = style.ColorWithOpacity(color, layer.NumberValue("line-opacity", evaluation, 1))
		pixels := layer.NumberValue("line-width", evaluation, 1)
		width := pixels / geometryScale
		gap := layer.NumberValue("line-gap-width", evaluation, 0) / geometryScale
		if width <= 0 || color.Alpha == 0 {
			continue
		}
		dashes := evaluatedNumbers(layer, "line-dasharray", evaluation)
		basePaint := linePaint{
			color: color, width: width, pixels: pixels,
			offset:  layer.NumberValue("line-offset", evaluation, 0) / geometryScale,
			dashKey: numberListKey(dashes), dashes: dashes,
			lineCap:  layer.StringValue("line-cap", evaluation, "butt"),
			lineJoin: layer.StringValue("line-join", evaluation, "miter"),
		}
		paints := []linePaint{basePaint}
		if gap > 0 {
			distance := (gap + width) / 2
			first := basePaint
			first.offset -= distance
			second := basePaint
			second.offset += distance
			paints = []linePaint{first, second}
		}
		for _, paint := range paints {
			paintKey := linePaintKey(paint)
			batchIndex, exists := indexes[paintKey]
			if !exists {
				batchIndex = len(batches)
				indexes[paintKey] = batchIndex
				batches = append(batches, sinks.newLineBatch(paint))
			}
			batch := &batches[batchIndex]
			sinks.plan.add(&batch.features, i)
			if batch.borrowed {
				continue
			}
			switch feature.GeometryType {
			case mvt.LineStringType:
				batch.paths = append(batch.paths, feature.Lines...)
			case mvt.PolygonType:
				batch.paths = appendPolygonPaths(batch.paths, feature.Polygons)
			}
		}
	}
	return emitLines(batches, layer.Order, options.Indexed, limit, sinks)
}

func emitLines(batches []lineBatch, order int, indexed bool, limit int, sinks lineSinks) error {
	borrowing := sinks.plan.borrowing()
	for i := range batches {
		batch := &batches[i]
		paint := batch.paint
		route, pattern := sinks.route(paint)
		switch route {
		case skippedRoute:
			continue
		case extrudedRoute:
			if err := sinks.plan.beginRun(stableExtruded, order, paint.lineCap, paint.lineJoin, &batch.features); err != nil {
				return err
			}
			var mesh geometry.ExtrudedMesh
			var err error
			if !borrowing {
				mesh, err = sinks.tessellateExtruded(batch.paths, paint, limit, indexed)
			}
			if err == nil {
				err = sinks.extruded(mesh, paint.color, paint.pixels/2)
			}
			sinks.plan.end()
			if err != nil {
				return err
			}
		case dashedRoute:
			if err := sinks.plan.beginRun(stableDashed, order, paint.lineCap, paint.lineJoin, &batch.features); err != nil {
				return err
			}
			var mesh geometry.DashedMesh
			var err error
			if !borrowing {
				mesh, err = sinks.tessellateDashed(batch.paths, limit, indexed)
			}
			if err == nil {
				err = sinks.dashed(mesh, paint.color, paint.pixels, paint.width, pattern)
			}
			sinks.plan.end()
			if err != nil {
				return err
			}
		default:
			mesh, err := geometry.TessellateLines(batch.paths, geometry.LineStyle{Width: paint.width, Offset: paint.offset, Dashes: paint.dashes, Cap: paint.lineCap, Join: paint.lineJoin}, limit, indexed)
			if err != nil {
				return resourceError(err)
			}
			if err := sinks.baked(mesh, paint.color); err != nil {
				return err
			}
		}
	}
	return nil
}

// tessellateExtruded tessellates into the sinks' scratch storage, if any.
func (s lineSinks) tessellateExtruded(paths [][]geometry.Point, paint linePaint, limit int, indexed bool) (geometry.ExtrudedMesh, error) {
	var scratch geometry.ExtrudedMesh
	if s.scratch != nil {
		scratch = s.scratch.extruded
	}
	mesh, err := geometry.TessellateExtrudedLinesInto(scratch, paths, geometry.ExtrudedLineStyle{Cap: paint.lineCap, Join: paint.lineJoin}, limit, indexed)
	if err != nil {
		return geometry.ExtrudedMesh{}, resourceError(err)
	}
	if s.scratch != nil {
		s.scratch.extruded = mesh
	}
	return mesh, nil
}

// tessellateDashed is tessellateExtruded for dashed lines.
func (s lineSinks) tessellateDashed(paths [][]geometry.Point, limit int, indexed bool) (geometry.DashedMesh, error) {
	var scratch geometry.DashedMesh
	if s.scratch != nil {
		scratch = s.scratch.dashed
	}
	mesh, err := geometry.TessellateDashedLinesInto(scratch, paths, limit, indexed)
	if err != nil {
		return geometry.DashedMesh{}, resourceError(err)
	}
	if s.scratch != nil {
		s.scratch.dashed = mesh
	}
	return mesh, nil
}

func resourceError(err error) error {
	return fmt.Errorf("%w: %v", mvt.ErrFeatureResourceLimit, err)
}

func evaluatedNumbers(layer style.CompiledLayer, name string, evaluation style.Context) []float64 {
	value, exists := layer.Value(name, evaluation)
	if !exists {
		return nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	numbers := make([]float64, 0, len(items))
	for _, item := range items {
		number, ok := style.Number(item)
		if !ok || number < 0 {
			return nil
		}
		numbers = append(numbers, number)
	}
	return numbers
}

func numberListKey(numbers []float64) string {
	var builder strings.Builder
	for _, number := range numbers {
		builder.WriteString(strconv.FormatFloat(number, 'g', -1, 64))
		builder.WriteByte(',')
	}
	return builder.String()
}

func linePaintKey(paint linePaint) string {
	return fmt.Sprintf("%d/%d/%d/%d/%.9g/%.9g/%s/%s/%s",
		paint.color.Red, paint.color.Green, paint.color.Blue, paint.color.Alpha,
		paint.width, paint.offset, paint.dashKey, paint.lineCap, paint.lineJoin)
}

func appendPolygonPaths(paths [][]geometry.Point, polygons []mvt.Polygon) [][]geometry.Point {
	for _, polygon := range polygons {
		paths = append(paths, closedRing(polygon.Exterior))
		for _, hole := range polygon.Holes {
			paths = append(paths, closedRing(hole))
		}
	}
	return paths
}

func closedRing(ring []geometry.Point) []geometry.Point {
	if len(ring) == 0 {
		return nil
	}
	closed := make([]geometry.Point, len(ring)+1)
	copy(closed, ring)
	closed[len(ring)] = ring[0]
	return closed
}

// featureCount is features.Len, with no features for a nil source.
func featureCount(features mvt.Features) int {
	if features == nil {
		return 0
	}
	return features.Len()
}
