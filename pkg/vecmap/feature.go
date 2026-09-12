package vecmap

import (
	"errors"
	"fmt"
	"math"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
)

const (
	maxTileFeatures       = 100_000
	maxTileGeometryPoints = 1_000_000
	maxTileStyleTriangles = 500_000
	maxStyleTriangulation = 50_000_000
)

var errFeatureResourceLimit = errors.New("MVT feature resource limit exceeded")

type vectorFeature struct {
	id         uint64
	hasID      bool
	geometryID uint32
	properties featureProperties
	points     []roadPoint
	lines      [][]roadPoint
	polygons   []vectorPolygon
}

type vectorPolygon struct {
	exterior  []roadPoint
	holes     [][]roadPoint
	triangles []roadPoint
	// When present, triangles holds unique vertices and indices retains topology.
	indices []uint32
}

type resourceLimitSummary struct {
	skipped          int
	lastFeatureIndex int
	last             error
}

func (s *resourceLimitSummary) add(featureIndex int, err error) {
	s.skipped++
	s.lastFeatureIndex = featureIndex
	s.last = err
}

func (l *mvtLayer) decodeFeatures(
	totalFeatures, totalPoints, totalTriangles *int,
	budget *triangulationBudget,
) ([]vectorFeature, resourceLimitSummary) {
	return l.decodeFeaturesGeometry(totalFeatures, totalPoints, totalTriangles, budget, false)
}

func (l *mvtLayer) decodeFeaturesGeometry(
	totalFeatures, totalPoints, totalTriangles *int,
	budget *triangulationBudget, indexed bool,
) ([]vectorFeature, resourceLimitSummary) {
	features := make([]vectorFeature, 0, len(l.features))
	var limits resourceLimitSummary
	for featureIndex, data := range l.features {
		if *totalFeatures >= maxTileFeatures {
			limits.add(featureIndex, fmt.Errorf("%w: tile exceeds %d-feature limit", errFeatureResourceLimit, maxTileFeatures))
			break
		}
		feature, err := decodeMVTFeature(data)
		if err != nil {
			continue
		}
		properties, err := l.featureProperties(feature.tags)
		if err != nil {
			continue
		}
		decoded := vectorFeature{
			id:         feature.id,
			hasID:      feature.hasID,
			geometryID: feature.geometryID,
			properties: properties,
		}
		featureTriangles := 0
		switch feature.geometryID {
		case mvtPointType:
			decoded.points, err = decodeMVTPoints(feature.geometry, l.extent)
		case mvtLineStringType:
			decoded.lines, err = decodeMVTLineStrings(feature.geometry, l.extent)
		case mvtPolygonType:
			var rings [][]roadPoint
			rings, err = decodeMVTPolygonRings(feature.geometry, l.extent)
			if err == nil {
				decoded.polygons, err = groupMVTRings(rings)
			}
			if err == nil {
				for index := range decoded.polygons {
					mesh, triangulationErr := triangulateFeaturePolygon(decoded.polygons[index], budget, indexed)
					if triangulationErr != nil {
						err = triangulationErr
						break
					}
					triangleCount := len(mesh.Vertices) / 3
					if mesh.Indices != nil {
						triangleCount = len(mesh.Indices) / 3
					}
					if triangleCount > maxTileStyleTriangles-*totalTriangles-featureTriangles {
						err = fmt.Errorf("%w: tile exceeds %d styled-triangle limit", errFeatureResourceLimit, maxTileStyleTriangles)
						break
					}
					decoded.polygons[index].triangles = mesh.Vertices
					decoded.polygons[index].indices = mesh.Indices
					featureTriangles += triangleCount
				}
			}
		default:
			continue
		}
		if err != nil {
			if errors.Is(err, errFeatureResourceLimit) || errors.Is(err, errPolygonResourceLimit) {
				limits.add(featureIndex, err)
			}
			continue
		}
		pointCount := decoded.pointCount()
		if pointCount == 0 {
			continue
		}
		if pointCount > maxTileGeometryPoints-*totalPoints {
			limits.add(featureIndex, fmt.Errorf("%w: tile exceeds %d-point limit", errFeatureResourceLimit, maxTileGeometryPoints))
			continue
		}
		*totalFeatures++
		*totalPoints += pointCount
		*totalTriangles += featureTriangles
		features = append(features, decoded)
	}
	return features, limits
}

