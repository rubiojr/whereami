package vecmap

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
	id       vectorTileID
	distance float64
}

func (tile vectorTileID) parent() (vectorTileID, bool) {
	if tile.Z == 0 {
		return vectorTileID{}, false
	}
	return vectorTileID{X: tile.X / 2, Y: tile.Y / 2, Z: tile.Z - 1}, true
}

func tileContains(container, tile vectorTileID) bool {
	if container.Z > tile.Z {
		return false
	}
	shift := tile.Z - container.Z
	return tile.X>>shift == container.X && tile.Y>>shift == container.Y
}

func tilesOverlap(left, right vectorTileID) bool {
	return tileContains(left, right) || tileContains(right, left)
}

func visibleTileCover(camera Camera) []vectorTileID {
	camera = camera.normalized()
	if camera.Width <= 0 || camera.Height <= 0 {
		return nil
	}

	sourceZoom := math.Max(0, math.Min(maximumSourceZoom, math.Floor(camera.Zoom)))
	zoom := uint32(sourceZoom)
	dimension := int64(1) << zoom
	worldSize := tileSize * float64(dimension)
	centerX, centerY := mercatorWorldPoint(camera.Center, worldSize)
	scale := math.Exp2(camera.Zoom - float64(zoom))
	angle := camera.Bearing * math.Pi / 180
	cosAngle, sinAngle := math.Cos(angle), math.Sin(angle)

	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, corner := range [...]roadPoint{
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

	startX := int64(math.Floor(minX/tileSize)) - tilePrefetchRing
	endX := int64(math.Floor(maxX/tileSize)) + tilePrefetchRing
	startY := maxInt64(0, int64(math.Floor(minY/tileSize))-tilePrefetchRing)
	endY := minInt64(dimension-1, int64(math.Floor(maxY/tileSize))+tilePrefetchRing)
	startX, endX = boundedTileRange(startX, endX, centerX/tileSize)
	startY, endY = boundedTileRange(startY, endY, centerY/tileSize)
	if startY > endY {
		return nil
	}

	candidates := make(map[vectorTileID]tileCandidate, maximumCoverSide*maximumCoverSide)
	for y := startY; y <= endY; y++ {
		for x := startX; x <= endX; x++ {
			wrappedX := x % dimension
			if wrappedX < 0 {
				wrappedX += dimension
			}
			id := vectorTileID{X: uint32(wrappedX), Y: uint32(y), Z: zoom}
			dx := float64(x)*tileSize + tileSize/2 - centerX
			dy := float64(y)*tileSize + tileSize/2 - centerY
			candidate := tileCandidate{id: id, distance: dx*dx + dy*dy}
			if previous, exists := candidates[id]; !exists || candidate.distance < previous.distance {
				candidates[id] = candidate
			}
		}
	}

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

	cover := make([]vectorTileID, len(ordered))
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
