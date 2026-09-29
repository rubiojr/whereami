package tiles

import (
	"bytes"
	"encoding/binary"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/compiler"
	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/glyph"
	"github.com/rubiojr/whereami/pkg/vecmap/mvt"
	"github.com/rubiojr/whereami/pkg/vecmap/placement"
	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/sprite"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pbfBytes(field uint64, value []byte) []byte {
	result := binary.AppendUvarint(nil, field<<3|2)
	result = binary.AppendUvarint(result, uint64(len(value)))
	return append(result, value...)
}

func pbfInt(field, value uint64) []byte {
	return binary.AppendUvarint(binary.AppendUvarint(nil, field<<3), value)
}

func featurePBF(kind uint64, commands []uint32) []byte {
	var packed []byte
	for _, value := range commands {
		packed = binary.AppendUvarint(packed, uint64(value))
	}
	return append(pbfInt(3, kind), pbfBytes(4, packed)...)
}

func layerPBF(name string, features ...[]byte) []byte {
	data := pbfBytes(1, []byte(name))
	for _, feature := range features {
		data = append(data, pbfBytes(2, feature)...)
	}
	data = append(data, pbfInt(5, 256)...)
	data = append(data, pbfInt(15, 2)...)
	return pbfBytes(3, data)
}

func preparePBF() []byte {
	land := layerPBF("land", featurePBF(3, []uint32{9, 0, 0, 18, 512, 0, 511, 512, 15}))
	labels := layerPBF("labels", featurePBF(1, []uint32{9, 256, 256}), featurePBF(1, []uint32{9, 320, 256}))
	return append(land, labels...)
}

func prepareStyle(t *testing.T) []style.CompiledLayer {
	t.Helper()
	layers, err := style.Parse([]byte(`{"version":8,"layers":[
		{"id":"background","type":"background","paint":{"background-color":"white"}},
		{"id":"land","type":"fill","source-layer":"land","paint":{"fill-pattern":"dots","fill-opacity":0.5}},
		{"id":"labels","type":"symbol","source-layer":"labels",
		 "layout":{"text-field":"A","text-font":["Test"],"text-size":16,"icon-image":"dot"},
		 "paint":{"text-color":"black","text-halo-color":"white","text-halo-width":1}}
	]}`))
	require.NoError(t, err)
	return layers
}

func prepareAssets() Assets {
	image := sprite.Image{Width: 4, Height: 3, PixelRatio: 1.5, Pixels: bytes.Repeat([]byte{255}, 4*3*4)}
	return Assets{
		Fonts:  map[string]map[uint32]glyph.Glyph{"Test": {'A': {ID: 'A', Width: 2, Height: 2, Advance: 4, Bitmap: bytes.Repeat([]byte{90}, 64)}, ' ': {ID: ' ', Advance: 2}}},
		Sprite: func(string, style.Color, float64) (sprite.Image, bool) { return image, true },
		SpriteEntry: func(string) (sprite.Entry, bool) {
			return sprite.Entry{Width: image.Width, Height: image.Height, PixelRatio: image.PixelRatio}, true
		},
	}
}

