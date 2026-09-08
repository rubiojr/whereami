package vecmap

import (
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	quick "github.com/rubiojr/whereami/internal/miqtquick"
)

const (
	libertyCollisionCellSize    = 64.0
	libertyCollisionZoomStep    = 1.0 / 8.0
	libertyCollisionBearingStep = 2.0
	libertyCollisionPanStep     = 64.0
	maxViewportSymbolReferences = 100_000
)

type libertySymbolKey struct {
	tile  vectorTileID
	wrap  int
	index int
}

type retainedLibertySymbolTransform struct {
	node          *quick.QSGTransformNode
	tile          vectorTileID
	anchor        roadPoint
	offset        roadPoint
	localAngle    float64
	viewportAlign bool
}

type retainedLibertySymbolTile struct {
	node *quick.QSGTransformNode
	tile vectorTileID
	wrap int
}

type retainedLibertySymbolLayer struct {
	root  *quick.QSGNode
	order int
	tiles []retainedLibertySymbolTile
}

type libertyCollisionBox struct {
	left   float64
	top    float64
	right  float64
	bottom float64
}

type libertySymbolReference struct {
	key         libertySymbolKey
	candidate   *libertySymbolCandidate
	textBox     libertyCollisionBox
	iconBox     libertyCollisionBox
	textPresent bool
	iconPresent bool
	textVisible bool
	iconVisible bool
}

type libertyAcceptedSymbol struct {
	text bool
	icon bool
}

type libertyCollisionCell struct {
	x int
	y int
}

func acceptedLibertySymbols(
	camera Camera,
	tiles []loadedRoadTile,
	sdfLayouts map[libertySDFLayoutKey]*sdfTextLayout,
) map[libertySymbolKey]libertyAcceptedSymbol {
	references := make([]libertySymbolReference, 0)
	wraps := libertyWorldWraps(camera)
	for _, tile := range tiles {
		if tile.roads == nil {
			continue
		}
		for _, wrap := range wraps {
			transform := wrappedRoadCameraTransform(camera, tile.id, wrap)
			for index := len(tile.roads.symbols) - 1; index >= 0; index-- {
				candidate := &tile.roads.symbols[index]
				layout := sdfLayouts[libertySDFLayoutKey{tile: tile.id, index: index}]
				textBox, textPresent, textVisible, iconBox, iconPresent, iconVisible := libertyCandidateCollisionBoxes(
					transform,
					*candidate,
					layout,
					camera.Width,
					camera.Height,
				)
				if !textVisible && !iconVisible {
					continue
				}
				references = append(references, libertySymbolReference{
					key:         libertySymbolKey{tile: tile.id, wrap: wrap, index: index},
					candidate:   candidate,
					textBox:     textBox,
					iconBox:     iconBox,
					textPresent: textPresent,
					iconPresent: iconPresent,
					textVisible: textVisible,
					iconVisible: iconVisible,
				})
				if len(references) >= maxViewportSymbolReferences {
					break
				}
			}
			if len(references) >= maxViewportSymbolReferences {
				break
			}
		}
		if len(references) >= maxViewportSymbolReferences {
			break
		}
	}
	sort.SliceStable(references, func(first, second int) bool {
		if references[first].candidate.order != references[second].candidate.order {
			return references[first].candidate.order > references[second].candidate.order
		}
		return references[first].candidate.sortKey < references[second].candidate.sortKey
	})
	accepted := make(map[libertySymbolKey]libertyAcceptedSymbol, len(references))
	occupied := make(map[libertyCollisionCell][]libertyCollisionBox)
	for _, reference := range references {
		textCollision := reference.textVisible && !reference.candidate.textAllowsOverlap &&
			libertyCollisionGridIntersects(occupied, reference.textBox, camera.Width, camera.Height)
		iconCollision := reference.iconVisible && !reference.candidate.iconAllowsOverlap &&
			libertyCollisionGridIntersects(occupied, reference.iconBox, camera.Width, camera.Height)
		textAccepted := reference.textVisible && !textCollision
		iconAccepted := reference.iconVisible && !iconCollision
		if reference.textPresent && !reference.textVisible && !reference.candidate.textOptional {
			iconAccepted = false
		}
		if reference.textPresent && textCollision && !reference.candidate.textOptional {
			iconAccepted = false
		}
		if reference.iconPresent && iconCollision && !reference.candidate.iconOptional {
			textAccepted = false
		}
		if !textAccepted && !iconAccepted {
			continue
		}
		if textAccepted && !reference.candidate.textAllowsOverlap {
			libertyAddCollisionBox(occupied, reference.textBox, camera.Width, camera.Height)
		}
		if iconAccepted && !reference.candidate.iconAllowsOverlap {
			libertyAddCollisionBox(occupied, reference.iconBox, camera.Width, camera.Height)
		}
		accepted[reference.key] = libertyAcceptedSymbol{text: textAccepted, icon: iconAccepted}
	}
	return accepted
}

