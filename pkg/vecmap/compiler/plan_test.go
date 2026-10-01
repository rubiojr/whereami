package compiler

import (
	"bytes"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/mvt"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/sprite"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var planTile = view.TileID{Z: 9, X: 1, Y: 1}

// planLayers has every stable kind (background, fill with an extruded outline,
// pattern, extruded and shader-dashed lines) and a dynamic offset line.
func planLayers() ([]style.CompiledLayer, map[string]mvt.FeatureSlice) {
	layers := tileLayers("background", "fill", "fill", "line", "line", "line")
	width := []any{"interpolate", []any{"linear"}, []any{"zoom"}, 8.0, 1.0, 12.0, 9.0}
	layers[0].Paint = map[string]any{"background-color": "#ffffff"}
	layers[1].Paint = map[string]any{"fill-color": []any{"interpolate", []any{"linear"}, []any{"zoom"}, 8.0, "#102030", 12.0, "#405060"}, "fill-outline-color": "#405060"}
	layers[2].ID, layers[2].Paint = "pattern", map[string]any{"fill-pattern": "dots"}
	layers[3].ID, layers[3].Paint = "plain", map[string]any{"line-color": "#ff0000", "line-width": width}
	layers[3].Layout = map[string]any{"line-cap": "round", "line-join": "round"}
	layers[4].ID, layers[4].Paint = "dashed", map[string]any{"line-width": width, "line-dasharray": []any{2.0, 1.0}}
	layers[5].ID, layers[5].Paint = "offset", map[string]any{"line-width": width, "line-offset": 3.0}
	bend := mvt.Feature{GeometryType: mvt.LineStringType, Lines: [][]geometry.Point{{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 14, Y: 9}}}}
	return layers, map[string]mvt.FeatureSlice{"source": {polygonFeature(nil), bend}}
}

func dotsLookup(present bool) SpriteLookup {
	return func(name string, _ style.Color, _ float64) (sprite.Image, bool) {
		return sprite.Image{Width: 2, Height: 2, PixelRatio: 1, Pixels: bytes.Repeat([]byte{255}, 16)}, present && name == "dots"
	}
}

type plannedBuild struct {
	scene   *scene.Scene
	sources []DrawSource
	plan    *StablePlan
}

func (p *plannedBuild) stable() scene.Mesh {
	for _, mesh := range p.scene.Meshes {
		if mesh.ID == StableMesh {
			return mesh
		}
	}
	return scene.Mesh{}
}

// planBuild compiles and packs a split fragment, borrowing base's StableMesh
// when base is set.
func planBuild(layers []style.CompiledLayer, sources map[string]mvt.FeatureSlice, zoom float64, indexed, compact bool, base *plannedBuild, lookup SpriteLookup) (*plannedBuild, error) {
	planner := NewStablePlanner(nil)
	if base != nil {
		planner = NewStablePlanner(base.plan)
	}
	var primitives []Primitive
	options := LayerOptions{SourceZoom: 9, Zoom: zoom, Indexed: indexed, ExtrudeLines: true, ShaderDashes: true}
	if err := CompileTilePlanned(layers, sources, options, planner, func(p Primitive) error { primitives = append(primitives, p); return nil }, nil); err != nil {
		return nil, err
	}
	b := NewFragmentBuilder(indexed, 0, 0)
	b.Split()
	if compact {
		b.CompactVertices()
	}
	if base != nil {
		b.Borrow(base.stable(), base.plan)
	}
	b.Reserve(primitives)
	for _, primitive := range primitives {
		b.Primitive(planTile, primitive, lookup)
	}
	packed, drawSources, err := b.Finish()
	if err != nil {
		return nil, err
	}
	return &plannedBuild{packed, drawSources, b.StablePlan(planner.Plan())}, nil
}