func TestPreparedTileAssetRefresh(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		data, layers := preparePBF(), prepareStyle(t)
		p, err := Prepare(data, layers, PrepareOptions{Tile: testTile, Zoom: 3, Indexed: indexed})
		require.NoError(t, err)
		assert.Equal(t, []string{"Test"}, compiler.FontStacks(p.TextRequests()))
		requests := 0
		for range p.TextRequests() {
			requests++
		}
		assert.Equal(t, 2, requests)
		// Source/style objects may be discarded after preparation. Paint values
		// and geometry are already evaluated; asset refresh does not recompile them.
		clear(data)
		layers[0].Paint["background-color"] = "black"
		layers[2].Layout["text-field"] = "B"
		missing, err := p.Build(Assets{})
		require.NoError(t, err)
		assert.Equal(t, []string{"Test"}, missing.MissingFonts)
		require.Len(t, missing.Fragment.Scene.Draws, 1)
		assert.Equal(t, [4]float32{1, 1, 1, 1}, missing.Fragment.Scene.Draws[0].Material.Color)
		assets := prepareAssets()
		built, err := p.Build(assets)
		require.NoError(t, err)
		assert.Empty(t, built.MissingFonts)
		require.Len(t, built.Fragment.Symbols, 2)
		assert.True(t, built.Fragment.Symbols[0].TextReady)
		assert.True(t, built.Fragment.Symbols[0].HasTextBounds)
		assert.True(t, built.Fragment.Symbols[0].HasSprite)
		require.Len(t, built.Fragment.Draws, 8)
		assert.Equal(t, [2]float64{4 / 1.5, 3 / 1.5}, built.Fragment.Draws[1].PatternPeriod)
		assert.Equal(t, []Part{Base, Base, Icon, Icon, Text, Text, Text, Text}, func() []Part {
			var parts []Part
			for _, source := range built.Fragment.Draws {
				parts = append(parts, source.Part)
			}
			return parts
		}())
		for i := 2; i < 8; i++ {
			assert.Equal(t, i%2, built.Fragment.Draws[i].Candidate)
		}
		set := newSet(t, retained.Limits{})
		require.NoError(t, set.Apply([]Change{{testTile, built.Fragment}}))
		selected := selectTiles(t, set, []view.TileID{testTile}, nil, testCamera(testTile))
		assert.Len(t, selected.Scene.Draws, 8)
		// Font maps may merge non-Latin ranges. No range-0 assumption is built in.
		unicodeLayers := prepareStyle(t)
		unicodeLayers[2].Layout["text-field"] = "中"
		unicode, err := Prepare(preparePBF(), unicodeLayers, PrepareOptions{Tile: testTile, Zoom: 3, Indexed: indexed})
		require.NoError(t, err)
		chinese := assets.Fonts["Test"]['A']
		chinese.ID = '中'
		assets.Fonts["Test"]['中'] = chinese
		unicodeResult, err := unicode.Build(assets)
		require.NoError(t, err)
		assert.Empty(t, unicodeResult.MissingFonts)
		assert.True(t, unicodeResult.Fragment.Symbols[0].TextReady)
		assert.Equal(t, "A", built.Fragment.Symbols[0].Candidate.Text)
		require.Len(t, missing.Fragment.Scene.Draws, 1, "earlier builds remain immutable")
	}
}

func TestPreparedFallbackAndPartialAtlas(t *testing.T) {
	layers := prepareStyle(t)
	layers[2].Layout["text-field"] = "مرحبا"
	p, err := Prepare(preparePBF(), layers, PrepareOptions{Tile: testTile, Zoom: 3})
	require.NoError(t, err)
	assets := prepareAssets()
	without, err := p.Build(assets)
	require.NoError(t, err)
	assert.False(t, without.Fragment.Symbols[0].TextReady)
	assets.FallbackEligible = glyph.LegacyFallbackEligible
	with, err := p.Build(assets)
	require.NoError(t, err)
	assert.True(t, with.Fragment.Symbols[0].TextReady)
	assert.False(t, with.Fragment.Symbols[0].HasTextBounds)
	assert.Equal(t, []string{"Test"}, with.MissingFonts)
	for _, draw := range with.Fragment.Draws {
		assert.NotEqual(t, Text, draw.Part)
	}
	// Fifty maximum-size glyphs exceed the 2048 atlas. Whole-label coverage
	// suppresses meshes, while metric-ready bounds still reserve collision space.
	bitmap := bytes.Repeat([]byte{90}, (255+6)*(255+6))
	var text strings.Builder
	for i := range 50 {
		code := uint32(0x4e00 + i*256)
		text.WriteRune(rune(code))
		assets.Fonts["Test"][code] = glyph.Glyph{ID: code, Width: 255, Height: 255, Advance: 255, Bitmap: bitmap}
	}
	layers[2].Layout["text-field"] = text.String()
	p, err = Prepare(preparePBF(), layers, PrepareOptions{Tile: testTile, Zoom: 3, Indexed: true})
	require.NoError(t, err)
	partial, err := p.Build(assets)
	require.NoError(t, err)
	assert.Empty(t, partial.MissingFonts, "glyphs exist; atlas coverage is a separate condition")
	assert.True(t, partial.Fragment.Symbols[0].TextReady)
	assert.True(t, partial.Fragment.Symbols[0].HasTextBounds)
	for _, draw := range partial.Fragment.Draws {
		assert.NotEqual(t, Text, draw.Part)
	}
}

