//go:build vecmap_rhi

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func feedDocument() scene.Document {
	return scene.Document{Width: 160, Height: 120, Transforms: []scene.Affine{{M11: 1, M22: 1}}, Scene: scene.Scene{
		Meshes: []scene.Mesh{{ID: 7, Vertices: []scene.Vertex{{}, {X: 10}, {Y: 10}}}},
		Draws:  []scene.Draw{{Mesh: 7, Count: 3, Material: scene.Material{Color: [4]float32{0, 1, 0, 1}}}},
	}}
}

func TestSceneFeedReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scene.json")
	write := func(d scene.Document) {
		t.Helper()
		data, err := json.Marshal(d)
		require.NoError(t, err)
		temporary := path + ".new"
		require.NoError(t, os.WriteFile(temporary, data, 0600))
		require.NoError(t, os.Rename(temporary, path))
	}
	document := feedDocument()
	write(document)
	f, err := newSceneFeed(path, 5*time.Millisecond)
	require.NoError(t, err)
	t.Cleanup(f.close)
	first := f.latest.Load()
	assert.Equal(t, uint64(1), first.Scene.Meshes[0].Revision, "source revision zero is remapped")
	assert.NotEqual(t, document.Scene.Meshes[0].ID, first.Scene.Meshes[0].ID)
	write(document) // same bytes, new inode: no target or upload churn
	assert.Never(t, func() bool { return f.latest.Load() != first }, 30*time.Millisecond, time.Millisecond)
	document.Transforms = append(document.Transforms, scene.Affine{M11: 1, M22: 1, DX: 40})
	document.Scene.Draws[0].Transform = 1
	document.Scene.Meshes[0].Vertices[1].X = 20
	write(document)
	require.Eventually(t, func() bool { return f.latest.Load() != first }, 5*time.Second, time.Millisecond)
	second := f.latest.Load()
	assert.Equal(t, first.Scene.Meshes[0].ID, second.Scene.Meshes[0].ID)
	assert.Equal(t, uint64(2), second.Scene.Meshes[0].Revision)
	assert.Equal(t, 1, second.Scene.Draws[0].Transform)
	assert.Len(t, second.Transforms, 2)
	assert.Equal(t, float32(10), first.Scene.Meshes[0].Vertices[1].X, "old packet payload stays immutable")
	assert.Len(t, first.Transforms, 1)
	require.NoError(t, os.WriteFile(path, []byte(`{"Scene":`), 0600))
	require.Eventually(t, func() bool { return len(f.errors) > 0 }, 5*time.Second, time.Millisecond)
	assert.Error(t, <-f.errors)
	assert.Same(t, second, f.latest.Load())
	document.Scene.Meshes[0].Vertices[1].X = 30
	write(document)
	require.Eventually(t, func() bool { return f.latest.Load() != second }, 5*time.Second, time.Millisecond)
	assert.Equal(t, uint64(3), f.latest.Load().Scene.Meshes[0].Revision, "invalid input consumes no revision")
	f.close()
	f.close()
}

func TestSceneFeedRejection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scene.json")
	_, err := newSceneFeed(path, -time.Second)
	assert.ErrorContains(t, err, "negative")
	_, err = newSceneFeed(path, 0)
	assert.Error(t, err)
	document := feedDocument()
	data, err := json.Marshal(document)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0600))
	f, err := newSceneFeed(path, 0)
	require.NoError(t, err)
	t.Cleanup(f.close)
	initial := f.latest.Load()
	assert.Error(t, f.replace(append(data, []byte(` {}`)...)), "trailing document rejects")
	for _, mutate := range []func(*scene.Document){
		func(d *scene.Document) { d.Width++ },
		func(d *scene.Document) { d.Transforms = make([]scene.Affine, 65537) },
		func(d *scene.Document) { d.Scene.Draws[0].Transform = 2 },
		func(d *scene.Document) {
			d.Camera = &view.Camera{}
			d.TileSpaces = []scene.TileSpace{{Tile: view.TileID{Z: 1, X: 0, Y: 0}}}
		},
	} {
		d := feedDocument()
		mutate(&d)
		data, err := json.Marshal(d)
		require.NoError(t, err)
		assert.Error(t, f.replace(data))
		assert.Same(t, initial, f.latest.Load())
	}
	// A sparse oversized file rejects without attempting JSON decoding.
	file, err := os.OpenFile(path, os.O_WRONLY, 0600)
	require.NoError(t, err)
	require.NoError(t, file.Truncate(maxDocumentBytes+1))
	require.NoError(t, file.Close())
	_, err = readDocumentBytes(path)
	assert.ErrorContains(t, err, "exceeds")
}

func TestSceneCameraMapping(t *testing.T) {
	old := feedDocument()
	newDocument := feedDocument()
	newDocument.Transforms = []scene.Affine{{DX: 900}, {M11: 1, M22: 1, DX: 30}}
	newDocument.Scene.Draws[0].Transform = 1
	camera := streamCamera{affine: scene.Affine{M11: 2, M22: 2, DX: 10}, dpr: 2}
	assert.Nil(t, camera.frame(nil).Scene)
	assert.Equal(t, float32(2), camera.frame(nil).DevicePixelRatio)
	assert.Equal(t, float32(10), camera.frame(&old).Transforms[0].DX)
	assert.Equal(t, float32(70), camera.frame(&newDocument).Transforms[1].DX)
	assert.Equal(t, float32(30), newDocument.Transforms[1].DX, "camera updates cannot mutate target data")
	geo := view.NewCamera(view.Coordinate{Latitude: 40, Longitude: -3}, 5, 20, 160, 120)
	old.Camera = &geo
	old.TileSpaces = []scene.TileSpace{{Tile: view.TileID{Z: 4, X: 7, Y: 6}}}
	newDocument.Camera = &geo
	newDocument.TileSpaces = []scene.TileSpace{{Tile: view.TileID{Z: 3, X: 3, Y: 3}}, {Tile: view.TileID{Z: 5, X: 15, Y: 12}, Wrap: 1}}
	for _, animate := range []bool{false, true} {
		c := traceCamera(old, 0.4, animate)
		require.NotNil(t, c.geographic)
		for _, d := range []*scene.Document{&old, &newDocument} {
			assert.Equal(t, d.FrameAt(*c.geographic).Transforms, c.frame(d).Transforms)
			assert.Same(t, &d.Scene, c.frame(d).Scene)
		}
	}
	assert.NotEqual(t, traceCamera(feedDocument(), 0.4, false).affine, traceCamera(feedDocument(), 0.4, true).affine)
}
