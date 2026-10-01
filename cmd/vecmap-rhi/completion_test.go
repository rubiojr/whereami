//go:build vecmap_rhi && integration && linux

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	qt "github.com/mappu/miqt/qt6"
	rhi "github.com/rubiojr/whereami/internal/qtrhi"
	"github.com/rubiojr/whereami/internal/vecmaprhi"
	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNativeFinishFailures(t *testing.T) {
	if os.Getenv("QSG_RHI_BACKEND") != "vulkan" {
		t.Skip("requires the checked Qt Vulkan runtime with ELF interposition enabled")
	}
	require.True(t, rhi.LibraryHasSymbol("Qt6Gui", 6, "qt_rhi_checked_vulkan_finish_v1"), "load the checked Qt runtime")
	flags, err := exec.CommandContext(t.Context(), "pkg-config", "--cflags", "Qt6Gui").Output()
	require.NoError(t, err)
	library := filepath.Join(t.TempDir(), "vulkan-faults.so")
	args := append([]string{"-std=c++17", "-shared", "-fPIC", "-o", library, "testdata/vulkan-faults.cpp"}, strings.Fields(string(flags))...)
	args = append(args, "-ldl")
	output, err := exec.CommandContext(t.Context(), "c++", args...).CombinedOutput()
	require.NoError(t, err, "%s", output)
	binary, err := os.Executable()
	require.NoError(t, err)
	for _, loop := range []string{"basic", "threaded"} {
		for _, phase := range []string{"upload", "release"} {
			if phase == "release" && loop != "basic" {
				continue // the direct Planner probe is GUI-owned in the basic loop
			}
			for _, operation := range []string{"wait", "reset", "begin"} {
				for _, result := range []string{"-1", "-4"} { // host-memory error and device loss
					t.Run(loop+"-"+phase+"-"+operation+"-"+result, func(t *testing.T) {
						cmd := exec.CommandContext(t.Context(), binary, "-test.run=^TestNativeFinishFailureProcess$", "-test.timeout=20s", "-test.v")
						cmd.Env = append(os.Environ(), "LD_PRELOAD="+library, "QT_RHI_LEAK_CHECK=1", "QSG_RENDER_LOOP="+loop, "WHEREAMI_QRHI_FAILURE="+operation, "WHEREAMI_QRHI_FAILURE_RESULT="+result, "WHEREAMI_QRHI_FAILURE_PHASE="+phase)
						output, err := cmd.CombinedOutput()
						require.NoError(t, err, "%s", output)
						text := string(output)
						assert.Contains(t, text, "injected_native_failure operation="+operation+" result="+result)
						assert.Contains(t, text, "live_meshes=0 live_textures=0")
						if phase == "release" {
							assert.Contains(t, text, "release_failure reset=true planner_busy=true")
							assert.Contains(t, text, "completion_drains=4")
						} else {
							assert.Contains(t, text, "planner_ready=false native_generation=1 batch_failures=0")
							assert.Contains(t, text, "completion_drains=3")
						}
						assert.NotContains(t, text, "unreleased resources")
						t.Log(text)
					})
				}
			}
		}
	}
}