func TestPrepareFailureAndLimits(t *testing.T) {
	var zero Prepared
	_, err := zero.Build(Assets{})
	assert.ErrorIs(t, err, ErrInput)
	_, err = (*Prepared)(nil).Build(Assets{})
	assert.ErrorIs(t, err, ErrInput)
	base := PrepareOptions{Tile: testTile, Zoom: 3}
	for _, mutate := range []func(*PrepareOptions){
		func(o *PrepareOptions) { o.Tile.Z = 15 }, func(o *PrepareOptions) { o.Zoom = math.NaN() },
		func(o *PrepareOptions) { o.Zoom = math.Inf(1) }, func(o *PrepareOptions) { o.Zoom = -1 },
		func(o *PrepareOptions) { o.Coarser = -1 }, func(o *PrepareOptions) { o.Coarser = view.MaxCoarser + 1 },
		func(o *PrepareOptions) { o.TriangleLimit = -1 }, func(o *PrepareOptions) { o.CandidateLimit = placement.MaxSymbols + 1 },
		func(o *PrepareOptions) { o.ElementLimit = compiler.MaxSceneElements + 1 }, func(o *PrepareOptions) { o.DrawLimit = compiler.MaxFragmentDraws + 1 },
	} {
		o := base
		mutate(&o)
		p, err := Prepare(nil, nil, o)
		assert.Error(t, err)
		assert.Nil(t, p)
	}
	for _, layers := range [][]style.CompiledLayer{make([]style.CompiledLayer, MaxStyleLayers+1), {{Order: -1}}, {{Order: 1}, {Order: 1}}} {
		p, err := Prepare(nil, layers, base)
		assert.Error(t, err)
		assert.Nil(t, p)
	}
	p, err := Prepare([]byte{0}, nil, base)
	assert.Error(t, err)
	assert.Nil(t, p)
	p, err = Prepare(make([]byte, mvt.MaxTileBytes+1), nil, base)
	assert.ErrorIs(t, err, mvt.ErrTileResourceLimit)
	assert.Nil(t, p)
	for _, o := range []PrepareOptions{{Tile: testTile, Zoom: 3, TriangleLimit: 1}, {Tile: testTile, Zoom: 3, CandidateLimit: 1}} {
		p, err := Prepare(preparePBF(), prepareStyle(t), o)
		assert.ErrorIs(t, err, mvt.ErrFeatureResourceLimit)
		assert.Nil(t, p)
	}
	for _, o := range []PrepareOptions{{Tile: testTile, Zoom: 3, ElementLimit: 1}, {Tile: testTile, Zoom: 3, DrawLimit: 1}} {
		p, err := Prepare(preparePBF(), prepareStyle(t), o)
		require.NoError(t, err)
		result, err := p.Build(prepareAssets())
		assert.ErrorIs(t, err, geometry.ErrGeometryLimit)
		assert.Nil(t, result)
	}
}

