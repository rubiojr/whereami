// Package pbf contains vecmap's shared protobuf wire reader for tiles and glyphs.
// Callers bound message sizes before parsing; Bytes borrows the input buffer.
package pbf

import (
	"errors"
	"fmt"
	"math"
)

const (
	WireVarint  = 0
	WireFixed64 = 1
	WireBytes   = 2
	WireFixed32 = 5
)

type Reader struct {
	data   []byte
	offset int
}

func NewReader(data []byte) Reader { return Reader{data: data} }

func (r *Reader) More() bool { return r.offset < len(r.data) }

func (r *Reader) Field() (int, int, error) {
	key, err := r.readVarint()
	if err != nil {
		return 0, 0, err
	}
	if key>>3 == 0 {
		return 0, 0, errors.New("protobuf field number is zero")
	}
	// Validate before conversion: oversized keys must not alias a known field on
	// 32-bit targets. Protobuf field numbers are limited to 29 bits.
	if key>>3 > 1<<29-1 {
		return 0, 0, errors.New("protobuf field number exceeds 29 bits")
	}
	return int(key >> 3), int(key & 0x7), nil
}

func (r *Reader) Varint(wire int) (uint64, error) {
	if wire != WireVarint {
		return 0, fmt.Errorf("protobuf field has wire type %d, want varint", wire)
	}
	return r.readVarint()
}

func (r *Reader) Bytes(wire int) ([]byte, error) {
	if wire != WireBytes {
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

func (r *Reader) PackedUint32(wire int) ([]uint32, error) {
	if wire == WireVarint {
		value, err := r.readVarint()
		if err != nil {
			return nil, err
		}
		if value > math.MaxUint32 {
			return nil, errors.New("protobuf uint32 overflows")
		}
		return []uint32{uint32(value)}, nil
	}
	payload, err := r.Bytes(wire)
	if err != nil {
		return nil, err
	}
	packed := NewReader(payload)
	values := make([]uint32, 0, len(payload)/2)
	for packed.More() {
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

func (r *Reader) Fixed32(wire int) (uint32, error) {
	if wire != WireFixed32 {
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

func (r *Reader) Fixed64(wire int) (uint64, error) {
	if wire != WireFixed64 {
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

func (r *Reader) Skip(wire int) error {
	switch wire {
	case WireVarint:
		_, err := r.readVarint()
		return err
	case WireFixed64:
		return r.advance(8)
	case WireBytes:
		_, err := r.Bytes(wire)
		return err
	case WireFixed32:
		return r.advance(4)
	default:
		return fmt.Errorf("protobuf wire type %d is unsupported", wire)
	}
}

func (r *Reader) advance(count int) error {
	if count > len(r.data)-r.offset {
		return errors.New("protobuf fixed-width field is truncated")
	}
	r.offset += count
	return nil
}

func (r *Reader) readVarint() (uint64, error) {
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
