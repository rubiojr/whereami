package vecmap

import (
	"unicode"
	"unicode/utf8"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/glyph"
	"github.com/rubiojr/whereami/pkg/vecmap/placement"
)

const maximumSDFSceneLayouts = 10_000

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
		prepared := glyph.TextLayout{Glyphs: layout.glyphs, Scale: layout.scale}
		mesh, err := glyph.BuildLayoutMesh(&prepared, atlas, indexed)
		if err != nil {
			return nil, err
		}
		layout.vertices, layout.indexedVertices, layout.indices = mesh.Expanded, mesh.Vertices, mesh.Indices
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
	return glyph.AtlasNeedsRebuild(atlas, glyphs)
}

func buildRetainedSDFAtlas(
	current map[sdfGlyphKey]sdfGlyph,
	retained map[sdfGlyphKey]sdfGlyph,
) (*sdfGlyphAtlas, map[sdfGlyphKey]sdfGlyph) {
	// Validated font ranges are normal inputs. Preserve the adapter's nil-atlas
	// degradation on invalid current data; headless callers can inspect the error.
	atlas, resident, _ := glyph.BuildRetainedAtlas(current, retained)
	return atlas, resident
}

func sdfLayoutFitsAtlas(layout *sdfTextLayout, atlas *sdfGlyphAtlas) bool {
	if layout == nil {
		return false
	}
	prepared := glyph.TextLayout{Glyphs: layout.glyphs, Scale: layout.scale}
	return glyph.FitsAtlas(&prepared, atlas)
}

func sdfTextEligible(text string) bool {
	return glyph.TextEligible(text)
}

func qtTextFallbackEligible(text string) bool {
	if text == "" || !utf8.ValidString(text) || utf8.RuneCountInString(text) > maximumSymbolTextRunes {
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
	return placement.NormalizeText(text)
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
	if layout == nil {
		return nil
	}
	prepared := glyph.TextLayout{Glyphs: layout.glyphs, Scale: layout.scale}
	mesh, _ := glyph.BuildLayoutMesh(&prepared, atlas, false)
	return mesh.Expanded
}