func TestBorrowedBuildsMatchFullBuilds(t *testing.T) {
	layers, sources := planLayers()
	for _, indexed := range []bool{false, true} {
		for _, compact := range []bool{false, true} {
			base, err := planBuild(layers, sources, 9, indexed, compact, nil, dotsLookup(true))
			require.NoError(t, err)
			require.NotEmpty(t, base.plan.runs)
			for _, zoom := range []float64{9.25, 10, 11, 9.5} {
				want, err := planBuild(layers, sources, zoom, indexed, compact, nil, dotsLookup(true))
				require.NoError(t, err)
				got, err := planBuild(layers, sources, zoom, indexed, compact, base, dotsLookup(true))
				require.NoError(t, err)
				require.Equal(t, want.scene, got.scene, "zoom %g", zoom)
				require.Equal(t, want.sources, got.sources)
				require.Equal(t, want.plan, got.plan)
				require.Same(t, base.plan, got.plan, "a borrowing build keeps its plan")
				stable, previous := got.stable(), base.stable()
				if compact {
					require.NotEmpty(t, stable.Positions)
					assert.Same(t, &previous.Positions[0], &stable.Positions[0], "and shares the mesh")
				}
				require.NotEmpty(t, stable.Vertices) // dashed lines keep every attribute
				assert.Same(t, &previous.Vertices[0], &stable.Vertices[0], "and shares the mesh")
				base = got
			}
		}
	}
}

func TestPlannedCompilationMismatches(t *testing.T) {
	layers, sources := planLayers()
	base, err := planBuild(layers, sources, 9, true, true, nil, dotsLookup(true))
	require.NoError(t, err)
	compile := func(layers []style.CompiledLayer, sources map[string]mvt.FeatureSlice, zoom float64) error {
		return CompileTilePlanned(layers, sources, LayerOptions{SourceZoom: 9, Zoom: zoom, Indexed: true, ExtrudeLines: true, ShaderDashes: true},
			NewStablePlanner(base.plan), func(Primitive) error { return nil }, nil)
	}
	require.NoError(t, compile(layers, sources, 10))

	more := map[string]mvt.FeatureSlice{"source": append(append(mvt.FeatureSlice{}, sources["source"]...), polygonFeature(nil))}
	assert.ErrorIs(t, compile(layers, more, 10), ErrStableMismatch, "another feature joins a batch")

	hidden, _ := planLayers()
	hidden[1].Paint = map[string]any{"fill-color": "#102030", "fill-opacity": []any{"step", []any{"zoom"}, 1.0, 10.0, 0.0}, "fill-outline-color": "#405060"}
	assert.ErrorIs(t, compile(hidden, sources, 10), ErrStableMismatch, "paint hides a batch")

	// Each stable kind can be the first batch that differs: read another
	// source layer with more features, or drop the background.
	other := map[string]mvt.FeatureSlice{"source": sources["source"], "other": append(append(mvt.FeatureSlice{}, sources["source"]...), polygonFeature(nil))}
	for _, layer := range []int{2, 3, 4} {
		moved, _ := planLayers()
		moved[layer].SourceLayer = "other"
		assert.ErrorIs(t, compile(moved, other, 10), ErrStableMismatch, "layer %d", layer)
	}
	assert.ErrorIs(t, CompileTilePlanned(layers, sources, LayerOptions{SourceZoom: 9, Zoom: 10, Indexed: true, ExtrudeLines: true, ShaderDashes: true},
		NewStablePlanner(&StablePlan{runs: base.plan.runs[1:]}), func(Primitive) error { return nil }, nil), ErrStableMismatch, "background")

	assert.ErrorIs(t, compile(layers[:4], sources, 10), ErrStableMismatch, "a batch is missing at the end")
	extra, _ := planLayers()
	extra = append(extra, tileLayers("fill", "fill", "fill", "fill", "fill", "fill", "fill")[6])
	assert.ErrorIs(t, compile(extra, sources, 10), ErrStableMismatch, "one batch more")

	assert.ErrorIs(t, CompileTilePlanned(layers, sources, LayerOptions{SourceZoom: 9, Zoom: 9}, NewStablePlanner(nil), func(Primitive) error { return nil }, nil),
		ErrOptions, "planning needs extruded lines")

	assembly := tileAssembly{plan: NewStablePlanner(base.plan), remaining: 100, limit: 100, emit: func(Primitive) error { return nil }}
	assert.ErrorIs(t, assembly.append(Primitive{borrowed: solidShape, Color: style.Color{Alpha: 255}}, false), ErrStableMismatch, "a placeholder outside a batch")
}

