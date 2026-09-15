package vecmap

import (
	"fmt"
	"iter"

	"github.com/rubiojr/whereami/pkg/vecmap/compiler"
	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/placement"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/sprite"
)

// RenderFixture is an offline, fixed-style scene for comparing GPU backends.
// It reuses the existing decoder, Earcut, style compiler, glyph layout and
// collision code. It is not a replacement for the live tile scheduler.
type RenderFixture struct {
	Scene        *scene.Scene
	Camera       Camera
	Labels       int
	MissingFonts []string
	bucket       *tileBucket
	sdf          *sdfScene
	accepted     map[libertySymbolKey]libertyAcceptedSymbol
}

// CompileRenderFixture compiles the pinned z9 tile at zoom 10 with a fixed
// 512x512 placement viewport. Glyph ranges are keyed by exact style font stack;
// missing fonts are reported rather than substituted or fetched in a benchmark.
func CompileRenderFixture(tileData []byte, glyphRanges map[string][]byte) (*RenderFixture, error) {
	return CompileRenderFixtureWithOptions(tileData, glyphRanges, RenderFixtureOptions{})
}

// RenderFixtureOptions selects preparation before scene publication.
type RenderFixtureOptions struct {
	// DirectIndexed preserves topology during construction, without IndexMesh.
	DirectIndexed bool
}

// CompileRenderFixtureWithOptions selects how the same fixed-style scene is
// prepared. All geometry is finalized before publishing resource IDs/revisions.
func CompileRenderFixtureWithOptions(tileData []byte, glyphRanges map[string][]byte, options RenderFixtureOptions) (*RenderFixture, error) {
	return compileRenderFixture(tileData, glyphRanges, options, nil)
}

// FixtureGlyphLoader supplies range 0-255 PBF data keyed by exact font stack.
// Font requests are sorted and unique. Missing entries are reported in the
// resulting fixture; an error aborts compilation before scene publication.
type FixtureGlyphLoader func(fontStacks []string) (map[string][]byte, error)

// CompileRenderFixtureWithGlyphLoader calls load once after successful tile/style
// compilation, before glyph layout, collision and scene packing. The callback is
// synchronous on the caller's goroutine: use a preparation worker, not a render
// callback. A nil loader is equivalent to providing no glyph ranges. File/network
// access and caching belong to the caller; this function performs neither.
func CompileRenderFixtureWithGlyphLoader(tileData []byte, load FixtureGlyphLoader, options RenderFixtureOptions) (*RenderFixture, error) {
	return compileRenderFixture(tileData, nil, options, load)
}

func compileRenderFixture(tileData []byte, glyphRanges map[string][]byte, options RenderFixtureOptions, load FixtureGlyphLoader) (*RenderFixture, error) {
	bucket, err := prepareFixtureBucket(tileData, options.DirectIndexed)
	if err != nil {
		return nil, err
	}
	if load != nil {
		glyphRanges, err = load(fixtureFontStacks(bucket.symbols))
		if err != nil {
			return nil, fmt.Errorf("load fixture glyphs: %w", err)
		}
	}
	center := tileLocalCoordinate(pinnedTile, roadPoint{X: 128, Y: 128})
	fixture := &RenderFixture{Camera: NewCamera(center, 10, 0, 512, 512), bucket: bucket}
	glyphs, err := decodeFixtureGlyphs(glyphRanges)
	if err != nil {
		return nil, err
	}
	layouts, missing, err := fixtureLayouts(bucket, glyphs)
	if err != nil {
		return nil, err
	}
	fixture.MissingFonts = missing
	neededGlyphs := sdfLayoutGlyphs(layouts)
	atlas, _ := buildRetainedSDFAtlas(neededGlyphs, nil)
	fixture.sdf, err = buildSDFSceneGeometry(layouts, atlas, options.DirectIndexed)
	if err != nil {
		return nil, err
	}
	fixture.accepted = acceptedLibertySymbols(fixture.Camera, []loadedRoadTile{{id: pinnedTile, roads: bucket}}, layouts)
	builder := fixtureBuilder{fixture: fixture,
		packing: compiler.NewSceneBuilder(options.DirectIndexed, 0)}
	if fixture.sdf != nil {
		builder.atlas = builder.packing.GlyphAtlas(atlas)
	}
	layers, err := compiledLibertyLayers()
	if err != nil {
		return nil, err
	}
	for _, layer := range layers {
		for _, primitive := range libertyPrimitivesAtOrder(bucket.liberty, layer.Order) {
			builder.primitive(primitive)
		}
		if layer.Kind == "symbol" {
			builder.symbols(layer.Order)
		}
	}
	fixture.Scene, err = builder.packing.Finish()
	if err != nil {
		return nil, err
	}
	return fixture, nil
}