func TestNativeFinishFailureProcess(t *testing.T) {
	if os.Getenv("WHEREAMI_QRHI_FAILURE") == "" {
		t.Skip("runs as a native fault subprocess")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if os.Getenv("WHEREAMI_QRHI_FAILURE_PHASE") == "release" {
		testNativeReleaseFailure(t)
		return
	}
	err := display(completionDocument(), benchmarkOptions{duration: time.Second, foreground: true, budget: retained.Budget{Bytes: 1024, Resources: 1}})
	require.ErrorIs(t, err, vecmaprhi.ErrNativeCompletion)
}

func TestUnpatchedVulkanFinish(t *testing.T) {
	if os.Getenv("WHEREAMI_TEST_UNPATCHED_QT") != "1" {
		t.Skip("run separately against an unpatched Vulkan runtime")
	}
	require.False(t, rhi.LibraryHasSymbol("Qt6Gui", 6, "qt_rhi_checked_vulkan_finish_v1"))
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	err := display(completionDocument(), benchmarkOptions{duration: time.Second, foreground: true, budget: retained.Budget{Bytes: 1024, Resources: 1}})
	require.ErrorIs(t, err, vecmaprhi.ErrUnsupportedCompletion)
}

func testNativeReleaseFailure(t *testing.T) {
	t.Helper()
	app := qt.NewQApplication([]string{"native-release-failure"})
	defer app.Delete()
	document := completionDocument()
	planner, err := retained.NewPlanner(retained.ResidencyLimits{Bytes: 1024, Resources: 1})
	require.NoError(t, err)
	require.NoError(t, planner.SetTarget(&document.Scene))
	item := rhi.NewQQuickItem()
	defer item.Delete()
	item.SetFlag(rhi.QQuickItem__ItemHasContents)
	var renderer *vecmaprhi.BatchRenderer
	var stats vecmaprhi.Stats
	var results []vecmaprhi.BatchResult
	var queued *retained.Batch
	frame := scene.Frame{Transforms: document.Transforms}
	item.OnUpdatePaintNode(func(_ func(*rhi.QSGNode, *rhi.QQuickItem__UpdatePaintNodeData) *rhi.QSGNode, old *rhi.QSGNode, _ *rhi.QQuickItem__UpdatePaintNodeData) *rhi.QSGNode {
		if old == nil {
			renderer = vecmaprhi.NewBatchRenderer(item, func(value vecmaprhi.Stats) { stats = value }, func(result vecmaprhi.BatchResult) { results = append(results, result) })
			require.NoError(t, renderer.Sync(frame, nil))
		}
		if queued != nil {
			require.NoError(t, renderer.Sync(frame, queued))
			queued = nil
		}
		return renderer.Node.QSGNode
	})
	engine, err := createBenchmarkWindow(document, item, benchmarkOptions{foreground: true})
	require.NoError(t, err)
	defer func() {
		if engine != nil {
			engine.Delete()
		}
	}()
	pump := func(ready func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !ready() && time.Now().Before(deadline) {
			item.Update()
			qt.QCoreApplication_ProcessEvents()
			runtime.Gosched()
		}
		require.True(t, ready(), "native batch did not complete")
	}
	pump(func() bool { return renderer != nil && renderer.Initialized() })
	budget := retained.Budget{Bytes: 1024, Resources: 1}
	queued, err = planner.Next(budget)
	require.NoError(t, err)
	require.NotNil(t, queued)
	pump(func() bool { return len(results) > 0 })
	require.Len(t, results, 1)
	require.True(t, results[0].Success)
	require.NoError(t, planner.Acknowledge(results[0].Ticket, true))
	results = nil
	require.NoError(t, planner.SetTarget(nil))
	queued, err = planner.Next(budget)
	require.NoError(t, err)
	require.NotNil(t, queued)
	require.Len(t, queued.Releases, 1)
	pump(func() bool { return len(results) > 0 })
	require.Len(t, results, 1)
	assert.True(t, results[0].Reset)
	assert.False(t, results[0].Success)
	assert.Zero(t, results[0].Ticket, "namespace reset must not acknowledge retirement")
	assert.ErrorIs(t, results[0].Err, vecmaprhi.ErrNativeCompletion)
	_, err = planner.Next(budget)
	assert.ErrorIs(t, err, retained.ErrBusy, "retirement remains unacknowledged and charged")
	engine.Delete()
	engine = nil
	// Destruction disconnects the observer and does not acknowledge the failed batch.
	assert.Len(t, results, 1)
	assert.Zero(t, stats.LiveMeshes)
	assert.Zero(t, stats.LiveTextures)
	assert.Equal(t, uint64(4), stats.CompletionDrains)
	fmt.Printf("release_failure reset=%t planner_busy=%t live_meshes=%d live_textures=%d completion_drains=%d\n", results[0].Reset, errors.Is(err, retained.ErrBusy), stats.LiveMeshes, stats.LiveTextures, stats.CompletionDrains)
}

func completionDocument() scene.Document {
	return scene.Document{Width: 128, Height: 128, Transforms: []scene.Affine{{M11: 1, M22: 1}}, Scene: scene.Scene{
		Meshes: []scene.Mesh{{ID: 1, Revision: 1, Vertices: []scene.Vertex{{}, {X: 100}, {Y: 100}}}},
		Draws:  []scene.Draw{{Mesh: 1, Count: 3, Material: scene.Material{Color: [4]float32{1, 0, 0, 1}}}},
	}}
}
