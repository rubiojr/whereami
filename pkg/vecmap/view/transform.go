package view

import "math"

// Affine maps tile-local coordinates into logical viewport pixels.
type Affine struct{ M11, M12, M21, M22, DX, DY float64 }

func (t Affine) MapPoint(point ScreenPoint) ScreenPoint {
	return ScreenPoint{X: t.M11*point.X + t.M12*point.Y + t.DX, Y: t.M21*point.X + t.M22*point.Y + t.DY}
}

// TileTransform maps a canonical tile into a normalized camera's viewport.
// Wrap zero chooses the nearest world copy; other wraps offset that copy.
func TileTransform(camera Camera, tile TileID, wrap int) Affine {
	worldSize := TileSize * math.Exp2(float64(tile.Z))
	centerX, centerY := mercatorWorldPoint(camera.Center, worldSize)
	deltaX := float64(tile.X)*TileSize - centerX
	if worldSize > 0 {
		deltaX -= math.Round(deltaX/worldSize) * worldSize
	}
	deltaY := float64(tile.Y)*TileSize - centerY
	scale := math.Exp2(camera.Zoom - float64(tile.Z))
	angle := -camera.Bearing * math.Pi / 180
	cosAngle, sinAngle := math.Cos(angle), math.Sin(angle)
	m11, m12, m21, m22 := scale*cosAngle, -scale*sinAngle, scale*sinAngle, scale*cosAngle
	t := Affine{M11: m11, M12: m12, M21: m21, M22: m22, DX: camera.Width/2 + m11*deltaX + m12*deltaY, DY: camera.Height/2 + m21*deltaX + m22*deltaY}
	t.DX += t.M11 * float64(wrap) * worldSize
	t.DY += t.M21 * float64(wrap) * worldSize
	return t
}

// TileCoordinate unprojects a tile-local position into WGS84 coordinates.
func TileCoordinate(tile TileID, point ScreenPoint) Coordinate {
	worldSize := TileSize * math.Exp2(float64(tile.Z))
	return coordinateFromMercator(float64(tile.X)*TileSize+point.X, float64(tile.Y)*TileSize+point.Y, worldSize)
}

// PatternPhase aligns repeating patterns across tile and world-copy boundaries.
func PatternPhase(tile TileID, wrap int, width, height float64) (float64, float64) {
	worldTiles := int64(1) << tile.Z
	x := float64(int64(tile.X)+int64(wrap)*worldTiles) * TileSize
	y := float64(tile.Y) * TileSize
	if width > 0 {
		x = math.Mod(x, width)
	}
	if height > 0 {
		y = math.Mod(y, height)
	}
	return x, y
}

// WorldWraps returns bounded world copies needed for the camera viewport.
func WorldWraps(camera Camera) []int {
	worldPixels := TileSize * math.Exp2(camera.Zoom)
	diagonal := math.Hypot(camera.Width, camera.Height)
	if worldPixels <= 0 || worldPixels > diagonal*2 {
		return []int{0}
	}
	maximumWrap := min(int(math.Ceil(diagonal/worldPixels))+1, 8)
	wraps := make([]int, 0, maximumWrap*2+1)
	for wrap := -maximumWrap; wrap <= maximumWrap; wrap++ {
		wraps = append(wraps, wrap)
	}
	return wraps
}
