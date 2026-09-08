package vecmap

import (
	"sort"

	qt "github.com/mappu/miqt/qt6"
	quick "github.com/rubiojr/whereami/internal/miqtquick"
)

func newLibertyRasterTileNode(
	item *quick.QQuickItem,
	camera Camera,
	tile vectorTileID,
	wrap int,
	bucket *tileBucket,
	layer compiledLibertyLayer,
	zoom float64,
) *quick.QSGTransformNode {
	if len(bucket.raster.rgba) == 0 || !layer.visibleAt(zoom) || libertyLayerHidden(layer) {
		return nil
	}
	evaluation := libertyEvaluation{zoom: zoom}
	opacity := libertyEvaluatedNumber(layer, "raster-opacity", evaluation, 1)
	imageNode := quick.NewQSGRGBAImageNode(
		item,
		bucket.raster.rgba,
		bucket.raster.width,
		bucket.raster.height,
		0,
		0,
		tileSize,
		tileSize,
		float32(opacity),
	)
	if imageNode == nil {
		return nil
	}
	transformNode := quick.NewQSGTransformNode()
	if transformNode == nil {
		imageNode.Delete()
		return nil
	}
	clipNode := quick.NewQSGClipNode()
	if clipNode == nil {
		imageNode.Delete()
		transformNode.Delete()
		return nil
	}
	clipNode.SetRect(0, 0, tileSize, tileSize)
	clipNode.AppendChildNode(imageNode)
	transformNode.AppendChildNode(clipNode.QSGNode)
	setWrappedTileTransform(transformNode, camera, tile, wrap)
	return transformNode
}

func newLibertyLayerTileNode(
	item *quick.QQuickItem,
	camera Camera,
	tile vectorTileID,
	wrap int,
	primitives []libertyRenderPrimitive,
	order int,
) *quick.QSGTransformNode {
	matching := libertyPrimitivesAtOrder(primitives, order)
	if len(matching) == 0 {
		return nil
	}
	transformNode := quick.NewQSGTransformNode()
	if transformNode == nil {
		return nil
	}
	clipNode := quick.NewQSGClipNode()
	if clipNode == nil {
		transformNode.Delete()
		return nil
	}
	clipNode.SetRect(0, 0, tileSize, tileSize)
	transformNode.AppendChildNode(clipNode.QSGNode)
	for _, primitive := range matching {
		if primitive.patternName != "" {
			sprite, exists := libertySprite(primitive.patternName, mapColor{red: 255, green: 255, blue: 255, alpha: 255}, 1)
			if !exists {
				continue
			}
			patternWidth := float64(sprite.width) / sprite.pixelRatio * primitive.patternScale
			patternHeight := float64(sprite.height) / sprite.pixelRatio * primitive.patternScale
			phaseX, phaseY := libertyPatternPhase(tile, wrap, patternWidth, patternHeight)
			patternNode := quick.NewQSGPatternNode(
				item,
				pointVertices(primitive.triangles),
				sprite.pixels,
				sprite.width,
				sprite.height,
				float32(patternWidth),
				float32(patternHeight),
				float32(phaseX),
				float32(phaseY),
				float32(primitive.opacity),
			)
			if patternNode != nil {
				clipNode.AppendChildNode(patternNode)
			}
			continue
		}
		appendGeometryNode(
			clipNode.QSGNode,
			primitive.triangles,
			quick.QSGGeometry__DrawTriangles,
			primitive.color,
		)
	}
	setWrappedTileTransform(transformNode, camera, tile, wrap)
	return transformNode
}

func libertyPrimitivesAtOrder(primitives []libertyRenderPrimitive, order int) []libertyRenderPrimitive {
	start := sort.Search(len(primitives), func(index int) bool { return primitives[index].order >= order })
	end := start + sort.Search(len(primitives)-start, func(index int) bool {
		return primitives[start+index].order > order
	})
	return primitives[start:end]
}

func newTileNodeWrapped(camera Camera, bucket *tileBucket, wrap int) *quick.QSGTransformNode {
	transformNode := quick.NewQSGTransformNode()
	if transformNode == nil {
		return nil
	}
	clipNode := quick.NewQSGClipNode()
	if clipNode == nil {
		transformNode.Delete()
		return nil
	}
	clipNode.SetRect(0, 0, tileSize, tileSize)
	transformNode.AppendChildNode(clipNode.QSGNode)

	appendTileContents(clipNode.QSGNode, bucket)
	setWrappedTileTransform(transformNode, camera, bucket.tile, wrap)
	return transformNode
}

func appendTileContents(parent *quick.QSGNode, bucket *tileBucket) {
	appendGeometryNode(parent, backgroundTriangles(), quick.QSGGeometry__DrawTriangles, mapBackgroundColor)
	appendGeometryNode(parent, bucket.land.triangles, quick.QSGGeometry__DrawTriangles, mapLandColor)
	appendGeometryNode(parent, bucket.land.cutouts, quick.QSGGeometry__DrawTriangles, mapBackgroundColor)
	appendGeometryNode(parent, bucket.water.triangles, quick.QSGGeometry__DrawTriangles, mapWaterColor)
	appendGeometryNode(parent, bucket.water.cutouts, quick.QSGGeometry__DrawTriangles, mapLandColor)
	appendGeometryNode(parent, segmentPoints(bucket.segments), quick.QSGGeometry__DrawLines, mapRoadColor)
}

var (
	mapBackgroundColor = mapColor{red: 20, green: 27, blue: 30, alpha: 255}
	mapLandColor       = mapColor{red: 42, green: 57, blue: 51, alpha: 255}
	mapWaterColor      = mapColor{red: 28, green: 72, blue: 91, alpha: 255}
	mapRoadColor       = mapColor{red: 121, green: 220, blue: 255, alpha: 255}
)

func appendGeometryNode(parent *quick.QSGNode, points []roadPoint, mode quick.QSGGeometry__DrawingMode, colorValue mapColor) {
	if len(points) == 0 {
		return
	}
	if mode == quick.QSGGeometry__DrawTriangles && len(points)%3 != 0 ||
		mode == quick.QSGGeometry__DrawLines && len(points)%2 != 0 {
		return
	}
	geometry := quick.NewQSGPointGeometry(pointVertices(points))
	if geometry == nil {
		return
	}
	geometry.SetDrawingMode(uint(mode))

	material := quick.NewQSGFlatColorMaterial()
	color := qt.NewQColor11(colorValue.red, colorValue.green, colorValue.blue, colorValue.alpha)
	material.SetColor(color)
	color.Delete()

	node := quick.NewQSGGeometryNode()
	node.SetGeometry(geometry)
	node.SetFlag(quick.QSGNode__OwnsGeometry)
	node.SetMaterial(material.QSGMaterial)
	node.SetFlag(quick.QSGNode__OwnsMaterial)
	parent.AppendChildNode(node.QSGNode)
}

func setWrappedTileTransform(node *quick.QSGTransformNode, camera Camera, tile vectorTileID, wrap int) {
	if node == nil {
		return
	}
	transform := wrappedRoadCameraTransform(camera, tile, wrap)
	node.SetAffine(
		float32(transform.M11),
		float32(transform.M12),
		float32(transform.M21),
		float32(transform.M22),
		float32(transform.DX),
		float32(transform.DY),
	)
}
