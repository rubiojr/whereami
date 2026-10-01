package main

import (
	"io"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requireBuildTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"pkg-config", "wayland-scanner", "gcc"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is required to build the preload", tool)
		}
	}
}

func TestPreloadBuildsAndRequiresWaylandClient(t *testing.T) {
	requireBuildTools(t)
	library, err := buildPreload(t.TempDir())
	require.NoError(t, err)
	symbols, err := exec.Command("nm", "-D", "--defined-only", library).Output()
	require.NoError(t, err)
	for _, symbol := range []string{"wl_proxy_marshal_flags", "wl_display_connect", "wl_display_connect_to_fd"} {
		assert.Contains(t, string(symbols), " T "+symbol+"\n")
	}
	assert.NotContains(t, string(symbols), "wp_presentation_interface", "protocol code stays private")
	err = run([]string{"true"}, "", 0, io.Discard, io.Discard)
	assert.ErrorContains(t, err, "never connected to a Wayland display")
}
