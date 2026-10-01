package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const refresh = 16_666_667

// presentedLog writes a vsynced 60 Hz trace for surface 43 with one frame per
// listed vblank, 5 ms of commit-to-display latency, a cursor surface and a
// discarded frame.
func presentedLog(vblanks ...uint64) string {
	var b strings.Builder
	b.WriteString("wayland-present 1\nclock 1\n")
	for _, vblank := range vblanks {
		shown := 1_000_000_000 + vblank*refresh
		fmt.Fprintf(&b, "presented 43 %d %d %d %d %d\n", shown-5_000_000, shown, refresh, 1000+vblank, kindVsync|kindHWClock|kindHWCompletion)
	}
	b.WriteString("presented 12 1 2 0 0 0\ndiscarded 43 99\n")
	return b.String()
}

func TestSummaryMeasuresMainSurfacePresentation(t *testing.T) {
	log, err := parseLog([]byte(presentedLog(0, 1, 2, 3, 5, 6, 9)))
	require.NoError(t, err)
	s, err := summarize(log, 1)
	require.NoError(t, err)
	assert.Equal(t, uint32(43), s.surface, "the cursor surface commits less")
	assert.Equal(t, 2, s.surfaces)
	assert.Equal(t, 7, s.presented)
	assert.Equal(t, 1, s.discarded)
	assert.Equal(t, time.Duration(refresh), s.refresh)
	// Warmup drops vblank 0; vblanks 4, 7 and 8 have no new frame.
	assert.Equal(t, []time.Duration{refresh, refresh, 2 * refresh, refresh, 3 * refresh}, s.intervals)
	assert.Len(t, s.latencies, 6)
	assert.Equal(t, 5*time.Millisecond, s.latencies[0])
	assert.Equal(t, 2, s.late)
	assert.Equal(t, 3, s.missed)
	assert.Equal(t, 6, s.vsync)
	assert.Equal(t, 6, s.hwCompletion)
	assert.Zero(t, s.zero)
	var out bytes.Buffer
	s.print(&out)
	assert.Contains(t, out.String(), "presentation surface=43 surfaces=2 presented=7 discarded=1 clock=1 refresh=16.666667ms\n")
	assert.Contains(t, out.String(), "presentation_refreshes late_frames=2 missed_refreshes=3\n")
}

func TestRefreshPeriodsWithoutVsyncSequence(t *testing.T) {
	previous := feedback{shown: 1000, refresh: 100}
	assert.Equal(t, 3, refreshPeriods(previous, feedback{shown: 1290}))
	previous.refresh = 0
	assert.Zero(t, refreshPeriods(previous, feedback{shown: 1290}), "unknown refresh")
}

func TestSummarySortsFeedbackByDisplayTime(t *testing.T) {
	// Feedback can be dispatched out of commit order.
	log, err := parseLog([]byte("presented 7 0 300 100 0 0\npresented 7 0 100 100 0 0\npresented 7 0 200 100 0 0\n"))
	require.NoError(t, err)
	s, err := summarize(log, 0)
	require.NoError(t, err)
	assert.Equal(t, []time.Duration{100, 100}, s.intervals)
	assert.Zero(t, s.late)
}

func TestParseLogRejectsMalformedRecords(t *testing.T) {
	for _, input := range []string{"wayland-present 2\n", "presented 1 2 3\n", "discarded 1 x\n", "frame 1\n"} {
		_, err := parseLog([]byte(input))
		assert.Error(t, err, input)
	}
	log, err := parseLog([]byte("wayland-present 1\nunsupported\n"))
	require.NoError(t, err)
	assert.False(t, log.supported)
	_, err = summarize(log, 0)
	assert.Error(t, err, "no feedback")
}
