package vecmap

import (
	"fmt"
	"math"
	"slices"
	"unicode/utf8"

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
	if err := verifyTileChecksum(tileData, pinnedTileSHA256); err != nil {
		return nil, err
	}
	bucket, err := decodeRoadBucket(tileData, pinnedTile)
	if err != nil {
		return nil, err
	}
	if err := compileLibertyTile(bucket, 10); err != nil {
		return nil, err
	}
	center := tileLocalCoordinate(pinnedTile, roadPoint{128, 128})
	fixture := &RenderFixture{Scene: &scene.Scene{}, Camera: NewCamera(center, 10, 0, 512, 512), bucket: bucket}
	glyphs, err := decodeFixtureGlyphs(glyphRanges)
	if err != nil {
		return nil, err
	}
	layouts, missing := fixtureLayouts(bucket, glyphs)
	fixture.MissingFonts = missing
	neededGlyphs := sdfLayoutGlyphs(layouts)
	atlas, _ := buildRetainedSDFAtlas(neededGlyphs, nil)
	fixture.sdf = buildSDFScene(layouts, atlas)
	fixture.accepted = acceptedLibertySymbols(fixture.Camera, []loadedRoadTile{{id: pinnedTile, roads: bucket}}, layouts)
	builder := fixtureBuilder{fixture: fixture, textures: make(map[string]uint64)}
	if fixture.sdf != nil {
		pixels := make([]byte, len(atlas.pixels)*4)
		for index, value := range atlas.pixels {
			pixels[index*4], pixels[index*4+1], pixels[index*4+2], pixels[index*4+3] = value, value, value, 255
		}
		builder.atlas = builder.texture("glyph-atlas", atlas.width, atlas.height, pixels)
	}
	layers, err := compiledLibertyLayers()
	if err != nil {
		return nil, err
	}
	for _, layer := range layers {
		for _, primitive := range libertyPrimitivesAtOrder(bucket.liberty, layer.order) {
			builder.primitive(primitive)
		}
		if layer.kind == "symbol" {
			builder.symbols(layer.order)
		}
	}
	fixture.Scene.Meshes = []scene.Mesh{{ID: 1, Revision: 1, Vertices: builder.vertices}}
	if err := fixture.Scene.Validate(); err != nil {
		return nil, err
	}
	return fixture, nil
}

func decodeFixtureGlyphs(ranges map[string][]byte) (map[string]map[uint32]sdfGlyph, error) {
	glyphs := make(map[string]map[uint32]sdfGlyph)
	for font, data := range ranges {
		rangeData, err := decodeSDFGlyphRange(data, glyphRangeKey{fontStack: font})
		if err != nil {
			return nil, fmt.Errorf("font %q: %w", font, err)
		}
		glyphs[font] = rangeData.glyphs
	}
	return glyphs, nil
}

func fixtureLayouts(bucket *tileBucket, glyphs map[string]map[uint32]sdfGlyph) (map[libertySDFLayoutKey]*sdfTextLayout, []string) {
	layouts := make(map[libertySDFLayoutKey]*sdfTextLayout)
	missing := make(map[string]bool)
	for index, candidate := range bucket.symbols {
		if candidate.text == "" {
			continue
		}
		available := glyphs[candidate.fontStack]
		if !fixtureTextComplete(candidate.text, available) {
			missing[candidate.fontStack] = true
			continue
		}
		if layout := shapeSDFText(candidate, available); layout != nil {
			layouts[libertySDFLayoutKey{tile: pinnedTile, index: index}] = layout
		}
	}
	fonts := make([]string, 0, len(missing))
	for font := range missing {
		fonts = append(fonts, font)
	}
	slices.Sort(fonts)
	return layouts, fonts
}

func fixtureTextComplete(text string, glyphs map[uint32]sdfGlyph) bool {
	if glyphs == nil || !utf8.ValidString(text) || !sdfTextEligible(text) {
		return false
	}
	for _, code := range text {
		if code == '\n' || code == '\r' {
			continue
		}
		if _, exists := glyphs[uint32(code)]; !exists {
			return false
		}
	}
	return true
}

// Frame creates camera data without changing the scene or its resource IDs.
// Style widths/visibility and collision decisions stay fixed in this fixture.
func (f *RenderFixture) Frame(camera Camera) scene.Frame {
	t := roadCameraTransform(camera, pinnedTile)
	return scene.Frame{Scene: f.Scene, DevicePixelRatio: 1, Transforms: []scene.Affine{{M11: float32(t.M11), M12: float32(t.M12), DX: float32(t.DX), M21: float32(t.M21), M22: float32(t.M22), DY: float32(t.DY)}}}
}

type fixtureBuilder struct {
	fixture  *RenderFixture
	vertices []scene.Vertex
	textures map[string]uint64
	atlas    uint64
}

func (b *fixtureBuilder) texture(key string, width, height int, pixels []byte) uint64 {
	if id, exists := b.textures[key]; exists {
		return id
	}
	id := uint64(len(b.textures) + 1)
	b.textures[key] = id
	b.fixture.Scene.Textures = append(b.fixture.Scene.Textures, scene.Texture{ID: id, Revision: 1, Width: width, Height: height, RGBA: pixels})
	return id
}

