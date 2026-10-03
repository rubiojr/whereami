//go:build vecmap_rhi

package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDRMClient(t *testing.T) {
	client, ok := parseDRMClient("pos:\t0\nflags:\t02100002\ndrm-driver:\tamdgpu\ndrm-client-id:\t310\n" +
		"drm-total-vram:\t73112 KiB\ndrm-total-gtt:\t68220 KiB\ndrm-memory-vram:\t73112 KiB\n" +
		"drm-engine-gfx:\t52065383 ns\ndrm-engine-compute:\t0 ns\ndrm-engine-capacity-gfx:\t2\n")
	assert.True(t, ok)
	assert.Equal(t, "310", client.id)
	assert.Equal(t, map[string]time.Duration{"gfx": 52065383, "compute": 0}, client.engines)
	assert.Equal(t, uint64(73112), client.vramKiB)
	assert.Equal(t, uint64(68220), client.gttKiB)

	_, ok = parseDRMClient("pos:\t0\nflags:\t02\nmnt_id:\t15\n")
	assert.False(t, ok, "not a DRM descriptor")
}

func TestReadThreadCPU(t *testing.T) {
	root := t.TempDir()
	for tid, files := range map[string][2]string{
		"100": {"vecmap-rhi\n", "1500000 20 3\n"},
		"101": {"vecmap-rhi\n", "2000000 0 1\n"},
		"102": {"vecmap-rhi\n", "500000 0 1\n"},
		"103": {"QSGRenderThread\n", "3000000 0 9\n"},
		"104": {"disk cache\n", "7 0 0\n"},
		"105": {"gone\n", ""},
	} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, tid), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, tid, "comm"), []byte(files[0]), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(root, tid, "schedstat"), []byte(files[1]), 0o644))
	}
	threads, err := readThreadCPU(root, 100)
	require.NoError(t, err)
	assert.Equal(t, []threadCPU{
		{name: "QSGRenderThread", threads: 1, cpu: 3 * time.Millisecond},
		{name: "vecmap-rhi", threads: 2, cpu: 2500 * time.Microsecond},
		{name: "main", threads: 1, cpu: 1500 * time.Microsecond},
		{name: "disk_cache", threads: 1, cpu: 7},
	}, threads)

	threads, err = readThreadCPU("/proc/self/task", os.Getpid())
	require.NoError(t, err)
	require.NotEmpty(t, threads)
	assert.True(t, slices.ContainsFunc(threads, func(thread threadCPU) bool { return thread.name == "main" }))
}

func TestParseStatusKiB(t *testing.T) {
	sizes := parseStatusKiB("Name:\tvecmap-rhi\nVmHWM:\t  532040 kB\nVmRSS:\t  498112 kB\nRssAnon:\t  401200 kB\nThreads:\t32\n")
	assert.Equal(t, map[string]uint64{"VmHWM": 532040, "VmRSS": 498112, "RssAnon": 401200}, sizes)
}

func TestMeasurementSplitsMemoryAndWritesProfiles(t *testing.T) {
	dir := t.TempDir()
	m, err := startMeasurement(filepath.Join(dir, "cpu.pprof"), filepath.Join(dir, "heap.pprof"))
	require.NoError(t, err)
	costs, err := m.finish()
	require.NoError(t, err)
	require.NoError(t, m.close())
	assert.NotZero(t, costs.peak.rssKiB)
	assert.NotZero(t, costs.peak.goKiB)
	assert.LessOrEqual(t, costs.peak.heapObjectKiB, costs.peak.goKiB)
	assert.Positive(t, costs.peak.at, "when the peak was sampled")
	assert.NotZero(t, costs.goRuntime.residentBytes)
	assert.NotZero(t, costs.status["VmRSS"])
	for _, name := range []string{"cpu.pprof", "heap.pprof"} {
		info, err := os.Stat(filepath.Join(dir, name))
		require.NoError(t, err)
		assert.NotZero(t, info.Size(), name)
	}
	again, err := m.finish()
	require.NoError(t, err)
	assert.Zero(t, again.peak, "a finished measurement only reads the costs")

	costs, err = (*measurement)(nil).finish()
	require.NoError(t, err)
	assert.NotZero(t, costs.user+costs.system)
}
