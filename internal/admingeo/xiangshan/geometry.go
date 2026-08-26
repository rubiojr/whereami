package xiangshan

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"

	xs "github.com/ringsaturn/xiangshan/generated/xiangshan/v1"
)

const compressedCoordinateScale = 100000.0

type point struct {
	x float64
	y float64
}

type raycastResult struct {
	inside bool
	on     bool
}

type decodedDivision struct {
	name     string
	id       string
	subtype  uint8
	bbox     [4]float32
	contains bool
}

func decodeAndTest(ctx context.Context, data []byte, record divisionRecord, lng, lat float64) (result decodedDivision, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = decodedDivision{}
			err = fmt.Errorf("xiangshan: malformed slab FlatBuffer: %v", recovered)
		}
	}()
	if err := ctx.Err(); err != nil {
		return decodedDivision{}, err
	}
	if len(data) < 8 {
		return decodedDivision{}, fmt.Errorf("xiangshan: malformed slab FlatBuffer: too small")
	}
	payloadSize := binary.LittleEndian.Uint32(data[:4])
	if uint64(payloadSize) != uint64(len(data)-4) {
		return decodedDivision{}, fmt.Errorf("xiangshan: malformed slab FlatBuffer: size prefix %d does not match %d", payloadSize, len(data)-4)
	}
	rootOffset := binary.LittleEndian.Uint32(data[4:8])
	rootPosition := uint64(4) + uint64(rootOffset)
	if rootOffset < 4 || rootPosition+4 > uint64(len(data)) {
		return decodedDivision{}, fmt.Errorf("xiangshan: malformed slab FlatBuffer: invalid root offset %d", rootOffset)
	}

	division := xs.GetSizePrefixedRootAsCompressedDivision(data, 0)
	result.name = string(division.Name())
	result.id = string(division.Id())
	result.subtype = uint8(division.Subtype())
	if result.name == "" || result.id == "" {
		return decodedDivision{}, fmt.Errorf("xiangshan: malformed slab FlatBuffer: missing division name or ID")
	}
	if result.subtype > subtypeLocality || result.subtype != record.subtype {
		return decodedDivision{}, fmt.Errorf("xiangshan: malformed slab FlatBuffer: subtype %d does not match index subtype %d", result.subtype, record.subtype)
	}
	var bbox xs.BBox
	if division.Bbox(&bbox) == nil {
		return decodedDivision{}, fmt.Errorf("xiangshan: malformed slab FlatBuffer: missing bbox")
	}
	result.bbox = [4]float32{bbox.Xmin(), bbox.Xmax(), bbox.Ymin(), bbox.Ymax()}
	if result.bbox != record.bbox {
		return decodedDivision{}, fmt.Errorf("xiangshan: malformed slab FlatBuffer: bbox does not match index")
	}
	if !bboxContains(result.bbox, lng, lat) {
		return result, nil
	}

	polygonCount := division.PolygonsLength()
	if polygonCount < 1 || polygonCount > len(data)/4 {
		return decodedDivision{}, fmt.Errorf("xiangshan: malformed slab FlatBuffer: invalid polygon count %d", polygonCount)
	}
	query := point{x: lng, y: lat}
	var polygon xs.CompressedPolygon
	contained := false
	pointBudget := uint64(len(data) / 2)
	for i := 0; i < polygonCount; i++ {
		if err := ctx.Err(); err != nil {
			return decodedDivision{}, err
		}
		if !division.Polygons(&polygon, i) {
			return decodedDivision{}, fmt.Errorf("xiangshan: malformed slab FlatBuffer: missing polygon %d", i)
		}
		contains, err := compressedPolygonContains(ctx, &polygon, query, len(data), &pointBudget)
		if err != nil {
			return decodedDivision{}, err
		}
		contained = contained || contains
	}
	result.contains = contained
	return result, nil
}

func compressedPolygonContains(ctx context.Context, polygon *xs.CompressedPolygon, query point, bufferSize int, pointBudget *uint64) (bool, error) {
	var exterior xs.CompressedRing
	if polygon.Exterior(&exterior) == nil {
		return false, fmt.Errorf("xiangshan: malformed slab FlatBuffer: missing exterior ring")
	}
	insideExterior, err := compressedRingContains(ctx, &exterior, query, pointBudget)
	if err != nil {
		return false, err
	}
	holeCount := polygon.HolesLength()
	if holeCount < 0 || holeCount > bufferSize/4 {
		return false, fmt.Errorf("xiangshan: malformed slab FlatBuffer: invalid hole count %d", holeCount)
	}
	var hole xs.CompressedRing
	insideHole := false
	for i := 0; i < holeCount; i++ {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if !polygon.Holes(&hole, i) {
			return false, fmt.Errorf("xiangshan: malformed slab FlatBuffer: missing hole %d", i)
		}
		inHole, err := compressedRingContains(ctx, &hole, query, pointBudget)
		if err != nil {
			return false, err
		}
		insideHole = insideHole || inHole
	}
	return insideExterior && !insideHole, nil
}

