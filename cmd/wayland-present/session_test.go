//go:build integration

package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Runs Qt's qml tool on the desktop's compositor. Keep the window uncovered:
// a hidden surface gets no presented feedback.
func TestRecordsQtWaylandPresentation(t *testing.T) {
	requireBuildTools(t)
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Skip("needs a Wayland session")
	}
	bins, err := exec.Command("qtpaths6", "--query", "QT_INSTALL_BINS").Output()
	if err != nil {
		t.Skip("qtpaths6 is not installed")
	}
	qml := filepath.Join(strings.TrimSpace(string(bins)), "qml")
	if _, err := os.Stat(qml); err != nil {
		t.Skip("Qt's qml tool is not installed")
	}
	scene := filepath.Join(t.TempDir(), "spin.qml")
	require.NoError(t, os.WriteFile(scene, []byte(`import QtQuick
Window { width: 200; height: 200; visible: true
 Rectangle { width: 100; height: 100; anchors.centerIn: parent; color: "steelblue"
  RotationAnimation on rotation { from: 0; to: 360; duration: 1000; loops: Animation.Infinite } }
 Timer { interval: 2000; running: true; onTriggered: Qt.quit() }
}
`), 0o600))
	t.Setenv("QT_QPA_PLATFORM", "wayland")
	logPath := filepath.Join(t.TempDir(), "present.log")
	var out bytes.Buffer
	require.NoError(t, run([]string{qml, scene}, logPath, 0, &out, io.Discard))
	t.Log(out.String())
	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	log, err := parseLog(data)
	require.NoError(t, err)
	s, err := summarize(log, 0)
	require.NoError(t, err)
	assert.Greater(t, s.presented, 10)
	assert.Equal(t, s.presented-1, len(s.intervals))
	for _, latency := range s.latencies {
		assert.Less(t, latency.Seconds(), 1.0, "commits and presentation share the compositor's clock")
	}
	assert.Contains(t, out.String(), "presentation_interval samples=")
}
