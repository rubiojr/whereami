package vecmap

import (
	"math"
	"slices"

	quick "github.com/rubiojr/whereami/internal/miqtquick"
)

type retainedTileTransform struct {
	node *quick.QSGTransformNode
	wrap int
}

func (i *Item) updatePaintNode(
	_ func(*quick.QSGNode, *quick.QQuickItem__UpdatePaintNodeData) *quick.QSGNode,
	oldNode *quick.QSGNode,
	_ *quick.QQuickItem__UpdatePaintNodeData,
) *quick.QSGNode {
	if i.closed.Load() {
		return oldNode
	}
	i.paintNodeUpdates.Add(1)
	quickItem := i.quickItem
	cameraController := i.camera
	if quickItem == nil || cameraController == nil {
		return oldNode
	}
	width := quickItem.Width()
	height := quickItem.Height()
	snapshot := cameraController.current()
	camera := snapshot.Camera.WithViewport(width, height)
	i.requestTiles(camera)
	tiles := i.tiles.Load()
	if tiles == nil {
		return oldNode
	}
	createdRoot := false
	if oldNode == nil {
		oldNode = quick.NewQSGNode()
		if oldNode == nil {
			return nil
		}
		// Qt owns and has already destroyed nodes from the invalidated scene graph.
		i.resetRetainedSceneGraph()
		createdRoot = true
	}

	styleZoom := math.Round(camera.Zoom*16) / 16
	i.requestStyledTiles(tiles, styleZoom)
	styled := i.styledTiles.Load()
	scene := i.renderedScene
	sceneChanged := false
	if styled != nil && styled.tileRevision == tiles.contentRevision && styled.styleZoom == styleZoom && styled != scene {
		scene = styled
		sceneChanged = true
	}
	viewportChanged := width != i.lastWidth || height != i.lastHeight
	worldWraps := libertyWorldWraps(camera)
	worldWrapsChanged := !slices.Equal(worldWraps, i.lastWorldWraps)
	glyphRevision := i.glyphs.currentRevision()
	glyphsChanged := glyphRevision != i.lastGlyphRevision
	reconciledScene := false
	if scene != nil && libertySceneNeedsFullReconcile(
		sceneChanged,
		createdRoot,
		viewportChanged,
		worldWrapsChanged,
		glyphsChanged,
		i.sdfAtlasRetry,
	) {
		tilesChanged := scene.tileRevision != i.lastTileRevision
		styleZoomChanged := scene.styleZoom != i.lastStyleZoom
		consumedGlyphRevision := i.reconcileTileNodes(oldNode, camera, scene)
		reconciledScene = true
		if !createdRoot && !tilesChanged && (styleZoomChanged || worldWrapsChanged) {
			i.geometryUpdates.Add(1)
		}
		i.lastTileRevision = scene.tileRevision
		i.lastStyleZoom = scene.styleZoom
		i.renderedScene = scene
		i.lastWorldWraps = append(i.lastWorldWraps[:0], worldWraps...)
		i.lastGlyphRevision = consumedGlyphRevision
	} else if scene != nil && (glyphsChanged || i.sdfAtlasRetry) {
		i.lastGlyphRevision = i.reconcileLibertyGlyphNodes(oldNode, camera, scene)
		reconciledScene = true
	}

	cameraChanged := viewportChanged || snapshot.Revision != i.lastCameraRevision
	if cameraChanged && !createdRoot {
		if !reconciledScene && (viewportChanged || i.libertySymbolPlacementChanged(camera)) {
			i.reconcileLibertySymbolNodes(camera, scene)
		}
		updatedTransforms := 0
		for tile, nodes := range i.tileNodes {
			for _, retained := range nodes {
				setWrappedTileTransform(retained.node, camera, tile, retained.wrap)
				updatedTransforms++
			}
		}
		for layerIndex := range i.symbolLayers {
			for tileIndex := range i.symbolLayers[layerIndex].tiles {
				tile := &i.symbolLayers[layerIndex].tiles[tileIndex]
				setWrappedTileTransform(tile.node, camera, tile.tile, tile.wrap)
				updatedTransforms++
			}
		}
		if camera.Zoom != i.lastCameraZoom || camera.Bearing != i.lastCameraBearing {
			for index := range i.symbolTransforms {
				symbol := &i.symbolTransforms[index]
				setLibertyCounterTransform(
					symbol.node,
					camera,
					symbol.tile,
					symbol.anchor,
					symbol.offset,
					symbol.localAngle,
					symbol.viewportAlign,
				)
			}
		}
		i.transformUpdates.Add(uint64(updatedTransforms))
	}
	if cameraChanged || createdRoot {
		i.lastWidth = width
		i.lastHeight = height
		i.lastCameraRevision = snapshot.Revision
		i.lastCameraZoom = camera.Zoom
		i.lastCameraBearing = camera.Bearing
	}
	return oldNode
}

