package mvt

import (
	"errors"
	"fmt"
	"math"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
)

const (
	MaxTileFeatures       = 100_000
	MaxTileStyleTriangles = 500_000
	MaxStyleTriangulation = 50_000_000
)

type Properties map[string]any

func (p Properties) Get(name string) (any, bool) {
	value, exists := p[name]
	return value, exists
}

// Feature is Go-owned source geometry and properties, independent of style zoom.
// Treat it and all reachable slices/maps as immutable after preparation.
type Feature struct {
	ID           uint64
	HasID        bool
	GeometryType uint32
	Properties   Properties
	Points       []geometry.Point
	Lines        [][]geometry.Point
	Polygons     []Polygon
}

// Polygon retains source rings and prepared topology. Nil Indices means Vertices
// is an expanded triangle list; otherwise it contains indexed positions.
type Polygon struct {
	Exterior []geometry.Point
	Holes    [][]geometry.Point
	Vertices []geometry.Point
	Indices  []uint32
}

func (f Feature) PointCount() int {
	count := len(f.Points)
	for _, line := range f.Lines {
		count += len(line)
	}
	for _, polygon := range f.Polygons {
		count += len(polygon.Exterior)
		for _, hole := range polygon.Holes {
			count += len(hole)
		}
	}
	return count
}

// LimitSummary preserves the existing degradation policy: malformed features
// are skipped silently, resource-limited features are counted and the last
// original feature index/error is retained. Logging belongs to the caller.
type LimitSummary struct {
	Skipped          int
	LastFeatureIndex int
	Last             error
}

func (s *LimitSummary) add(index int, err error) {
	s.Skipped++
	s.LastFeatureIndex = index
	s.Last = err
}

// Decoder carries aggregate budgets across layers in one preparation job. Use
// NewDecoder, once per tile; it is not safe for concurrent use. Failed geometry
// still spends triangulation work, but only accepted features charge output.
type Decoder struct {
	indexed                     bool
	features, points, triangles int
	budget                      geometry.Budget
}

func NewDecoder(indexed bool) Decoder {
	return Decoder{indexed: indexed, budget: geometry.Budget{Remaining: MaxStyleTriangulation}}
}

// DecodeLayer prepares owned features, reusing the existing Earcut implementation.
// The layer must come from DecodeLayers. Budgets are shared across every call,
// including duplicate layer names; accepted feature order is preserved.
func (d *Decoder) DecodeLayer(l *Layer) ([]Feature, LimitSummary) {
	features := make([]Feature, 0, min(len(l.features), MaxTileFeatures-d.features))
	var limits LimitSummary
	for index, data := range l.features {
		if d.features >= MaxTileFeatures {
			limits.add(index, fmt.Errorf("%w: tile exceeds %d-feature limit", ErrFeatureResourceLimit, MaxTileFeatures))
			break
		}
		feature, triangles, err := d.prepareFeature(l, data)
		if err != nil {
			if errors.Is(err, ErrFeatureResourceLimit) || errors.Is(err, geometry.ErrResourceLimit) {
				limits.add(index, err)
			}
			continue
		}
		pointCount := feature.PointCount()
		if pointCount == 0 {
			continue
		}
		if pointCount > MaxGeometryPoints-d.points {
			limits.add(index, fmt.Errorf("%w: tile exceeds %d-point limit", ErrFeatureResourceLimit, MaxGeometryPoints))
			continue
		}
		d.features++
		d.points += pointCount
		d.triangles += triangles
		features = append(features, feature)
	}
	return features, limits
}

func (d *Decoder) prepareFeature(l *Layer, data []byte) (Feature, int, error) {
	raw, err := DecodeFeature(data)
	if err != nil {
		return Feature{}, 0, err
	}
	properties, err := l.FeatureProperties(raw.Tags)
	if err != nil {
		return Feature{}, 0, err
	}
	feature := Feature{ID: raw.ID, HasID: raw.HasID, GeometryType: raw.GeometryType, Properties: properties}
	triangles := 0
	switch raw.GeometryType {
	case PointType:
		feature.Points, err = DecodePoints(raw.Geometry, l.extent)
	case LineStringType:
		feature.Lines, err = DecodeLineStrings(raw.Geometry, l.extent)
	case PolygonType:
		var rings [][]geometry.Point
		rings, err = DecodePolygonRings(raw.Geometry, l.extent)
		if err == nil {
			feature.Polygons, err = GroupRings(rings)
		}
		if err == nil {
			triangles, err = d.triangulate(feature.Polygons)
		}
	}
	if err != nil {
		return Feature{}, 0, err
	}
	return feature, triangles, nil
}

func (d *Decoder) triangulate(polygons []Polygon) (int, error) {
	triangles := 0
	for index := range polygons {
		polygon := &polygons[index]
		var mesh geometry.Mesh
		var err error
		if d.indexed {
			mesh, err = geometry.TriangulatePolygon(polygon.Exterior, polygon.Holes, &d.budget)
		} else {
			mesh.Vertices, err = geometry.TriangulatePolygonExpanded(polygon.Exterior, polygon.Holes, &d.budget)
		}
		if err != nil {
			return 0, err
		}
		count := len(mesh.Vertices) / 3
		if mesh.Indices != nil {
			count = len(mesh.Indices) / 3
		}
		if count > MaxTileStyleTriangles-d.triangles-triangles {
			return 0, fmt.Errorf("%w: tile exceeds %d styled-triangle limit", ErrFeatureResourceLimit, MaxTileStyleTriangles)
		}
		polygon.Vertices, polygon.Indices = mesh.Vertices, mesh.Indices
		triangles += count
	}
	return triangles, nil
}

// GroupRings groups MVT winding-ordered rings, ignoring zero-area rings. The
// result borrows input ring slices; no cleanup, reversal or triangulation occurs.
func GroupRings(rings [][]geometry.Point) ([]Polygon, error) {
	polygons := make([]Polygon, 0, len(rings))
	for _, ring := range rings {
		area := geometry.SignedRingArea(ring)
		if math.Abs(area) <= geometry.Epsilon {
			continue
		}
		if area > 0 {
			polygons = append(polygons, Polygon{Exterior: ring})
			continue
		}
		if len(polygons) == 0 {
			return nil, errors.New("MVT polygon starts with an interior ring")
		}
		last := len(polygons) - 1
		polygons[last].Holes = append(polygons[last].Holes, ring)
	}
	if len(polygons) == 0 {
		return nil, errors.New("MVT polygon has no exterior rings")
	}
	return polygons, nil
}