func compressedRingContains(ctx context.Context, ring *xs.CompressedRing, query point, pointBudget *uint64) (bool, error) {
	pointCount := uint64(ring.PointCount())
	data := ring.DataBytes()
	if pointCount < 3 || pointCount > uint64(len(data))/2 {
		return false, fmt.Errorf("xiangshan: malformed slab FlatBuffer: invalid ring point count %d for %d bytes", pointCount, len(data))
	}
	if pointCount > *pointBudget {
		return false, fmt.Errorf("xiangshan: malformed slab FlatBuffer: geometry exceeds buffer complexity limit")
	}
	*pointBudget -= pointCount
	decoder := ringDecoder{data: data}
	first, err := decoder.nextPoint()
	if err != nil {
		return false, err
	}
	previous := first
	inside := false
	onBoundary := false
	for i := uint64(1); i < pointCount; i++ {
		if i&255 == 0 {
			if err := ctx.Err(); err != nil {
				return false, err
			}
		}
		current, err := decoder.nextPoint()
		if err != nil {
			return false, err
		}
		result := raycastSegment(previous, current, query)
		if result.on {
			onBoundary = true
		}
		if result.inside {
			inside = !inside
		}
		previous = current
	}
	if err := decoder.finish(pointCount); err != nil {
		return false, err
	}
	result := raycastSegment(previous, first, query)
	if result.on {
		onBoundary = true
	}
	if result.inside {
		inside = !inside
	}
	return onBoundary || inside, nil
}

type ringDecoder struct {
	data []byte
	pos  int
	lng  int64
	lat  int64
}

func (d *ringDecoder) nextPoint() (point, error) {
	deltaLng, err := d.nextDelta()
	if err != nil {
		return point{}, err
	}
	deltaLat, err := d.nextDelta()
	if err != nil {
		return point{}, err
	}
	if addOverflowsInt64(d.lng, int64(deltaLng)) || addOverflowsInt64(d.lat, int64(deltaLat)) {
		return point{}, fmt.Errorf("xiangshan: malformed slab FlatBuffer: coordinate delta overflow")
	}
	d.lng += int64(deltaLng)
	d.lat += int64(deltaLat)
	if d.lng < -18000000 || d.lng > 18000000 || d.lat < -9000000 || d.lat > 9000000 {
		return point{}, fmt.Errorf("xiangshan: malformed slab FlatBuffer: coordinate outside WGS84 range")
	}
	return point{x: float64(d.lng) / compressedCoordinateScale, y: float64(d.lat) / compressedCoordinateScale}, nil
}

func (d *ringDecoder) nextDelta() (int32, error) {
	if d.pos >= len(d.data) {
		return 0, fmt.Errorf("xiangshan: malformed slab FlatBuffer: truncated compressed ring")
	}
	value, n := binary.Uvarint(d.data[d.pos:])
	if n <= 0 || value > math.MaxUint32 {
		return 0, fmt.Errorf("xiangshan: malformed slab FlatBuffer: invalid compressed coordinate")
	}
	d.pos += n
	encoded := uint32(value)
	return int32(encoded>>1) ^ -int32(encoded&1), nil
}

func (d *ringDecoder) finish(_ uint64) error {
	if d.pos != len(d.data) {
		return fmt.Errorf("xiangshan: malformed slab FlatBuffer: compressed ring has %d trailing bytes", len(d.data)-d.pos)
	}
	return nil
}

func addOverflowsInt64(a, b int64) bool {
	return (b > 0 && a > math.MaxInt64-b) || (b < 0 && a < math.MinInt64-b)
}

// raycastSegment is adapted from github.com/ringsaturn/xiangshan/internal/geom,
// itself derived from github.com/tidwall/geojson. Copyright (c) 2026 Han Xiao,
// used under the MIT License; see LICENSE_XIANGSHAN in this package.
func raycastSegment(a, b, p point) raycastResult {
	py := p.y
	if a.y < b.y {
		if py < a.y || py > b.y {
			return raycastResult{}
		}
	} else if a.y > b.y && (py < b.y || py > a.y) {
		return raycastResult{}
	}
	if a.y == b.y {
		if a.x == b.x {
			if p.x == a.x && py == a.y {
				return raycastResult{on: true}
			}
			return raycastResult{}
		}
		if py == b.y && ((a.x < b.x && p.x >= a.x && p.x <= b.x) || (a.x >= b.x && p.x >= b.x && p.x <= a.x)) {
			return raycastResult{on: true}
		}
	}
	if a.x == b.x && p.x == b.x && ((a.y < b.y && py >= a.y && py <= b.y) || (a.y >= b.y && py >= b.y && py <= a.y)) {
		return raycastResult{on: true}
	}
	if (p.x-a.x)/(b.x-a.x) == (py-a.y)/(b.y-a.y) {
		return raycastResult{on: true}
	}
	for py == a.y || py == b.y {
		py = math.Nextafter(py, math.Inf(1))
	}
	if a.y < b.y {
		if py < a.y || py > b.y {
			return raycastResult{}
		}
	} else if py < b.y || py > a.y {
		return raycastResult{}
	}
	if a.x > b.x {
		if p.x >= a.x {
			return raycastResult{}
		}
		if p.x <= b.x {
			return raycastResult{inside: true}
		}
	} else {
		if p.x >= b.x {
			return raycastResult{}
		}
		if p.x <= a.x {
			return raycastResult{inside: true}
		}
	}
	if a.y < b.y {
		if (py-a.y)/(p.x-a.x) >= (b.y-a.y)/(b.x-a.x) {
			return raycastResult{inside: true}
		}
	} else if (py-b.y)/(p.x-b.x) >= (a.y-b.y)/(a.x-b.x) {
		return raycastResult{inside: true}
	}
	return raycastResult{}
}

func bboxContains(bbox [4]float32, lng, lat float64) bool {
	return lng >= float64(bbox[0]) && lng <= float64(bbox[1]) && lat >= float64(bbox[2]) && lat <= float64(bbox[3])
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
