package vecmap

import (
	"errors"
	"maps"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFixtureFontStacks(t *testing.T) {
	fonts := fixtureFontStacks([]libertySymbolCandidate{
		{text: "hello", fontStack: "Zulu"},
		{text: "world", fontStack: "Alpha"},
		{text: "again", fontStack: "Zulu"},
		{fontStack: "icon-only"},
		// The old no-glyph pass also reported fonts for unsupported text.
		{text: "العربية", fontStack: "Arabic"},
	})
	assert.Equal(t, []string{"Alpha", "Arabic", "Zulu"}, fonts)
	assert.Empty(t, fixtureFontStacks(nil))
}

func TestFixtureLayoutsPreserveCandidateIndexesAndLimit(t *testing.T) {
	bucket := &tileBucket{symbols: []libertySymbolCandidate{
		{iconName: "airport"}, {text: "A", fontStack: "font", textSize: 24}, {text: "B", fontStack: "font", textSize: 24},
	}}
	fonts := map[string]map[uint32]sdfGlyph{"font": {'A': testSDFGlyph('A', 2, 2, 0, -2, 4, 90)}}
	layouts, missing, err := fixtureLayouts(bucket, fonts)
	require.NoError(t, err)
	assert.Len(t, layouts, 1)
	assert.Contains(t, layouts, libertySDFLayoutKey{tile: pinnedTile, index: 1})
	assert.Equal(t, []string{"font"}, missing)
	bucket.symbols = make([]libertySymbolCandidate, maximumSDFSceneLayouts+1)
	layouts, missing, err = fixtureLayouts(bucket, fonts)
	assert.ErrorIs(t, err, geometry.ErrGeometryLimit)
	assert.Nil(t, layouts)
	assert.Nil(t, missing)
}

func TestFixtureGlyphLoaderInvalidTile(t *testing.T) {
	called := false
	fixture, err := CompileRenderFixtureWithGlyphLoader([]byte("invalid tile"), func([]string) (map[string][]byte, error) {
		called = true
		return nil, nil
	}, RenderFixtureOptions{})
	require.Error(t, err)
	assert.Nil(t, fixture)
	assert.False(t, called, "invalid input must fail before invoking external loading")
}

func TestFixtureGlyphLoader(t *testing.T) {
	data, glyphs := fixtureInputs(t)
	probe, err := CompileRenderFixture(data, nil)
	require.NoError(t, err)
	require.NotEmpty(t, probe.MissingFonts)
	partial := maps.Clone(glyphs)
	delete(partial, probe.MissingFonts[0])
	for _, indexed := range []bool{false, true} {
		options := RenderFixtureOptions{DirectIndexed: indexed}
		for _, ranges := range []map[string][]byte{glyphs, partial, nil} {
			want, err := CompileRenderFixtureWithOptions(data, ranges, options)
			require.NoError(t, err)
			calls := 0
			got, err := CompileRenderFixtureWithGlyphLoader(data, func(fonts []string) (map[string][]byte, error) {
				calls++
				assert.Equal(t, probe.MissingFonts, fonts)
				return ranges, nil
			}, options)
			require.NoError(t, err)
			assert.Equal(t, 1, calls)
			assert.Equal(t, want.Scene, got.Scene)
			assert.Equal(t, want.Camera, got.Camera)
			assert.Equal(t, want.Labels, got.Labels)
			assert.Equal(t, want.MissingFonts, got.MissingFonts)
			legacy, err := legacyCompileRenderFixtureWithOptions(data, ranges, options)
			require.NoError(t, err)
			assert.Equal(t, legacy.Scene, got.Scene)
			assert.Equal(t, legacy.Labels, got.Labels)
			assert.Equal(t, legacy.MissingFonts, got.MissingFonts)
		}
	}
	none, err := CompileRenderFixtureWithGlyphLoader(data, nil, RenderFixtureOptions{})
	require.NoError(t, err)
	assert.Equal(t, probe.Scene, none.Scene)
	assert.Equal(t, probe.MissingFonts, none.MissingFonts)

	failure := errors.New("glyph source unavailable")
	failed, err := CompileRenderFixtureWithGlyphLoader(data, func([]string) (map[string][]byte, error) {
		return nil, failure
	}, RenderFixtureOptions{})
	assert.ErrorIs(t, err, failure)
	assert.Nil(t, failed)
	malformed, err := CompileRenderFixtureWithGlyphLoader(data, func(fonts []string) (map[string][]byte, error) {
		return map[string][]byte{fonts[0]: {0xff}}, nil
	}, RenderFixtureOptions{})
	assert.Error(t, err)
	assert.Nil(t, malformed)
}

// Reproduce the old command's throwaway no-glyph scene, then its real scene.
// Keep this outside b.Loop so benchmark keepalive rules don't prolong the probe's
// lifetime relative to the original command. File I/O and JSON are excluded.
func fixtureWithDiscovery(data []byte, load FixtureGlyphLoader, options RenderFixtureOptions, twoPass bool) (*RenderFixture, error) {
	if !twoPass {
		return CompileRenderFixtureWithGlyphLoader(data, load, options)
	}
	probe, err := CompileRenderFixtureWithOptions(data, nil, options)
	if err != nil {
		return nil, err
	}
	glyphs, err := load(probe.MissingFonts)
	if err != nil {
		return nil, err
	}
	return CompileRenderFixtureWithOptions(data, glyphs, options)
}

func BenchmarkFixtureGlyphDiscovery(b *testing.B) {
	data, glyphs := fixtureInputs(b)
	load := func(fonts []string) (map[string][]byte, error) {
		selected := make(map[string][]byte, len(fonts))
		for _, font := range fonts {
			selected[font] = glyphs[font]
		}
		return selected, nil
	}
	for _, mode := range []string{"expanded", "direct"} {
		for _, strategy := range []string{"two-pass", "loader"} {
			b.Run(mode+"/"+strategy, func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					_, err := fixtureWithDiscovery(data, load, RenderFixtureOptions{DirectIndexed: mode == "direct"}, strategy == "two-pass")
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
