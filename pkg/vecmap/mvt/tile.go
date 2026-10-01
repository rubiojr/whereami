package mvt

// Tile owns prepared geometry and properties in one FeatureSet per source-layer
// name. Duplicate source-layer names are appended in input order. Treat all
// data as immutable after publication.
type Tile struct {
	Layers map[string]*FeatureSet
	Limits []LayerLimits
}

type LayerLimits struct {
	Name string
	LimitSummary
}

// DecodeTile prepares a bounded PBF tile without I/O, style evaluation, logging
// or toolkit dependencies. indexed preserves Earcut topology directly. Structural
// errors return nil; per-feature failures retain prior accepted content and report
// resource degradation in Limits, matching vecmap's existing policy.
func DecodeTile(data []byte, indexed bool) (*Tile, error) {
	layers, err := DecodeLayers(data)
	if err != nil {
		return nil, err
	}
	tile := &Tile{Layers: make(map[string]*FeatureSet, len(layers))}
	decoder := NewDecoder(indexed)
	for _, layer := range layers {
		set := tile.Layers[layer.name]
		if set == nil {
			set = &FeatureSet{}
		}
		limits := decoder.decodeLayerInto(layer, set)
		if limits.Skipped > 0 {
			tile.Limits = append(tile.Limits, LayerLimits{Name: layer.name, LimitSummary: limits})
		}
		if set.Len() > 0 {
			tile.Layers[layer.name] = set
		}
	}
	return tile, nil
}
