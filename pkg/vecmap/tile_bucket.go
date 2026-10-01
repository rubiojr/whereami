package vecmap

type tileBucket struct {
	tile         vectorTileID
	featureCount int
	segments     []roadSegment
	land         fillBucket
	water        fillBucket
	sourceLayers map[string]vectorFeatures
	// Compiler output is contiguous and ordered by style layer for render-time lookup.
	liberty []libertyRenderPrimitive
	raster  naturalEarthRaster
	// Symbols follow the same style-layer ordering contract as liberty.
	symbols      []libertySymbolCandidate
	compiledZoom float64
	compiled     bool
}

type fillBucket struct {
	featureCount int
	triangles    []roadPoint
	cutouts      []roadPoint
}

func (b *tileBucket) fillTriangleCount() int {
	if b == nil {
		return 0
	}
	return fillTriangleCount(b.land) + fillTriangleCount(b.water)
}