func sameLibertyAcceptedSymbols(first, second map[libertySymbolKey]libertyAcceptedSymbol) bool {
	if len(first) != len(second) {
		return false
	}
	for key, placement := range first {
		other, exists := second[key]
		if !exists || other != placement {
			return false
		}
	}
	return true
}

func libertyCollisionGridIntersects(
	occupied map[libertyCollisionCell][]libertyCollisionBox,
	box libertyCollisionBox,
	viewportWidth, viewportHeight float64,
) bool {
	collision := false
	libertyForEachCollisionCell(box, viewportWidth, viewportHeight, func(cell libertyCollisionCell) bool {
		for _, occupiedBox := range occupied[cell] {
			if libertyBoxesIntersect(box, occupiedBox) {
				collision = true
				return false
			}
		}
		return true
	})
	return collision
}

func libertyAddCollisionBox(
	occupied map[libertyCollisionCell][]libertyCollisionBox,
	box libertyCollisionBox,
	viewportWidth, viewportHeight float64,
) {
	libertyForEachCollisionCell(box, viewportWidth, viewportHeight, func(cell libertyCollisionCell) bool {
		occupied[cell] = append(occupied[cell], box)
		return true
	})
}

func libertyForEachCollisionCell(
	box libertyCollisionBox,
	viewportWidth, viewportHeight float64,
	visit func(libertyCollisionCell) bool,
) {
	left := int(math.Floor(max(0, box.left) / libertyCollisionCellSize))
	top := int(math.Floor(max(0, box.top) / libertyCollisionCellSize))
	right := int(math.Floor(min(viewportWidth, box.right) / libertyCollisionCellSize))
	bottom := int(math.Floor(min(viewportHeight, box.bottom) / libertyCollisionCellSize))
	for y := top; y <= bottom; y++ {
		for x := left; x <= right; x++ {
			if !visit(libertyCollisionCell{x: x, y: y}) {
				return
			}
		}
	}
}

func libertyCandidateCollisionBoxes(
	transform affineTransform,
	candidate libertySymbolCandidate,
	sdfLayout *sdfTextLayout,
	viewportWidth, viewportHeight float64,
) (libertyCollisionBox, bool, bool, libertyCollisionBox, bool, bool) {
	anchor := transform.mapPoint(candidate.anchor)
	textBox := libertyCollisionBox{}
	textPresent := candidate.text != "" && candidate.textColor.alpha > 0
	textVisible := false
	if textPresent && libertyTextRenderable(candidate.text, sdfLayout) {
		offsetX := candidate.textOffset.X * candidate.textSize
		offsetY := candidate.textOffset.Y * candidate.textSize
		if sdfLayout != nil {
			textBox = libertyRotatedCollisionBox(
				roadPoint{X: anchor.X + offsetX, Y: anchor.Y + offsetY},
				sdfLayout.bounds,
				libertyScreenSymbolAngle(transform, candidate.lineAngle, candidate.textRotate, candidate.viewportAligned),
				candidate.textPadding,
			)
		} else {
			lineCount := max(1, strings.Count(candidate.text, "\n")+1)
			characterCount := max(1, utf8.RuneCountInString(candidate.text))
			textWidth := float64(characterCount) * candidate.textSize * (0.58 + candidate.letterSpacing)
			maximumWidth := candidate.maximumWidth * candidate.textSize
			if maximumWidth > 0 && textWidth > maximumWidth {
				lineCount = max(lineCount, int(math.Ceil(textWidth/maximumWidth)))
				textWidth = maximumWidth
			}
			haloExtent := candidate.haloWidth + candidate.haloBlur
			textWidth += 2 * haloExtent
			textHeight := float64(lineCount)*candidate.textSize*candidate.lineHeight + 2*haloExtent
			x, y := libertyAnchoredOrigin(candidate.textAnchor, textWidth, textHeight)
			textBox = libertyRotatedCollisionBox(
				roadPoint{X: anchor.X + offsetX, Y: anchor.Y + offsetY},
				libertyCollisionBox{left: x, top: y, right: x + textWidth, bottom: y + textHeight},
				libertyScreenSymbolAngle(transform, candidate.lineAngle, candidate.textRotate, candidate.viewportAligned),
				candidate.textPadding,
			)
		}
		textVisible = libertyCollisionBoxVisible(textBox, viewportWidth, viewportHeight)
	}
	iconBox := libertyCollisionBox{}
	iconPresent := false
	iconVisible := false
	if candidate.iconName != "" {
		if err := loadLibertySprites(); err == nil {
			if entry, exists := libertySpriteIndex[candidate.iconName]; exists && entry.PixelRatio > 0 {
				iconPresent = true
				iconWidth := float64(entry.Width) / entry.PixelRatio * candidate.iconSize
				iconHeight := float64(entry.Height) / entry.PixelRatio * candidate.iconSize
				x, y := libertyAnchoredOrigin(candidate.iconAnchor, iconWidth, iconHeight)
				offsetX := candidate.iconOffset.X * candidate.iconSize
				offsetY := candidate.iconOffset.Y * candidate.iconSize
				iconBox = libertyRotatedCollisionBox(
					roadPoint{
						X: anchor.X + offsetX,
						Y: anchor.Y + offsetY,
					},
					libertyCollisionBox{left: x, top: y, right: x + iconWidth, bottom: y + iconHeight},
					libertyScreenSymbolAngle(transform, candidate.iconLineAngle, candidate.iconRotate, candidate.iconViewportAligned),
					candidate.iconPadding,
				)
				iconVisible = libertyCollisionBoxVisible(iconBox, viewportWidth, viewportHeight)
			}
		}
	}
	return textBox, textPresent, textVisible, iconBox, iconPresent, iconVisible
}

