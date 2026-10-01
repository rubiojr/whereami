//go:build vecmap_rhi && integration

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testViewerReload(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "scene.json")
	output := filepath.Join(t.TempDir(), "final.png")
	document := feedDocument()
	initial, err := json.Marshal(document)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, initial, 0600))
	document.Transforms = []scene.Affine{{M11: 1, M22: 1, DX: 1000}, {M11: 1, M22: 1, DX: 50}}
	document.Scene.Draws[0].Transform = 1
	document.Labels = 123 // visible in final process metrics too
	replacement, err := json.Marshal(document)
	require.NoError(t, err)
	changed := make(chan error, 1)
	go func() {
		time.Sleep(150 * time.Millisecond)
		err := os.WriteFile(path+".new", replacement, 0600)
		if err == nil {
			err = os.Rename(path+".new", path)
		}
		changed <- err
	}()
	// Desktop Vulkan can deliver only a handful of frames during its first
	// second. Allow checked startup/upload/retirement to finish; readiness and
	// replacement pixels are still mandatory, never inferred from elapsed time.
	// Diagnostics connect and disconnect the swapchain signals in both loops.
	err = run(path, benchmarkOptions{duration: 5 * time.Second, screenshot: output, foreground: true, diagnostics: true, reload: 10 * time.Millisecond, budget: retained.Budget{Bytes: 1024, Resources: 1}})
	require.NoError(t, <-changed)
	require.NoError(t, err)
	image := qt.NewQImage8(output)
	defer image.Delete()
	require.False(t, image.IsNull())
	scale := float64(image.Width()) / float64(document.Width)
	old := image.PixelColor(int(2*scale), int(2*scale))
	defer runtime.KeepAlive(old)
	assert.Greater(t, old.Red(), 200)
	current := image.PixelColor(int(52*scale), int(2*scale))
	defer runtime.KeepAlive(current)
	assert.InDelta(t, 0, current.Red(), 2)
	assert.InDelta(t, 255, current.Green(), 2)
}
