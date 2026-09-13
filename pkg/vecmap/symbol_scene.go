package vecmap

import (
	"math"
	"sort"
	"strings"

	quick "github.com/rubiojr/whereami/internal/miqtquick"
	"github.com/rubiojr/whereami/pkg/vecmap/placement"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
)

const (
	libertyCollisionZoomStep    = 1.0 / 8.0
	libertyCollisionBearingStep = 2.0
	libertyCollisionPanStep     = 64.0
	maxViewportSymbolReferences = placement.MaxCollisionReferences
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

type libertySymbolReference = placement.CollisionReference[libertySymbolKey]
type libertyAcceptedSymbol = placement.Accepted

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
				projected := projectLibertySymbol(transform, candidate, layout, camera.Width, camera.Height)
				if !projected.Text.Visible && !projected.Icon.Visible {
					continue
				}
				references = append(references, libertySymbolReference{
					Key:   libertySymbolKey{tile: tile.id, wrap: wrap, index: index},
					Order: candidate.order, SortKey: candidate.sortKey,
					Text: projected.Text,
					Icon: projected.Icon,
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
	accepted, err := placement.SelectSymbols(references, placement.CollisionOptions{Width: camera.Width, Height: camera.Height})
	if err != nil {
		reportVectorWarning("vecmap symbol collision preparation failed: %v", err)
		return nil
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

func libertyCandidateCollisionBoxes(
	transform affineTransform,
	candidate libertySymbolCandidate,
	sdfLayout *sdfTextLayout,
	viewportWidth, viewportHeight float64,
) (libertyCollisionBox, bool, bool, libertyCollisionBox, bool, bool) {
	projected := projectLibertySymbol(transform, &candidate, sdfLayout, viewportWidth, viewportHeight)
	return legacyCollisionBox(projected.Text.Box), projected.Text.Present, projected.Text.Visible,
		legacyCollisionBox(projected.Icon.Box), projected.Icon.Present, projected.Icon.Visible
}

func projectLibertySymbol(transform affineTransform, candidate *libertySymbolCandidate, sdfLayout *sdfTextLayout, width, height float64) placement.ProjectedSymbol {
	context := placement.ProjectionContext{Transform: view.Affine(transform), Width: width, Height: height,
		TextReady: candidate.text != "" && candidate.textColor.Alpha > 0 && libertyTextRenderable(candidate.text, sdfLayout)}
	var bounds placement.Box
	if sdfLayout != nil {
		bounds = placement.Box{Left: sdfLayout.bounds.left, Top: sdfLayout.bounds.top, Right: sdfLayout.bounds.right, Bottom: sdfLayout.bounds.bottom}
		context.TextBounds = &bounds
	}
	var sprite placement.SpriteMetrics
	if candidate.iconName != "" {
		if err := loadLibertySprites(); err == nil {
			if entry, exists := libertySpriteIndex[candidate.iconName]; exists && entry.PixelRatio > 0 {
				sprite = placement.SpriteMetrics{Width: entry.Width, Height: entry.Height, PixelRatio: entry.PixelRatio}
				context.Sprite = &sprite
			}
		}
	}
	symbol := projectionSymbol(candidate)
	return placement.ProjectSymbol(&symbol, context)
}

func legacyCollisionBox(box placement.Box) libertyCollisionBox {
	return libertyCollisionBox{left: box.Left, top: box.Top, right: box.Right, bottom: box.Bottom}
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
		if placement.Icon && candidate.iconName != "" {
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
		if placement.Text && candidate.text != "" && candidate.textColor.Alpha > 0 {
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
	return placement.RenderedSymbolAngle(lineAngle, rotate, viewportAlign)
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
	return placement.AnchoredOrigin(anchor, width, height)
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
	return [4]int{color.Red, color.Green, color.Blue, color.Alpha}
}
