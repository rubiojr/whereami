// Package mvt prepares bounded MVT tiles as owned toolkit-neutral feature data.
// It reuses vecmap's protobuf, geometry decoding and Earcut preparation behavior.
package mvt

import (
	"errors"
	"fmt"
	"math"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
)

const (
	MaxGeometryPoints = 1_000_000
	MaxPolygonPoints  = geometry.MaxPolygonPoints
	moveToCommand     = 1
	lineToCommand     = 2
	closePathCommand  = 7
)

var ErrFeatureResourceLimit = errors.New("MVT feature resource limit exceeded")

// DecodePoints decodes a point or multipoint command stream. Positions are in
// 256-unit tile-local coordinates; extent is the layer's nonzero MVT extent.
// Input is neither modified nor retained, and results own their slices. All
// decoders return nil output on failure and preserve the existing point limits.
func DecodePoints(commands []uint32, extent uint32) ([]geometry.Point, error) {
	var (
		x      int64
		y      int64
		points []geometry.Point
	)
	for index := 0; index < len(commands); {
		commandInteger := commands[index]
		index++
		command := commandInteger & 0x7
		count := commandInteger >> 3
		if command != moveToCommand || count == 0 {
			return nil, errors.New("MVT point geometry has an invalid command")
		}
		if uint64(count)*2 > uint64(len(commands)-index) {
			return nil, errors.New("MVT point geometry is truncated")
		}
		if uint64(count) > uint64(MaxGeometryPoints-len(points)) {
			return nil, fmt.Errorf("%w: point geometry exceeds %d-point limit", ErrFeatureResourceLimit, MaxGeometryPoints)
		}
		for range count {
			x += decodeZigZag(commands[index])
			y += decodeZigZag(commands[index+1])
			index += 2
			point := tileLocalPoint(extent, x, y)
			if !validPoint(point) {
				return nil, errors.New("MVT point geometry contains an invalid point")
			}
			points = append(points, point)
		}
	}
	return points, nil
}

// DecodeLineStrings decodes open paths, retaining repeated points and the delta
// cursor across paths. MaxGeometryPoints bounds all paths in this feature.
// Extent, coordinate units and ownership follow DecodePoints.
func DecodeLineStrings(commands []uint32, extent uint32) ([][]geometry.Point, error) {
	var (
		x     int64
		y     int64
		line  []geometry.Point
		lines [][]geometry.Point
		count int
	)
	for index := 0; index < len(commands); {
		commandInteger := commands[index]
		index++
		command := commandInteger & 0x7
		commandCount := commandInteger >> 3
		if commandCount == 0 {
			return nil, errors.New("MVT line command has zero count")
		}
		if command != moveToCommand && command != lineToCommand {
			return nil, fmt.Errorf("MVT line geometry has unsupported command %d", command)
		}
		if command == moveToCommand {
			if commandCount != 1 {
				return nil, errors.New("MVT line MoveTo command count is not one")
			}
			if len(line) > 0 {
				if len(line) < 2 {
					return nil, errors.New("MVT line has fewer than two points")
				}
				lines = append(lines, line)
			}
			line = nil
		} else if line == nil {
			return nil, errors.New("MVT LineTo command appears before MoveTo")
		}
		if uint64(commandCount)*2 > uint64(len(commands)-index) {
			return nil, errors.New("MVT line geometry is truncated")
		}
		if uint64(commandCount) > uint64(MaxGeometryPoints-count) {
			return nil, fmt.Errorf("%w: line geometry exceeds %d-point limit", ErrFeatureResourceLimit, MaxGeometryPoints)
		}
		for range commandCount {
			x += decodeZigZag(commands[index])
			y += decodeZigZag(commands[index+1])
			index += 2
			point := tileLocalPoint(extent, x, y)
			if !validPoint(point) {
				return nil, errors.New("MVT line contains an invalid point")
			}
			line = append(line, point)
			count++
		}
	}
	if len(line) > 0 {
		if len(line) < 2 {
			return nil, errors.New("MVT line has fewer than two points")
		}
		lines = append(lines, line)
	}
	return lines, nil
}

// DecodePolygonRings decodes closed rings without appending a closing point or
// changing winding. MaxPolygonPoints bounds the whole feature, including holes;
// exhaustion wraps geometry.ErrResourceLimit. Ring grouping and triangulation
// are separate operations. Extent, coordinate units and ownership follow DecodePoints.
func DecodePolygonRings(commands []uint32, extent uint32) ([][]geometry.Point, error) {
	var (
		x          int64
		y          int64
		pointCount int
		ring       []geometry.Point
		rings      [][]geometry.Point
	)
	for index := 0; index < len(commands); {
		commandInteger := commands[index]
		index++
		command := commandInteger & 0x7
		count := commandInteger >> 3
		if count == 0 {
			return nil, errors.New("MVT polygon command has zero count")
		}
		switch command {
		case moveToCommand, lineToCommand:
			if command == moveToCommand {
				if count != 1 {
					return nil, errors.New("MVT polygon MoveTo command count is not one")
				}
				if ring != nil {
					return nil, errors.New("MVT polygon starts a ring before closing the previous ring")
				}
			} else if ring == nil {
				return nil, errors.New("MVT polygon LineTo command appears before MoveTo")
			}
			if uint64(count)*2 > uint64(len(commands)-index) {
				return nil, errors.New("MVT polygon command is truncated")
			}
			for range count {
				x += decodeZigZag(commands[index])
				y += decodeZigZag(commands[index+1])
				index += 2
				point := tileLocalPoint(extent, x, y)
				if !validPoint(point) {
					return nil, errors.New("MVT polygon contains an invalid point")
				}
				if pointCount >= MaxPolygonPoints {
					return nil, fmt.Errorf("%w: polygon exceeds %d-point limit", geometry.ErrResourceLimit, MaxPolygonPoints)
				}
				ring = append(ring, point)
				pointCount++
			}
		case closePathCommand:
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

func tileLocalPoint(extent uint32, x, y int64) geometry.Point {
	scale := view.TileSize / float64(extent)
	return geometry.Point{X: float64(x) * scale, Y: float64(y) * scale}
}

func validPoint(point geometry.Point) bool {
	return !math.IsNaN(point.X) && !math.IsInf(point.X, 0) &&
		!math.IsNaN(point.Y) && !math.IsInf(point.Y, 0)
}

func decodeZigZag(value uint32) int64 {
	return int64(value>>1) ^ -int64(value&1)
}
