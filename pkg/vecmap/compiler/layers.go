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
// lines baked.
type lineSinks struct {
	baked    SolidSink
	extruded ExtrudedSink
	dashed   DashedSink
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
	paint linePaint
	paths [][]geometry.Point
}

// CompileFill emits solid batches, then patterns, then outlines, preserving the
// existing first-seen ordering within each group. It also handles fill-extrusion
// as flat geometry. Callers select layer kind, visibility and source features.
// Earlier sink calls remain accepted on a later error. Inputs are not mutated or
// retained; emitted meshes own their buffers, while names can borrow style data.
func CompileFill(features []mvt.Feature, layer style.CompiledLayer, options LayerOptions, solid SolidSink, pattern PatternSink) error {
	return compileFill(features, layer, options, solid, pattern, lineSinks{baked: solid})
}

func compileFill(features []mvt.Feature, layer style.CompiledLayer, options LayerOptions, solid SolidSink, pattern PatternSink, outlines lineSinks) error {
	geometryScale, limit, err := options.validated()
	if err != nil || solid == nil || pattern == nil || outlines.baked == nil {
		return ErrOptions
	}
	type fillBatch struct {
		color style.Color
		mesh  geometry.Builder[geometry.Point]
	}
	type patternBatch struct {
		name    string
		opacity float64
		mesh    geometry.Builder[geometry.Point]
	}
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
	for _, feature := range features {
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
			for _, polygon := range feature.Polygons {
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
			for _, polygon := range feature.Polygons {
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
			outlineBatches = append(outlineBatches, lineBatch{paint: paint})
		}
		outlineBatches[outlineIndex].paths = appendPolygonPaths(outlineBatches[outlineIndex].paths, feature.Polygons)
	}
	for _, batch := range batches {
		if err := solid(geometry.Mesh{Vertices: batch.mesh.Vertices, Indices: batch.mesh.Indices}, batch.color); err != nil {
			return err
		}
	}
	for _, batch := range patternBatches {
		if err := pattern(geometry.Mesh{Vertices: batch.mesh.Vertices, Indices: batch.mesh.Indices}, batch.name, 1/geometryScale, batch.opacity); err != nil {
			return err
		}
	}
	return emitLines(outlineBatches, options.Indexed, limit, outlines)
}

// CompileLine evaluates and batches line/polygon features in first-seen paint
// order, then tessellates with the existing geometry engine. Gap lines emit the
// negative-offset side before the positive side. Caller visibility/publication
// and streaming ownership/error rules match CompileFill.
func CompileLine(features []mvt.Feature, layer style.CompiledLayer, options LayerOptions, solid SolidSink) error {
	return compileLine(features, layer, options, lineSinks{baked: solid})
}

func compileLine(features []mvt.Feature, layer style.CompiledLayer, options LayerOptions, sinks lineSinks) error {
	geometryScale, limit, err := options.validated()
	if err != nil || sinks.baked == nil {
		return ErrOptions
	}
	batches := make([]lineBatch, 0, 4)
	indexes := make(map[string]int)
	for _, feature := range features {
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
				batches = append(batches, lineBatch{paint: paint})
			}
			switch feature.GeometryType {
			case mvt.LineStringType:
				batches[batchIndex].paths = append(batches[batchIndex].paths, feature.Lines...)
			case mvt.PolygonType:
				batches[batchIndex].paths = appendPolygonPaths(batches[batchIndex].paths, feature.Polygons)
			}
		}
	}
	return emitLines(batches, options.Indexed, limit, sinks)
}

func emitLines(batches []lineBatch, indexed bool, limit int, sinks lineSinks) error {
	for _, batch := range batches {
		paint := batch.paint
		// Dash lengths and path offsets depend on the evaluated width or offset,
		// so only plain lines have a width-independent form. The width guard
		// matches the baked tessellator, which emits nothing at or below Epsilon.
		if sinks.extruded != nil && len(paint.dashes) == 0 && paint.offset == 0 {
			if paint.width <= geometry.Epsilon {
				continue
			}
			mesh, err := geometry.TessellateExtrudedLines(batch.paths, geometry.ExtrudedLineStyle{Cap: paint.lineCap, Join: paint.lineJoin}, limit, indexed)
			if err != nil {
				return resourceError(err)
			}
			if err := sinks.extruded(mesh, paint.color, paint.pixels/2); err != nil {
				return err
			}
			continue
		}
		// Dash lengths are multiples of the width, so a pattern and the distance
		// along each path replace baked dashes. Dash caps and joins, path offsets
		// and longer patterns have no such form here.
		if pattern, ok := geometry.NewDashPattern(paint.dashes, paint.width); ok && sinks.dashed != nil && paint.offset == 0 && paint.lineCap != "round" && paint.lineCap != "square" {
			mesh, err := geometry.TessellateDashedLines(batch.paths, limit, indexed)
			if err != nil {
				return resourceError(err)
			}
			if err := sinks.dashed(mesh, paint.color, paint.pixels, paint.width, pattern); err != nil {
				return err
			}
			continue
		}
		mesh, err := geometry.TessellateLines(batch.paths, geometry.LineStyle{Width: paint.width, Offset: paint.offset, Dashes: paint.dashes, Cap: paint.lineCap, Join: paint.lineJoin}, limit, indexed)
		if err != nil {
			return resourceError(err)
		}
		if err := sinks.baked(mesh, paint.color); err != nil {
			return err
		}
	}
	return nil
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
