package pbf

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReaderFieldsAndBorrowing(t *testing.T) {
	data := []byte{10, 3, 'm', 'v', 't', 16, 150, 1, 29, 1, 2, 3, 4, 33, 1, 2, 3, 4, 5, 6, 7, 8}
	r := NewReader(data)
	field, wire, err := r.Field()
	require.NoError(t, err)
	assert.Equal(t, 1, field)
	bytes, err := r.Bytes(wire)
	require.NoError(t, err)
	assert.Equal(t, []byte("mvt"), bytes)
	assert.Same(t, &data[2], &bytes[0])
	_, wire, err = r.Field()
	require.NoError(t, err)
	integer, err := r.Varint(wire)
	require.NoError(t, err)
	assert.Equal(t, uint64(150), integer)
	_, wire, err = r.Field()
	require.NoError(t, err)
	bits32, err := r.Fixed32(wire)
	require.NoError(t, err)
	assert.Equal(t, uint32(0x04030201), bits32)
	_, wire, err = r.Field()
	require.NoError(t, err)
	bits64, err := r.Fixed64(wire)
	require.NoError(t, err)
	assert.Equal(t, uint64(0x0807060504030201), bits64)
	assert.False(t, r.More())
}

func TestPackedUint32(t *testing.T) {
	for _, wire := range []int{WireVarint, WireBytes} {
		data := binary.AppendUvarint(nil, math.MaxUint32)
		if wire == WireBytes {
			data = append([]byte{byte(len(data))}, data...)
		}
		r := NewReader(data)
		values, err := r.PackedUint32(wire)
		require.NoError(t, err)
		assert.Equal(t, []uint32{math.MaxUint32}, values)
		assert.False(t, r.More())
		for _, invalid := range [][]byte{{0x80}, binary.AppendUvarint(nil, uint64(math.MaxUint32)+1)} {
			if wire == WireBytes {
				invalid = append([]byte{byte(len(invalid))}, invalid...)
			}
			r = NewReader(invalid)
			values, err = r.PackedUint32(wire)
			require.Error(t, err)
			assert.Nil(t, values)
		}
	}
	r := NewReader([]byte{0})
	_, err := r.PackedUint32(WireFixed32)
	require.Error(t, err)
}

func TestReaderRejectsInvalidFields(t *testing.T) {
	for _, data := range [][]byte{
		nil, {0}, {0x80},
		binary.AppendUvarint(nil, uint64(1)<<32|24),
		binary.AppendUvarint(nil, (uint64(1)<<32|3)<<3|WireBytes),
		binary.AppendUvarint(nil, uint64(1)<<32),
		{255, 255, 255, 255, 255, 255, 255, 255, 255, 2},
	} {
		r := NewReader(data)
		_, _, err := r.Field()
		require.Error(t, err)
	}
	r := NewReader(binary.AppendUvarint(nil, ((1<<29)-1)<<3|WireVarint))
	field, wire, err := r.Field()
	require.NoError(t, err)
	assert.Equal(t, (1<<29)-1, field)
	assert.Equal(t, WireVarint, wire)
}

func TestReaderRejectsWrongWireAndTruncation(t *testing.T) {
	readers := []struct {
		name string
		wire int
		read func(*Reader, int) error
	}{
		{"varint", WireVarint, func(r *Reader, wire int) error { _, err := r.Varint(wire); return err }},
		{"bytes", WireBytes, func(r *Reader, wire int) error { _, err := r.Bytes(wire); return err }},
		{"fixed32", WireFixed32, func(r *Reader, wire int) error { _, err := r.Fixed32(wire); return err }},
		{"fixed64", WireFixed64, func(r *Reader, wire int) error { _, err := r.Fixed64(wire); return err }},
	}
	for _, tt := range readers {
		t.Run(tt.name, func(t *testing.T) {
			r := NewReader(nil)
			require.Error(t, tt.read(&r, tt.wire))
			r = NewReader(make([]byte, 10))
			require.ErrorContains(t, tt.read(&r, 7), "wire type")
		})
	}
	r := NewReader([]byte{3, 1, 2})
	_, err := r.Bytes(WireBytes)
	require.ErrorContains(t, err, "truncated")
}

func TestSkip(t *testing.T) {
	for _, tt := range []struct {
		wire int
		data []byte
	}{
		{WireVarint, []byte{0x80, 1}},
		{WireBytes, []byte{3, 1, 2, 3}},
		{WireFixed32, make([]byte, 4)},
		{WireFixed64, make([]byte, 8)},
	} {
		r := NewReader(tt.data)
		require.NoError(t, r.Skip(tt.wire))
		assert.False(t, r.More())
		r = NewReader(tt.data[:len(tt.data)-1])
		require.Error(t, r.Skip(tt.wire))
	}
	r := NewReader(nil)
	require.Error(t, r.Skip(3))
	r = NewReader(binary.AppendUvarint(nil, math.MaxUint64))
	value, err := r.Varint(WireVarint)
	require.NoError(t, err)
	assert.Equal(t, uint64(math.MaxUint64), value)
}
