package xiangshan

import (
	"encoding/binary"
	"fmt"
	"math"
)

const (
	indexHeaderSize = 24
	recordSize      = 29
)

type divisionRecord struct {
	subtype uint8
	bbox    [4]float32
	offset  uint64
	length  uint32
}

type compactIndex struct {
	records []divisionRecord
	coarse  map[[2]int16][]uint32
	fine    map[[2]int16][]uint32
}

type indexCursor struct {
	data []byte
	pos  int
}

func parseIndex(data []byte, slabSize int64, maxChunkBytes uint32) (*compactIndex, error) {
	if len(data) < indexHeaderSize {
		return nil, fmt.Errorf("xiangshan: index header: file too small")
	}
	if string(data[:4]) != "XSCI" {
		return nil, fmt.Errorf("xiangshan: index header: invalid magic %q", data[:4])
	}
	if data[4] != 1 {
		return nil, fmt.Errorf("xiangshan: index header: unsupported version %d", data[4])
	}
	if data[5] != 0 || data[6] != 0 || data[7] != 0 {
		return nil, fmt.Errorf("xiangshan: index header: reserved bytes are nonzero")
	}

	divCount := binary.LittleEndian.Uint32(data[8:12])
	coarseCount := binary.LittleEndian.Uint32(data[12:16])
	fineCount := binary.LittleEndian.Uint32(data[16:20])
	preindexCount := binary.LittleEndian.Uint32(data[20:24])
	minimum, ok := checkedIndexMinimum(divCount, coarseCount, fineCount, preindexCount)
	if !ok || minimum > uint64(len(data)) {
		return nil, fmt.Errorf("xiangshan: index header counts exceed file size")
	}
	if uint64(divCount) > uint64(maxInt()) || uint64(coarseCount) > uint64(maxInt()) || uint64(fineCount) > uint64(maxInt()) {
		return nil, fmt.Errorf("xiangshan: index header counts exceed platform limits")
	}

	c := indexCursor{data: data, pos: indexHeaderSize}
	records := make([]divisionRecord, int(divCount))
	for i := range records {
		raw, err := c.take(recordSize)
		if err != nil {
			return nil, fmt.Errorf("xiangshan: division record %d: %w", i, err)
		}
		r := divisionRecord{
			subtype: raw[0],
			bbox: [4]float32{
				math.Float32frombits(binary.LittleEndian.Uint32(raw[1:5])),
				math.Float32frombits(binary.LittleEndian.Uint32(raw[5:9])),
				math.Float32frombits(binary.LittleEndian.Uint32(raw[9:13])),
				math.Float32frombits(binary.LittleEndian.Uint32(raw[13:17])),
			},
			offset: binary.LittleEndian.Uint64(raw[17:25]),
			length: binary.LittleEndian.Uint32(raw[25:29]),
		}
		if err := validateRecord(r, slabSize, maxChunkBytes); err != nil {
			return nil, fmt.Errorf("xiangshan: division record %d: %w", i, err)
		}
		records[i] = r
	}

	coarse, err := parseGrid(&c, coarseCount, divCount, "coarse")
	if err != nil {
		return nil, err
	}
	fine, err := parseGrid(&c, fineCount, divCount, "fine")
	if err != nil {
		return nil, err
	}
	if err := validatePreindex(&c, preindexCount, divCount); err != nil {
		return nil, err
	}
	if c.pos != len(data) {
		return nil, fmt.Errorf("xiangshan: index has %d trailing bytes", len(data)-c.pos)
	}

	return &compactIndex{records: records, coarse: coarse, fine: fine}, nil
}

func checkedIndexMinimum(divisions, coarse, fine, preindex uint32) (uint64, bool) {
	total := uint64(indexHeaderSize)
	parts := [][2]uint64{
		{uint64(divisions), recordSize},
		{uint64(coarse), 6},
		{uint64(fine), 6},
		{uint64(preindex), 8},
	}
	for _, part := range parts {
		if part[0] != 0 && part[1] > math.MaxUint64/part[0] {
			return 0, false
		}
		amount := part[0] * part[1]
		if amount > math.MaxUint64-total {
			return 0, false
		}
		total += amount
	}
	return total, true
}

