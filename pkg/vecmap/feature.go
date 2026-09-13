package vecmap

import "github.com/rubiojr/whereami/pkg/vecmap/mvt"

const maxTileStyleTriangles = mvt.MaxTileStyleTriangles

var errFeatureResourceLimit = mvt.ErrFeatureResourceLimit

// Shared source data passes from headless preparation to style evaluation without
// conversion slices or maps. Published features are immutable.
type vectorFeature = mvt.Feature
type vectorPolygon = mvt.Polygon
type featureProperties = mvt.Properties
type resourceLimitSummary = mvt.LimitSummary

func addResourceLimit(s *resourceLimitSummary, index int, err error) {
	s.Skipped++
	s.LastFeatureIndex = index
	s.Last = err
}
