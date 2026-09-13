package vecmap

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/glyph"
)

const (
	maximumSDFSceneLayouts = 10_000
	sdfAtlasVertexElements = 4
)

type libertySDFLayoutKey struct {
	tile  vectorTileID
	index int
}

type sdfGlyphKey = glyph.Key

type sdfPositionedGlyph = glyph.PositionedGlyph

type sdfTextLayout struct {
	glyphs          []sdfPositionedGlyph
	bounds          libertyCollisionBox
	vertices        []float32
	indexedVertices []geometry.TextVertex
	indices         []uint32
	scale           float64
}

type sdfAtlasRect = glyph.Rect
type sdfGlyphAtlas = glyph.Atlas

type sdfScene struct {
	atlas          *sdfGlyphAtlas
	layouts        map[libertySDFLayoutKey]*sdfTextLayout
	rendered       map[libertySDFLayoutKey]struct{}
	renderedLabels int
}

func prepareSDFLayouts(
	tiles []loadedRoadTile,
	manager *glyphManager,
) (map[libertySDFLayoutKey]*sdfTextLayout, bool) {
	if manager == nil {
		return nil, false
	}
	layouts := make(map[libertySDFLayoutKey]*sdfTextLayout)
	considered := 0
	hasCandidates := false
	for _, tile := range tiles {
		if tile.roads == nil {
			continue
		}
		for index, candidate := range tile.roads.symbols {
			if !sdfTextEligible(candidate.text) || candidate.fontStack == "" || candidate.textColor.Alpha == 0 {
				continue
			}
			hasCandidates = true
			considered++
			if considered > maximumSDFSceneLayouts {
				return layouts, hasCandidates
			}
			candidate.text = normalizeLibertySymbolText(candidate.text)
			glyphs, ready := manager.resolve(candidate.fontStack, candidate.text)
			if !ready {
				continue
			}
			layout := shapeSDFText(candidate, glyphs)
			if layout != nil {
				layouts[libertySDFLayoutKey{tile: tile.id, index: index}] = layout
			}
		}
	}
	return layouts, hasCandidates
}

func buildSDFScene(
	layouts map[libertySDFLayoutKey]*sdfTextLayout,
	atlas *sdfGlyphAtlas,
) *sdfScene {
	result, _ := buildSDFSceneGeometry(layouts, atlas, false)
	return result
}

func buildSDFSceneGeometry(layouts map[libertySDFLayoutKey]*sdfTextLayout, atlas *sdfGlyphAtlas, indexed bool) (*sdfScene, error) {
	if len(layouts) == 0 || atlas == nil {
		return nil, nil
	}
	renderable := make(map[libertySDFLayoutKey]*sdfTextLayout, len(layouts))
	for key, layout := range layouts {
		if !sdfLayoutFitsAtlas(layout, atlas) {
			continue
		}
		layout.vertices, layout.indexedVertices, layout.indices = nil, nil, nil
		if indexed {
			mesh := geometry.NewBuilder[geometry.TextVertex](true, len(layout.glyphs)*6)
			if err := sdfLayoutQuads(layout, atlas, func(quad [4]geometry.TextVertex) error {
				return mesh.Quad(quad[0], quad[1], quad[2], quad[3])
			}); err != nil {
				return nil, err
			}
			layout.indexedVertices, layout.indices = mesh.Vertices, mesh.Indices
		} else {
			layout.vertices = sdfLayoutVertices(layout, atlas)
		}
		if len(layout.vertices) == 0 && len(layout.indices) == 0 {
			continue
		}
		renderable[key] = layout
	}
	if len(renderable) == 0 {
		return nil, nil
	}
	return &sdfScene{
		atlas:    atlas,
		layouts:  renderable,
		rendered: make(map[libertySDFLayoutKey]struct{}),
	}, nil
}

