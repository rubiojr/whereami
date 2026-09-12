package vecmap

import (
	"errors"
	"fmt"
	"math"
)

const (
	protobufWireVarint  = 0
	protobufWireFixed64 = 1
	protobufWireBytes   = 2
	protobufWireFixed32 = 5
	mvtPointType        = 1
	mvtLineStringType   = 2
	mvtPolygonType      = 3
	mvtMoveToCommand    = 1
	mvtLineToCommand    = 2
	mvtClosePathCommand = 7
)

var errRoadResourceLimit = errors.New("MVT road resource limit exceeded")

type mvtValue struct {
	value    any
	text     string
	isString bool
}

type featureProperties map[string]any

func (p featureProperties) get(name string) (any, bool) {
	value, exists := p[name]
	return value, exists
}

type mvtLayer struct {
	name     string
	extent   uint32
	features [][]byte
	keys     []string
	values   []mvtValue
}

type mvtFeature struct {
	id         uint64
	hasID      bool
	tags       []uint32
	geometry   []uint32
	geometryID uint32
}

type protobufReader struct {
	data   []byte
	offset int
}

func decodeRoadBucket(data []byte, tile vectorTileID) (*tileBucket, error) {
	return decodeRoadBucketGeometry(data, tile, false)
}

func decodeRoadBucketGeometry(data []byte, tile vectorTileID, indexed bool) (*tileBucket, error) {
	if len(data) < 2 {
		return nil, errors.New("MVT data is too short")
	}
	if tile.Z > 30 {
		return nil, fmt.Errorf("MVT tile zoom %d is unsupported", tile.Z)
	}

	layers, err := decodeMVTLayers(data)
	if err != nil {
		return nil, err
	}
	bucket := &tileBucket{tile: tile, sourceLayers: make(map[string][]vectorFeature, len(layers))}
	fillTriangulationBudget := triangulationBudget{remaining: maxTriangulationOps}
	styleTriangulationBudget := triangulationBudget{remaining: maxStyleTriangulation}
	totalFeatures := 0
	totalPoints := 0
	totalTriangles := 0
	for _, layer := range layers {
		var fillLimits resourceLimitSummary
		switch layer.name {
		case "transportation":
			if err := layer.appendRoads(bucket); err != nil {
				if !errors.Is(err, errRoadResourceLimit) {
					return nil, fmt.Errorf("decode transportation layer: %w", err)
				}
			}
		case "land", "landcover", "landuse", "park":
			fillLimits = layer.appendPolygons(&bucket.land, &fillTriangulationBudget)
		case "water":
			fillLimits = layer.appendPolygons(&bucket.water, &fillTriangulationBudget)
		}
		reportMVTResourceLimits(tile, layer.name, "fill", fillLimits)
		if fillTriangleCount(bucket.land)+fillTriangleCount(bucket.water) > maxFillTriangles {
			return nil, fmt.Errorf("MVT tile fill geometry exceeds %d-triangle limit", maxFillTriangles)
		}
		features, featureLimits := layer.decodeFeaturesGeometry(
			&totalFeatures,
			&totalPoints,
			&totalTriangles,
			&styleTriangulationBudget,
			indexed,
		)
		reportMVTResourceLimits(tile, layer.name, "styled", featureLimits)
		if len(features) > 0 {
			bucket.sourceLayers[layer.name] = append(bucket.sourceLayers[layer.name], features...)
		}
	}
	if err := compileLibertyTileGeometry(bucket, float64(tile.Z), indexed); err != nil {
		return nil, err
	}
	return bucket, nil
}

func reportMVTResourceLimits(tile vectorTileID, layer, stage string, limits resourceLimitSummary) {
	if limits.skipped == 0 {
		return
	}
	reportVectorWarning(
		"vecmap tile z=%d x=%d y=%d degraded %s layer %q: skipped %d resource-limited feature(s), last feature index=%d: %v",
		tile.Z,
		tile.X,
		tile.Y,
		stage,
		layer,
		limits.skipped,
		limits.lastFeatureIndex,
		limits.last,
	)
}

func decodeMVTLayers(data []byte) ([]*mvtLayer, error) {
	reader := protobufReader{data: data}
	layers := make([]*mvtLayer, 0, 16)
	for reader.more() {
		field, wire, err := reader.field()
		if err != nil {
			return nil, fmt.Errorf("decode MVT tile: %w", err)
		}
		if field != 3 {
			if err := reader.skip(wire); err != nil {
				return nil, fmt.Errorf("decode MVT tile field %d: %w", field, err)
			}
			continue
		}

		payload, err := reader.bytes(wire)
		if err != nil {
			return nil, fmt.Errorf("decode MVT layer: %w", err)
		}
		layer, err := decodeMVTLayer(payload)
		if err != nil {
			return nil, fmt.Errorf("decode MVT layer: %w", err)
		}
		layers = append(layers, layer)
	}
	return layers, nil
}

