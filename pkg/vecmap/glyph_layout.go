package vecmap

import (
	"unicode"
	"unicode/utf8"

	"github.com/rubiojr/whereami/pkg/vecmap/compiler"
	"github.com/rubiojr/whereami/pkg/vecmap/glyph"
	"github.com/rubiojr/whereami/pkg/vecmap/placement"
)

const maximumSDFSceneLayouts = glyph.MaxPreparedLayouts

type libertySDFLayoutKey struct {
	tile  vectorTileID
	index int
}

type sdfGlyphKey = glyph.Key

type sdfPositionedGlyph = glyph.PositionedGlyph

type sdfTextLayout = glyph.PreparedLayout

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
	renderable, err := glyph.PrepareLayouts(layouts, atlas, indexed)
	if err != nil {
		return nil, err
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
	return glyph.LayoutGlyphs(layouts)
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
	return libertyTextRequest(candidate).Layout(glyphs)
}

func libertyTextRequest(candidate libertySymbolCandidate) compiler.TextRequest {
	return compiler.TextRequest{Text: candidate.text, FontStack: candidate.fontStack, Options: glyph.LayoutOptions{
		TextSize: candidate.textSize, LetterSpacing: candidate.letterSpacing,
		MaximumWidth: candidate.maximumWidth, LineHeight: candidate.lineHeight,
		Anchor: candidate.textAnchor, Justify: candidate.textJustify,
		HaloWidth: candidate.haloWidth, HaloBlur: candidate.haloBlur,
	}}
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
	mesh, _ := glyph.BuildLayoutMesh(&layout.TextLayout, atlas, false)
	return mesh.Expanded
}