func TestBorrowedBuildMismatchesAndRejections(t *testing.T) {
	layers, sources := planLayers()
	withDots, err := planBuild(layers, sources, 9, true, true, nil, dotsLookup(true))
	require.NoError(t, err)
	withoutDots, err := planBuild(layers, sources, 9, true, true, nil, dotsLookup(false))
	require.NoError(t, err)
	_, err = planBuild(layers, sources, 10, true, true, withDots, dotsLookup(false))
	assert.ErrorIs(t, err, ErrStableMismatch, "a missing sprite would leave the pattern out")
	_, err = planBuild(layers, sources, 10, true, true, withoutDots, dotsLookup(true))
	assert.ErrorIs(t, err, ErrStableMismatch, "a present sprite would pack the pattern")
	got, err := planBuild(layers, sources, 10, true, true, withoutDots, dotsLookup(false))
	require.NoError(t, err)
	want, err := planBuild(layers, sources, 10, true, true, nil, dotsLookup(false))
	require.NoError(t, err)
	require.Equal(t, want.scene, got.scene)

	stable := withDots.stable()
	finish := func(prepare func(*FragmentBuilder)) error {
		b := NewFragmentBuilder(true, 0, 0)
		prepare(b)
		_, _, err := b.Finish()
		return err
	}
	for name, prepare := range map[string]func(*FragmentBuilder){
		"unsplit":  func(b *FragmentBuilder) { b.Borrow(stable, withDots.plan) },
		"no plan":  func(b *FragmentBuilder) { b.Split(); b.Borrow(stable, nil) },
		"wrong ID": func(b *FragmentBuilder) { b.Split(); b.Borrow(scene.Mesh{ID: DynamicMesh}, withDots.plan) },
		"after packing": func(b *FragmentBuilder) {
			b.Split()
			b.Primitive(planTile, Primitive{Mesh: BackgroundGeometry(true), Color: style.Color{Alpha: 255}}, nil)
			b.Borrow(stable, withDots.plan)
		},
		"stable geometry": func(b *FragmentBuilder) {
			b.Split()
			b.Borrow(stable, withDots.plan)
			b.Primitive(planTile, Primitive{Mesh: BackgroundGeometry(true), Color: style.Color{Alpha: 255}}, nil)
		},
		"unknown run": func(b *FragmentBuilder) {
			b.Split()
			b.Borrow(stable, withDots.plan)
			b.Primitive(planTile, Primitive{borrowed: solidShape, StableRun: 99}, nil)
		},
		"extruded width": func(b *FragmentBuilder) {
			b.Split()
			b.Borrow(stable, withDots.plan)
			b.Primitive(planTile, Primitive{borrowed: extrudedShape, StableRun: 1}, nil)
		},
		"dash unit": func(b *FragmentBuilder) {
			b.Split()
			b.Borrow(stable, withDots.plan)
			b.Primitive(planTile, Primitive{borrowed: dashedShape, StableRun: 1, HalfWidth: 1}, nil)
		},
	} {
		assert.ErrorIs(t, finish(prepare), ErrPackingInput, name)
	}

	b := NewFragmentBuilder(true, 0, 0)
	b.Split()
	assert.Nil(t, b.StablePlan(withDots.plan), "before Finish")
	_, _, err = b.Finish()
	require.NoError(t, err)
	assert.Nil(t, b.StablePlan(nil))
	assert.NotNil(t, b.StablePlan(withDots.plan))
	assert.Positive(t, withDots.plan.RetainedBytes())
	assert.Zero(t, (*StablePlan)(nil).RetainedBytes())
}
