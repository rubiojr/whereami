package mvt

import (
	"errors"
	"fmt"
	"math"

	"github.com/rubiojr/whereami/pkg/vecmap/internal/pbf"
)

const (
	MaxTileBytes   = 2 << 20
	PointType      = 1
	LineStringType = 2
	PolygonType    = 3
)

var ErrTileResourceLimit = errors.New("MVT tile resource limit exceeded")

type mvtValue struct {
	value    any
	text     string
	isString bool
}

// Layer is parsed metadata plus borrowed feature payloads. Treat it as immutable
// and keep its input bytes unchanged until all layer preparation is complete.
type Layer struct {
	name     string
	extent   uint32
	features [][]byte
	keys     []string
	values   []mvtValue
}

func (l *Layer) Name() string   { return l.name }
func (l *Layer) Extent() uint32 { return l.extent }

// FeatureData borrows both the outer slice and payloads for legacy consumers.
// Neither may be modified. DecodeTile returns fully owned data instead.
func (l *Layer) FeatureData() [][]byte { return l.features }

// RawFeature owns its tag/command arrays. Properties and topology are prepared
// separately, allowing the existing fallback adapter to reuse the same parser.
type RawFeature struct {
	ID           uint64
	HasID        bool
	Tags         []uint32
	Geometry     []uint32
	GeometryType uint32
}

// DecodeLayers parses at most MaxTileBytes, retaining layer order and duplicate
// names. Tile/layer framing errors fail atomically; feature errors are handled
// later during preparation. Feature payloads borrow data. Zero bytes are a valid
// tile without layers: servers answer empty tiles that way.
func DecodeLayers(data []byte) ([]*Layer, error) {
	if len(data) > MaxTileBytes {
		return nil, fmt.Errorf("%w: data exceeds %d-byte limit", ErrTileResourceLimit, MaxTileBytes)
	}
	reader := pbf.NewReader(data)
	layers := make([]*Layer, 0, 16)
	for reader.More() {
		field, wire, err := reader.Field()
		if err != nil {
			return nil, fmt.Errorf("decode MVT tile: %w", err)
		}
		if field != 3 {
			if err := reader.Skip(wire); err != nil {
				return nil, fmt.Errorf("decode MVT tile field %d: %w", field, err)
			}
			continue
		}
		payload, err := reader.Bytes(wire)
		if err != nil {
			return nil, fmt.Errorf("decode MVT layer: %w", err)
		}
		layer, err := decodeLayer(payload)
		if err != nil {
			return nil, fmt.Errorf("decode MVT layer: %w", err)
		}
		layers = append(layers, layer)
	}
	return layers, nil
}

func decodeLayer(data []byte) (*Layer, error) {
	layer := &Layer{extent: 4096}
	reader := pbf.NewReader(data)
	for reader.More() {
		field, wire, err := reader.Field()
		if err != nil {
			return nil, err
		}
		if err := decodeLayerField(layer, &reader, field, wire); err != nil {
			return nil, err
		}
	}
	if layer.extent == 0 {
		return nil, errors.New("MVT layer has zero extent")
	}
	return layer, nil
}

func decodeLayerField(layer *Layer, reader *pbf.Reader, field, wire int) error {
	switch field {
	case 1:
		value, err := reader.Bytes(wire)
		if err != nil {
			return err
		}
		layer.name = string(value)
	case 2:
		value, err := reader.Bytes(wire)
		if err != nil {
			return err
		}
		layer.features = append(layer.features, value)
	case 3:
		value, err := reader.Bytes(wire)
		if err != nil {
			return err
		}
		layer.keys = append(layer.keys, string(value))
	case 4:
		value, err := reader.Bytes(wire)
		if err != nil {
			return err
		}
		decoded, err := decodeValue(value)
		if err != nil {
			return err
		}
		layer.values = append(layer.values, decoded)
	case 5:
		value, err := reader.Varint(wire)
		if err != nil {
			return err
		}
		if value > math.MaxUint32 {
			return errors.New("MVT layer extent overflows uint32")
		}
		layer.extent = uint32(value)
	default:
		return reader.Skip(wire)
	}
	return nil
}

