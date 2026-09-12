package scene

import (
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeographicFramesRetainResources(t *testing.T) {
	camera := view.NewCamera(view.Coordinate{}, 4, 0, 256, 256)
	document := Document{Scene: Scene{Meshes: []Mesh{{ID: 1, Revision: 7, Vertices: make([]Vertex, 3)}}}, Camera: &camera, TileSpaces: []TileSpace{{Tile: view.TileID{8, 8, 4}}}}
	first := document.FrameAt(camera)
	zoomed := document.FrameAt(camera.ZoomedAt(5, view.ScreenPoint{128, 128}))
	require.Same(t, first.Scene, zoomed.Scene)
	assert.Equal(t, uint64(7), zoomed.Scene.Meshes[0].Revision)
	require.Len(t, zoomed.Transforms, 1)
	assert.Equal(t, 2*first.Transforms[0].M11, zoomed.Transforms[0].M11)
	assert.Equal(t, 2*first.Transforms[0].M22, zoomed.Transforms[0].M22)
}

func TestDocumentRejectsInvalidGeographicMetadata(t *testing.T) {
	camera := view.NewCamera(view.Coordinate{}, 4, 0, 256, 256)
	document := Document{Width: 256, Height: 256, Scene: Scene{Meshes: []Mesh{{ID: 1, Vertices: make([]Vertex, 3)}}, Draws: []Draw{{Mesh: 1, Count: 3}}}, Camera: &camera, TileSpaces: []TileSpace{{Tile: view.TileID{0, 0, 40}}}}
	assert.ErrorContains(t, document.Validate(), "invalid tile space")
	document.TileSpaces[0].Tile = view.TileID{0, 0, 4}
	require.NoError(t, document.Validate())
	document.Camera = nil
	assert.ErrorContains(t, document.Validate(), "require a camera")
	document.Camera = &camera
	document.Scene.Draws[0].Transform = 1
	assert.ErrorContains(t, document.Validate(), "missing transform")
}
