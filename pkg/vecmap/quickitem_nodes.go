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
	if len(bucket.raster.rgba) == 0 || !layer.VisibleAt(zoom) || libertyLayerHidden(layer) {
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
) (*quick.QSGTransformNode, bool) {
	matching := libertyPrimitivesAtOrder(primitives, order)
	if len(matching) == 0 {
		return nil, true
	}
	transformNode := quick.NewQSGTransformNode()
	if transformNode == nil {
		return nil, false
	}
	clipNode := quick.NewQSGClipNode()
	if clipNode == nil {
		transformNode.Delete()
		return nil, false
	}
	clipNode.SetRect(0, 0, tileSize, tileSize)
	transformNode.AppendChildNode(clipNode.QSGNode)
	complete := true
	for _, primitive := range matching {
		if primitive.patternName != "" {
			sprite, exists := libertySprite(primitive.patternName, mapColor{Red: 255, Green: 255, Blue: 255, Alpha: 255}, 1)
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
			} else {
				complete = false
			}
			continue
		}
		if !appendGeometryNode(
			clipNode.QSGNode,
			primitive.triangles,
			quick.QSGGeometry__DrawTriangles,
			primitive.color,
		) {
			complete = false
		}
	}
	if !complete {
		transformNode.Delete()
		return nil, false
	}
	setWrappedTileTransform(transformNode, camera, tile, wrap)
	return transformNode, true
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
	mapBackgroundColor = mapColor{Red: 20, Green: 27, Blue: 30, Alpha: 255}
	mapLandColor       = mapColor{Red: 42, Green: 57, Blue: 51, Alpha: 255}
	mapWaterColor      = mapColor{Red: 28, Green: 72, Blue: 91, Alpha: 255}
	mapRoadColor       = mapColor{Red: 121, Green: 220, Blue: 255, Alpha: 255}
)

func appendGeometryNode(parent *quick.QSGNode, points []roadPoint, mode quick.QSGGeometry__DrawingMode, colorValue mapColor) bool {
	if len(points) == 0 {
		return true
	}
	if mode == quick.QSGGeometry__DrawTriangles && len(points)%3 != 0 ||
		mode == quick.QSGGeometry__DrawLines && len(points)%2 != 0 {
		return false
	}
	geometry := quick.NewQSGPointGeometry(pointVertices(points))
	if geometry == nil {
		return false
	}
	geometry.SetDrawingMode(uint(mode))

	material := quick.NewQSGFlatColorMaterial()
	if material == nil {
		geometry.Delete()
		return false
	}
	color := qt.NewQColor11(colorValue.Red, colorValue.Green, colorValue.Blue, colorValue.Alpha)
	material.SetColor(color)
	color.Delete()

	node := quick.NewQSGGeometryNode()
	if node == nil {
		geometry.Delete()
		material.Delete()
		return false
	}
	node.SetGeometry(geometry)
	node.SetFlag(quick.QSGNode__OwnsGeometry)
	node.SetMaterial(material.QSGMaterial)
	node.SetFlag(quick.QSGNode__OwnsMaterial)
	parent.AppendChildNode(node.QSGNode)
	return true
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
