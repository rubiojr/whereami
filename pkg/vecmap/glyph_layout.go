package vecmap

import (
	"cmp"
	"math"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
)

const (
	sdfGlyphEmSize         = 24.0
	sdfDefaultBaseline     = -17.0
	minimumSDFAtlasSize    = 256
	maximumSDFAtlasSize    = 2048
	maximumSDFSceneLayouts = 10_000
	sdfAtlasVertexElements = 4
)

type libertySDFLayoutKey struct {
	tile  vectorTileID
	index int
}

type sdfGlyphKey struct {
	fontStack string
	id        uint32
}

type sdfPositionedGlyph struct {
	key   sdfGlyphKey
	glyph sdfGlyph
	x     float64
	y     float64
}

type sdfTextLayout struct {
	glyphs          []sdfPositionedGlyph
	bounds          libertyCollisionBox
	vertices        []float32
	indexedVertices []geometry.TextVertex
	indices         []uint32
	scale           float64
}

type sdfAtlasRect struct {
	x      int
	y      int
	width  int
	height int
}

type sdfGlyphAtlas struct {
	pixels    []byte
	width     int
	height    int
	positions map[sdfGlyphKey]sdfAtlasRect
}

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
			if len(positioned.glyph.bitmap) > 0 {
				glyphs[positioned.key] = positioned.glyph
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
		if _, exists := atlas.positions[key]; !exists {
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
	if atlas == nil || atlas.width > currentAtlas.width || sdfAtlasNeedsRebuild(atlas, current) {
		merged = current
		atlas = currentAtlas
	}
	resident := make(map[sdfGlyphKey]sdfGlyph, len(atlas.positions))
	for key := range atlas.positions {
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
		if len(positioned.glyph.bitmap) == 0 {
			continue
		}
		if _, exists := atlas.positions[positioned.key]; !exists {
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
	if candidate.textSize <= 0 {
		return nil
	}
	spacing := candidate.letterSpacing * sdfGlyphEmSize
	maximumWidth := candidate.maximumWidth * sdfGlyphEmSize
	lines := breakSDFLines(candidate.text, glyphs, spacing, maximumWidth)
	if len(lines) == 0 {
		return nil
	}
	lineHeight := candidate.lineHeight * sdfGlyphEmSize
	if lineHeight <= 0 {
		lineHeight = 1.2 * sdfGlyphEmSize
	}
	horizontalAlign, verticalAlign := libertyAnchorAlignment(candidate.textAnchor)
	justify := libertyTextJustification(candidate.textJustify, horizontalAlign)
	positioned := make([]sdfPositionedGlyph, 0, len([]rune(candidate.text)))
	maximumLineWidth := 0.0
	for lineIndex, line := range lines {
		lineWidth := measureSDFLine(line, glyphs, spacing)
		maximumLineWidth = max(maximumLineWidth, lineWidth)
		x := -justify * lineWidth
		y := float64(lineIndex)*lineHeight + sdfDefaultBaseline
		for index, codePoint := range line {
			glyph, exists := glyphs[uint32(codePoint)]
			if !exists {
				return nil
			}
			positioned = append(positioned, sdfPositionedGlyph{
				key:   sdfGlyphKey{fontStack: candidate.fontStack, id: uint32(codePoint)},
				glyph: glyph,
				x:     x,
				y:     y,
			})
			x += float64(glyph.advance)
			if index+1 < len(line) {
				x += spacing
			}
		}
	}
	if len(positioned) == 0 {
		return nil
	}
	blockHeight := float64(len(lines)) * lineHeight
	shiftX := (justify - horizontalAlign) * maximumLineWidth
	shiftY := -verticalAlign*blockHeight + 0.5*lineHeight
	for index := range positioned {
		positioned[index].x += shiftX
		positioned[index].y += shiftY
	}
	scale := candidate.textSize / sdfGlyphEmSize
	renderedHaloWidth := min(max(0, candidate.haloWidth), glyphPBFBorder*scale)
	haloExtent := renderedHaloWidth + max(0, candidate.haloBlur)
	bounds := sdfPositionedGlyphBounds(positioned, scale)
	if math.IsInf(bounds.left, 1) {
		bounds = libertyCollisionBox{
			left:   -horizontalAlign * maximumLineWidth * scale,
			top:    -verticalAlign * blockHeight * scale,
			right:  (1 - horizontalAlign) * maximumLineWidth * scale,
			bottom: (1 - verticalAlign) * blockHeight * scale,
		}
	}
	bounds.left -= haloExtent
	bounds.top -= haloExtent
	bounds.right += haloExtent
	bounds.bottom += haloExtent
	return &sdfTextLayout{
		glyphs: positioned,
		scale:  scale,
		bounds: bounds,
	}
}

func sdfPositionedGlyphBounds(glyphs []sdfPositionedGlyph, scale float64) libertyCollisionBox {
	bounds := libertyCollisionBox{
		left:   math.Inf(1),
		top:    math.Inf(1),
		right:  math.Inf(-1),
		bottom: math.Inf(-1),
	}
	for _, positioned := range glyphs {
		if positioned.glyph.width == 0 || positioned.glyph.height == 0 {
			continue
		}
		left := (positioned.x + float64(positioned.glyph.left)) * scale
		top := (positioned.y - float64(positioned.glyph.top)) * scale
		bounds.left = min(bounds.left, left)
		bounds.top = min(bounds.top, top)
		bounds.right = max(bounds.right, left+float64(positioned.glyph.width)*scale)
		bounds.bottom = max(bounds.bottom, top+float64(positioned.glyph.height)*scale)
	}
	return bounds
}

func breakSDFLines(text string, glyphs map[uint32]sdfGlyph, spacing, maximumWidth float64) [][]rune {
	paragraphs := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	lines := make([][]rune, 0, len(paragraphs))
	for _, paragraph := range paragraphs {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			lines = append(lines, nil)
			continue
		}
		current := make([]rune, 0, len([]rune(paragraph)))
		currentWidth := 0.0
		for _, word := range words {
			wordRunes := []rune(word)
			wordWidth := measureSDFLine(wordRunes, glyphs, spacing)
			if maximumWidth > 0 && wordWidth > maximumWidth {
				if len(current) > 0 {
					lines = append(lines, current)
					current = nil
					currentWidth = 0
				}
				for _, codePoint := range wordRunes {
					glyph, exists := glyphs[uint32(codePoint)]
					glyphWidth := math.Inf(1)
					if exists {
						glyphWidth = float64(glyph.advance)
					}
					candidateWidth := glyphWidth
					if len(current) > 0 {
						candidateWidth += currentWidth + spacing
					}
					if len(current) > 0 && candidateWidth > maximumWidth {
						lines = append(lines, current)
						current = []rune{codePoint}
						currentWidth = glyphWidth
						continue
					}
					current = append(current, codePoint)
					currentWidth = candidateWidth
				}
				continue
			}
			candidateWidth := wordWidth
			if len(current) > 0 {
				space, exists := glyphs[' ']
				if !exists {
					candidateWidth = math.Inf(1)
				} else {
					candidateWidth += currentWidth + float64(space.advance) + 2*spacing
				}
			}
			if maximumWidth > 0 && len(current) > 0 && candidateWidth > maximumWidth {
				lines = append(lines, current)
				current = append([]rune(nil), wordRunes...)
				currentWidth = wordWidth
				continue
			}
			if len(current) > 0 {
				current = append(current, ' ')
			}
			current = append(current, wordRunes...)
			currentWidth = candidateWidth
		}
		lines = append(lines, current)
	}
	return lines
}

func measureSDFLine(line []rune, glyphs map[uint32]sdfGlyph, spacing float64) float64 {
	width := 0.0
	for index, codePoint := range line {
		glyph, exists := glyphs[uint32(codePoint)]
		if !exists {
			return math.Inf(1)
		}
		width += float64(glyph.advance)
		if index+1 < len(line) {
			width += spacing
		}
	}
	return width
}

func libertyAnchorAlignment(anchor string) (float64, float64) {
	horizontal := 0.5
	vertical := 0.5
	if strings.Contains(anchor, "left") {
		horizontal = 0
	} else if strings.Contains(anchor, "right") {
		horizontal = 1
	}
	if strings.Contains(anchor, "top") {
		vertical = 0
	} else if strings.Contains(anchor, "bottom") {
		vertical = 1
	}
	return horizontal, vertical
}

func libertyTextJustification(justify string, horizontalAlign float64) float64 {
	switch justify {
	case "left":
		return 0
	case "right":
		return 1
	case "center":
		return 0.5
	default:
		return horizontalAlign
	}
}

func buildSDFAtlas(glyphs map[sdfGlyphKey]sdfGlyph) *sdfGlyphAtlas {
	if len(glyphs) == 0 {
		return nil
	}
	keys := make([]sdfGlyphKey, 0, len(glyphs))
	for key := range glyphs {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(first, second sdfGlyphKey) int {
		firstGlyph, secondGlyph := glyphs[first], glyphs[second]
		if byHeight := cmp.Compare(secondGlyph.height, firstGlyph.height); byHeight != 0 {
			return byHeight
		}
		if byWidth := cmp.Compare(secondGlyph.width, firstGlyph.width); byWidth != 0 {
			return byWidth
		}
		if byStack := cmp.Compare(first.fontStack, second.fontStack); byStack != 0 {
			return byStack
		}
		return cmp.Compare(first.id, second.id)
	})
	for size := minimumSDFAtlasSize; size <= maximumSDFAtlasSize; size *= 2 {
		positions, fits := packSDFGlyphs(keys, glyphs, size)
		if !fits && size < maximumSDFAtlasSize {
			continue
		}
		if len(positions) == 0 {
			return nil
		}
		pixels := make([]byte, size*size)
		for key, rectangle := range positions {
			glyph := glyphs[key]
			bitmapWidth := int(glyph.width) + 2*glyphPBFBorder
			bitmapHeight := int(glyph.height) + 2*glyphPBFBorder
			for y := range bitmapHeight {
				for x := range bitmapWidth {
					distance := glyph.bitmap[y*bitmapWidth+x]
					target := (rectangle.y+glyphAtlasGuard+y)*size + rectangle.x + glyphAtlasGuard + x
					pixels[target] = distance
				}
			}
		}
		return &sdfGlyphAtlas{pixels: pixels, width: size, height: size, positions: positions}
	}
	return nil
}

func packSDFGlyphs(keys []sdfGlyphKey, glyphs map[sdfGlyphKey]sdfGlyph, size int) (map[sdfGlyphKey]sdfAtlasRect, bool) {
	positions := make(map[sdfGlyphKey]sdfAtlasRect, len(keys))
	x, y, shelfHeight := 0, 0, 0
	for _, key := range keys {
		glyph := glyphs[key]
		width := int(glyph.width) + 2*glyphAtlasPadding
		height := int(glyph.height) + 2*glyphAtlasPadding
		if width > size || height > size {
			return nil, false
		}
		if x+width > size {
			x = 0
			y += shelfHeight
			shelfHeight = 0
		}
		if y+height > size {
			return positions, false
		}
		positions[key] = sdfAtlasRect{x: x, y: y, width: width, height: height}
		x += width
		shelfHeight = max(shelfHeight, height)
	}
	return positions, true
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
		rectangle, exists := atlas.positions[positioned.key]
		if !exists || len(positioned.glyph.bitmap) == 0 {
			continue
		}
		scale := layout.scale
		x1 := (positioned.x + float64(positioned.glyph.left) - glyphAtlasPadding) * scale
		y1 := (positioned.y - float64(positioned.glyph.top) - glyphAtlasPadding) * scale
		x2 := x1 + float64(rectangle.width)*scale
		y2 := y1 + float64(rectangle.height)*scale
		u1 := float64(rectangle.x) / float64(atlas.width)
		v1 := float64(rectangle.y) / float64(atlas.height)
		u2 := float64(rectangle.x+rectangle.width) / float64(atlas.width)
		v2 := float64(rectangle.y+rectangle.height) / float64(atlas.height)
		if err := emit(geometry.TextQuad(x1, y1, x2, y2, u1, v1, u2, v2)); err != nil {
			return err
		}
	}
	return nil
}