func libertySceneNeedsFullReconcile(
	sceneChanged, createdRoot, viewportChanged bool,
	worldWrapsChanged, glyphsChanged, sdfAtlasRetry bool,
) bool {
	_, _, _ = viewportChanged, glyphsChanged, sdfAtlasRetry
	return sceneChanged || createdRoot || worldWrapsChanged
}

func (i *Item) requestStyledTiles(tiles *roadTileSnapshot, styleZoom float64) {
	if i.styleCompiler == nil || (i.hasStyleRequest &&
		i.lastStyleRequestRevision == tiles.contentRevision && i.lastStyleRequestZoom == styleZoom) {
		return
	}
	i.lastStyleRequestRevision = tiles.contentRevision
	i.lastStyleRequestZoom = styleZoom
	i.hasStyleRequest = true
	i.styleCompiler.request(tiles.contentRevision, styleZoom, tiles.tiles)
}

func (i *Item) resetRetainedSceneGraph() {
	i.clearRetainedSceneGraph()
	i.tileNodes = make(map[vectorTileID][]retainedTileTransform)
	i.retainedTiles = make(map[vectorTileID]struct{})
}

func (i *Item) clearRetainedSceneGraph() {
	i.sceneNodes = nil
	i.symbolTransforms = nil
	i.symbolLayers = nil
	i.acceptedSymbols = nil
	i.renderedSDFScene = nil
	i.hasSymbolPlacement = false
	i.tileNodes = nil
	i.retainedTiles = nil
	i.lastWorldWraps = nil
	i.sdfAtlas = nil
	i.sdfAtlasGeneration = 0
	i.sdfAtlasRetry = false
	i.sdfAtlasRetryAttempts = 0
}

func (i *Item) reconcileTileNodes(root *quick.QSGNode, camera Camera, scene *libertySceneSnapshot) uint64 {
	tiles := scene.tiles
	consumedGlyphRevision := i.glyphs.currentRevision()
	desired := make(map[vectorTileID]*tileBucket, len(tiles))
	for _, tile := range tiles {
		if tile.roads != nil {
			desired[tile.id] = tile.roads
		}
	}
	for tile := range i.retainedTiles {
		if _, keep := desired[tile]; !keep {
			i.geometryRemovals.Add(1)
		}
	}
	for tile := range desired {
		if _, exists := i.retainedTiles[tile]; !exists {
			i.geometryBuilds.Add(1)
		}
	}
	for _, node := range i.sceneNodes {
		root.RemoveChildNode(node)
		node.Delete()
	}
	i.sceneNodes = i.sceneNodes[:0]
	i.tileNodes = make(map[vectorTileID][]retainedTileTransform, len(desired))
	i.symbolTransforms = i.symbolTransforms[:0]
	i.symbolLayers = i.symbolLayers[:0]
	i.acceptedSymbols = nil
	i.renderedSDFScene = nil
	i.hasSymbolPlacement = false
	i.retainedTiles = make(map[vectorTileID]struct{}, len(desired))
	for tile := range desired {
		i.retainedTiles[tile] = struct{}{}
	}

	if hasLibertyPrimitives(tiles) {
		return i.appendLibertyScene(root, camera, scene)
	}
	i.discardLibertySDFAtlas(root)
	wraps := libertyWorldWraps(camera)
	for _, tile := range tiles {
		if tile.roads == nil {
			continue
		}
		for _, wrap := range wraps {
			node := newTileNodeWrapped(camera, tile.roads, wrap)
			if node == nil {
				continue
			}
			root.AppendChildNode(node.QSGNode)
			i.sceneNodes = append(i.sceneNodes, node.QSGNode)
			i.tileNodes[tile.id] = append(i.tileNodes[tile.id], retainedTileTransform{node: node, wrap: wrap})
		}
	}
	return consumedGlyphRevision
}

func hasLibertyPrimitives(tiles []loadedRoadTile) bool {
	for _, tile := range tiles {
		if tile.roads != nil && (len(tile.roads.liberty) > 0 || len(tile.roads.symbols) > 0 ||
			len(tile.roads.raster.rgba) > 0 || tile.roads.sourceLayers != nil) {
			return true
		}
	}
	return false
}
