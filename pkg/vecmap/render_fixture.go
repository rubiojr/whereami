package vecmap

import (
	"fmt"
	"iter"

	"github.com/rubiojr/whereami/pkg/vecmap/compiler"
	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
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
	builder := fixtureBuilder{fixture: fixture, indexed: options.DirectIndexed,
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
	indexed bool
	atlas   uint64
}

func (b *fixtureBuilder) primitive(primitive libertyRenderPrimitive) {
	material := scene.Material{Color: compiler.PackedColor(primitive.color)}
	if primitive.patternName != "" {
		sprite, exists := libertySprite(primitive.patternName, mapColor{255, 255, 255, 255}, 1)
		if !exists {
			return
		}
		width, height := float64(sprite.width)/sprite.pixelRatio*primitive.patternScale, float64(sprite.height)/sprite.pixelRatio*primitive.patternScale
		x, y := libertyPatternPhase(pinnedTile, 0, width, height)
		material = scene.Material{Kind: scene.Pattern, Texture: b.packing.Texture("pattern/"+primitive.patternName, sprite.width, sprite.height, sprite.pixels), Color: [4]float32{1, 1, 1, float32(primitive.opacity)}, PatternSize: [2]float32{float32(width), float32(height)}, PatternPhase: [2]float32{float32(x), float32(y)}}
	}
	b.packing.Geometry(geometry.Mesh{Vertices: primitive.triangles, Indices: primitive.indices}, material, [4]float32{0, 0, tileSize, tileSize})
}

func (b *fixtureBuilder) symbols(order int) {
	start, end := libertySymbolRangeAtOrder(b.fixture.bucket.symbols, order)
	for index := start; index < end; index++ {
		candidate := b.fixture.bucket.symbols[index]
		accepted := b.fixture.accepted[libertySymbolKey{tile: pinnedTile, index: index}]
		if accepted.Icon && candidate.iconName != "" {
			sprite, exists := libertySprite(candidate.iconName, candidate.iconColor, candidate.iconOpacity)
			if exists {
				width, height := float64(sprite.width)/sprite.pixelRatio*candidate.iconSize, float64(sprite.height)/sprite.pixelRatio*candidate.iconSize
				x, y := libertyAnchoredOrigin(candidate.iconAnchor, width, height)
				quad := geometry.TextQuad(x, y, x+width, y+height, 0, 0, 1, 1)
				key := fmt.Sprintf("icon/%s/%v/%g", candidate.iconName, candidate.iconColor, candidate.iconOpacity)
				material := scene.Material{Kind: scene.Image, Texture: b.packing.Texture(key, sprite.width, sprite.height, sprite.pixels), Color: [4]float32{1, 1, 1, 1}, MapAligned: !candidate.iconViewportAligned}
				b.packing.IndexedText(candidate.anchor, quad[:], []uint32{0, 1, 2, 0, 2, 3}, roadPoint{X: candidate.iconOffset.X * candidate.iconSize, Y: candidate.iconOffset.Y * candidate.iconSize}, libertyRenderedSymbolAngle(candidate.iconLineAngle, candidate.iconRotate, candidate.iconViewportAligned), material)
			}
		}
	}
	// A layer's halo pass precedes its fill pass. This keeps label geometry
	// batchable without letting a later glyph halo cover an earlier glyph fill.
	for _, kind := range []scene.Kind{scene.SDFHalo, scene.SDFFill} {
		for index := start; index < end; index++ {
			if b.fixture.sdf == nil {
				continue
			}
			candidate := b.fixture.bucket.symbols[index]
			if !b.fixture.accepted[libertySymbolKey{tile: pinnedTile, index: index}].Text {
				continue
			}
			layout := b.fixture.sdf.layouts[libertySDFLayoutKey{tile: pinnedTile, index: index}]
			if layout == nil {
				continue
			}
			color := candidate.textColor
			if kind == scene.SDFHalo {
				if candidate.haloWidth <= 0 || candidate.haloColor.Alpha == 0 {
					continue
				}
				color = candidate.haloColor
			} else {
				b.fixture.Labels++
			}
			offset := roadPoint{X: candidate.textOffset.X * candidate.textSize, Y: candidate.textOffset.Y * candidate.textSize}
			angle := libertyRenderedSymbolAngle(candidate.lineAngle, candidate.textRotate, candidate.viewportAligned)
			material := scene.Material{Kind: kind, Texture: b.atlas, Color: compiler.PackedColor(color), FontScale: float32(layout.Scale), HaloWidth: float32(candidate.haloWidth), HaloBlur: float32(candidate.haloBlur), MapAligned: !candidate.viewportAligned}
			if b.indexed {
				b.packing.IndexedText(candidate.anchor, layout.Vertices, layout.Indices, offset, angle, material)
			} else {
				b.packing.ExpandedText(candidate.anchor, layout.Expanded, offset, angle, material)
			}
		}
	}
}