func TestPreparedEmptyAndDegradation(t *testing.T) {
	p, err := Prepare(layerPBF("empty"), nil, PrepareOptions{Tile: testTile, Zoom: 3})
	require.NoError(t, err)
	blank, err := p.Build(Assets{})
	require.NoError(t, err)
	assert.Empty(t, blank.Fragment.Scene.Meshes)
	assert.Empty(t, blank.Fragment.Draws)
	s := newSet(t, retained.Limits{})
	require.NoError(t, s.Apply([]Change{{testTile, blank.Fragment}}))
	assert.Equal(t, []view.TileID{testTile}, selectTiles(t, s, []view.TileID{testTile}, nil, testCamera(testTile)).Cover)
	commands := make([]uint32, 4+2*mvt.MaxPolygonPoints)
	commands[0], commands[3] = 9, uint32(mvt.MaxPolygonPoints)<<3|2
	commands = append(commands, 15)
	p, err = Prepare(layerPBF("land", featurePBF(3, commands)), prepareStyle(t), PrepareOptions{Tile: testTile, Zoom: 3})
	require.NoError(t, err)
	limits := p.Limits()
	require.Len(t, limits, 1)
	assert.Equal(t, 1, limits[0].Skipped)
	assert.Greater(t, p.RetainedBytes(), uint64(len(limits[0].Name)+len(limits[0].Last.Error())), "degradation diagnostics are charged with prepared storage")
	limits[0].Name = "changed"
	built, err := p.Build(Assets{})
	require.NoError(t, err)
	assert.Equal(t, "land", built.Limits[0].Name)
}

func TestPreparedBuildFailureAndConcurrency(t *testing.T) {
	oversized := prepareStyle(t)
	oversized[2].Layout["text-size"] = 1e100
	huge, err := Prepare(preparePBF(), oversized, PrepareOptions{Tile: testTile, Zoom: 3})
	require.NoError(t, err)
	result, err := huge.Build(prepareAssets())
	assert.ErrorIs(t, err, glyph.ErrLayoutGeometry, "finite style values can still overflow packed float32 geometry")
	assert.Nil(t, result)
	p, err := Prepare(preparePBF(), prepareStyle(t), PrepareOptions{Tile: testTile, Zoom: 3, Indexed: true})
	require.NoError(t, err)
	bad := prepareAssets()
	g := bad.Fonts["Test"]['A']
	g.Bitmap = []byte{90}
	bad.Fonts["Test"]['A'] = g
	result, err = p.Build(bad)
	assert.Error(t, err)
	assert.Nil(t, result)
	bad = prepareAssets()
	bad.Sprite = func(string, style.Color, float64) (sprite.Image, bool) { return sprite.Image{PixelRatio: -1}, true }
	result, err = p.Build(bad)
	assert.ErrorIs(t, err, compiler.ErrPackingInput)
	assert.Nil(t, result)
	assets := prepareAssets()
	baseline, err := p.Build(assets)
	require.NoError(t, err)
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			built, err := p.Build(assets)
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, baseline, built)
		})
	}
	workers.Wait()
}

func FuzzPreparedTile(f *testing.F) {
	f.Add(preparePBF(), true)
	f.Add(layerPBF("empty"), false)
	f.Add([]byte{0}, false)
	f.Fuzz(func(t *testing.T, data []byte, indexed bool) {
		if len(data) > 32<<10 {
			return
		}
		p, err := Prepare(data, prepareStyle(t), PrepareOptions{Tile: testTile, Zoom: 3, Indexed: indexed,
			TriangleLimit: 1024, CandidateLimit: 16, ElementLimit: 32768, DrawLimit: 256})
		if err != nil {
			require.Nil(t, p)
			return
		}
		built, err := p.Build(prepareAssets())
		if err != nil {
			require.Nil(t, built)
			return
		}
		require.NoError(t, built.Fragment.Scene.Validate())
		assert.Len(t, built.Fragment.Draws, len(built.Fragment.Scene.Draws))
		assert.LessOrEqual(t, len(built.Fragment.Symbols), 16)
		set := newSet(t, retained.Limits{})
		require.NoError(t, set.Apply([]Change{{testTile, built.Fragment}}))
		selected := selectTiles(t, set, []view.TileID{testTile}, nil, testCamera(testTile))
		assert.LessOrEqual(t, len(selected.Scene.Draws), len(built.Fragment.Draws))
	})
}