func fixtureColor(c mapColor) [4]float32 {
	return [4]float32{float32(c.red) / 255, float32(c.green) / 255, float32(c.blue) / 255, float32(c.alpha) / 255}
}

func (b *fixtureBuilder) draw(first int, material scene.Material, clip [4]float32) {
	count := len(b.vertices) - first
	if count == 0 {
		return
	}
	b.fixture.Scene.Draws = scene.AppendDraw(b.fixture.Scene.Draws, scene.Draw{Mesh: 1, First: uint32(first), Count: uint32(count), Material: material, Clip: clip})
}

func (b *fixtureBuilder) primitive(primitive libertyRenderPrimitive) {
	material := scene.Material{Color: fixtureColor(primitive.color)}
	if primitive.patternName != "" {
		sprite, exists := libertySprite(primitive.patternName, mapColor{255, 255, 255, 255}, 1)
		if !exists {
			return
		}
		width, height := float64(sprite.width)/sprite.pixelRatio*primitive.patternScale, float64(sprite.height)/sprite.pixelRatio*primitive.patternScale
		x, y := libertyPatternPhase(pinnedTile, 0, width, height)
		material = scene.Material{Kind: scene.Pattern, Texture: b.texture("pattern/"+primitive.patternName, sprite.width, sprite.height, sprite.pixels), Color: [4]float32{1, 1, 1, float32(primitive.opacity)}, PatternSize: [2]float32{float32(width), float32(height)}, PatternPhase: [2]float32{float32(x), float32(y)}}
	}
	first := len(b.vertices)
	for _, point := range primitive.triangles {
		b.vertices = append(b.vertices, scene.Vertex{X: float32(point.X), Y: float32(point.Y)})
	}
	b.draw(first, material, [4]float32{0, 0, tileSize, tileSize})
}

func (b *fixtureBuilder) symbols(order int) {
	start, end := libertySymbolRangeAtOrder(b.fixture.bucket.symbols, order)
	for index := start; index < end; index++ {
		candidate := b.fixture.bucket.symbols[index]
		accepted := b.fixture.accepted[libertySymbolKey{tile: pinnedTile, index: index}]
		if accepted.icon && candidate.iconName != "" {
			sprite, exists := libertySprite(candidate.iconName, candidate.iconColor, candidate.iconOpacity)
			if exists {
				width, height := float64(sprite.width)/sprite.pixelRatio*candidate.iconSize, float64(sprite.height)/sprite.pixelRatio*candidate.iconSize
				x, y := libertyAnchoredOrigin(candidate.iconAnchor, width, height)
				vertices := appendSDFQuad(nil, x, y, x+width, y+height, 0, 0, 1, 1)
				first := b.symbolVertices(candidate.anchor, vertices, roadPoint{candidate.iconOffset.X * candidate.iconSize, candidate.iconOffset.Y * candidate.iconSize}, libertyRenderedSymbolAngle(candidate.iconLineAngle, candidate.iconRotate, candidate.iconViewportAligned))
				key := fmt.Sprintf("icon/%s/%v/%g", candidate.iconName, candidate.iconColor, candidate.iconOpacity)
				b.draw(first, scene.Material{Kind: scene.Image, Texture: b.texture(key, sprite.width, sprite.height, sprite.pixels), Color: [4]float32{1, 1, 1, 1}, MapAligned: !candidate.iconViewportAligned}, [4]float32{})
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
			if !b.fixture.accepted[libertySymbolKey{tile: pinnedTile, index: index}].text {
				continue
			}
			layout := b.fixture.sdf.layouts[libertySDFLayoutKey{tile: pinnedTile, index: index}]
			if layout == nil {
				continue
			}
			color := candidate.textColor
			if kind == scene.SDFHalo {
				if candidate.haloWidth <= 0 || candidate.haloColor.alpha == 0 {
					continue
				}
				color = candidate.haloColor
			} else {
				b.fixture.Labels++
			}
			first := b.symbolVertices(candidate.anchor, layout.vertices, roadPoint{candidate.textOffset.X * candidate.textSize, candidate.textOffset.Y * candidate.textSize}, libertyRenderedSymbolAngle(candidate.lineAngle, candidate.textRotate, candidate.viewportAligned))
			b.draw(first, scene.Material{Kind: kind, Texture: b.atlas, Color: fixtureColor(color), FontScale: float32(layout.scale), HaloWidth: float32(candidate.haloWidth), HaloBlur: float32(candidate.haloBlur), MapAligned: !candidate.viewportAligned}, [4]float32{})
		}
	}
}

func (b *fixtureBuilder) symbolVertices(anchor roadPoint, vertices []float32, offset roadPoint, angle float64) int {
	first := len(b.vertices)
	sin, cos := math.Sincos(angle)
	for index := 0; index < len(vertices); index += 4 {
		x, y := float64(vertices[index])+offset.X, float64(vertices[index+1])+offset.Y
		b.vertices = append(b.vertices, scene.Vertex{X: float32(anchor.X), Y: float32(anchor.Y), OffsetX: float32(x*cos - y*sin), OffsetY: float32(x*sin + y*cos), U: vertices[index+2], V: vertices[index+3]})
	}
	return first
}
