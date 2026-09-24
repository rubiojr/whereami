package view

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTileTransformMatchesGeographicProjection(t *testing.T) {
	for _, test := range []struct {
		tile          TileID
		center        Coordinate
		zoom, bearing float64
	}{
		{TileID{250, 193, 9}, Coordinate{40.4168, -3.7038}, 9.5, 37},
		{TileID{0, 2, 2}, Coordinate{0, 179.9}, 2.5, 90},
		{TileID{8192, 8192, 14}, Coordinate{}, 20, 225},
	} {
		camera := NewCamera(test.center, test.zoom, test.bearing, 800, 600)
		transform := TileTransform(camera, test.tile, 0)
		for _, point := range []ScreenPoint{{32, 64}, {128, 128}, {240, 220}} {
			want := camera.FromCoordinate(TileCoordinate(test.tile, point))
			got := transform.MapPoint(point)
			require.InDelta(t, want.X, got.X, 1e-6)
			require.InDelta(t, want.Y, got.Y, 1e-6)
		}
		wrapped := TileTransform(camera, test.tile, 1)
		world := TileSize * math.Exp2(float64(test.tile.Z))
		assert.InDelta(t, world*transform.M11, wrapped.DX-transform.DX, 1e-6)
		assert.InDelta(t, world*transform.M21, wrapped.DY-transform.DY, 1e-6)
	}
}

func BenchmarkTileTransform(b *testing.B) {
	camera := NewCamera(Coordinate{40, -3}, 9.5, 37, 800, 600)
	tile := TileID{250, 193, 9}
	b.ReportAllocs()
	for b.Loop() {
		TileTransform(camera, tile, 0)
	}
}

func TestRootWorldCopyCoversViewport(t *testing.T) {
	for _, longitude := range []float64{-180, -179.9, -90, 0, 90, 179.9} {
		for _, zoom := range []float64{0, 2, 10} {
			for _, bearing := range []float64{0, 37, 90, 225} {
				for _, size := range [][2]float64{{256, 256}, {1300, 800}} {
					camera := NewCamera(Coordinate{Longitude: longitude}, zoom, bearing, size[0], size[1])
					transform := TileTransform(camera, TileID{}, 0)
					center := transform.MapPoint(ScreenPoint{128, 128})
					want := camera.FromCoordinate(Coordinate{})
					assert.InDelta(t, want.X, center.X, 1e-6)
					assert.InDelta(t, want.Y, center.Y, 1e-6)
					wraps := WorldWraps(camera)
					for _, x := range []float64{0, size[0] / 2, size[0]} {
						for _, y := range []float64{0, size[1] / 2, size[1]} {
							dx, dy := x-transform.DX, y-transform.DY
							det := transform.M11*transform.M22 - transform.M12*transform.M21
							localX := (dx*transform.M22 - dy*transform.M12) / det
							localY := (dy*transform.M11 - dx*transform.M21) / det
							if localY < 0 || localY > TileSize {
								continue
							} // beyond Mercator's poles
							covered := false
							for _, wrap := range wraps {
								wrapped := localX - float64(wrap)*TileSize
								covered = covered || (wrapped >= -1e-6 && wrapped <= TileSize+1e-6)
							}
							assert.True(t, covered, "lon=%g zoom=%g bearing=%g viewport=%v point=(%g,%g) wraps=%v", longitude, zoom, bearing, size, x, y, wraps)
						}
					}
				}
			}
		}
	}
}
