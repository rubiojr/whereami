package compiler

import (
	"errors"
	"fmt"

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
}

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
func CompileTile(layers []style.CompiledLayer, sources map[string][]mvt.Feature, options LayerOptions,
	emit func(Primitive) error, symbols func(style.CompiledLayer) error,
) error {
	_, limit, err := options.validated()
	if err != nil || emit == nil {
		return ErrOptions
	}
	assembly := tileAssembly{remaining: limit, limit: limit, emit: emit}
	for _, layer := range layers {
		if !layer.VisibleAt(options.Zoom) || layer.Hidden() {
			continue
		}
		if err := assembly.layer(layer, sources[layer.SourceLayer], options, symbols); err != nil {
			return err
		}
	}
	return nil
}

type tileAssembly struct {
	remaining int
	limit     int
	emit      func(Primitive) error
}

func (a *tileAssembly) layer(layer style.CompiledLayer, features []mvt.Feature, options LayerOptions, symbols func(style.CompiledLayer) error) error {
	solid := func(mesh geometry.Mesh, color style.Color) error {
		return a.append(Primitive{Order: layer.Order, LayerID: layer.ID, Mesh: mesh, Color: color}, false)
	}
	switch layer.Kind {
	case "background":
		context := style.Context{Zoom: options.Zoom}
		color, ok := layer.ColorValue("background-color", context, style.Color{Alpha: 255})
		if !ok {
			return nil
		}
		opacity := layer.NumberValue("background-opacity", context, 1)
		return solid(BackgroundGeometry(options.Indexed), style.ColorWithOpacity(color, opacity))
	case "fill", "fill-extrusion":
		return CompileFill(features, layer, options, solid, func(mesh geometry.Mesh, name string, scale, opacity float64) error {
			return a.append(Primitive{Order: layer.Order, LayerID: layer.ID, Mesh: mesh, PatternName: name, PatternScale: scale, Opacity: opacity}, true)
		})
	case "line":
		return CompileLine(features, layer, options, solid)
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
	if count == 0 {
		return nil
	}
	if pattern {
		if primitive.PatternName == "" || primitive.Opacity <= 0 {
			return nil
		}
	} else if primitive.Color.Alpha == 0 {
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
