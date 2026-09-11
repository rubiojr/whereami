package vecmap

import (
	"math"

	quick "github.com/rubiojr/whereami/internal/miqtquick"
)

func (i *Item) appendLibertyScene(root *quick.QSGNode, camera Camera, scene *libertySceneSnapshot) uint64 {
	styleZoom := scene.styleZoom
	tiles := scene.tiles
	if i.sdfAtlas != nil && i.sdfAtlas.QSGNode() != nil {
		root.RemoveChildNode(i.sdfAtlas.QSGNode())
	}
	defer func() {
		if i.sdfAtlas != nil && i.sdfAtlas.QSGNode() != nil {
			root.AppendChildNode(i.sdfAtlas.QSGNode())
		}
	}()
	layers, err := compiledLibertyLayers()
	if err != nil {
		i.sdfLabels.Store(0)
		i.sdfAtlasGlyphs.Store(0)
		return i.glyphs.currentRevision()
	}
	glyphRevision, sdfScene, activeSDFAtlas, acceptedSymbols := i.prepareLibertySymbols(camera, scene)
	wraps := libertyWorldWraps(camera)
	failedTiles := make(map[vectorTileID]struct{})
	layerRootFailure := false
	for _, layer := range layers {
		layerRoot := quick.NewQSGNode()
		if layerRoot == nil {
			layerRootFailure = true
			for _, tile := range tiles {
				failedTiles[tile.id] = struct{}{}
			}
			continue
		}
		root.AppendChildNode(layerRoot)
		i.sceneNodes = append(i.sceneNodes, layerRoot)
		if layer.kind == "symbol" {
			symbolLayer := retainedLibertySymbolLayer{root: layerRoot, order: layer.order}
			i.appendLibertySymbolLayerNodes(&symbolLayer, camera, tiles, wraps, acceptedSymbols, sdfScene, activeSDFAtlas)
			i.symbolLayers = append(i.symbolLayers, symbolLayer)
			continue
		}
		retainedLayer := retainedLibertyBasemapLayer{
			layer: layer,
			root:  layerRoot,
			nodes: make(map[libertyLayerTileKey]*quick.QSGTransformNode),
		}
		for _, tile := range tiles {
			if tile.roads == nil {
				continue
			}
			for _, wrap := range wraps {
				node, complete := i.newLibertyBasemapTileNode(camera, tile, wrap, layer, styleZoom)
				if !complete {
					failedTiles[tile.id] = struct{}{}
				}
				if node == nil {
					continue
				}
				key := libertyLayerTileKey{tile: tile.id, wrap: wrap}
				layerRoot.AppendChildNode(node.QSGNode)
				retainedLayer.nodes[key] = node
				retainedLayer.order = append(retainedLayer.order, key)
				i.basemapNodeBuilds.Add(1)
				i.tileNodes[tile.id] = append(i.tileNodes[tile.id], retainedTileTransform{node: node, wrap: wrap})
			}
		}
		i.basemapLayers = append(i.basemapLayers, retainedLayer)
	}
	i.retainedLiberty = !layerRootFailure
	if i.recordBasemapFailures(failedTiles, layerRootFailure) {
		for tile := range failedTiles {
			delete(i.retainedTiles, tile)
		}
	}
	i.recordSDFStats(sdfScene, activeSDFAtlas)
	return glyphRevision
}

func (i *Item) newLibertyBasemapTileNode(
	camera Camera,
	tile loadedRoadTile,
	wrap int,
	layer compiledLibertyLayer,
	styleZoom float64,
) (*quick.QSGTransformNode, bool) {
	if layer.kind == "raster" {
		if len(tile.roads.raster.rgba) == 0 || !layer.visibleAt(styleZoom) || libertyLayerHidden(layer) {
			return nil, true
		}
		node := newLibertyRasterTileNode(i.quickItem, camera, tile.id, wrap, tile.roads, layer, styleZoom)
		return node, node != nil
	}
	return newLibertyLayerTileNode(i.quickItem, camera, tile.id, wrap, tile.roads.liberty, layer.order)
}