func sdfLayoutGlyphs(layouts map[libertySDFLayoutKey]*sdfTextLayout) map[sdfGlyphKey]sdfGlyph {
	glyphs := make(map[sdfGlyphKey]sdfGlyph)
	for _, layout := range layouts {
		for _, positioned := range layout.glyphs {
			if len(positioned.Glyph.Bitmap) > 0 {
				glyphs[positioned.Key] = positioned.Glyph
			}
		}
	}
	return glyphs
}

func sdfGlyphKeySet(glyphs map[sdfGlyphKey]sdfGlyph) map[sdfGlyphKey]struct{} {
	keys := make(map[sdfGlyphKey]struct{}, len(glyphs))
	for key := range glyphs {
		keys[key] = struct{}{}
	}
	return keys
}

func sameSDFGlyphKeys(first, second map[sdfGlyphKey]struct{}) bool {
	if len(first) != len(second) {
		return false
	}
	for key := range first {
		if _, exists := second[key]; !exists {
			return false
		}
	}
	return true
}

func sdfAtlasNeedsRebuild(atlas *sdfGlyphAtlas, glyphs map[sdfGlyphKey]sdfGlyph) bool {
	if len(glyphs) == 0 {
		return false
	}
	if atlas == nil {
		return true
	}
	for key := range glyphs {
		if _, exists := atlas.Positions[key]; !exists {
			return true
		}
	}
	return false
}

func buildRetainedSDFAtlas(
	current map[sdfGlyphKey]sdfGlyph,
	retained map[sdfGlyphKey]sdfGlyph,
) (*sdfGlyphAtlas, map[sdfGlyphKey]sdfGlyph) {
	currentAtlas := buildSDFAtlas(current)
	if currentAtlas == nil {
		return nil, nil
	}
	merged := make(map[sdfGlyphKey]sdfGlyph, len(current)+len(retained))
	for key, glyph := range retained {
		merged[key] = glyph
	}
	for key, glyph := range current {
		merged[key] = glyph
	}
	atlas := buildSDFAtlas(merged)
	if atlas == nil || atlas.Width > currentAtlas.Width || sdfAtlasNeedsRebuild(atlas, current) {
		merged = current
		atlas = currentAtlas
	}
	resident := make(map[sdfGlyphKey]sdfGlyph, len(atlas.Positions))
	for key := range atlas.Positions {
		if glyph, exists := merged[key]; exists {
			resident[key] = glyph
		}
	}
	return atlas, resident
}

func sdfLayoutFitsAtlas(layout *sdfTextLayout, atlas *sdfGlyphAtlas) bool {
	if layout == nil || atlas == nil {
		return false
	}
	for _, positioned := range layout.glyphs {
		if len(positioned.Glyph.Bitmap) == 0 {
			continue
		}
		if _, exists := atlas.Positions[positioned.Key]; !exists {
			return false
		}
	}
	return true
}

func sdfTextEligible(text string) bool {
	if text == "" {
		return false
	}
	runeCount := 0
	for _, codePoint := range text {
		runeCount++
		if runeCount > maximumSymbolTextRunes {
			return false
		}
		if codePoint > rune(maximumGlyphCodePoint) || unicode.Is(unicode.M, codePoint) {
			return false
		}
		if codePoint == '\n' || codePoint == '\r' || unicode.IsSpace(codePoint) || unicode.Is(unicode.Common, codePoint) {
			continue
		}
		if !unicode.In(
			codePoint,
			unicode.Latin,
			unicode.Greek,
			unicode.Cyrillic,
			unicode.Armenian,
			unicode.Georgian,
			unicode.Han,
			unicode.Hiragana,
			unicode.Katakana,
			unicode.Hangul,
			unicode.Bopomofo,
		) {
			return false
		}
	}
	return true
}

func qtTextFallbackEligible(text string) bool {
	if text == "" || utf8.RuneCountInString(text) > maximumSymbolTextRunes {
		return false
	}
	for _, codePoint := range text {
		// Qt repeatedly searches every installed font when no font provides
		// OpenType shaping for Thaana, stalling scene rebuilds.
		if unicode.Is(unicode.Thaana, codePoint) {
			return false
		}
	}
	return true
}

