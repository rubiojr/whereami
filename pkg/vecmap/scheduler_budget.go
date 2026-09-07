package vecmap

import "fmt"

const (
	maximumCoverRoadSegments  = 1_000_000
	maximumCoverFillTriangles = 1_000_000
	maximumCoverSymbols       = 250_000
)

type tileCoverResources struct {
	roadSegments  int
	fillTriangles int
	symbols       int
}

func (s *tileSchedulerState) admitTile(tile vectorTileID, bucket *tileBucket) error {
	return s.admitTileWithLimits(tile, bucket, tileCoverResources{
		roadSegments:  maximumCoverRoadSegments,
		fillTriangles: maximumCoverFillTriangles,
		symbols:       maximumCoverSymbols,
	})
}

func (s *tileSchedulerState) admitTileWithLimits(
	tile vectorTileID,
	bucket *tileBucket,
	limits tileCoverResources,
) error {
	bucket.tile = tile
	s.loaded[tile] = bucket
	resources := s.selectedCoverResources()
	var err error
	switch {
	case resources.roadSegments > limits.roadSegments:
		err = fmt.Errorf(
			"road cover exceeds %d-segment limit (selected=%d)",
			limits.roadSegments,
			resources.roadSegments,
		)
	case resources.fillTriangles > limits.fillTriangles:
		err = fmt.Errorf(
			"fill cover exceeds %d-triangle limit (selected=%d)",
			limits.fillTriangles,
			resources.fillTriangles,
		)
	case resources.symbols > limits.symbols:
		err = fmt.Errorf(
			"symbol cover exceeds %d-candidate limit (selected=%d)",
			limits.symbols,
			resources.symbols,
		)
	}
	if err != nil {
		delete(s.loaded, tile)
	}
	return err
}

func (s *tileSchedulerState) selectedCoverResources() tileCoverResources {
	resources := tileCoverResources{}
	tiles, _ := s.renderSelection()
	for _, tile := range tiles {
		if tile.roads == nil {
			continue
		}
		resources.roadSegments += len(tile.roads.segments)
		resources.fillTriangles += tile.roads.fillTriangleCount()
		resources.symbols += len(tile.roads.symbols)
	}
	return resources
}