func (i *Item) reconcileLibertyTiles(
	root *quick.QSGNode,
	camera Camera,
	scene *libertySceneSnapshot,
) uint64 {
	desiredTiles := make(map[vectorTileID]loadedRoadTile, len(scene.tiles))
	changed := make(map[vectorTileID]struct{})
	failedTiles := make(map[vectorTileID]struct{})
	for _, tile := range scene.tiles {
		if tile.roads == nil {
			continue
		}
		desiredTiles[tile.id] = tile
		if i.retainedTiles[tile.id] != tile.contentIdentity() {
			changed[tile.id] = struct{}{}
		}
	}
	for tile := range i.retainedTiles {
		if _, exists := desiredTiles[tile]; !exists {
			changed[tile] = struct{}{}
		}
	}
	wraps := libertyWorldWraps(camera)
	i.tileNodes = make(map[vectorTileID][]retainedTileTransform, len(desiredTiles))
	for layerIndex := range i.basemapLayers {
		layer := &i.basemapLayers[layerIndex]
		for key, node := range layer.nodes {
			if _, replace := changed[key.tile]; !replace {
				continue
			}
			layer.root.RemoveChildNode(node.QSGNode)
			node.Delete()
			delete(layer.nodes, key)
			i.basemapNodeRemovals.Add(1)
		}
		for _, key := range layer.order {
			if node := layer.nodes[key]; node != nil {
				layer.root.RemoveChildNode(node.QSGNode)
			}
		}
		for _, tile := range scene.tiles {
			if _, rebuild := changed[tile.id]; !rebuild || tile.roads == nil {
				continue
			}
			for _, wrap := range wraps {
				node, complete := i.newLibertyBasemapTileNode(camera, tile, wrap, layer.layer, scene.styleZoom)
				if !complete {
					failedTiles[tile.id] = struct{}{}
				}
				if node != nil {
					layer.nodes[libertyLayerTileKey{tile: tile.id, wrap: wrap}] = node
					i.basemapNodeBuilds.Add(1)
				}
			}
		}
		layer.order = layer.order[:0]
		for _, tile := range scene.tiles {
			for _, wrap := range wraps {
				key := libertyLayerTileKey{tile: tile.id, wrap: wrap}
				node := layer.nodes[key]
				if node == nil {
					continue
				}
				layer.root.AppendChildNode(node.QSGNode)
				layer.order = append(layer.order, key)
				i.tileNodes[tile.id] = append(i.tileNodes[tile.id], retainedTileTransform{node: node, wrap: wrap})
			}
		}
	}
	retryFailures := i.recordBasemapFailures(failedTiles, false)
	i.retainedTiles = make(map[vectorTileID]*tileBucket, len(desiredTiles))
	for tile, loaded := range desiredTiles {
		if _, failed := failedTiles[tile]; failed && retryFailures {
			continue
		}
		i.retainedTiles[tile] = loaded.contentIdentity()
	}
	return i.reconcileLibertyGlyphNodes(root, camera, scene)
}

func (i *Item) recordBasemapFailures(failed map[vectorTileID]struct{}, layerRootFailure bool) bool {
	if len(failed) == 0 && !layerRootFailure {
		i.basemapRetry = false
		i.basemapRetryAttempts = 0
		return false
	}
	if i.basemapRetryAttempts == 0 {
		i.basemapRetry = true
		i.basemapRetryAttempts = 1
		queueQuickItemUpdate(i.quickItem)
		return true
	}
	i.basemapRetry = false
	i.basemapRetryAttempts = 0
	return false
}