func triangulateFeaturePolygon(polygon vectorPolygon, budget *triangulationBudget, indexed bool) (geometry.Mesh, error) {
	if !indexed {
		vertices, err := triangulatePolygonBounded(polygon, budget)
		return geometry.Mesh{Vertices: vertices}, err
	}
	core := budget.geometryBudget()
	mesh, err := geometry.TriangulatePolygon(polygon.exterior, polygon.holes, core)
	budget.updateGeometryBudget(core)
	return mesh, err
}

func (f vectorFeature) pointCount() int {
	count := len(f.points)
	for _, line := range f.lines {
		count += len(line)
	}
	for _, polygon := range f.polygons {
		count += len(polygon.exterior)
		for _, hole := range polygon.holes {
			count += len(hole)
		}
	}
	return count
}

func decodeMVTPoints(geometry []uint32, extent uint32) ([]roadPoint, error) {
	var (
		x      int64
		y      int64
		points []roadPoint
	)
	for index := 0; index < len(geometry); {
		commandInteger := geometry[index]
		index++
		command := commandInteger & 0x7
		count := commandInteger >> 3
		if command != mvtMoveToCommand || count == 0 {
			return nil, errors.New("MVT point geometry has an invalid command")
		}
		if uint64(count)*2 > uint64(len(geometry)-index) {
			return nil, errors.New("MVT point geometry is truncated")
		}
		if uint64(count) > uint64(maxTileGeometryPoints-len(points)) {
			return nil, fmt.Errorf("%w: point geometry exceeds %d-point limit", errFeatureResourceLimit, maxTileGeometryPoints)
		}
		for range count {
			x += decodeZigZag(geometry[index])
			y += decodeZigZag(geometry[index+1])
			index += 2
			point := tileLocalPoint(extent, x, y)
			if !validRoadPoint(point) {
				return nil, errors.New("MVT point geometry contains an invalid point")
			}
			points = append(points, point)
		}
	}
	return points, nil
}

func decodeMVTLineStrings(geometry []uint32, extent uint32) ([][]roadPoint, error) {
	var (
		x     int64
		y     int64
		line  []roadPoint
		lines [][]roadPoint
		count int
	)
	for index := 0; index < len(geometry); {
		commandInteger := geometry[index]
		index++
		command := commandInteger & 0x7
		commandCount := commandInteger >> 3
		if commandCount == 0 {
			return nil, errors.New("MVT line command has zero count")
		}
		if command != mvtMoveToCommand && command != mvtLineToCommand {
			return nil, fmt.Errorf("MVT line geometry has unsupported command %d", command)
		}
		if command == mvtMoveToCommand {
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
		if uint64(commandCount)*2 > uint64(len(geometry)-index) {
			return nil, errors.New("MVT line geometry is truncated")
		}
		if uint64(commandCount) > uint64(maxTileGeometryPoints-count) {
			return nil, fmt.Errorf("%w: line geometry exceeds %d-point limit", errFeatureResourceLimit, maxTileGeometryPoints)
		}
		for range commandCount {
			x += decodeZigZag(geometry[index])
			y += decodeZigZag(geometry[index+1])
			index += 2
			point := tileLocalPoint(extent, x, y)
			if !validRoadPoint(point) {
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

func groupMVTRings(rings [][]roadPoint) ([]vectorPolygon, error) {
	polygons := make([]vectorPolygon, 0, len(rings))
	for _, ring := range rings {
		area := signedRingArea(ring)
		if math.Abs(area) <= polygonEpsilon {
			continue
		}
		if area > 0 {
			polygons = append(polygons, vectorPolygon{exterior: ring})
			continue
		}
		if len(polygons) == 0 {
			return nil, errors.New("MVT polygon starts with an interior ring")
		}
		last := len(polygons) - 1
		polygons[last].holes = append(polygons[last].holes, ring)
	}
	if len(polygons) == 0 {
		return nil, errors.New("MVT polygon has no exterior rings")
	}
	return polygons, nil
}
