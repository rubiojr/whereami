// Package placement prepares toolkit-neutral symbol candidates and collision
// decisions, including projected boxes. Source features and metric snapshots are
// borrowed synchronously; readiness and rendering policy are caller-owned.
package placement

import (
	"math"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/mvt"
)

const MaxLineAnchors = 16

// Anchor uses tile-local coordinates and radians. Angle is the existing upright
// text angle; RawAngle is the source segment direction. Angle is intentionally
// not normalized after flipping, so a westbound line has Angle=2*pi, RawAngle=pi.
type Anchor struct {
	Point    geometry.Point
	Angle    float64
	RawAngle float64
}

// FeatureAnchors reuses the existing selection/order rules. Line placement uses
// repeated anchors; line-center uses one midpoint per nondegenerate line. Other
// placement strings select points then exterior-ring centroids, falling back to
// line midpoints only when neither points nor polygons produced anchors.
//
// Input is validated, bounded MVT source geometry (normally from mvt.DecodeTile).
// Results own their slice. Total output/work follows input cardinality, with at
// most MaxLineAnchors per line; callers retain their per-tile candidate budgets.
func FeatureAnchors(feature mvt.Feature, placement string, spacing float64) []Anchor {
	if placement == "line" || placement == "line-center" {
		anchors := make([]Anchor, 0, len(feature.Lines))
		for _, line := range feature.Lines {
			if placement == "line-center" {
				if anchor, ok := LineAnchor(line, 0.5); ok {
					anchors = append(anchors, anchor)
				}
				continue
			}
			anchors = append(anchors, RepeatedLineAnchors(line, spacing)...)
		}
		return anchors
	}
	anchors := make([]Anchor, 0, len(feature.Points)+len(feature.Polygons))
	for _, point := range feature.Points {
		anchors = append(anchors, Anchor{Point: point})
	}
	for _, polygon := range feature.Polygons {
		anchors = append(anchors, Anchor{Point: PolygonCentroid(polygon.Exterior)})
	}
	if len(anchors) == 0 {
		for _, line := range feature.Lines {
			if anchor, ok := LineAnchor(line, 0.5); ok {
				anchors = append(anchors, anchor)
			}
		}
	}
	return anchors
}

// RepeatedLineAnchors evenly distributes at most 16 anchors along a line. Spacing
// is tile-local; nonpositive spacing or a shorter line uses its midpoint. NaN
// spacing and degenerate/nonfinite line lengths return nil. Inputs are not retained.
func RepeatedLineAnchors(line []geometry.Point, spacing float64) []Anchor {
	length := lineLength(line)
	if length <= geometry.Epsilon || !finite(length) || math.IsNaN(spacing) {
		return nil
	}
	if spacing <= 0 || length < spacing {
		anchor, ok := LineAnchor(line, 0.5)
		if !ok {
			return nil
		}
		return []Anchor{anchor}
	}
	// Clamp while still floating point: length/spacing may exceed int's range
	// (or overflow to infinity), especially with tiny spacing on 32-bit targets.
	count := int(min(float64(MaxLineAnchors), max(1, math.Floor(length/spacing))))
	anchors := make([]Anchor, 0, count)
	for index := range count {
		fraction := (float64(index) + 0.5) / float64(count)
		if anchor, ok := LineAnchor(line, fraction); ok {
			anchors = append(anchors, anchor)
		}
	}
	return anchors
}

// LineAnchor interpolates by arc length, clamping fraction to [0,1]. A target at
// a vertex uses the incoming segment. Length accumulation includes tiny segments,
// but anchor search skips segments at or below geometry.Epsilon, as before.
// Degenerate/nonfinite lengths, NaN fractions or an unreachable target fail.
func LineAnchor(line []geometry.Point, fraction float64) (Anchor, bool) {
	total := lineLength(line)
	if total <= geometry.Epsilon || !finite(total) || math.IsNaN(fraction) {
		return Anchor{}, false
	}
	target := max(0, min(1, fraction)) * total
	traversed := 0.0
	for index := 1; index < len(line); index++ {
		first, second := line[index-1], line[index]
		length := math.Hypot(second.X-first.X, second.Y-first.Y)
		if length <= geometry.Epsilon {
			continue
		}
		if traversed+length >= target {
			factor := (target - traversed) / length
			rawAngle := math.Atan2(second.Y-first.Y, second.X-first.X)
			angle := rawAngle
			if angle > math.Pi/2 || angle < -math.Pi/2 {
				angle += math.Pi
			}
			return Anchor{
				Point: geometry.Point{X: first.X + (second.X-first.X)*factor, Y: first.Y + (second.Y-first.Y)*factor},
				Angle: angle, RawAngle: rawAngle,
			}, true
		}
		traversed += length
	}
	return Anchor{}, false
}

func lineLength(line []geometry.Point) float64 {
	length := 0.0
	for index := 1; index < len(line); index++ {
		length += math.Hypot(line[index].X-line[index-1].X, line[index].Y-line[index-1].Y)
	}
	return length
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

// PolygonCentroid reuses the exterior-ring centroid arithmetic, including the
// near-zero-area fallback that adds points to the accumulated numerator before averaging.
// Holes are ignored, and the result is not guaranteed to lie inside the polygon.
// Input is finite, bounded tile-local geometry; an empty ring returns the origin.
func PolygonCentroid(ring []geometry.Point) geometry.Point {
	if len(ring) == 0 {
		return geometry.Point{}
	}
	area := 0.0
	centroid := geometry.Point{}
	for index, first := range ring {
		second := ring[(index+1)%len(ring)]
		cross := first.X*second.Y - second.X*first.Y
		area += cross
		centroid.X += (first.X + second.X) * cross
		centroid.Y += (first.Y + second.Y) * cross
	}
	if math.Abs(area) <= geometry.Epsilon {
		for _, point := range ring {
			centroid.X += point.X
			centroid.Y += point.Y
		}
		centroid.X /= float64(len(ring))
		centroid.Y /= float64(len(ring))
		return centroid
	}
	centroid.X /= 3 * area
	centroid.Y /= 3 * area
	return centroid
}
