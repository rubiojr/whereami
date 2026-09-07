package vecmap_test

import (
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap"
	"github.com/stretchr/testify/assert"
)

func TestPublicCameraAPI(t *testing.T) {
	camera := vecmap.NewCamera(
		vecmap.Coordinate{Latitude: 51.5074, Longitude: -0.1278},
		9,
		0,
		800,
		600,
	)
	point := camera.FromCoordinate(camera.Center)

	assert.Equal(t, vecmap.ScreenPoint{X: 400, Y: 300}, point)
}

func TestOptionsExposeReusableInitialization(t *testing.T) {
	initialCamera := vecmap.Camera{
		Center: vecmap.Coordinate{Latitude: 35.6762, Longitude: 139.6503},
		Zoom:   12,
		Width:  800,
		Height: 600,
	}
	options := vecmap.Options{
		CacheDir:      t.TempDir(),
		InitialCamera: &initialCamera,
	}

	assert.Equal(t, float64(12), options.InitialCamera.Zoom)
}