func TestPrepareCoarserConvertsPixelsAtTheDrawnZoom(t *testing.T) {
	data := append(layerPBF("land", featurePBF(3, []uint32{9, 0, 0, 18, 512, 0, 511, 512, 15})),
		layerPBF("roads", featurePBF(2, []uint32{9, 0, 256, 10, 400, 0}))...)
	layers, err := style.Parse([]byte(`{"version":8,"layers":[
		{"id":"land","type":"fill","source-layer":"land","paint":{"fill-pattern":"dots"}},
		{"id":"roads","type":"line","source-layer":"roads","paint":{"line-width":4,"line-offset":2}},
		{"id":"names","type":"symbol","source-layer":"roads",
		 "layout":{"text-field":"A","text-font":["Test"],"symbol-placement":"line","symbol-spacing":40}}
	]}`))
	require.NoError(t, err)
	prepare := func(zoom float64, coarser int) *Prepared {
		p, err := Prepare(data, layers, PrepareOptions{Tile: testTile, Zoom: zoom, Coarser: coarser, Indexed: true})
		require.NoError(t, err)
		return p
	}
	zoom := float64(testTile.Z)
	coarse, drawn, fine := prepare(zoom, 1), prepare(zoom+1, 0), prepare(zoom, 0)
	require.NotEmpty(t, coarse.primitives)
	require.NotEmpty(t, coarse.symbols)
	// This style does not depend on zoom, so only the drawn zoom shapes the output.
	assert.Equal(t, drawn.primitives, coarse.primitives)
	assert.Equal(t, drawn.symbols, coarse.symbols)
	assert.NotEqual(t, fine.primitives, coarse.primitives)
	assert.NotEqual(t, len(fine.symbols), len(coarse.symbols))
	built, err := coarse.Build(prepareAssets())
	require.NoError(t, err)
	require.NoError(t, built.Fragment.Scene.Validate())
}

func TestBuiltFragmentsHoldNoUnusedCapacity(t *testing.T) {
	data := append(preparePBF(), layerPBF("roads", featurePBF(2, []uint32{9, 0, 256, 10, 400, 0}), featurePBF(2, []uint32{9, 0, 300, 10, 400, 40}))...)
	layers, err := style.Parse([]byte(`{"version":8,"layers":[
		{"id":"background","type":"background","paint":{"background-color":"white"}},
		{"id":"land","type":"fill","source-layer":"land","paint":{"fill-color":"green"}},
		{"id":"roads","type":"line","source-layer":"roads","paint":{"line-width":4}},
		{"id":"dashed","type":"line","source-layer":"roads","paint":{"line-width":2,"line-dasharray":[2,1]}},
		{"id":"labels","type":"symbol","source-layer":"labels",
		 "layout":{"text-field":"A","text-font":["Test"],"text-size":16,"icon-image":"dot"},
		 "paint":{"text-color":"black","text-halo-color":"white","text-halo-width":1}}
	]}`))
	require.NoError(t, err)
	for _, indexed := range []bool{false, true} {
		for _, resident := range []bool{false, true} {
			p, err := Prepare(data, layers, PrepareOptions{Tile: testTile, Zoom: 3, Indexed: indexed,
				ResidentGeometry: resident, ResidentSymbols: resident, ResidentDashes: resident})
			require.NoError(t, err)
			built, err := p.BuildOwned(prepareAssets(), 1<<20)
			require.NoError(t, err)
			require.NotEmpty(t, built.Fragment.Scene.Meshes)
			var exact uint64
			for _, mesh := range built.Fragment.Scene.Meshes {
				assert.Equal(t, len(mesh.Vertices), cap(mesh.Vertices), "indexed=%t resident=%t mesh=%d", indexed, resident, mesh.ID)
				assert.Equal(t, len(mesh.Indices), cap(mesh.Indices), "indexed=%t resident=%t mesh=%d", indexed, resident, mesh.ID)
				exact += mesh.BufferBytes()
			}
			assert.GreaterOrEqual(t, built.Fragment.RetainedBytes(), exact)
			require.NoError(t, built.Fragment.Scene.Validate())
		}
	}
}
