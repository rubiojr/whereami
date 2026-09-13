package vecmap

import (
	"errors"
	"fmt"
	"math"

	"github.com/rubiojr/whereami/pkg/vecmap/internal/pbf"
	"github.com/rubiojr/whereami/pkg/vecmap/mvt"
)

const (
	protobufWireVarint  = pbf.WireVarint
	protobufWireFixed64 = pbf.WireFixed64
	protobufWireBytes   = pbf.WireBytes
	protobufWireFixed32 = pbf.WireFixed32
	mvtPointType        = mvt.PointType
	mvtLineStringType   = mvt.LineStringType
	mvtPolygonType      = mvt.PolygonType
	mvtMoveToCommand    = 1
	mvtLineToCommand    = 2
	mvtClosePathCommand = 7
)

var errRoadResourceLimit = errors.New("MVT road resource limit exceeded")

func decodeRoadBucket(data []byte, tile vectorTileID) (*tileBucket, error) {
	return decodeRoadBucketGeometry(data, tile, false)
}

func decodeRoadBucketGeometry(data []byte, tile vectorTileID, indexed bool) (*tileBucket, error) {
	bucket, err := decodeTileFeatures(data, tile, tileDecodeOptions{indexed: indexed, legacyGeometry: true})
	if err != nil {
		return nil, err
	}
	if err := compileLibertyTileGeometry(bucket, float64(tile.Z), indexed); err != nil {
		return nil, err
	}
	return bucket, nil
}

// The fixture chooses its style zoom after preparing owned headless source data.
func decodeStyledBucketGeometry(data []byte, tile vectorTileID, indexed bool) (*tileBucket, error) {
	return decodeTileFeatures(data, tile, tileDecodeOptions{indexed: indexed})
}

type tileDecodeOptions struct {
	indexed        bool
	legacyGeometry bool
}

func decodeTileFeatures(data []byte, tile vectorTileID, options tileDecodeOptions) (*tileBucket, error) {
	if len(data) < 2 {
		return nil, errors.New("MVT data is too short")
	}
	if tile.Z > 30 {
		return nil, fmt.Errorf("MVT tile zoom %d is unsupported", tile.Z)
	}
	layers, err := mvt.DecodeLayers(data)
	if err != nil {
		return nil, err
	}
	bucket := &tileBucket{tile: tile, sourceLayers: make(map[string][]vectorFeature, len(layers))}
	fillBudget := triangulationBudget{remaining: maxTriangulationOps}
	decoder := mvt.NewDecoder(options.indexed)
	for _, layer := range layers {
		if options.legacyGeometry {
			if err := appendLegacyGeometry(layer, bucket, &fillBudget); err != nil {
				return nil, err
			}
		}
		features, limits := decoder.DecodeLayer(layer)
		reportMVTResourceLimits(tile, layer.Name(), "styled", limits)
		if len(features) > 0 {
			bucket.sourceLayers[layer.Name()] = append(bucket.sourceLayers[layer.Name()], features...)
		}
	}
	return bucket, nil
}

func appendLegacyGeometry(layer *mvt.Layer, bucket *tileBucket, budget *triangulationBudget) error {
	var limits resourceLimitSummary
	switch layer.Name() {
	case "transportation":
		if err := appendRoads(layer, bucket); err != nil && !errors.Is(err, errRoadResourceLimit) {
			return fmt.Errorf("decode transportation layer: %w", err)
		}
	case "land", "landcover", "landuse", "park":
		limits = appendPolygons(layer, &bucket.land, budget)
	case "water":
		limits = appendPolygons(layer, &bucket.water, budget)
	}
	reportMVTResourceLimits(bucket.tile, layer.Name(), "fill", limits)
	if fillTriangleCount(bucket.land)+fillTriangleCount(bucket.water) > maxFillTriangles {
		return fmt.Errorf("MVT tile fill geometry exceeds %d-triangle limit", maxFillTriangles)
	}
	return nil
}

func reportMVTResourceLimits(tile vectorTileID, layer, stage string, limits resourceLimitSummary) {
	if limits.Skipped == 0 {
		return
	}
	reportVectorWarning(
		"vecmap tile z=%d x=%d y=%d degraded %s layer %q: skipped %d resource-limited feature(s), last feature index=%d: %v",
		tile.Z, tile.X, tile.Y, stage, layer, limits.Skipped, limits.LastFeatureIndex, limits.Last,
	)
}

func appendRoads(layer *mvt.Layer, bucket *tileBucket) error {
	for _, data := range layer.FeatureData() {
		feature, err := mvt.DecodeFeature(data)
		if err != nil {
			continue
		}
		class, err := layer.FeatureClass(feature.Tags)
		if err != nil {
			continue
		}
		if feature.GeometryType != mvtLineStringType || !supportedRoadClass(class) {
			continue
		}
		featureBucket := tileBucket{}
		if err := appendMVTLineGeometry(&featureBucket, feature.Geometry, layer.Extent()); err != nil {
			if errors.Is(err, errRoadResourceLimit) {
				return err
			}
			continue
		}
		if len(bucket.segments)+len(featureBucket.segments) > maxRoadSegments {
			return fmt.Errorf("%w: geometry exceeds %d-segment limit", errRoadResourceLimit, maxRoadSegments)
		}
		if len(featureBucket.segments) > 0 {
			bucket.segments = append(bucket.segments, featureBucket.segments...)
			bucket.featureCount++
		}
	}
	return nil
}

