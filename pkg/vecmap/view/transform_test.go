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