func (i *Item) prepareLibertySymbols(
	camera Camera,
	scene *libertySceneSnapshot,
) (uint64, *sdfScene, *quick.QSGSDFAtlas, map[libertySymbolKey]libertyAcceptedSymbol) {
	tiles := scene.tiles
	i.sdfAtlasRetry = false
	glyphRevision := i.glyphs.currentRevision()
	if i.sdfLayoutScene != scene || i.sdfLayoutGlyphRevision != glyphRevision {
		i.sdfLayouts, i.sdfLayoutHasCandidates = prepareSDFLayouts(tiles, i.glyphs)
		i.sdfLayoutGlyphs = sdfLayoutGlyphs(i.sdfLayouts)
		i.sdfLayoutGlyphKeys = sdfGlyphKeySet(i.sdfLayoutGlyphs)
		i.sdfLayoutScene = scene
		i.sdfLayoutGlyphRevision = glyphRevision
	}
	sdfLayouts := i.sdfLayouts
	glyphs := i.sdfLayoutGlyphs
	glyphKeys := i.sdfLayoutGlyphKeys
	if !sameSDFGlyphKeys(glyphKeys, i.sdfGlyphAtlasKeys) {
		if sdfAtlasNeedsRebuild(i.sdfGlyphAtlas, glyphs) {
			i.sdfGlyphAtlas, i.sdfGlyphAtlasGlyphs = buildRetainedSDFAtlas(glyphs, i.sdfGlyphAtlasGlyphs)
			i.sdfGlyphAtlasGeneration++
			i.sdfAtlasRetry = false
			i.sdfAtlasRetryAttempts = 0
		}
		i.sdfGlyphAtlasKeys = glyphKeys
	}
	sdfScene := buildSDFScene(sdfLayouts, i.sdfGlyphAtlas)
	if sdfScene != nil {
		if i.sdfAtlas == nil || i.sdfAtlasGeneration != i.sdfGlyphAtlasGeneration {
			replacement := quick.NewQSGSDFAtlas(
				i.quickItem,
				sdfScene.atlas.pixels,
				sdfScene.atlas.width,
				sdfScene.atlas.height,
			)
			if replacement != nil {
				if i.sdfAtlas != nil && i.sdfAtlas.QSGNode() != nil {
					i.sdfAtlas.QSGNode().Delete()
				}
				i.sdfAtlas = replacement
				i.sdfAtlasGeneration = i.sdfGlyphAtlasGeneration
				i.sdfAtlasRetry = false
				i.sdfAtlasRetryAttempts = 0
			} else {
				if i.sdfAtlasRetryAttempts == 0 {
					i.sdfAtlasRetryAttempts = 1
					i.sdfAtlasRetry = true
					queueQuickItemUpdate(i.quickItem)
				}
			}
		}
	} else if !i.sdfLayoutHasCandidates || (len(sdfLayouts) > 0 && len(glyphs) == 0) {
		if i.sdfAtlas != nil && i.sdfAtlas.QSGNode() != nil {
			i.sdfAtlas.QSGNode().Delete()
		}
		i.sdfAtlas = nil
		i.sdfAtlasGeneration = 0
		i.sdfGlyphAtlas = nil
		i.sdfGlyphAtlasGeneration = 0
		i.sdfGlyphAtlasKeys = nil
		i.sdfGlyphAtlasGlyphs = nil
		i.sdfAtlasRetry = false
		i.sdfAtlasRetryAttempts = 0
	}
	activeSDFAtlas := i.sdfAtlas
	if i.sdfAtlasGeneration != i.sdfGlyphAtlasGeneration {
		activeSDFAtlas = nil
	}
	var collisionLayouts map[libertySDFLayoutKey]*sdfTextLayout
	if sdfScene == nil || activeSDFAtlas == nil {
		collisionLayouts = nil
	} else {
		collisionLayouts = sdfScene.layouts
	}
	acceptedSymbols := acceptedLibertySymbols(camera, tiles, collisionLayouts)
	i.acceptedSymbols = acceptedSymbols
	i.renderedSDFScene = sdfScene
	i.recordLibertySymbolPlacement(camera)
	return glyphRevision, sdfScene, activeSDFAtlas, acceptedSymbols
}

func (i *Item) recordSDFStats(sdfScene *sdfScene, activeSDFAtlas *quick.QSGSDFAtlas) {
	if sdfScene != nil && activeSDFAtlas != nil {
		i.sdfLabels.Store(int64(sdfScene.renderedLabels))
		i.sdfAtlasGlyphs.Store(int64(len(sdfScene.atlas.positions)))
	} else {
		i.sdfLabels.Store(0)
		i.sdfAtlasGlyphs.Store(0)
	}
}

func (i *Item) discardLibertySDFAtlas(root *quick.QSGNode) {
	if i.sdfAtlas != nil && i.sdfAtlas.QSGNode() != nil {
		root.RemoveChildNode(i.sdfAtlas.QSGNode())
		i.sdfAtlas.QSGNode().Delete()
	}
	i.sdfAtlas = nil
	i.sdfAtlasGeneration = 0
	i.sdfAtlasRetry = false
	i.sdfAtlasRetryAttempts = 0
	i.sdfLabels.Store(0)
	i.sdfAtlasGlyphs.Store(0)
}

func (i *Item) appendLibertySymbolLayerNodes(
	layer *retainedLibertySymbolLayer,
	camera Camera,
	tiles []loadedRoadTile,
	wraps []int,
	accepted map[libertySymbolKey]libertyAcceptedSymbol,
	sdfScene *sdfScene,
	sdfAtlas *quick.QSGSDFAtlas,
) {
	if layer == nil || layer.root == nil {
		return
	}
	for _, tile := range tiles {
		if tile.roads == nil {
			continue
		}
		for _, wrap := range wraps {
			node, transforms := newLibertySymbolLayerTileNode(
				i.quickItem,
				camera,
				tile.id,
				wrap,
				tile.roads.symbols,
				layer.order,
				accepted,
				sdfScene,
				sdfAtlas,
			)
			if node == nil {
				continue
			}
			layer.root.AppendChildNode(node.QSGNode)
			layer.tiles = append(layer.tiles, retainedLibertySymbolTile{node: node, tile: tile.id, wrap: wrap})
			i.symbolTransforms = append(i.symbolTransforms, transforms...)
		}
	}
}