func validateRecord(r divisionRecord, slabSize int64, maxChunkBytes uint32) error {
	if r.subtype > subtypeLocality {
		return fmt.Errorf("invalid subtype %d", r.subtype)
	}
	xmin, xmax := float64(r.bbox[0]), float64(r.bbox[1])
	ymin, ymax := float64(r.bbox[2]), float64(r.bbox[3])
	if !finite(xmin) || !finite(xmax) || !finite(ymin) || !finite(ymax) {
		return fmt.Errorf("bbox contains a non-finite value")
	}
	if xmin < -180 || xmax > 180 || ymin < -90 || ymax > 90 || xmin > xmax || ymin > ymax {
		return fmt.Errorf("invalid WGS84 bbox [%g %g %g %g]", xmin, xmax, ymin, ymax)
	}
	if r.length < 8 {
		return fmt.Errorf("invalid slab length %d", r.length)
	}
	if r.length > maxChunkBytes {
		return fmt.Errorf("slab length %d exceeds limit %d", r.length, maxChunkBytes)
	}
	end := r.offset + uint64(r.length)
	if end < r.offset {
		return fmt.Errorf("slab range overflows uint64")
	}
	if r.offset > math.MaxInt64 {
		return fmt.Errorf("slab offset %d exceeds ReadAt range", r.offset)
	}
	if end > uint64(slabSize) {
		return fmt.Errorf("slab range [%d,%d) exceeds file size %d", r.offset, end, slabSize)
	}
	return nil
}

func parseGrid(c *indexCursor, count, divCount uint32, tier string) (map[[2]int16][]uint32, error) {
	if uint64(count)*6 > uint64(c.remaining()) {
		return nil, fmt.Errorf("xiangshan: %s grid count exceeds remaining index", tier)
	}
	cells := make(map[[2]int16][]uint32, int(count))
	for i := uint32(0); i < count; i++ {
		header, err := c.take(6)
		if err != nil {
			return nil, fmt.Errorf("xiangshan: %s grid cell %d: %w", tier, i, err)
		}
		key := [2]int16{
			int16(binary.LittleEndian.Uint16(header[0:2])),
			int16(binary.LittleEndian.Uint16(header[2:4])),
		}
		if !validGridKey(key, tier) {
			return nil, fmt.Errorf("xiangshan: %s grid cell %d has invalid key (%d,%d)", tier, i, key[0], key[1])
		}
		if _, exists := cells[key]; exists {
			return nil, fmt.Errorf("xiangshan: %s grid has duplicate cell (%d,%d)", tier, key[0], key[1])
		}
		n := uint32(binary.LittleEndian.Uint16(header[4:6]))
		if uint64(n)*4 > uint64(c.remaining()) {
			return nil, fmt.Errorf("xiangshan: %s grid cell %d candidates exceed remaining index", tier, i)
		}
		indices := make([]uint32, int(n))
		for j := range indices {
			raw, _ := c.take(4)
			idx := binary.LittleEndian.Uint32(raw)
			if idx >= divCount {
				return nil, fmt.Errorf("xiangshan: %s grid cell %d candidate %d is out of bounds", tier, i, idx)
			}
			indices[j] = idx
		}
		cells[key] = indices
	}
	return cells, nil
}

func validatePreindex(c *indexCursor, count, divCount uint32) error {
	if uint64(count)*8 > uint64(c.remaining()) {
		return fmt.Errorf("xiangshan: preindex count exceeds remaining index")
	}
	seen := make(map[[2]int16]struct{}, int(count))
	for i := uint32(0); i < count; i++ {
		raw, err := c.take(8)
		if err != nil {
			return fmt.Errorf("xiangshan: preindex cell %d: %w", i, err)
		}
		key := [2]int16{
			int16(binary.LittleEndian.Uint16(raw[0:2])),
			int16(binary.LittleEndian.Uint16(raw[2:4])),
		}
		if !validGridKey(key, "coarse") {
			return fmt.Errorf("xiangshan: preindex cell %d has invalid key (%d,%d)", i, key[0], key[1])
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("xiangshan: preindex has duplicate cell (%d,%d)", key[0], key[1])
		}
		seen[key] = struct{}{}
		idx := binary.LittleEndian.Uint32(raw[4:8])
		if idx >= divCount {
			return fmt.Errorf("xiangshan: preindex cell %d candidate %d is out of bounds", i, idx)
		}
	}
	return nil
}

func validGridKey(key [2]int16, tier string) bool {
	if tier == "fine" {
		return key[0] >= -720 && key[0] <= 720 && key[1] >= -360 && key[1] <= 360
	}
	return key[0] >= -180 && key[0] <= 180 && key[1] >= -90 && key[1] <= 90
}

func (c *indexCursor) take(n int) ([]byte, error) {
	if n < 0 || n > c.remaining() {
		return nil, fmt.Errorf("unexpected end of index")
	}
	start := c.pos
	c.pos += n
	return c.data[start:c.pos], nil
}

func (c *indexCursor) remaining() int {
	return len(c.data) - c.pos
}

func maxInt() int {
	return int(^uint(0) >> 1)
}