func prepareFixtureBucket(tileData []byte, indexed bool) (*tileBucket, error) {
	if err := verifyTileChecksum(tileData, pinnedTileSHA256); err != nil {
		return nil, err
	}
	bucket, err := decodeStyledBucketGeometry(tileData, pinnedTile, indexed)
	if err != nil {
		return nil, err
	}
	if err := compileLibertyTileGeometry(bucket, 10, indexed); err != nil {
		return nil, err
	}
	return bucket, nil
}

func fixtureFontStacks(candidates []libertySymbolCandidate) []string {
	return compiler.FontStacks(fixtureTextRequests(candidates))
}

func decodeFixtureGlyphs(ranges map[string][]byte) (map[string]map[uint32]sdfGlyph, error) {
	return compiler.DecodeFontRanges(ranges, 0)
}

func fixtureLayouts(bucket *tileBucket, glyphs map[string]map[uint32]sdfGlyph) (map[libertySDFLayoutKey]*sdfTextLayout, []string, error) {
	return compiler.PrepareTextLayouts(fixtureTextRequests(bucket.symbols), glyphs)
}

func fixtureTextRequests(candidates []libertySymbolCandidate) iter.Seq2[libertySDFLayoutKey, compiler.TextRequest] {
	return func(yield func(libertySDFLayoutKey, compiler.TextRequest) bool) {
		for index, candidate := range candidates {
			if !yield(libertySDFLayoutKey{tile: pinnedTile, index: index}, libertyTextRequest(candidate)) {
				return
			}
		}
	}
}

// Frame creates camera data without changing the scene or its resource IDs.
// Style widths/visibility and collision decisions stay fixed in this fixture.
func (f *RenderFixture) Frame(camera Camera) scene.Frame {
	t := roadCameraTransform(camera, pinnedTile)
	return scene.Frame{Scene: f.Scene, DevicePixelRatio: 1, Transforms: []scene.Affine{{M11: float32(t.M11), M12: float32(t.M12), DX: float32(t.DX), M21: float32(t.M21), M22: float32(t.M22), DY: float32(t.DY)}}}
}

type fixtureBuilder struct {
	fixture *RenderFixture
	packing *compiler.SceneBuilder
	atlas   uint64
}

func (b *fixtureBuilder) primitive(primitive libertyRenderPrimitive) {
	b.packing.Primitive(pinnedTile, 0, compiler.Primitive{
		Order: primitive.order, LayerID: primitive.layerID,
		Mesh:  geometry.Mesh{Vertices: primitive.triangles, Indices: primitive.indices},
		Color: primitive.color, PatternName: primitive.patternName, PatternScale: primitive.patternScale, Opacity: primitive.opacity,
	}, fixtureSprite)
}

func (b *fixtureBuilder) symbols(order int) {
	start, end := libertySymbolRangeAtOrder(b.fixture.bucket.symbols, order)
	symbolAt := func(offset int) compiler.RenderSymbol {
		index := start + offset
		var layout *sdfTextLayout
		if b.fixture.sdf != nil {
			layout = b.fixture.sdf.layouts[libertySDFLayoutKey{tile: pinnedTile, index: index}]
		}
		return compiler.RenderSymbol{Symbol: fixturePaintSymbol(&b.fixture.bucket.symbols[index]),
			Accepted: b.fixture.accepted[libertySymbolKey{tile: pinnedTile, index: index}], Layout: layout}
	}
	b.fixture.Labels += b.packing.SymbolLayer(end-start, symbolAt, b.atlas, fixtureSprite)
}

func fixtureSprite(name string, color mapColor, opacity float64) (sprite.Image, bool) {
	image, ok := libertySprite(name, color, opacity)
	return sprite.Image{Pixels: image.pixels, Width: image.width, Height: image.height, PixelRatio: image.pixelRatio}, ok
}

func fixturePaintSymbol(c *libertySymbolCandidate) placement.Symbol {
	return placement.Symbol{Anchor: c.anchor, LineAngle: c.lineAngle, IconLineAngle: c.iconLineAngle,
		ViewportAligned: c.viewportAligned, IconViewportAligned: c.iconViewportAligned,
		TextSize: c.textSize, TextColor: c.textColor, HaloColor: c.haloColor, HaloWidth: c.haloWidth, HaloBlur: c.haloBlur,
		TextOffset: c.textOffset, TextRotate: c.textRotate,
		IconName: c.iconName, IconSize: c.iconSize, IconColor: c.iconColor, IconOpacity: c.iconOpacity,
		IconAnchor: c.iconAnchor, IconOffset: c.iconOffset, IconRotate: c.iconRotate}
}
