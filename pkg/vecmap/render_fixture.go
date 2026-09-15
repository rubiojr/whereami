package vecmap

import "github.com/rubiojr/whereami/pkg/vecmap/fixture"

// RenderFixture is the shared offline, fixed-style GPU comparison scene.
type RenderFixture = fixture.Result

type RenderFixtureOptions = fixture.Options
type FixtureGlyphLoader = fixture.GlyphLoader

// CompileRenderFixture compiles the pinned tile with the default expanded topology.
func CompileRenderFixture(data []byte, ranges map[string][]byte) (*RenderFixture, error) {
	return fixture.Compile(data, ranges, RenderFixtureOptions{})
}

func CompileRenderFixtureWithOptions(data []byte, ranges map[string][]byte, options RenderFixtureOptions) (*RenderFixture, error) {
	return fixture.Compile(data, ranges, options)
}

func CompileRenderFixtureWithGlyphLoader(data []byte, load FixtureGlyphLoader, options RenderFixtureOptions) (*RenderFixture, error) {
	return fixture.CompileWithGlyphLoader(data, load, options)
}
