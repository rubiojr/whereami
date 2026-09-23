package main

import (
	"crypto/sha256"
	"fmt"

	"github.com/rubiojr/whereami/pkg/vecmap/compiler"
	"github.com/rubiojr/whereami/pkg/vecmap/fixture"
	"github.com/rubiojr/whereami/pkg/vecmap/glyph"
	"github.com/rubiojr/whereami/pkg/vecmap/liberty"
	"github.com/rubiojr/whereami/pkg/vecmap/mvt"
	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/tiles"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
)

// Keep the command's pinned-fixture contract while exercising the generic
// compiler. tiles.Prepare itself accepts arbitrary bounded MVT input.
func compileRetainedFixture(data []byte, load fixture.GlyphLoader, options fixture.Options) (*fixture.Result, error) {
	if len(data) > mvt.MaxTileBytes {
		return nil, mvt.ErrTileResourceLimit
	}
	if actual := fmt.Sprintf("%x", sha256.Sum256(data)); actual != fixture.TileSHA256 {
		return nil, fmt.Errorf("tile checksum mismatch: got %s", actual)
	}
	layers, err := liberty.Layers()
	if err != nil {
		return nil, err
	}
	p, err := tiles.Prepare(data, layers, tiles.PrepareOptions{Tile: fixture.Tile(), Zoom: 10, Indexed: options.DirectIndexed})
	if err != nil {
		return nil, err
	}
	var ranges map[string][]byte
	if load != nil {
		ranges, err = load(compiler.FontStacks(p.TextRequests()))
		if err != nil {
			return nil, fmt.Errorf("load fixture glyphs: %w", err)
		}
	}
	fonts, err := compiler.DecodeFontRanges(ranges, 0)
	if err != nil {
		return nil, err
	}
	built, err := p.Build(tiles.Assets{Fonts: fonts, Sprite: liberty.Sprite, SpriteEntry: liberty.SpriteEntry, FallbackEligible: glyph.LegacyFallbackEligible})
	if err != nil {
		return nil, err
	}
	set, err := tiles.New(retained.Limits{})
	if err != nil {
		return nil, err
	}
	if err := set.Apply([]tiles.Change{{Tile: fixture.Tile(), Fragment: built.Fragment}}); err != nil {
		return nil, err
	}
	camera := view.NewCamera(view.TileCoordinate(fixture.Tile(), view.ScreenPoint{X: 128, Y: 128}), 10, 0, 512, 512)
	snapshot, err := set.Select([]view.TileID{fixture.Tile()}, nil, camera, 0)
	if err != nil {
		return nil, err
	}
	labels := 0
	for _, draw := range snapshot.Scene.Draws {
		if draw.Material.Kind == scene.SDFFill {
			labels++
		}
	}
	return &fixture.Result{Scene: snapshot.Scene, Camera: camera, Labels: labels, MissingFonts: built.MissingFonts, Limits: built.Limits}, nil
}