func libertyScreenSymbolAngle(transform affineTransform, lineAngle, rotate float64, viewportAligned bool) float64 {
	localAngle := libertyRenderedSymbolAngle(lineAngle, rotate, viewportAligned)
	if viewportAligned {
		return localAngle
	}
	directionX := transform.M11*math.Cos(localAngle) + transform.M12*math.Sin(localAngle)
	directionY := transform.M21*math.Cos(localAngle) + transform.M22*math.Sin(localAngle)
	return math.Atan2(directionY, directionX)
}

func libertyRotatedCollisionBox(origin roadPoint, box libertyCollisionBox, angle, padding float64) libertyCollisionBox {
	cosAngle := math.Cos(angle)
	sinAngle := math.Sin(angle)
	result := libertyCollisionBox{
		left:   math.Inf(1),
		top:    math.Inf(1),
		right:  math.Inf(-1),
		bottom: math.Inf(-1),
	}
	for _, point := range [...]roadPoint{
		{X: box.left, Y: box.top},
		{X: box.right, Y: box.top},
		{X: box.right, Y: box.bottom},
		{X: box.left, Y: box.bottom},
	} {
		x := origin.X + point.X*cosAngle - point.Y*sinAngle
		y := origin.Y + point.X*sinAngle + point.Y*cosAngle
		result.left = min(result.left, x)
		result.top = min(result.top, y)
		result.right = max(result.right, x)
		result.bottom = max(result.bottom, y)
	}
	return libertyPaddedCollisionBox(result.left, result.top, result.right, result.bottom, padding)
}

func libertyPaddedCollisionBox(left, top, right, bottom, padding float64) libertyCollisionBox {
	return libertyCollisionBox{
		left:   left - padding,
		top:    top - padding,
		right:  right + padding,
		bottom: bottom + padding,
	}
}

func libertyCollisionBoxVisible(box libertyCollisionBox, viewportWidth, viewportHeight float64) bool {
	return box.right >= 0 && box.bottom >= 0 && box.left <= viewportWidth && box.top <= viewportHeight
}

func libertyBoxesIntersect(first, second libertyCollisionBox) bool {
	return first.left < second.right && first.right > second.left &&
		first.top < second.bottom && first.bottom > second.top
}

