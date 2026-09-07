package vecmap

// Stats reports render-thread and tile-cover activity.
type Stats struct {
	PaintNodeUpdates uint64
	GeometryBuilds   uint64
	GeometryUpdates  uint64
	GeometryRemovals uint64
	TransformUpdates uint64
	TileLoaded       bool
	TilesRequested   int
	TilesLoaded      int
	TilesLoading     int
	TileErrors       int
	FallbackTiles    int
	RoadFeatures     int
	RoadSegments     int
	LandFeatures     int
	LandTriangles    int
	WaterFeatures    int
	WaterTriangles   int
	LibertyLayers    int
	LibertyTriangles int
	SymbolCandidates int
	SDFLabels        int
	SDFAtlasGlyphs   int
	RasterTiles      int
	TileError        string
}

// Stats returns atomic counters that distinguish retained-node reuse from
// geometry creation.
func (i *Item) Stats() Stats {
	if i == nil {
		return Stats{}
	}
	if final := i.finalStats.Load(); final != nil {
		return *final
	}
	stats := Stats{
		PaintNodeUpdates: i.paintNodeUpdates.Load(),
		GeometryBuilds:   i.geometryBuilds.Load(),
		GeometryUpdates:  i.geometryUpdates.Load(),
		GeometryRemovals: i.geometryRemovals.Load(),
		TransformUpdates: i.transformUpdates.Load(),
		SDFLabels:        int(i.sdfLabels.Load()),
		SDFAtlasGlyphs:   int(i.sdfAtlasGlyphs.Load()),
	}
	if tiles := i.tiles.Load(); tiles != nil {
		stats.TileLoaded = len(tiles.tiles) > 0
		stats.TilesRequested = tiles.requested
		stats.TilesLoaded = len(tiles.tiles)
		stats.TilesLoading = tiles.loading
		stats.TileErrors = tiles.errors
		stats.FallbackTiles = tiles.fallbacks
		stats.TileError = tiles.lastError
		statTiles := tiles.tiles
		if styled := i.styledTiles.Load(); styled != nil && styled.tileRevision == tiles.revision {
			statTiles = styled.tiles
		}
		for _, tile := range statTiles {
			stats.RoadFeatures += tile.roads.featureCount
			stats.RoadSegments += len(tile.roads.segments)
			stats.LandFeatures += tile.roads.land.featureCount
			stats.LandTriangles += fillTriangleCount(tile.roads.land)
			stats.WaterFeatures += tile.roads.water.featureCount
			stats.WaterTriangles += fillTriangleCount(tile.roads.water)
			orders := make(map[int]struct{})
			for _, primitive := range tile.roads.liberty {
				orders[primitive.order] = struct{}{}
				stats.LibertyTriangles += len(primitive.triangles) / 3
			}
			stats.LibertyLayers += len(orders)
			stats.SymbolCandidates += len(tile.roads.symbols)
			if len(tile.roads.raster.rgba) > 0 {
				stats.RasterTiles++
			}
		}
	}
	return stats
}
