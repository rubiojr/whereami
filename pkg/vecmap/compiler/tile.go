package compiler

import (
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/mvt"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
)

// Primitive is retained tile-local geometry with evaluated solid or pattern paint.
// Mesh buffers are owned; layer and pattern names can borrow immutable inputs.
// Resource IDs, textures, clipping and native handles belong to later adapters.
type Primitive struct {
	Order        int
	LayerID      string
	Mesh         geometry.Mesh
	Color        style.Color
	PatternName  string
	PatternScale float64
	Opacity      float64
	// Directions, when set, parallels Mesh.Vertices. Mesh then holds centerline
	// anchors and the rendered position is the anchor plus HalfWidth logical
	// pixels along the map-aligned direction, so the geometry is independent of
	// the evaluated line width. Only LayerOptions.ExtrudeLines produces it.
	Directions []geometry.Point
	HalfWidth  float64
	// Dynamic marks geometry whose vertices depend on the evaluated style zoom
	// (dashed or offset lines). It is set only with LayerOptions.ExtrudeLines.
	Dynamic bool
	// Distances, when set with Directions, parallels Mesh.Vertices and holds the
	// tile-unit distance of each vertex along its path. The line is drawn where
	// that distance, in multiples of DashUnit (the line width in tile units),
	// lies on a dash of Dashes. Only LayerOptions.ShaderDashes produces it.
	Distances []float64
	Dashes    geometry.DashPattern
	DashUnit  float64
	// StableRun, from CompileTilePlanned, is 1 + the index of the StablePlan run
	// of a primitive packed into StableMesh, and zero for other primitives.
	StableRun int
	// borrowed marks a placeholder for geometry of that shape already in a
	// borrowed StableMesh; Mesh, Directions and Distances are empty.
	borrowed shape
}

// shape is the kind of geometry a borrowed primitive stands for.
type shape uint8

const (
	solidShape shape = iota + 1 // fills, patterns and backgrounds
	extrudedShape
	dashedShape
)

// CompileTile traverses visible layers in document order and emits geometry with
// a shared rendered-triangle budget. TriangleLimit applies both per batch and to
// total emitted geometry; zero selects MaxTriangles. Symbols are dispatched at
// their original position to symbols, which owns candidate limits/storage. A nil
// symbols callback skips symbols; emit is required. Unsupported kinds are skipped.
//
// Callbacks are synchronous and not retained. Prior successful callback effects
// survive later errors. Stage output until success for atomic publication. Source
// features and styles obey CompileFill's bounded-input contract; they are neither
// modified nor retained, apart from immutable name strings in emitted primitives.
func CompileTile[F mvt.Features](layers []style.CompiledLayer, sources map[string]F, options LayerOptions,
	emit func(Primitive) error, symbols func(style.CompiledLayer) error,
) error {
	return CompileTilePlanned(layers, sources, options, nil, emit, symbols)
}

// CompileTilePlanned is CompileTile following stable batches with planner,
// which needs options.ExtrudeLines; nil follows none. A planner that borrows
// returns ErrStableMismatch as soon as a stable batch differs from its
// reference, and emits placeholders for the stable primitives instead of
// geometry: they pack only with a FragmentBuilder borrowing the reference's
// StableMesh. Use a planner for one compilation.
func CompileTilePlanned[F mvt.Features](layers []style.CompiledLayer, sources map[string]F, options LayerOptions, planner *StablePlanner,
	emit func(Primitive) error, symbols func(style.CompiledLayer) error,
) error {
	_, limit, err := options.validated()
	if err != nil || emit == nil || (planner != nil && !options.ExtrudeLines) {
		return ErrOptions
	}
	scratch := lineScratches.Get().(*lineScratch)
	defer lineScratches.Put(scratch)
	assembly := tileAssembly{remaining: limit, limit: limit, emit: emit, lines: scratch, plan: planner}
	for _, layer := range layers {
		if !layer.VisibleAt(options.Zoom) || layer.Hidden() {
			continue
		}
		if err := assembly.layer(layer, sources[layer.SourceLayer], options, symbols); err != nil {
			return err
		}
	}
	return planner.finish()
}

type tileAssembly struct {
	remaining int
	limit     int
	emit      func(Primitive) error
	lines     *lineScratch
	plan      *StablePlanner
}

// lineScratches keeps tessellation storage across the tiles a compiler
// prepares. It holds only scratch vertices and indices, never emitted data.
var lineScratches = sync.Pool{New: func() any { return new(lineScratch) }}

