package glyph

import (
	"errors"
	"fmt"
	"math"

	"github.com/rubiojr/whereami/pkg/vecmap/internal/pbf"
)

// DecodeRange reuses the existing glyph PBF decoder. Start must be a multiple of
// 256 in the BMP. It validates every stack, including nonmatching ones, matches
// by range name and substitutes the requested font stack in the result.
// Output owns its maps/bitmaps; errors return nil, never partial data.
func DecodeRange(data []byte, fontStack string, start uint32) (*Range, error) {
	if len(data) > MaxRangeBytes {
		return nil, errors.New("glyph range exceeds byte limit")
	}
	if start > MaxCodePoint || start%RangeSize != 0 {
		return nil, errors.New("glyph range start is invalid")
	}
	reader := pbf.NewReader(data)
	var matched *Range
	stackCount := 0
	for reader.More() {
		field, wire, err := reader.Field()
		if err != nil {
			return nil, fmt.Errorf("decode glyph range: %w", err)
		}
		if field != 1 {
			if err := reader.Skip(wire); err != nil {
				return nil, fmt.Errorf("decode glyph range: %w", err)
			}
			continue
		}
		payload, err := reader.Bytes(wire)
		if err != nil {
			return nil, fmt.Errorf("decode glyph range stack: %w", err)
		}
		stackCount++
		if stackCount > MaxRangeStacks {
			return nil, errors.New("glyph range exceeds font stack limit")
		}
		stack, err := decodeStack(payload, start)
		if err != nil {
			return nil, err
		}
		if stack.RangeName == RangeName(start) {
			if matched != nil {
				return nil, errors.New("glyph range contains duplicate matching stacks")
			}
			matched = stack
		}
	}
	if matched == nil {
		return nil, fmt.Errorf("glyph range %s for %q is missing", RangeName(start), fontStack)
	}
	matched.FontStack = fontStack
	return matched, nil
}

func decodeStack(data []byte, start uint32) (*Range, error) {
	reader := pbf.NewReader(data)
	stack := &Range{Glyphs: make(map[uint32]Glyph)}
	for reader.More() {
		field, wire, err := reader.Field()
		if err != nil {
			return nil, fmt.Errorf("decode glyph stack: %w", err)
		}
		if err := decodeStackField(stack, &reader, field, wire, start); err != nil {
			return nil, err
		}
	}
	if stack.FontStack == "" || stack.RangeName == "" {
		return nil, errors.New("glyph stack is missing name or range")
	}
	return stack, nil
}

func decodeStackField(stack *Range, reader *pbf.Reader, field, wire int, start uint32) error {
	switch field {
	case 1, 2:
		value, err := reader.Bytes(wire)
		if err != nil {
			return fmt.Errorf("decode glyph stack metadata: %w", err)
		}
		if field == 1 {
			stack.FontStack = string(value)
		} else {
			stack.RangeName = string(value)
		}
	case 3:
		if len(stack.Glyphs) >= MaxGlyphsPerRange {
			return errors.New("glyph range exceeds glyph count limit")
		}
		payload, err := reader.Bytes(wire)
		if err != nil {
			return fmt.Errorf("decode glyph: %w", err)
		}
		glyph, err := decodeGlyph(payload, start)
		if err != nil {
			return err
		}
		if _, exists := stack.Glyphs[glyph.ID]; exists {
			return fmt.Errorf("glyph range contains duplicate glyph %d", glyph.ID)
		}
		stack.Glyphs[glyph.ID] = glyph
	default:
		if err := reader.Skip(wire); err != nil {
			return fmt.Errorf("decode glyph stack: %w", err)
		}
	}
	return nil
}

func decodeGlyph(data []byte, start uint32) (Glyph, error) {
	reader := pbf.NewReader(data)
	var glyph Glyph
	var required uint8
	for reader.More() {
		field, wire, err := reader.Field()
		if err != nil {
			return Glyph{}, fmt.Errorf("decode glyph: %w", err)
		}
		mask, err := decodeGlyphField(&glyph, &reader, field, wire)
		if err != nil {
			return Glyph{}, err
		}
		required |= mask
	}
	if required != 0b11_1111 {
		return Glyph{}, errors.New("glyph is missing required metrics")
	}
	if glyph.ID < start || glyph.ID >= start+RangeSize {
		return Glyph{}, fmt.Errorf("glyph %d is outside requested range", glyph.ID)
	}
	if err := glyph.validate(); err != nil {
		return Glyph{}, err
	}
	// Borrow the last bitmap field until validation, then copy once. Repeated
	// bitmap fields retain protobuf last-value behavior without throwaway copies.
	glyph.Bitmap = append([]byte(nil), glyph.Bitmap...)
	return glyph, nil
}

func decodeGlyphField(g *Glyph, reader *pbf.Reader, field, wire int) (uint8, error) {
	switch field {
	case 1, 3, 4, 7:
		value, err := reader.Varint(wire)
		if err != nil || value > math.MaxUint32 {
			return 0, errors.New("glyph unsigned metric or id is invalid")
		}
		switch field {
		case 1:
			g.ID = uint32(value)
			return 1 << 0, nil
		case 3:
			g.Width = uint32(value)
			return 1 << 1, nil
		case 4:
			g.Height = uint32(value)
			return 1 << 2, nil
		default:
			g.Advance = uint32(value)
			return 1 << 5, nil
		}
	case 2:
		value, err := reader.Bytes(wire)
		if err != nil {
			return 0, fmt.Errorf("decode glyph bitmap: %w", err)
		}
		g.Bitmap = value
	case 5, 6:
		value, err := reader.Varint(wire)
		if err != nil || value > math.MaxUint32 {
			return 0, errors.New("glyph signed metric is invalid")
		}
		metric := int32(uint32(value)>>1) ^ -int32(value&1)
		if field == 5 {
			g.Left = metric
			return 1 << 3, nil
		}
		g.Top = metric
		return 1 << 4, nil
	default:
		if err := reader.Skip(wire); err != nil {
			return 0, fmt.Errorf("decode glyph: %w", err)
		}
	}
	return 0, nil
}
