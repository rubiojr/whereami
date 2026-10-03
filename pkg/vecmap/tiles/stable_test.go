package tiles

import (
	"os"
	"strings"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/compiler"
	"github.com/rubiojr/whereami/pkg/vecmap/fixture"
	"github.com/rubiojr/whereami/pkg/vecmap/glyph"
	"github.com/rubiojr/whereami/pkg/vecmap/liberty"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/sprite"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireReuseMatchesFull prepares and builds data at each zoom twice, in full
// and borrowing the previous zoom's reused build, and requires identical
// output. It reports which zooms borrowed.
func requireReuseMatchesFull(t *testing.T, data []byte, layers []style.CompiledLayer, options PrepareOptions, zooms []float64, assets func(zoom float64) Assets) []bool {
	t.Helper()
	source, err := Decode(data, options.Indexed)
	require.NoError(t, err)
	var base StableBase
	borrowed := make([]bool, len(zooms))
	for i, zoom := range zooms {
		options.Zoom = zoom
		full, err := PrepareSource(source, layers, options)
		require.NoError(t, err)
		want, err := full.BuildOwned(assets(zoom), 1<<30)
		require.NoError(t, err)
		reused, err := PrepareSourceReusing(source, layers, options, base)
		require.NoError(t, err)
		got, err := reused.BuildOwned(assets(zoom), 1<<30)
		require.NoError(t, err)
		require.Equal(t, want.Fragment.Scene, got.Fragment.Scene, "zoom %g", zoom)
		require.Equal(t, want.Fragment.Draws, got.Fragment.Draws, "zoom %g", zoom)
		require.Equal(t, want.Fragment.Symbols, got.Fragment.Symbols, "zoom %g", zoom)
		require.Equal(t, want.MissingFonts, got.MissingFonts)
		require.Equal(t, want.Limits, got.Limits)
		require.Equal(t, want.Fragment.stable, got.Fragment.stable, "the plan a later build borrows, zoom %g", zoom)
		require.False(t, want.Borrowed)
		assert.True(t, !got.Borrowed || reused.Borrowed())
		if borrowed[i] = got.Borrowed; borrowed[i] {
			// The fragment shares the base's buffers rather than copies.
			mesh := fragmentMesh(got.Fragment, compiler.StableMesh)
			require.NotNil(t, mesh)
			for _, pair := range [][2]int{{len(base.mesh.Vertices), len(mesh.Vertices)}, {len(base.mesh.Offsets), len(mesh.Offsets)}, {len(base.mesh.Positions), len(mesh.Positions)}} {
				require.Equal(t, pair[0], pair[1])
			}
			if len(mesh.Vertices) > 0 {
				assert.Same(t, &base.mesh.Vertices[0], &mesh.Vertices[0])
			}
			if len(mesh.Positions) > 0 {
				assert.Same(t, &base.mesh.Positions[0], &mesh.Positions[0])
			}
		}
		base = got.Fragment.StableBase()
		require.True(t, base.Valid(), "zoom %g", zoom)
	}
	return borrowed
}

func residentOptions(indexed, compact, dashes bool) PrepareOptions {
	return PrepareOptions{Tile: testTile, Indexed: indexed, ResidentGeometry: true, ResidentSymbols: true, ResidentDashes: dashes, CompactVertices: compact}
}

func constantAssets(Assets) func(float64) Assets {
	return func(float64) Assets { return prepareAssets() }
}

func TestReuseStableMatchesFullBuild(t *testing.T) {
	zooms := []float64{3, 3.0625, 3.125, 4, 3.0625, 5, 2.5}
	for _, indexed := range []bool{false, true} {
		for _, compact := range []bool{false, true} {
			for _, dashes := range []bool{false, true} {
				for _, packed := range []bool{false, true} {
					options := residentOptions(indexed, compact, dashes)
					options.PackedVertices = packed
					options.ShortIndices = indexed && packed
					borrowed := requireReuseMatchesFull(t, residentPBF(), residentStyle(t), options, zooms, constantAssets(Assets{}))
					assert.Equal(t, []bool{false, true, true, true, true, true, true}, borrowed, "indexed %t compact %t dashes %t packed %t", indexed, compact, dashes, packed)
				}
			}
		}
	}
}

// styleWith is residentStyle with one more layer before the labels.
func styleWith(t *testing.T, layer string) []style.CompiledLayer {
	t.Helper()
	layers, err := style.Parse([]byte(`{"version":8,"layers":[
		{"id":"background","type":"background","paint":{"background-color":"white"}},
		{"id":"land","type":"fill","source-layer":"land","paint":{"fill-color":"#203040","fill-outline-color":"#000000"}},
		{"id":"casing","type":"line","source-layer":"roads","layout":{"line-cap":"round","line-join":"round"},
		 "paint":{"line-color":"#888888","line-width":["interpolate",["linear"],["zoom"],2,2,6,10]}},
		` + layer + `,
		{"id":"labels","type":"symbol","source-layer":"labels","layout":{"text-field":"A","text-font":["Test"]}}
	]}`))
	require.NoError(t, err)
	return layers
}

func TestReuseStableFallsBackWhenStableGeometryChanges(t *testing.T) {
	zooms := []float64{3, 3.5, 4, 4.5, 3.5}
	for name, test := range map[string]struct {
		layer  string
		assets func(float64) Assets
		want   []bool
	}{
		// A batch that paint hides packs nothing, so the mesh changes.
		"opacity reaches zero": {layer: `{"id":"water","type":"fill","source-layer":"land","paint":{"fill-color":"#0000ff","fill-opacity":["step",["zoom"],1,4,0]}}`,
			want: []bool{false, true, false, true, false}},
		"layer becomes visible": {layer: `{"id":"water","type":"fill","source-layer":"land","minzoom":4,"paint":{"fill-color":"#0000ff"}}`,
			want: []bool{false, true, false, true, false}},
		// An offset line is baked into the dynamic mesh instead.
		"line gains an offset": {layer: `{"id":"edge","type":"line","source-layer":"roads","paint":{"line-color":"#ff0000","line-offset":["step",["zoom"],0,4,2]}}`,
			want: []bool{false, true, false, true, false}},
		"pattern sprite goes missing": {layer: `{"id":"dots","type":"fill","source-layer":"land","paint":{"fill-pattern":"dots"}}`,
			assets: func(zoom float64) Assets {
				assets := prepareAssets()
				if zoom >= 4 {
					sprites := assets.Sprite
					assets.Sprite = func(name string, color style.Color, opacity float64) (sprite.Image, bool) {
						if name == "dots" {
							return sprite.Image{}, false
						}
						return sprites(name, color, opacity)
					}
				}
				return assets
			},
			want: []bool{false, true, false, true, false}},
	} {
		t.Run(name, func(t *testing.T) {
			assets := test.assets
			if assets == nil {
				assets = constantAssets(Assets{})
			}
			for _, indexed := range []bool{false, true} {
				borrowed := requireReuseMatchesFull(t, residentPBF(), styleWith(t, test.layer), residentOptions(indexed, true, true), zooms, assets)
				assert.Equal(t, test.want, borrowed)
			}
		})
	}
}

func TestReuseStableNeedsTheSameInputs(t *testing.T) {
	layers := residentStyle(t)
	options := residentOptions(true, true, true)
	options.Zoom = 3
	source, err := Decode(residentPBF(), true)
	require.NoError(t, err)
	p, err := PrepareSource(source, layers, options)
	require.NoError(t, err)
	built, err := p.BuildOwned(prepareAssets(), 1<<20)
	require.NoError(t, err)
	base := built.Fragment.StableBase()
	require.True(t, base.Valid())
	options.Zoom = 3.0625
	other, err := Decode(residentPBF(), true)
	require.NoError(t, err)
	changed := options
	changed.TriangleLimit = 1000
	for name, prepare := range map[string]func() (*Prepared, error){
		"same inputs":    func() (*Prepared, error) { return PrepareSourceReusing(source, layers, options, base) },
		"another decode": func() (*Prepared, error) { return PrepareSourceReusing(other, layers, options, base) },
		"other layers":   func() (*Prepared, error) { return PrepareSourceReusing(source, residentStyle(t), options, base) },
		"other options":  func() (*Prepared, error) { return PrepareSourceReusing(source, layers, changed, base) },
		"no base":        func() (*Prepared, error) { return PrepareSourceReusing(source, layers, options, StableBase{}) },
		"not resident": func() (*Prepared, error) {
			return PrepareSourceReusing(source, layers, PrepareOptions{Tile: testTile, Zoom: 3, Indexed: true}, base)
		},
		"invalid options": func() (*Prepared, error) {
			return PrepareSourceReusing(source, layers, PrepareOptions{Tile: testTile, Zoom: -1, Indexed: true}, base)
		},
	} {
		p, err := prepare()
		if name == "invalid options" {
			assert.ErrorIs(t, err, ErrInput)
			continue
		}
		require.NoError(t, err, name)
		assert.Equal(t, name == "same inputs", p.Borrowed(), name)
	}
	assert.False(t, (&Fragment{Scene: &scene.Scene{}}).StableBase().Valid())
	assert.False(t, (*Fragment)(nil).StableBase().Valid())
	unsplit, err := PrepareSource(source, layers, PrepareOptions{Tile: testTile, Zoom: 3, Indexed: true})
	require.NoError(t, err)
	unsplitBuilt, err := unsplit.BuildOwned(prepareAssets(), 1<<20)
	require.NoError(t, err)
	assert.False(t, unsplitBuilt.Fragment.StableBase().Valid(), "a single-mesh fragment has nothing to borrow")
}

func TestReuseStableOnPinnedTile(t *testing.T) {
	path := os.Getenv("WHEREAMI_VECTOR_TILE_FIXTURE")
	if path == "" {
		t.Skip("set the pinned tile fixture environment variable")
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	layers, err := liberty.Layers()
	require.NoError(t, err)
	assets := func(float64) Assets {
		return Assets{Sprite: liberty.Sprite, SpriteEntry: liberty.SpriteEntry, FallbackEligible: glyph.LegacyFallbackEligible}
	}
	for _, coarser := range []int{0, 1} {
		// Coarser 1 also packs its vertices and indices.
		options := PrepareOptions{Tile: fixture.Tile(), Coarser: coarser, Indexed: true, ResidentGeometry: true, ResidentSymbols: true, ResidentDashes: true, CompactVertices: true,
			PackedVertices: coarser == 1, ShortIndices: coarser == 1}
		var zooms []float64
		for k := range 16 {
			zooms = append(zooms, 8.5+float64(k)/16)
		}
		borrowed := requireReuseMatchesFull(t, data, layers, options, zooms, assets)
		assert.Equal(t, strings.Repeat("x", len(zooms)-1), strings.Repeat("x", count(borrowed)), "coarser %d borrows at every step: %v", coarser, borrowed)
	}
}

func count(values []bool) int {
	n := 0
	for _, value := range values {
		if value {
			n++
		}
	}
	return n
}