func (i *Item) reconcileLibertyGlyphNodes(
	root *quick.QSGNode,
	camera Camera,
	scene *libertySceneSnapshot,
) uint64 {
	glyphRevision := i.glyphs.currentRevision()
	if root == nil || scene == nil || len(i.symbolLayers) == 0 {
		return glyphRevision
	}
	if i.sdfAtlas != nil && i.sdfAtlas.QSGNode() != nil {
		root.RemoveChildNode(i.sdfAtlas.QSGNode())
	}
	i.clearLibertySymbolTileNodes()
	glyphRevision, sdfScene, sdfAtlas, accepted := i.prepareLibertySymbols(camera, scene)
	wraps := libertyWorldWraps(camera)
	for layerIndex := range i.symbolLayers {
		i.appendLibertySymbolLayerNodes(
			&i.symbolLayers[layerIndex],
			camera,
			scene.tiles,
			wraps,
			accepted,
			sdfScene,
			sdfAtlas,
		)
	}
	if i.sdfAtlas != nil && i.sdfAtlas.QSGNode() != nil {
		root.AppendChildNode(i.sdfAtlas.QSGNode())
	}
	i.recordSDFStats(sdfScene, sdfAtlas)
	return glyphRevision
}

func (i *Item) reconcileLibertySymbolNodes(camera Camera, scene *libertySceneSnapshot) {
	if scene == nil || len(i.symbolLayers) == 0 {
		return
	}
	i.recordLibertySymbolPlacement(camera)
	sdfScene := i.renderedSDFScene
	var sdfAtlas *quick.QSGSDFAtlas
	var collisionLayouts map[libertySDFLayoutKey]*sdfTextLayout
	if sdfScene != nil && i.sdfAtlas != nil && i.sdfAtlasGeneration == i.sdfGlyphAtlasGeneration {
		sdfAtlas = i.sdfAtlas
		collisionLayouts = sdfScene.layouts
	}
	accepted := acceptedLibertySymbols(camera, scene.tiles, collisionLayouts)
	if sameLibertyAcceptedSymbols(i.acceptedSymbols, accepted) {
		return
	}
	i.clearLibertySymbolTileNodes()
	if sdfScene != nil {
		sdfScene.rendered = make(map[libertySDFLayoutKey]struct{})
		sdfScene.renderedLabels = 0
	}
	wraps := libertyWorldWraps(camera)
	for layerIndex := range i.symbolLayers {
		i.appendLibertySymbolLayerNodes(
			&i.symbolLayers[layerIndex],
			camera,
			scene.tiles,
			wraps,
			accepted,
			sdfScene,
			sdfAtlas,
		)
	}
	i.acceptedSymbols = accepted
	i.recordSDFStats(sdfScene, sdfAtlas)
}

func (i *Item) clearLibertySymbolTileNodes() {
	for layerIndex := range i.symbolLayers {
		layer := &i.symbolLayers[layerIndex]
		for _, tile := range layer.tiles {
			layer.root.RemoveChildNode(tile.node.QSGNode)
			tile.node.Delete()
		}
		layer.tiles = layer.tiles[:0]
	}
	i.symbolTransforms = i.symbolTransforms[:0]
}

func (i *Item) libertySymbolPlacementChanged(camera Camera) bool {
	if !i.hasSymbolPlacement || math.Abs(camera.Zoom-i.lastSymbolPlacementZoom) >= libertyCollisionZoomStep {
		return true
	}
	bearingDelta := math.Abs(camera.Bearing - i.lastSymbolPlacementAngle)
	bearingDelta = math.Min(bearingDelta, 360-bearingDelta)
	return bearingDelta >= libertyCollisionBearingStep ||
		libertySymbolPanDistance(camera, i.lastSymbolPlacementCenter) >= libertyCollisionPanStep
}

func (i *Item) recordLibertySymbolPlacement(camera Camera) {
	i.lastSymbolPlacementZoom = camera.Zoom
	i.lastSymbolPlacementAngle = camera.Bearing
	i.lastSymbolPlacementCenter = camera.Center
	i.hasSymbolPlacement = true
}

func libertySymbolPanDistance(camera Camera, previous Coordinate) float64 {
	worldSize := tileSize * math.Exp2(camera.Zoom)
	currentX, currentY := mercatorWorldPoint(camera.Center, worldSize)
	previousX, previousY := mercatorWorldPoint(previous, worldSize)
	deltaX := math.Abs(currentX - previousX)
	if worldSize > 0 {
		deltaX = math.Min(deltaX, worldSize-deltaX)
	}
	return math.Hypot(deltaX, currentY-previousY)
}
