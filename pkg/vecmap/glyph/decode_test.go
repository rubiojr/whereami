package glyph

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func bytesField(data []byte, field int, value []byte) []byte {
	data = binary.AppendUvarint(data, uint64(field<<3|2))
	data = binary.AppendUvarint(data, uint64(len(value)))
	return append(data, value...)
}

func varintField(data []byte, field int, value uint64) []byte {
	data = binary.AppendUvarint(data, uint64(field<<3))
	return binary.AppendUvarint(data, value)
}

func encodeGlyph(g Glyph) []byte {
	data := varintField(nil, 1, uint64(g.ID))
	if g.Bitmap != nil {
		data = bytesField(data, 2, g.Bitmap)
	}
	data = varintField(data, 3, uint64(g.Width))
	data = varintField(data, 4, uint64(g.Height))
	data = varintField(data, 5, uint64(uint32(g.Left<<1)^uint32(g.Left>>31)))
	data = varintField(data, 6, uint64(uint32(g.Top<<1)^uint32(g.Top>>31)))
	return varintField(data, 7, uint64(g.Advance))
}

func encodeStack(name, rangeName string, glyphs ...[]byte) []byte {
	data := bytesField(nil, 1, []byte(name))
	data = bytesField(data, 2, []byte(rangeName))
	for _, glyph := range glyphs {
		data = bytesField(data, 3, glyph)
	}
	return data
}

func encodeRange(glyphs ...[]byte) []byte {
	return bytesField(nil, 1, encodeStack("server fallback", "0-255", glyphs...))
}

func TestDecodeRangeOwnsDataAndPreservesMetrics(t *testing.T) {
	want := Glyph{ID: 65, Width: 2, Height: 3, Left: -2, Top: 11, Advance: 7,
		Bitmap: bytes.Repeat([]byte{91}, (2+2*PBFBorder)*(3+2*PBFBorder))}
	data := encodeRange(encodeGlyph(want), encodeGlyph(Glyph{ID: 32, Advance: 6}))
	decoded, err := DecodeRange(data, "requested font", 0)
	require.NoError(t, err)
	clear(data)
	assert.Equal(t, "requested font", decoded.FontStack)
	assert.Equal(t, "0-255", decoded.RangeName)
	assert.Equal(t, want, decoded.Glyphs[65])
	assert.Equal(t, Glyph{ID: 32, Advance: 6}, decoded.Glyphs[32])
	assert.Equal(t, "65280-65535", RangeName(65280))
	assert.Equal(t, "4294967295-4294967550", RangeName(math.MaxUint32))
	last := bytesField(nil, 1, encodeStack("font", RangeName(65280), encodeGlyph(Glyph{ID: 65535})))
	decoded, err = DecodeRange(last, "font", 65280)
	require.NoError(t, err)
	assert.Contains(t, decoded.Glyphs, uint32(65535))
}

func TestDecodeRangeValidationAndAtomicFailure(t *testing.T) {
	goodGlyph := encodeGlyph(Glyph{ID: 65})
	good := encodeRange(goodGlyph)
	for _, start := range []uint32{1, 255, 65536, math.MaxUint32} {
		decoded, err := DecodeRange(good, "font", start)
		assert.ErrorContains(t, err, "start is invalid")
		assert.Nil(t, decoded)
	}
	for _, tt := range []struct {
		name    string
		data    []byte
		message string
	}{
		{"missing", nil, "is missing"},
		{"range field", []byte{0}, "decode glyph range"},
		{"range unknown wire", []byte{0x13}, "decode glyph range"},
		{"range payload", []byte{0x0a, 3}, "decode glyph range stack"},
		{"stack field", bytesField(nil, 1, []byte{0}), "decode glyph stack"},
		{"stack name", bytesField(nil, 1, []byte{0x0a, 4}), "metadata"},
		{"stack range", bytesField(nil, 1, []byte{0x12, 4}), "metadata"},
		{"stack missing name", bytesField(nil, 1, encodeStack("", "0-255")), "missing name or range"},
		{"stack missing range", bytesField(nil, 1, encodeStack("font", "")), "missing name or range"},
		{"stack unknown wire", bytesField(nil, 1, []byte{0x23}), "decode glyph stack"},
		{"glyph payload", bytesField(nil, 1, []byte{0x1a, 4}), "decode glyph"},
		{"duplicate glyph", encodeRange(goodGlyph, goodGlyph), "duplicate glyph"},
		{"duplicate stack", append(append([]byte(nil), good...), good...), "duplicate matching stacks"},
		{"late framing", append(append([]byte(nil), good...), 0), "decode glyph range"},
		{"nonmatching invalid glyph", bytesField(append([]byte(nil), good...), 1, encodeStack("font", "256-511", encodeGlyph(Glyph{ID: 256}))), "outside requested range"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			decoded, err := DecodeRange(tt.data, "font", 0)
			assert.ErrorContains(t, err, tt.message)
			assert.Nil(t, decoded)
		})
	}
}

