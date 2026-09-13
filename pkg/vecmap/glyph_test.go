package vecmap

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeSDFGlyphRange(t *testing.T) {
	key := glyphRangeKey{fontStack: "Noto Sans Regular", start: 0}
	bitmap := make([]byte, (2+2*glyphPBFBorder)*(3+2*glyphPBFBorder))
	for index := range bitmap {
		bitmap[index] = byte(index)
	}
	data := encodeGlyphRangeTest(
		key.fontStack+", Noto Naskh Arabic Regular",
		glyphRangeName(key.start),
		encodeGlyphTest(65, bitmap, 2, 3, -2, 11, 7),
	)

	decoded, err := decodeSDFGlyphRange(data, key)
	require.NoError(t, err)
	assert.Equal(t, key.fontStack, decoded.FontStack)
	require.Contains(t, decoded.Glyphs, uint32(65))
	glyph := decoded.Glyphs[65]
	assert.Equal(t, uint32(2), glyph.Width)
	assert.Equal(t, uint32(3), glyph.Height)
	assert.Equal(t, int32(-2), glyph.Left)
	assert.Equal(t, int32(11), glyph.Top)
	assert.Equal(t, uint32(7), glyph.Advance)
	assert.Equal(t, bitmap, glyph.Bitmap)
}

func TestDecodeSDFGlyphRejectsMalformedMetrics(t *testing.T) {
	key := glyphRangeKey{fontStack: "Noto Sans Regular", start: 0}
	tests := []struct {
		name      string
		glyph     []byte
		errorText string
	}{
		{
			name:      "bitmap size",
			glyph:     encodeGlyphTest(65, []byte{1}, 2, 3, 0, 0, 7),
			errorText: "bitmap has",
		},
		{
			name:      "outside range",
			glyph:     encodeGlyphTest(300, nil, 0, 0, 0, 0, 7),
			errorText: "outside requested range",
		},
		{
			name:      "dimension",
			glyph:     encodeGlyphTest(65, nil, 256, 0, 0, 0, 7),
			errorText: "dimensions exceed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := encodeGlyphRangeTest(key.fontStack, glyphRangeName(key.start), test.glyph)
			_, err := decodeSDFGlyphRange(data, key)
			assert.ErrorContains(t, err, test.errorText)
		})
	}
}

func TestGlyphRangeKeys(t *testing.T) {
	assert.Equal(t, []glyphRangeKey{
		{fontStack: "Noto Sans Regular", start: 0},
		{fontStack: "Noto Sans Regular", start: 256},
	}, glyphRangeKeys("Noto Sans Regular", "A"+string(rune(0x100))))
	assert.Nil(t, glyphRangeKeys("Noto Sans Regular", "\U0001f600"))
}

func TestGlyphManagerLoadsOnceAndPublishes(t *testing.T) {
	updates := make(chan struct{}, 1)
	loads := make(chan glyphRangeKey, 2)
	manager := newGlyphManager(func(_ context.Context, key glyphRangeKey) (*sdfGlyphRange, error) {
		loads <- key
		return &sdfGlyphRange{
			FontStack: key.fontStack,
			RangeName: glyphRangeName(key.start),
			Glyphs: map[uint32]sdfGlyph{
				65: {ID: 65, Advance: 12},
			},
		}, nil
	}, func() { updates <- struct{}{} })
	t.Cleanup(manager.close)

	_, ready := manager.resolve("Noto Sans Regular", "A")
	assert.False(t, ready)
	select {
	case <-updates:
	case <-time.After(time.Second):
		t.Fatal("glyph manager did not publish the loaded range")
	}
	glyphs, ready := manager.resolve("Noto Sans Regular", "A")
	require.True(t, ready)
	assert.Equal(t, uint32(12), glyphs[65].Advance)
	assert.Equal(t, uint64(1), manager.currentRevision())
	assert.Len(t, loads, 1)
	_, ready = manager.resolve("Noto Sans Regular", "A")
	assert.True(t, ready)
	assert.Len(t, loads, 1)
}

func TestGlyphManagerReportsLoadFailure(t *testing.T) {
	messages := make(chan string, 1)
	previousReporter := reportVectorError
	reportVectorError = func(format string, args ...any) {
		messages <- fmt.Sprintf(format, args...)
	}
	t.Cleanup(func() { reportVectorError = previousReporter })
	manager := newGlyphManager(func(_ context.Context, _ glyphRangeKey) (*sdfGlyphRange, error) {
		return nil, errors.New("glyph unavailable")
	}, nil)
	t.Cleanup(manager.close)

	_, ready := manager.resolve("Noto Sans Regular", "A")
	assert.False(t, ready)
	select {
	case message := <-messages:
		assert.Contains(t, message, "range 0-255")
		assert.Contains(t, message, "Noto Sans Regular")
		assert.Contains(t, message, "glyph unavailable")
	case <-time.After(time.Second):
		require.FailNow(t, "glyph failure was not reported")
	}
}

func encodeGlyphRangeTest(fontStack, rangeName string, glyphs ...[]byte) []byte {
	stack := appendProtobufBytesTest(nil, 1, []byte(fontStack))
	stack = appendProtobufBytesTest(stack, 2, []byte(rangeName))
	for _, glyph := range glyphs {
		stack = appendProtobufBytesTest(stack, 3, glyph)
	}
	return appendProtobufBytesTest(nil, 1, stack)
}

func encodeGlyphTest(id uint32, bitmap []byte, width, height uint32, left, top int32, advance uint32) []byte {
	data := appendProtobufVarintTest(nil, 1, uint64(id))
	if bitmap != nil {
		data = appendProtobufBytesTest(data, 2, bitmap)
	}
	data = appendProtobufVarintTest(data, 3, uint64(width))
	data = appendProtobufVarintTest(data, 4, uint64(height))
	data = appendProtobufVarintTest(data, 5, uint64(encodeZigZagTest(left)))
	data = appendProtobufVarintTest(data, 6, uint64(encodeZigZagTest(top)))
	return appendProtobufVarintTest(data, 7, uint64(advance))
}

func appendProtobufBytesTest(data []byte, field int, value []byte) []byte {
	data = appendVarintTest(data, uint64(field<<3|protobufWireBytes))
	data = appendVarintTest(data, uint64(len(value)))
	return append(data, value...)
}

func appendProtobufVarintTest(data []byte, field int, value uint64) []byte {
	if field > 0 {
		data = appendVarintTest(data, uint64(field<<3))
	}
	return appendVarintTest(data, value)
}

func appendVarintTest(data []byte, value uint64) []byte {
	for value >= 0x80 {
		data = append(data, byte(value)|0x80)
		value >>= 7
	}
	return append(data, byte(value))
}

func encodeZigZagTest(value int32) uint32 {
	return uint32(value<<1) ^ uint32(value>>31)
}

func TestGlyphRangeName(t *testing.T) {
	assert.Equal(t, "0-255", glyphRangeName(0))
	assert.Equal(t, "65280-65535", glyphRangeName(65280))
	assert.False(t, strings.Contains(glyphRangeName(256), " "))
}

func FuzzDecodeSDFGlyphRange(f *testing.F) {
	key := glyphRangeKey{fontStack: "Noto Sans Regular", start: 0}
	f.Add(encodeGlyphRangeTest(key.fontStack, glyphRangeName(key.start), encodeGlyphTest(32, nil, 0, 0, 0, 0, 6)))
	f.Add([]byte{0x0a, 0x01, 0xff})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = decodeSDFGlyphRange(data, key)
	})
}
