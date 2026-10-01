//go:build vecmap_rhi

package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMapLibreSettled(t *testing.T) {
	end := time.Unix(100, 0)
	settled, after := mapLibreSettled(end.Add(500*time.Millisecond), end, end.Add(-time.Second))
	assert.False(t, settled, "quiet for less than mapLibreQuiet since the trace ended")
	assert.Zero(t, after)
	settled, after = mapLibreSettled(end.Add(time.Second), end, end.Add(-time.Second))
	assert.True(t, settled)
	assert.Zero(t, after, "nothing rendered after the trace")
	settled, after = mapLibreSettled(end.Add(1500*time.Millisecond), end, end.Add(700*time.Millisecond))
	assert.False(t, settled, "rendered 0.8 s ago")
	assert.Equal(t, 700*time.Millisecond, after)
	settled, after = mapLibreSettled(end.Add(1700*time.Millisecond), end, end.Add(700*time.Millisecond))
	assert.True(t, settled)
	assert.Equal(t, 700*time.Millisecond, after)
}

func TestFrameEnds(t *testing.T) {
	var frames frameEnds
	start := time.Unix(100, 0)
	for i := range 3 {
		frames.add(start.Add(time.Duration(i) * 16 * time.Millisecond))
	}
	assert.Equal(t, uint64(3), frames.count)
	assert.Equal(t, start.Add(32*time.Millisecond), frames.last)
	assert.Equal(t, []time.Duration{16 * time.Millisecond, 16 * time.Millisecond}, frames.intervals)
}
