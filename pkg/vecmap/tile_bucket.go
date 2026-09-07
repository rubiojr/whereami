package vecmap

type tileBucket struct {
	tile         vectorTileID
	featureCount int
	segments     []roadSegment
	land         fillBucket
	water        fillBucket
	sourceLayers map[string][]vectorFeature
	liberty      []libertyRenderPrimitive
	raster       naturalEarthRaster
	symbols      []libertySymbolCandidate
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
