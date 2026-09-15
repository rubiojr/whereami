package fixture

import (
	"errors"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/compiler"
	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/glyph"
	"github.com/rubiojr/whereami/pkg/vecmap/mvt"
	"github.com/rubiojr/whereami/pkg/vecmap/placement"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func inputs(t testing.TB) ([]byte, map[string][]byte) {
	t.Helper()
	path, dir := os.Getenv("WHEREAMI_VECTOR_TILE_FIXTURE"), os.Getenv("WHEREAMI_VECTOR_GLYPH_FIXTURE_DIR")
	if path == "" || dir == "" {
		t.Skip("set pinned tile and glyph fixture environment variables")
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	ranges := make(map[string][]byte)
	for _, font := range []string{"Noto Sans Regular", "Noto Sans Bold", "Noto Sans Italic"} {
		ranges[font], err = os.ReadFile(filepath.Join(dir, url.PathEscape(font)+".pbf"))
		require.NoError(t, err)
	}
	return data, ranges
}

func TestPinnedFixtureHeadless(t *testing.T) {
	data, ranges := inputs(t)
	expanded, err := Compile(data, ranges, Options{})
	require.NoError(t, err)
	direct, err := Compile(data, ranges, Options{DirectIndexed: true})
	require.NoError(t, err)
	assert.Equal(t, 62, direct.Labels)
	assert.Empty(t, direct.MissingFonts)
	assert.Empty(t, direct.Limits)
	require.Len(t, direct.Scene.Draws, 45)
	assert.Equal(t, expanded.Scene.Draws, direct.Scene.Draws)
	assert.Equal(t, expanded.Scene.Textures, direct.Scene.Textures)
	require.Len(t, expanded.Scene.Meshes[0].Vertices, 782409)
	require.Len(t, direct.Scene.Meshes[0].Vertices, 351558)
	require.Len(t, direct.Scene.Meshes[0].Indices, 782409)
	for i, index := range direct.Scene.Meshes[0].Indices {
		want, got := expanded.Scene.Meshes[0].Vertices[i], direct.Scene.Meshes[0].Vertices[index]
		if want != got {
			require.Equal(t, want, got, "vertex %d", i)
		}
	}
	camera := direct.Camera
	camera.Bearing, camera.Zoom = 35, 10.5
	frame := direct.Frame(camera)
	assert.Same(t, direct.Scene, frame.Scene)
	assert.NotEqual(t, direct.Frame(direct.Camera).Transforms, frame.Transforms)
}

func TestLoaderAndAtomicFailure(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("bad"), make([]byte, mvt.MaxTileBytes+1)} {
		called := false
		result, err := CompileWithGlyphLoader(data, func([]string) (map[string][]byte, error) { called = true; return nil, nil }, Options{})
		require.Error(t, err)
		assert.Nil(t, result)
		assert.False(t, called)
	}
	data, ranges := inputs(t)
	for _, indexed := range []bool{false, true} {
		calls := 0
		result, err := CompileWithGlyphLoader(data, func(fonts []string) (map[string][]byte, error) {
			calls++
			assert.Equal(t, []string{"Noto Sans Bold", "Noto Sans Italic", "Noto Sans Regular"}, fonts)
			return ranges, nil
		}, Options{DirectIndexed: indexed})
		require.NoError(t, err)
		assert.Equal(t, 1, calls)
		assert.Equal(t, 62, result.Labels)
	}
	noFonts, err := CompileWithGlyphLoader(data, nil, Options{})
	require.NoError(t, err)
	assert.Len(t, noFonts.MissingFonts, 3)
	assert.Zero(t, noFonts.Labels)
	failure := errors.New("loader failed")
	result, err := CompileWithGlyphLoader(data, func([]string) (map[string][]byte, error) { return nil, failure }, Options{})
	assert.ErrorIs(t, err, failure)
	assert.Nil(t, result)
	result, err = Compile(data, map[string][]byte{"bad": {0xff}}, Options{})
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestPrepareTileBudgetsAndOrdering(t *testing.T) {
	layers, err := style.Parse([]byte(`{"version":8,"layers":[{"id":"background","type":"background","paint":{"background-color":"#ffffff"}},{"id":"first","type":"symbol","source-layer":"points","layout":{"text-field":"A"}},{"id":"second","type":"symbol","source-layer":"points","layout":{"text-field":"B"}}]}`))
	require.NoError(t, err)
	feature := mvt.Feature{GeometryType: mvt.PointType, Points: []geometry.Point{{X: 128, Y: 128}}}
	p, err := prepareTile(map[string][]mvt.Feature{"points": {feature}}, layers, true)
	require.NoError(t, err)
	require.Len(t, p.primitives, 1)
	require.Len(t, p.symbols, 2)
	assert.Equal(t, []string{"A", "B"}, []string{p.symbols[0].Text, p.symbols[1].Text})
	requests := textRequests(p.symbols)
	visits := 0
	for _, request := range requests {
		visits++
		assert.Equal(t, "A", request.Text)
		break
	}
	assert.Equal(t, 1, visits)
	feature.Points = make([]geometry.Point, placement.MaxSymbols/2+1)
	p, err = prepareTile(map[string][]mvt.Feature{"points": {feature}}, layers, false)
	assert.ErrorIs(t, err, mvt.ErrFeatureResourceLimit)
	assert.Nil(t, p, "earlier background and symbols must not publish on overflow")
}

func TestFinishFailureAndNoAtlas(t *testing.T) {
	p := &preparedTile{layers: []style.CompiledLayer{{Order: 0}}, primitives: []compiler.Primitive{{Mesh: compiler.BackgroundGeometry(false), Color: style.Color{Alpha: 255}}}}
	r, err := p.finish(nil, false)
	require.NoError(t, err)
	assert.Empty(t, r.Scene.Textures)
	assert.Zero(t, r.Labels)
	p.symbols = make([]placement.Symbol, placement.MaxSymbols+1)
	r, err = p.finish(nil, false)
	assert.ErrorIs(t, err, geometry.ErrGeometryLimit)
	assert.Nil(t, r)
	p.symbols = nil
	p.primitives[0].Mesh.Vertices[0].X = math.NaN()
	r, err = p.finish(nil, false)
	require.Error(t, err)
	assert.Nil(t, r)
}

func TestCollisionCompatibility(t *testing.T) {
	camera := view.NewCamera(view.TileCoordinate(Tile(), view.ScreenPoint{X: 128, Y: 128}), 10, 0, 512, 512)
	symbol := placement.Symbol{Anchor: geometry.Point{X: 128, Y: 128}, Text: "A", TextSize: 24, TextColor: style.Color{Alpha: 255}, IconName: "airport", IconSize: 1}
	layout := &glyph.PreparedLayout{TextLayout: glyph.TextLayout{Bounds: glyph.TextBounds{Left: -10, Top: -10, Right: 10, Bottom: 10}}}
	// Metric readiness suffices for collision even without any drawable atlas mesh.
	accepted, err := selectSymbols(camera, []placement.Symbol{symbol, symbol}, map[int]*glyph.PreparedLayout{0: layout, 1: layout})
	require.NoError(t, err)
	assert.True(t, accepted[1].Text)
	assert.False(t, accepted[0].Text, "reverse candidate order wins stable ties")
	assert.True(t, accepted[1].Icon)
	transform := view.TileTransform(camera, Tile(), 0)
	for _, tt := range []struct {
		text  string
		ready bool
	}{{"A", false}, {"مرحبا", true}, {"ދިވެހި", false}, {"", false}} {
		symbol.Text = tt.text
		projected := projectSymbol(transform, camera, &symbol, nil)
		assert.Equal(t, tt.ready, projected.Text.Visible, tt.text)
	}
	symbol.Text, symbol.IconName = "A", "missing"
	projected := projectSymbol(transform, camera, &symbol, layout)
	assert.True(t, projected.Text.Visible)
	assert.False(t, projected.Icon.Present)
	symbol.Anchor.X = -10000
	accepted, err = selectSymbols(camera, []placement.Symbol{symbol}, map[int]*glyph.PreparedLayout{0: layout})
	require.NoError(t, err)
	assert.Empty(t, accepted)
	_, err = selectSymbols(camera, make([]placement.Symbol, placement.MaxSymbols+1), nil)
	assert.ErrorIs(t, err, placement.ErrSymbolLimit)
	camera.Width = math.NaN()
	_, err = selectSymbols(camera, nil, nil)
	assert.ErrorIs(t, err, placement.ErrCollisionInput)
}

func BenchmarkCompile(b *testing.B) {
	data, ranges := inputs(b)
	for _, mode := range []string{"expanded", "direct"} {
		b.Run(mode, func(b *testing.B) {
			b.ReportAllocs()
			var mesh scene.Mesh
			for b.Loop() {
				result, err := Compile(data, ranges, Options{DirectIndexed: mode == "direct"})
				if err != nil {
					b.Fatal(err)
				}
				mesh = result.Scene.Meshes[0]
			}
			b.ReportMetric(float64(mesh.BufferBytes()), "geometry-B")
		})
	}
}