func decodeMVTLayer(data []byte) (*mvtLayer, error) {
	layer := &mvtLayer{extent: 4096}
	reader := protobufReader{data: data}
	for reader.more() {
		field, wire, err := reader.field()
		if err != nil {
			return nil, err
		}
		if err := decodeMVTLayerField(layer, &reader, field, wire); err != nil {
			return nil, err
		}
	}
	if layer.extent == 0 {
		return nil, errors.New("MVT layer has zero extent")
	}
	return layer, nil
}

func decodeMVTLayerField(layer *mvtLayer, reader *protobufReader, field, wire int) error {
	switch field {
	case 1:
		value, err := reader.bytes(wire)
		if err != nil {
			return err
		}
		layer.name = string(value)
	case 2:
		value, err := reader.bytes(wire)
		if err != nil {
			return err
		}
		layer.features = append(layer.features, value)
	case 3:
		value, err := reader.bytes(wire)
		if err != nil {
			return err
		}
		layer.keys = append(layer.keys, string(value))
	case 4:
		value, err := reader.bytes(wire)
		if err != nil {
			return err
		}
		decoded, err := decodeMVTValue(value)
		if err != nil {
			return err
		}
		layer.values = append(layer.values, decoded)
	case 5:
		value, err := reader.varint(wire)
		if err != nil {
			return err
		}
		if value > math.MaxUint32 {
			return errors.New("MVT layer extent overflows uint32")
		}
		layer.extent = uint32(value)
	default:
		if err := reader.skip(wire); err != nil {
			return err
		}
	}
	return nil
}

func decodeMVTValue(data []byte) (mvtValue, error) {
	reader := protobufReader{data: data}
	var value mvtValue
	for reader.more() {
		field, wire, err := reader.field()
		if err != nil {
			return mvtValue{}, err
		}
		switch field {
		case 1:
			text, err := reader.bytes(wire)
			if err != nil {
				return mvtValue{}, err
			}
			value.value = string(text)
			value.text = string(text)
			value.isString = true
		case 2:
			bits, err := reader.fixed32(wire)
			if err != nil {
				return mvtValue{}, err
			}
			value.value = math.Float32frombits(bits)
		case 3:
			bits, err := reader.fixed64(wire)
			if err != nil {
				return mvtValue{}, err
			}
			value.value = math.Float64frombits(bits)
		case 4:
			integer, err := reader.varint(wire)
			if err != nil {
				return mvtValue{}, err
			}
			value.value = int64(integer)
		case 5:
			integer, err := reader.varint(wire)
			if err != nil {
				return mvtValue{}, err
			}
			value.value = integer
		case 6:
			integer, err := reader.varint(wire)
			if err != nil {
				return mvtValue{}, err
			}
			value.value = decodeZigZag64(integer)
		case 7:
			boolean, err := reader.varint(wire)
			if err != nil {
				return mvtValue{}, err
			}
			value.value = boolean != 0
		default:
			if err := reader.skip(wire); err != nil {
				return mvtValue{}, err
			}
		}
	}
	return value, nil
}

