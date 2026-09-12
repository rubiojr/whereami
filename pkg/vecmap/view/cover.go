package view

import (
	"math"
	"sort"
)

const (
	maximumSourceZoom = 14
	tilePrefetchRing  = 1
	maximumCoverSide  = 8
)

type tileCandidate struct {
	id       TileID
	distance float64
}

// TileID identifies a canonical Web Mercator XYZ tile.
type TileID struct{ X, Y, Z uint32 }

// Valid reports whether this is a canonical XYZ address representable by TileID.
func (tile TileID) Valid() bool {
	if tile.Z > 32 {
		return false
	}
	dimension := uint64(1) << tile.Z
	return uint64(tile.X) < dimension && uint64(tile.Y) < dimension
}

// Parent returns the tile at the preceding source zoom, if one exists.
func (tile TileID) Parent() (TileID, bool) {
	if tile.Z == 0 {
		return TileID{}, false
	}
	return TileID{X: tile.X / 2, Y: tile.Y / 2, Z: tile.Z - 1}, true
}

// TileContains reports whether container covers tile.
func TileContains(container, tile TileID) bool {
	if container.Z > tile.Z {
		return false
	}
	shift := tile.Z - container.Z
	return tile.X>>shift == container.X && tile.Y>>shift == container.Y
}

// TilesOverlap reports whether either tile contains the other.
func TilesOverlap(left, right TileID) bool {
	return TileContains(left, right) || TileContains(right, left)
}

// VisibleTileCover returns nearest-first canonical tiles with a prefetch ring.
// It preserves vecmap's bounded policy: source zoom <=14 and at most 8x8 tiles.
func VisibleTileCover(camera Camera) []TileID {
	camera = camera.normalized()
	if camera.Width <= 0 || camera.Height <= 0 {
		return nil
	}

	sourceZoom := math.Max(0, math.Min(maximumSourceZoom, math.Floor(camera.Zoom)))
	zoom := uint32(sourceZoom)
	dimension := int64(1) << zoom
	worldSize := TileSize * float64(dimension)
	centerX, centerY := mercatorWorldPoint(camera.Center, worldSize)
	scale := math.Exp2(camera.Zoom - float64(zoom))
	angle := camera.Bearing * math.Pi / 180
	cosAngle, sinAngle := math.Cos(angle), math.Sin(angle)

	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, corner := range [...]ScreenPoint{
		{X: -camera.Width / 2, Y: -camera.Height / 2},
		{X: camera.Width / 2, Y: -camera.Height / 2},
		{X: camera.Width / 2, Y: camera.Height / 2},
		{X: -camera.Width / 2, Y: camera.Height / 2},
	} {
		dx := (cosAngle*corner.X - sinAngle*corner.Y) / scale
		dy := (sinAngle*corner.X + cosAngle*corner.Y) / scale
		minX = math.Min(minX, centerX+dx)
		maxX = math.Max(maxX, centerX+dx)
		minY = math.Min(minY, centerY+dy)
		maxY = math.Max(maxY, centerY+dy)
	}

	startX := int64(math.Floor(minX/TileSize)) - tilePrefetchRing
	endX := int64(math.Floor(maxX/TileSize)) + tilePrefetchRing
	startY := maxInt64(0, int64(math.Floor(minY/TileSize))-tilePrefetchRing)
	endY := minInt64(dimension-1, int64(math.Floor(maxY/TileSize))+tilePrefetchRing)
	startX, endX = boundedTileRange(startX, endX, centerX/TileSize)
	startY, endY = boundedTileRange(startY, endY, centerY/TileSize)
	if startY > endY {
		return nil
	}

	candidates := make(map[TileID]tileCandidate, maximumCoverSide*maximumCoverSide)
	for y := startY; y <= endY; y++ {
		for x := startX; x <= endX; x++ {
			wrappedX := x % dimension
			if wrappedX < 0 {
				wrappedX += dimension
			}
			id := TileID{X: uint32(wrappedX), Y: uint32(y), Z: zoom}
			dx := float64(x)*TileSize + TileSize/2 - centerX
			dy := float64(y)*TileSize + TileSize/2 - centerY
			candidate := tileCandidate{id: id, distance: dx*dx + dy*dy}
			if previous, exists := candidates[id]; !exists || candidate.distance < previous.distance {
				candidates[id] = candidate
			}
		}
	}

	return orderCandidates(candidates)
}

func orderCandidates(candidates map[TileID]tileCandidate) []TileID {
	ordered := make([]tileCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		ordered = append(ordered, candidate)
	}
	sort.Slice(ordered, func(left, right int) bool {
		if ordered[left].distance != ordered[right].distance {
			return ordered[left].distance < ordered[right].distance
		}
		if ordered[left].id.Y != ordered[right].id.Y {
			return ordered[left].id.Y < ordered[right].id.Y
		}
		return ordered[left].id.X < ordered[right].id.X
	})

	cover := make([]TileID, len(ordered))
	for index, candidate := range ordered {
		cover[index] = candidate.id
	}
	return cover
}

func boundedTileRange(start, end int64, center float64) (int64, int64) {
	if end-start+1 <= maximumCoverSide {
		return start, end
	}
	boundedStart := int64(math.Round(center - float64(maximumCoverSide)/2))
	boundedStart = maxInt64(start, boundedStart)
	boundedStart = minInt64(end-maximumCoverSide+1, boundedStart)
	return boundedStart, boundedStart + maximumCoverSide - 1
}

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