func (a *tileAssembly) layer(layer style.CompiledLayer, features mvt.Features, options LayerOptions, symbols func(style.CompiledLayer) error) error {
	// Stable sinks pack nothing while borrowing: the borrowed StableMesh holds
	// their geometry.
	borrowing := a.plan.borrowing()
	solid := func(mesh geometry.Mesh, color style.Color) error {
		primitive := Primitive{Order: layer.Order, LayerID: layer.ID, Mesh: mesh, Color: color}
		if borrowing {
			primitive.Mesh, primitive.borrowed = geometry.Mesh{}, solidShape
		}
		return a.append(primitive, false)
	}
	lines := lineSinks{baked: solid, scratch: a.lines, plan: a.plan}
	if options.ExtrudeLines {
		lines.baked = func(mesh geometry.Mesh, color style.Color) error {
			return a.append(Primitive{Order: layer.Order, LayerID: layer.ID, Mesh: mesh, Color: color, Dynamic: true}, false)
		}
		lines.extruded = func(mesh geometry.ExtrudedMesh, color style.Color, halfWidth float64) error {
			primitive := Primitive{Order: layer.Order, LayerID: layer.ID, Color: color, HalfWidth: halfWidth}
			if borrowing {
				primitive.borrowed = extrudedShape
				return a.append(primitive, false)
			}
			primitive.Mesh = geometry.Mesh{Vertices: make([]geometry.Point, len(mesh.Vertices)), Indices: slices.Clone(mesh.Indices)}
			primitive.Directions = make([]geometry.Point, len(mesh.Vertices))
			for i, vertex := range mesh.Vertices {
				primitive.Mesh.Vertices[i], primitive.Directions[i] = vertex.Anchor, vertex.Direction
			}
			return a.append(primitive, false)
		}
	}
	if options.ShaderDashes {
		lines.dashed = func(mesh geometry.DashedMesh, color style.Color, pixels, width float64, pattern geometry.DashPattern) error {
			primitive := Primitive{Order: layer.Order, LayerID: layer.ID, Color: color, HalfWidth: pixels / 2, Dashes: pattern, DashUnit: width}
			if borrowing {
				primitive.borrowed = dashedShape
				return a.append(primitive, false)
			}
			count := len(mesh.Vertices)
			primitive.Mesh = geometry.Mesh{Vertices: make([]geometry.Point, count), Indices: slices.Clone(mesh.Indices)}
			primitive.Directions, primitive.Distances = make([]geometry.Point, count), make([]float64, count)
			for i, vertex := range mesh.Vertices {
				primitive.Mesh.Vertices[i], primitive.Directions[i], primitive.Distances[i] = vertex.Anchor, vertex.Direction, vertex.Distance
			}
			return a.append(primitive, false)
		}
	}
	switch layer.Kind {
	case "background":
		context := style.Context{Zoom: options.Zoom}
		color, ok := layer.ColorValue("background-color", context, style.Color{Alpha: 255})
		if !ok {
			return nil
		}
		opacity := layer.NumberValue("background-opacity", context, 1)
		if err := a.plan.beginRun(stableBackground, layer.Order, "", "", nil); err != nil {
			return err
		}
		defer a.plan.end()
		return solid(BackgroundGeometry(options.Indexed), style.ColorWithOpacity(color, opacity))
	case "fill", "fill-extrusion":
		return compileFill(features, layer, options, solid, func(mesh geometry.Mesh, name string, scale, opacity float64) error {
			primitive := Primitive{Order: layer.Order, LayerID: layer.ID, Mesh: mesh, PatternName: name, PatternScale: scale, Opacity: opacity}
			if borrowing {
				primitive.Mesh, primitive.borrowed = geometry.Mesh{}, solidShape
			}
			return a.append(primitive, true)
		}, lines)
	case "line":
		return compileLine(features, layer, options, lines)
	case "symbol":
		if symbols != nil {
			return symbols(layer)
		}
	}
	return nil
}

func (a *tileAssembly) append(primitive Primitive, pattern bool) error {
	count := len(primitive.Mesh.Vertices)
	if primitive.Mesh.Indices != nil {
		count = len(primitive.Mesh.Indices)
	}
	var run *stableRun
	if a.plan != nil && a.plan.current > 0 {
		run, primitive.StableRun = a.plan.run(), a.plan.current
		if primitive.borrowed != 0 {
			count = run.elements
		}
	} else if primitive.borrowed != 0 {
		return ErrStableMismatch // stable geometry outside any planned batch
	}
	emitted := count != 0
	if pattern {
		emitted = emitted && primitive.PatternName != "" && primitive.Opacity > 0
	} else {
		emitted = emitted && primitive.Color.Alpha != 0
	}
	if run != nil {
		if !a.plan.borrowing() {
			run.elements, run.emitted = count, emitted
		} else if run.emitted != emitted {
			return ErrStableMismatch // paint now shows or hides the batch
		}
	}
	if !emitted {
		return nil
	}
	if count%3 != 0 {
		return errors.New("tile compiler produced an incomplete triangle")
	}
	triangles := count / 3
	if triangles > a.remaining {
		return fmt.Errorf("%w: tile geometry exceeds %d-triangle limit", mvt.ErrFeatureResourceLimit, a.limit)
	}
	a.remaining -= triangles
	return a.emit(primitive)
}

// BackgroundGeometry returns an owned 256-unit tile rectangle, retaining the
// original diagonal and triangle order in expanded and indexed representations.
func BackgroundGeometry(indexed bool) geometry.Mesh {
	if indexed {
		return geometry.Mesh{Vertices: []geometry.Point{{X: 0, Y: 0}, {X: view.TileSize, Y: 0}, {X: view.TileSize, Y: view.TileSize}, {X: 0, Y: view.TileSize}}, Indices: []uint32{0, 1, 2, 0, 2, 3}}
	}
	return geometry.Mesh{Vertices: []geometry.Point{
		{X: 0, Y: 0}, {X: view.TileSize, Y: 0}, {X: view.TileSize, Y: view.TileSize},
		{X: 0, Y: 0}, {X: view.TileSize, Y: view.TileSize}, {X: 0, Y: view.TileSize},
	}}
}