func (l *mvtLayer) appendRoads(bucket *tileBucket) error {
	for _, data := range l.features {
		feature, err := decodeMVTFeature(data)
		if err != nil {
			continue
		}
		class, err := l.featureClass(feature.tags)
		if err != nil {
			continue
		}
		if feature.geometryID != mvtLineStringType || !supportedRoadClass(class) {
			continue
		}

		featureBucket := tileBucket{}
		if err := appendMVTLineGeometry(&featureBucket, feature.geometry, l.extent); err != nil {
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

func (l *mvtLayer) appendPolygons(bucket *fillBucket, budget *triangulationBudget) resourceLimitSummary {
	var limits resourceLimitSummary
	for featureIndex, data := range l.features {
		feature, err := decodeMVTFeature(data)
		if err != nil {
			continue
		}
		if feature.geometryID != mvtPolygonType {
			continue
		}
		rings, err := decodeMVTPolygonRings(feature.geometry, l.extent)
		if err != nil {
			if errors.Is(err, errPolygonResourceLimit) {
				limits.add(featureIndex, err)
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
			triangleCount := (len(bucket.triangles) + len(bucket.cutouts) +
				len(featureTriangles) + len(featureCutouts) + len(triangles)) / 3
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
				limits.add(featureIndex, featureLimit)
			}
			continue
		}
		bucket.triangles = append(bucket.triangles, featureTriangles...)
		bucket.cutouts = append(bucket.cutouts, featureCutouts...)
		bucket.featureCount++
	}
	return limits
}

func decodeMVTPolygonRings(geometry []uint32, extent uint32) ([][]roadPoint, error) {
	var (
		x          int64
		y          int64
		pointCount int
		ring       []roadPoint
		rings      [][]roadPoint
	)
	for index := 0; index < len(geometry); {
		commandInteger := geometry[index]
		index++
		command := commandInteger & 0x7
		count := commandInteger >> 3
		if count == 0 {
			return nil, errors.New("MVT polygon command has zero count")
		}

		switch command {
		case mvtMoveToCommand, mvtLineToCommand:
			if command == mvtMoveToCommand {
				if count != 1 {
					return nil, errors.New("MVT polygon MoveTo command count is not one")
				}
				if ring != nil {
					return nil, errors.New("MVT polygon starts a ring before closing the previous ring")
				}
			} else if ring == nil {
				return nil, errors.New("MVT polygon LineTo command appears before MoveTo")
			}
			if uint64(count)*2 > uint64(len(geometry)-index) {
				return nil, errors.New("MVT polygon command is truncated")
			}
			for range count {
				x += decodeZigZag(geometry[index])
				y += decodeZigZag(geometry[index+1])
				index += 2
				point := tileLocalPoint(extent, x, y)
				if !validRoadPoint(point) {
					return nil, errors.New("MVT polygon contains an invalid point")
				}
				if pointCount >= maxFillRingPoints {
					return nil, fmt.Errorf("%w: polygon exceeds %d-point limit", errPolygonResourceLimit, maxFillRingPoints)
				}
				ring = append(ring, point)
				pointCount++
			}
		case mvtClosePathCommand:
			if count != 1 {
				return nil, errors.New("MVT polygon ClosePath command count is not one")
			}
			if len(ring) < 3 {
				return nil, errors.New("MVT polygon ring has fewer than three points")
			}
			rings = append(rings, ring)
			ring = nil
		default:
			return nil, fmt.Errorf("MVT polygon geometry has unsupported command %d", command)
		}
	}
	if ring != nil {
		return nil, errors.New("MVT polygon ring is not closed")
	}
	return rings, nil
}

func decodeMVTFeature(data []byte) (mvtFeature, error) {
	reader := protobufReader{data: data}
	var feature mvtFeature
	for reader.more() {
		field, wire, err := reader.field()
		if err != nil {
			return mvtFeature{}, err
		}
		switch field {
		case 1:
			value, err := reader.varint(wire)
			if err != nil {
				return mvtFeature{}, err
			}
			feature.id = value
			feature.hasID = true
		case 2:
			values, err := reader.packedUint32(wire)
			if err != nil {
				return mvtFeature{}, err
			}
			feature.tags = append(feature.tags, values...)
		case 3:
			value, err := reader.varint(wire)
			if err != nil {
				return mvtFeature{}, err
			}
			if value > math.MaxUint32 {
				return mvtFeature{}, errors.New("MVT geometry type overflows uint32")
			}
			feature.geometryID = uint32(value)
		case 4:
			values, err := reader.packedUint32(wire)
			if err != nil {
				return mvtFeature{}, err
			}
			feature.geometry = append(feature.geometry, values...)
		default:
			if err := reader.skip(wire); err != nil {
				return mvtFeature{}, err
			}
		}
	}
	return feature, nil
}

func (l *mvtLayer) featureClass(tags []uint32) (string, error) {
	properties, err := l.featureProperties(tags)
	if err != nil {
		return "", err
	}
	value, exists := properties.get("class")
	if !exists {
		return "", nil
	}
	class, _ := value.(string)
	return class, nil
}

func (l *mvtLayer) featureProperties(tags []uint32) (featureProperties, error) {
	if len(tags)%2 != 0 {
		return nil, errors.New("MVT feature has an odd tag count")
	}
	properties := make(featureProperties, len(tags)/2)
	for index := 0; index < len(tags); index += 2 {
		keyIndex := tags[index]
		valueIndex := tags[index+1]
		if uint64(keyIndex) >= uint64(len(l.keys)) || uint64(valueIndex) >= uint64(len(l.values)) {
			return nil, errors.New("MVT feature tag index is out of range")
		}
		properties[l.keys[keyIndex]] = l.values[valueIndex].value
	}
	return properties, nil
}

func appendMVTLineGeometry(bucket *tileBucket, geometry []uint32, extent uint32) error {
	var (
		x        int64
		y        int64
		previous roadPoint
		hasPoint bool
	)
	for index := 0; index < len(geometry); {
		commandInteger := geometry[index]
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
		if uint64(count)*2 > uint64(len(geometry)-index) {
			return errors.New("MVT geometry command is truncated")
		}

		for range count {
			x += decodeZigZag(geometry[index])
			y += decodeZigZag(geometry[index+1])
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
	return !math.IsNaN(point.X) && !math.IsInf(point.X, 0) &&
		!math.IsNaN(point.Y) && !math.IsInf(point.Y, 0)
}

func decodeZigZag(value uint32) int64 {
	return int64(value>>1) ^ -int64(value&1)
}

func decodeZigZag64(value uint64) int64 {
	return int64(value>>1) ^ -int64(value&1)
}

func (r *protobufReader) more() bool {
	return r.offset < len(r.data)
}

func (r *protobufReader) field() (int, int, error) {
	key, err := r.readVarint()
	if err != nil {
		return 0, 0, err
	}
	field := int(key >> 3)
	if field == 0 {
		return 0, 0, errors.New("protobuf field number is zero")
	}
	return field, int(key & 0x7), nil
}

func (r *protobufReader) varint(wire int) (uint64, error) {
	if wire != protobufWireVarint {
		return 0, fmt.Errorf("protobuf field has wire type %d, want varint", wire)
	}
	return r.readVarint()
}

func (r *protobufReader) bytes(wire int) ([]byte, error) {
	if wire != protobufWireBytes {
		return nil, fmt.Errorf("protobuf field has wire type %d, want bytes", wire)
	}
	length, err := r.readVarint()
	if err != nil {
		return nil, err
	}
	if length > uint64(len(r.data)-r.offset) {
		return nil, errors.New("protobuf bytes field is truncated")
	}
	start := r.offset
	r.offset += int(length)
	return r.data[start:r.offset], nil
}

func (r *protobufReader) packedUint32(wire int) ([]uint32, error) {
	if wire == protobufWireVarint {
		value, err := r.readVarint()
		if err != nil {
			return nil, err
		}
		if value > math.MaxUint32 {
			return nil, errors.New("protobuf uint32 overflows")
		}
		return []uint32{uint32(value)}, nil
	}
	payload, err := r.bytes(wire)
	if err != nil {
		return nil, err
	}
	packed := protobufReader{data: payload}
	values := make([]uint32, 0, len(payload)/2)
	for packed.more() {
		value, err := packed.readVarint()
		if err != nil {
			return nil, err
		}
		if value > math.MaxUint32 {
			return nil, errors.New("protobuf uint32 overflows")
		}
		values = append(values, uint32(value))
	}
	return values, nil
}

func (r *protobufReader) fixed32(wire int) (uint32, error) {
	if wire != protobufWireFixed32 {
		return 0, fmt.Errorf("protobuf field has wire type %d, want fixed32", wire)
	}
	if len(r.data)-r.offset < 4 {
		return 0, errors.New("protobuf fixed32 field is truncated")
	}
	value := uint32(r.data[r.offset]) |
		uint32(r.data[r.offset+1])<<8 |
		uint32(r.data[r.offset+2])<<16 |
		uint32(r.data[r.offset+3])<<24
	r.offset += 4
	return value, nil
}

func (r *protobufReader) fixed64(wire int) (uint64, error) {
	if wire != protobufWireFixed64 {
		return 0, fmt.Errorf("protobuf field has wire type %d, want fixed64", wire)
	}
	if len(r.data)-r.offset < 8 {
		return 0, errors.New("protobuf fixed64 field is truncated")
	}
	var value uint64
	for index := range 8 {
		value |= uint64(r.data[r.offset+index]) << (index * 8)
	}
	r.offset += 8
	return value, nil
}

func (r *protobufReader) skip(wire int) error {
	switch wire {
	case protobufWireVarint:
		_, err := r.readVarint()
		return err
	case protobufWireFixed64:
		return r.advance(8)
	case protobufWireBytes:
		_, err := r.bytes(wire)
		return err
	case protobufWireFixed32:
		return r.advance(4)
	default:
		return fmt.Errorf("protobuf wire type %d is unsupported", wire)
	}
}

func (r *protobufReader) advance(count int) error {
	if count > len(r.data)-r.offset {
		return errors.New("protobuf fixed-width field is truncated")
	}
	r.offset += count
	return nil
}

func (r *protobufReader) readVarint() (uint64, error) {
	var value uint64
	for shift := uint(0); shift < 64; shift += 7 {
		if r.offset >= len(r.data) {
			return 0, errors.New("protobuf varint is truncated")
		}
		current := r.data[r.offset]
		r.offset++
		if shift == 63 && current > 1 {
			return 0, errors.New("protobuf varint overflows uint64")
		}
		value |= uint64(current&0x7f) << shift
		if current < 0x80 {
			return value, nil
		}
	}
	return 0, errors.New("protobuf varint overflows uint64")
}