func TestDecodeRangeBudgets(t *testing.T) {
	var glyphs [][]byte
	for id := range MaxGlyphsPerRange {
		glyphs = append(glyphs, encodeGlyph(Glyph{ID: uint32(id)}))
	}
	data := encodeRange(glyphs...)
	decoded, err := DecodeRange(data, "font", 0)
	require.NoError(t, err)
	assert.Len(t, decoded.Glyphs, MaxGlyphsPerRange)
	decoded, err = DecodeRange(encodeRange(append(glyphs, glyphs[0])...), "font", 0)
	assert.ErrorContains(t, err, "glyph count limit")
	assert.Nil(t, decoded)
	for range MaxRangeStacks - 1 {
		data = bytesField(data, 1, encodeStack("font", "unmatched"))
	}
	_, err = DecodeRange(data, "font", 0)
	require.NoError(t, err)
	decoded, err = DecodeRange(bytesField(data, 1, encodeStack("font", "unmatched")), "font", 0)
	assert.ErrorContains(t, err, "font stack limit")
	assert.Nil(t, decoded)
	data = encodeRange()
	data = bytesField(data, 2, make([]byte, MaxRangeBytes-len(data)-4))
	require.Len(t, data, MaxRangeBytes)
	_, err = DecodeRange(data, "font", 0)
	require.NoError(t, err)
	decoded, err = DecodeRange(append(data, 0), "font", 0)
	assert.ErrorContains(t, err, "byte limit")
	assert.Nil(t, decoded)
}

func TestDecodeGlyphValidation(t *testing.T) {
	good := encodeGlyph(Glyph{ID: 65})
	for _, tt := range []struct {
		name    string
		data    []byte
		message string
	}{
		{"missing metrics", nil, "missing required metrics"},
		{"field", []byte{0}, "decode glyph"},
		{"unknown wire", append(append([]byte(nil), good...), 0x43), "decode glyph"},
		{"bitmap field", []byte{0x12, 5}, "bitmap"},
		{"outside", encodeGlyph(Glyph{ID: 256}), "outside requested range"},
		{"width", encodeGlyph(Glyph{Width: 256}), "dimensions exceed"},
		{"height", encodeGlyph(Glyph{Height: 256}), "dimensions exceed"},
		{"advance", encodeGlyph(Glyph{Advance: 256}), "dimensions exceed"},
		{"left low", encodeGlyph(Glyph{Left: -129}), "bearing exceeds"},
		{"left high", encodeGlyph(Glyph{Left: 128}), "bearing exceeds"},
		{"top low", encodeGlyph(Glyph{Top: -129}), "bearing exceeds"},
		{"top high", encodeGlyph(Glyph{Top: 128}), "bearing exceeds"},
		{"bitmap length", encodeGlyph(Glyph{Width: 2, Height: 2, Bitmap: []byte{1}}), "bitmap has"},
		{"bitmap for empty", encodeGlyph(Glyph{Bitmap: []byte{1}}), "bitmap has"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			decoded, err := DecodeRange(encodeRange(good, tt.data), "font", 0)
			assert.ErrorContains(t, err, tt.message)
			assert.Nil(t, decoded)
		})
	}
	for _, field := range []int{1, 3, 4, 5, 6, 7} {
		for _, data := range [][]byte{varintField(nil, field, math.MaxUint64), bytesField(nil, field, nil)} {
			decoded, err := DecodeRange(encodeRange(data), "font", 0)
			assert.ErrorContains(t, err, "invalid")
			assert.Nil(t, decoded)
		}
	}
	for _, g := range []Glyph{{Left: -128, Top: 127, Advance: 255}, {Left: 127, Top: -128, Width: 255}, {Height: 255}} {
		decoded, err := DecodeRange(encodeRange(encodeGlyph(g)), "font", 0)
		require.NoError(t, err)
		assert.Equal(t, g, decoded.Glyphs[0])
	}
}

func TestDecodeRangeUnknownAndRepeatedFields(t *testing.T) {
	g := Glyph{ID: 65, Width: 1, Height: 1, Bitmap: bytes.Repeat([]byte{9}, 49)}
	encoded := bytesField(nil, 2, []byte{1}) // Overwritten before validation.
	encoded = append(encoded, encodeGlyph(g)...)
	encoded = varintField(encoded, 7, 12)
	encoded = varintField(encoded, 8, 1)
	stack := encodeStack("server", "0-255", encoded)
	stack = varintField(stack, 4, 1)
	data := varintField(bytesField(nil, 1, stack), 2, 1)
	decoded, err := DecodeRange(data, "requested", 0)
	require.NoError(t, err)
	g.Advance = 12
	assert.Equal(t, g, decoded.Glyphs[65])
}

func TestDecodeLocalGlyphRangesHeadless(t *testing.T) {
	dir := os.Getenv("WHEREAMI_VECTOR_GLYPH_FIXTURE_DIR")
	if dir == "" {
		t.Skip("set WHEREAMI_VECTOR_GLYPH_FIXTURE_DIR for the local glyph ranges")
	}
	for _, font := range []string{"Regular", "Bold", "Italic"} {
		data, err := os.ReadFile(filepath.Join(dir, "Noto%20Sans%20"+font+".pbf"))
		require.NoError(t, err)
		decoded, err := DecodeRange(data, "Noto Sans "+font, 0)
		require.NoError(t, err)
		require.Contains(t, decoded.Glyphs, uint32('A'))
		assert.NotEmpty(t, decoded.Glyphs['A'].Bitmap)
	}
}

func FuzzDecodeRange(f *testing.F) {
	f.Add(encodeRange(encodeGlyph(Glyph{ID: 32, Advance: 6})), uint32(0))
	f.Add([]byte{0x0a, 0x01, 0xff}, uint32(65280))
	f.Fuzz(func(t *testing.T, data []byte, start uint32) {
		decoded, err := DecodeRange(data, "font", start)
		if err != nil {
			require.Nil(t, decoded)
			return
		}
		require.LessOrEqual(t, len(decoded.Glyphs), MaxGlyphsPerRange)
		for id, glyph := range decoded.Glyphs {
			require.GreaterOrEqual(t, id, start)
			require.Less(t, id, start+RangeSize)
			require.NoError(t, glyph.validate())
		}
	})
}