func libertyTextRenderable(text string, sdfLayout *sdfTextLayout) bool {
	if sdfTextEligible(text) {
		return sdfLayout != nil
	}
	return qtTextFallbackEligible(text)
}

func normalizeLibertySymbolText(text string) string {
	var normalized strings.Builder
	normalized.Grow(len(text))
	previousCarriageReturn := false
	for _, codePoint := range text {
		if codePoint == '\n' && previousCarriageReturn {
			previousCarriageReturn = false
			continue
		}
		previousCarriageReturn = codePoint == '\r'
		switch {
		case codePoint == '\r' || codePoint == '\n':
			normalized.WriteByte('\n')
		case unicode.IsSpace(codePoint):
			normalized.WriteByte(' ')
		default:
			normalized.WriteRune(codePoint)
		}
	}
	return normalized.String()
}

func shapeSDFText(candidate libertySymbolCandidate, glyphs map[uint32]sdfGlyph) *sdfTextLayout {
	prepared, ok := glyph.LayoutText(candidate.text, candidate.fontStack, glyphs, glyph.LayoutOptions{
		TextSize: candidate.textSize, LetterSpacing: candidate.letterSpacing,
		MaximumWidth: candidate.maximumWidth, LineHeight: candidate.lineHeight,
		Anchor: candidate.textAnchor, Justify: candidate.textJustify,
		HaloWidth: candidate.haloWidth, HaloBlur: candidate.haloBlur,
	})
	if !ok {
		return nil
	}
	return &sdfTextLayout{glyphs: prepared.Glyphs, scale: prepared.Scale,
		bounds: libertyCollisionBox{left: prepared.Bounds.Left, top: prepared.Bounds.Top,
			right: prepared.Bounds.Right, bottom: prepared.Bounds.Bottom}}
}

func buildSDFAtlas(glyphs map[sdfGlyphKey]sdfGlyph) *sdfGlyphAtlas {
	// Inputs came from validated glyph ranges. Invalid data cannot produce a
	// usable atlas; the existing caller handles nil by deferring SDF rendering.
	atlas, _ := glyph.BuildAtlas(glyphs)
	return atlas
}

func sdfLayoutVertices(layout *sdfTextLayout, atlas *sdfGlyphAtlas) []float32 {
	if layout == nil || atlas == nil {
		return nil
	}
	vertices := make([]float32, 0, len(layout.glyphs)*6*sdfAtlasVertexElements)
	_ = sdfLayoutQuads(layout, atlas, func(quad [4]geometry.TextVertex) error {
		for _, index := range [...]int{0, 1, 2, 0, 2, 3} {
			vertex := quad[index]
			vertices = append(vertices, vertex.X, vertex.Y, vertex.U, vertex.V)
		}
		return nil
	})
	return vertices
}

func sdfLayoutQuads(layout *sdfTextLayout, atlas *sdfGlyphAtlas, emit func([4]geometry.TextVertex) error) error {
	for _, positioned := range layout.glyphs {
		rectangle, exists := atlas.Positions[positioned.Key]
		if !exists || len(positioned.Glyph.Bitmap) == 0 {
			continue
		}
		scale := layout.scale
		x1 := (positioned.X + float64(positioned.Glyph.Left) - glyphAtlasPadding) * scale
		y1 := (positioned.Y - float64(positioned.Glyph.Top) - glyphAtlasPadding) * scale
		x2 := x1 + float64(rectangle.Width)*scale
		y2 := y1 + float64(rectangle.Height)*scale
		u1 := float64(rectangle.X) / float64(atlas.Width)
		v1 := float64(rectangle.Y) / float64(atlas.Height)
		u2 := float64(rectangle.X+rectangle.Width) / float64(atlas.Width)
		v2 := float64(rectangle.Y+rectangle.Height) / float64(atlas.Height)
		if err := emit(geometry.TextQuad(x1, y1, x2, y2, u1, v1, u2, v2)); err != nil {
			return err
		}
	}
	return nil
}