func newLibertySymbolLayerTileNode(
	item *quick.QQuickItem,
	camera Camera,
	tile vectorTileID,
	wrap int,
	candidates []libertySymbolCandidate,
	order int,
	accepted map[libertySymbolKey]libertyAcceptedSymbol,
	sdfScene *sdfScene,
	sdfAtlas *quick.QSGSDFAtlas,
) (*quick.QSGTransformNode, []retainedLibertySymbolTransform) {
	if !libertyLayerHasAcceptedSymbol(tile, wrap, candidates, order, accepted) {
		return nil, nil
	}
	outer := quick.NewQSGTransformNode()
	if outer == nil {
		return nil, nil
	}
	setWrappedTileTransform(outer, camera, tile, wrap)
	transforms := make([]retainedLibertySymbolTransform, 0)
	start, end := libertySymbolRangeAtOrder(candidates, order)
	for index := start; index < end; index++ {
		candidate := candidates[index]
		placement, keep := accepted[libertySymbolKey{tile: tile, wrap: wrap, index: index}]
		if !keep {
			continue
		}
		if placement.icon && candidate.iconName != "" {
			sprite, exists := libertySprite(candidate.iconName, candidate.iconColor, candidate.iconOpacity)
			if exists {
				width := float64(sprite.width) / sprite.pixelRatio * candidate.iconSize
				height := float64(sprite.height) / sprite.pixelRatio * candidate.iconSize
				x, y := libertyAnchoredOrigin(candidate.iconAnchor, width, height)
				imageNode := quick.NewQSGRGBAImageNode(
					item,
					sprite.pixels,
					sprite.width,
					sprite.height,
					float32(x),
					float32(y),
					float32(width),
					float32(height),
					1,
				)
				if imageNode != nil {
					localAngle := libertyRenderedSymbolAngle(
						candidate.iconLineAngle,
						candidate.iconRotate,
						candidate.iconViewportAligned,
					)
					transform := newLibertyCounterTransform(
						camera,
						tile,
						candidate.anchor,
						roadPoint{X: candidate.iconOffset.X * candidate.iconSize, Y: candidate.iconOffset.Y * candidate.iconSize},
						localAngle,
						candidate.iconViewportAligned,
					)
					if transform == nil {
						imageNode.Delete()
					} else {
						transform.AppendChildNode(imageNode)
						outer.AppendChildNode(transform.QSGNode)
						transforms = append(transforms, retainedLibertySymbolTransform{
							node:          transform,
							tile:          tile,
							anchor:        candidate.anchor,
							offset:        roadPoint{X: candidate.iconOffset.X * candidate.iconSize, Y: candidate.iconOffset.Y * candidate.iconSize},
							localAngle:    localAngle,
							viewportAlign: candidate.iconViewportAligned,
						})
					}
				}
			}
		}
		if placement.text && candidate.text != "" && candidate.textColor.alpha > 0 {
			var textNode *quick.QSGNode
			usedSDF := false
			sdfEligible := sdfTextEligible(candidate.text)
			if sdfEligible && sdfScene != nil && sdfAtlas != nil {
				layout := sdfScene.layouts[libertySDFLayoutKey{tile: tile, index: index}]
				if layout != nil {
					textNode = sdfAtlas.NewTextNode(
						layout.vertices,
						libertyColorArray(candidate.textColor),
						libertyColorArray(candidate.haloColor),
						float32(layout.scale),
						float32(candidate.haloWidth),
						float32(candidate.haloBlur),
					)
					usedSDF = textNode != nil
				}
			}
			if textNode == nil && !sdfEligible && qtTextFallbackEligible(candidate.text) {
				horizontalAnchor, verticalAnchor := libertyTextAnchors(candidate.textAnchor)
				textNode = quick.NewQSGTextNode(
					item,
					candidate.text,
					libertyDesktopFont(candidate.fontFamily),
					float32(candidate.textSize),
					float32(candidate.letterSpacing*candidate.textSize),
					float32(candidate.lineHeight),
					float32(candidate.maximumWidth*candidate.textSize),
					libertyColorArray(candidate.textColor),
					libertyColorArray(candidate.haloColor),
					float32(candidate.haloWidth),
					horizontalAnchor,
					verticalAnchor,
				)
			}
			if textNode != nil {
				offset := roadPoint{X: candidate.textOffset.X * candidate.textSize, Y: candidate.textOffset.Y * candidate.textSize}
				localAngle := libertyRenderedSymbolAngle(candidate.lineAngle, candidate.textRotate, candidate.viewportAligned)
				transform := newLibertyCounterTransform(
					camera,
					tile,
					candidate.anchor,
					offset,
					localAngle,
					candidate.viewportAligned,
				)
				if transform == nil {
					textNode.Delete()
				} else {
					transform.AppendChildNode(textNode)
					outer.AppendChildNode(transform.QSGNode)
					if usedSDF {
						layoutKey := libertySDFLayoutKey{tile: tile, index: index}
						if _, counted := sdfScene.rendered[layoutKey]; !counted {
							sdfScene.rendered[layoutKey] = struct{}{}
							sdfScene.renderedLabels++
						}
					}
					transforms = append(transforms, retainedLibertySymbolTransform{
						node:          transform,
						tile:          tile,
						anchor:        candidate.anchor,
						offset:        offset,
						localAngle:    localAngle,
						viewportAlign: candidate.viewportAligned,
					})
				}
			}
		}
	}
	if len(transforms) == 0 {
		outer.Delete()
		return nil, nil
	}
	return outer, transforms
}