func decodeValue(data []byte) (mvtValue, error) {
	reader := pbf.NewReader(data)
	var value mvtValue
	for reader.More() {
		field, wire, err := reader.Field()
		if err != nil {
			return mvtValue{}, err
		}
		switch field {
		case 1:
			text, err := reader.Bytes(wire)
			if err != nil {
				return mvtValue{}, err
			}
			value.value = string(text)
			value.text = string(text)
			value.isString = true
		case 2:
			bits, err := reader.Fixed32(wire)
			if err != nil {
				return mvtValue{}, err
			}
			value.value = math.Float32frombits(bits)
		case 3:
			bits, err := reader.Fixed64(wire)
			if err != nil {
				return mvtValue{}, err
			}
			value.value = math.Float64frombits(bits)
		case 4:
			integer, err := reader.Varint(wire)
			if err != nil {
				return mvtValue{}, err
			}
			value.value = int64(integer)
		case 5:
			integer, err := reader.Varint(wire)
			if err != nil {
				return mvtValue{}, err
			}
			value.value = integer
		case 6:
			integer, err := reader.Varint(wire)
			if err != nil {
				return mvtValue{}, err
			}
			value.value = int64(integer>>1) ^ -int64(integer&1)
		case 7:
			boolean, err := reader.Varint(wire)
			if err != nil {
				return mvtValue{}, err
			}
			value.value = boolean != 0
		default:
			if err := reader.Skip(wire); err != nil {
				return mvtValue{}, err
			}
		}
	}
	return value, nil
}

// DecodeFeature parses a bounded raw feature. Invalid input returns no partial
// result. It retains packed/unpacked fields and explicit ID-zero presence.
func DecodeFeature(data []byte) (RawFeature, error) {
	if len(data) > MaxTileBytes {
		return RawFeature{}, fmt.Errorf("%w: feature data exceeds %d-byte limit", ErrTileResourceLimit, MaxTileBytes)
	}
	reader := pbf.NewReader(data)
	var feature RawFeature
	for reader.More() {
		field, wire, err := reader.Field()
		if err != nil {
			return RawFeature{}, err
		}
		switch field {
		case 1:
			value, err := reader.Varint(wire)
			if err != nil {
				return RawFeature{}, err
			}
			feature.ID = value
			feature.HasID = true
		case 2:
			values, err := reader.PackedUint32(wire)
			if err != nil {
				return RawFeature{}, err
			}
			feature.Tags = append(feature.Tags, values...)
		case 3:
			value, err := reader.Varint(wire)
			if err != nil {
				return RawFeature{}, err
			}
			if value > math.MaxUint32 {
				return RawFeature{}, errors.New("MVT geometry type overflows uint32")
			}
			feature.GeometryType = uint32(value)
		case 4:
			values, err := reader.PackedUint32(wire)
			if err != nil {
				return RawFeature{}, err
			}
			feature.Geometry = append(feature.Geometry, values...)
		default:
			if err := reader.Skip(wire); err != nil {
				return RawFeature{}, err
			}
		}
	}
	return feature, nil
}

func (l *Layer) FeatureClass(tags []uint32) (string, error) {
	properties, err := l.FeatureProperties(tags)
	if err != nil {
		return "", err
	}
	value, exists := properties.Get("class")
	if !exists {
		return "", nil
	}
	class, _ := value.(string)
	return class, nil
}

// FeatureProperties validates tag references and returns an owned property map.
func (l *Layer) FeatureProperties(tags []uint32) (Properties, error) {
	if err := l.checkTags(tags); err != nil {
		return nil, err
	}
	return l.properties(tags), nil
}

// checkTags validates key and value index pairs against the layer's tables.
func (l *Layer) checkTags(tags []uint32) error {
	if len(tags)%2 != 0 {
		return errors.New("MVT feature has an odd tag count")
	}
	for index := 0; index < len(tags); index += 2 {
		if uint64(tags[index]) >= uint64(len(l.keys)) || uint64(tags[index+1]) >= uint64(len(l.values)) {
			return errors.New("MVT feature tag index is out of range")
		}
	}
	return nil
}

// properties builds the property map of validated tags; a duplicate key keeps
// its last value.
func (l *Layer) properties(tags []uint32) Properties {
	properties := make(Properties, len(tags)/2)
	for index := 0; index < len(tags); index += 2 {
		properties[l.keys[tags[index]]] = l.values[tags[index+1]].value
	}
	return properties
}