func appendPolygons(layer *mvt.Layer, bucket *fillBucket, budget *triangulationBudget) resourceLimitSummary {
	var limits resourceLimitSummary
	for featureIndex, data := range layer.FeatureData() {
		feature, err := mvt.DecodeFeature(data)
		if err != nil {
			continue
		}
		if feature.GeometryType != mvtPolygonType {
			continue
		}
		rings, err := decodeMVTPolygonRings(feature.Geometry, layer.Extent())
		if err != nil {
			if errors.Is(err, errPolygonResourceLimit) {
				addResourceLimit(&limits, featureIndex, err)
			}
			continue
		}
		hasExterior := false
		validFeature := true
		var featureLimit error
		var featureTriangles, featureCutouts []roadPoint
		for _, ring := range rings {
			area := signedRingArea(ring)
			if math.Abs(area) <= polygonEpsilon {
				continue
			}
			cutout := area < 0
			if cutout && !hasExterior {
				validFeature = false
				break
			}
			if !cutout {
				hasExterior = true
			}
			triangles, err := triangulateRingBounded(ring, budget)
			if err != nil {
				if errors.Is(err, errPolygonResourceLimit) {
					featureLimit = err
				}
				validFeature = false
				break
			}
			triangleCount := (len(bucket.triangles) + len(bucket.cutouts) + len(featureTriangles) + len(featureCutouts) + len(triangles)) / 3
			if triangleCount > maxFillTriangles {
				featureLimit = fmt.Errorf("%w: fill geometry exceeds %d-triangle limit", errPolygonResourceLimit, maxFillTriangles)
				validFeature = false
				break
			}
			if cutout {
				featureCutouts = append(featureCutouts, triangles...)
			} else {
				featureTriangles = append(featureTriangles, triangles...)
			}
		}
		if !validFeature || len(featureTriangles) == 0 {
			if featureLimit != nil {
				addResourceLimit(&limits, featureIndex, featureLimit)
			}
			continue
		}
		bucket.triangles = append(bucket.triangles, featureTriangles...)
		bucket.cutouts = append(bucket.cutouts, featureCutouts...)
		bucket.featureCount++
	}
	return limits
}

func decodeMVTPolygonRings(commands []uint32, extent uint32) ([][]roadPoint, error) {
	return mvt.DecodePolygonRings(commands, extent)
}

func appendMVTLineGeometry(bucket *tileBucket, commands []uint32, extent uint32) error {
	var (
		x        int64
		y        int64
		previous roadPoint
		hasPoint bool
	)
	for index := 0; index < len(commands); {
		commandInteger := commands[index]
		index++
		command := commandInteger & 0x7
		count := commandInteger >> 3
		if count == 0 {
			return errors.New("MVT geometry command has zero count")
		}
		if command != mvtMoveToCommand && command != mvtLineToCommand {
			return fmt.Errorf("MVT line geometry has unsupported command %d", command)
		}
		if command == mvtMoveToCommand && count != 1 {
			return errors.New("MVT line MoveTo command count is not one")
		}
		if uint64(count)*2 > uint64(len(commands)-index) {
			return errors.New("MVT geometry command is truncated")
		}
		for range count {
			x += decodeZigZag(commands[index])
			y += decodeZigZag(commands[index+1])
			index += 2
			current := tileLocalPoint(extent, x, y)
			if command == mvtMoveToCommand {
				previous = current
				hasPoint = true
				continue
			}
			if !hasPoint {
				return errors.New("MVT LineTo command appears before MoveTo")
			}
			if validRoadPoint(previous) && validRoadPoint(current) && previous != current {
				if len(bucket.segments) >= maxRoadSegments {
					return fmt.Errorf("%w: geometry exceeds %d-segment limit", errRoadResourceLimit, maxRoadSegments)
				}
				bucket.segments = append(bucket.segments, roadSegment{Start: previous, End: current})
			}
			previous = current
		}
	}
	return nil
}

func tileLocalPoint(extent uint32, x, y int64) roadPoint {
	scale := tileSize / float64(extent)
	return roadPoint{X: float64(x) * scale, Y: float64(y) * scale}
}

func validRoadPoint(point roadPoint) bool {
	return !math.IsNaN(point.X) && !math.IsInf(point.X, 0) && !math.IsNaN(point.Y) && !math.IsInf(point.Y, 0)
}

func decodeZigZag(value uint32) int64 { return int64(value>>1) ^ -int64(value&1) }