func libertyLayerHasAcceptedSymbol(
	tile vectorTileID,
	wrap int,
	candidates []libertySymbolCandidate,
	order int,
	accepted map[libertySymbolKey]libertyAcceptedSymbol,
) bool {
	start, end := libertySymbolRangeAtOrder(candidates, order)
	for index := start; index < end; index++ {
		if _, keep := accepted[libertySymbolKey{tile: tile, wrap: wrap, index: index}]; keep {
			return true
		}
	}
	return false
}

func libertySymbolRangeAtOrder(candidates []libertySymbolCandidate, order int) (int, int) {
	start := sort.Search(len(candidates), func(index int) bool { return candidates[index].order >= order })
	end := start + sort.Search(len(candidates)-start, func(index int) bool {
		return candidates[start+index].order > order
	})
	return start, end
}

func newLibertyCounterTransform(
	camera Camera,
	tile vectorTileID,
	anchor, offset roadPoint,
	localAngle float64,
	viewportAlign bool,
) *quick.QSGTransformNode {
	node := quick.NewQSGTransformNode()
	if node == nil {
		return nil
	}
	if !setLibertyCounterTransform(node, camera, tile, anchor, offset, localAngle, viewportAlign) {
		node.Delete()
		return nil
	}
	return node
}

func libertyRenderedSymbolAngle(lineAngle, rotate float64, viewportAlign bool) float64 {
	if viewportAlign {
		return rotate
	}
	return lineAngle + rotate
}

func setLibertyCounterTransform(
	node *quick.QSGTransformNode,
	camera Camera,
	tile vectorTileID,
	anchor, offset roadPoint,
	localAngle float64,
	viewportAlign bool,
) bool {
	if node == nil {
		return false
	}
	transform := roadCameraTransform(camera, tile)
	determinant := transform.M11*transform.M22 - transform.M12*transform.M21
	if math.Abs(determinant) <= polygonEpsilon {
		return false
	}
	inverse11 := transform.M22 / determinant
	inverse12 := -transform.M12 / determinant
	inverse21 := -transform.M21 / determinant
	inverse22 := transform.M11 / determinant
	angle := localAngle
	if !viewportAlign {
		directionX := transform.M11*math.Cos(localAngle) + transform.M12*math.Sin(localAngle)
		directionY := transform.M21*math.Cos(localAngle) + transform.M22*math.Sin(localAngle)
		angle = math.Atan2(directionY, directionX)
	}
	cosAngle := math.Cos(angle)
	sinAngle := math.Sin(angle)
	m11 := inverse11*cosAngle + inverse12*sinAngle
	m12 := -inverse11*sinAngle + inverse12*cosAngle
	m21 := inverse21*cosAngle + inverse22*sinAngle
	m22 := -inverse21*sinAngle + inverse22*cosAngle
	dx := anchor.X + inverse11*offset.X + inverse12*offset.Y
	dy := anchor.Y + inverse21*offset.X + inverse22*offset.Y
	node.SetAffine(float32(m11), float32(m12), float32(m21), float32(m22), float32(dx), float32(dy))
	return true
}

func libertyAnchoredOrigin(anchor string, width, height float64) (float64, float64) {
	x := -width / 2
	y := -height / 2
	if strings.Contains(anchor, "left") {
		x = 0
	} else if strings.Contains(anchor, "right") {
		x = -width
	}
	if strings.Contains(anchor, "top") {
		y = 0
	} else if strings.Contains(anchor, "bottom") {
		y = -height
	}
	return x, y
}

func libertyTextAnchors(anchor string) (quick.TextAnchor, quick.TextAnchor) {
	horizontal := quick.TextAnchorCenter
	vertical := quick.TextAnchorCenter
	if strings.Contains(anchor, "left") {
		horizontal = quick.TextAnchorLeading
	} else if strings.Contains(anchor, "right") {
		horizontal = quick.TextAnchorTrailing
	}
	if strings.Contains(anchor, "top") {
		vertical = quick.TextAnchorLeading
	} else if strings.Contains(anchor, "bottom") {
		vertical = quick.TextAnchorTrailing
	}
	return horizontal, vertical
}

func libertyColorArray(color mapColor) [4]int {
	return [4]int{color.red, color.green, color.blue, color.alpha}
}
