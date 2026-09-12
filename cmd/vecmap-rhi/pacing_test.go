//go:build vecmap_rhi

package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPacingSeparatesTimerAndRendererGaps(t *testing.T) {
	start := time.Unix(100, 0)
	state := windowState{Visible: true, Exposed: true}
	var samples pacingSamples
	samples.add(start, start, 31, state)
	// The GUI is responsive but no new rendering arrives.
	for i := 1; i <= 20; i++ {
		samples.add(start.Add(time.Duration(i)*8*time.Millisecond), start, 31, state)
	}
	require.Len(t, samples.gaps, 1)
	assert.Equal(t, 8*time.Millisecond, samples.gaps[0].TimerInterval)
	assert.Equal(t, 160*time.Millisecond, samples.gaps[0].RenderAge)
	// Both event delivery and rendering stall, even while Qt says exposed.
	samples.add(start.Add(time.Second), start, 32, state)
	require.Len(t, samples.gaps, 2)
	assert.Equal(t, 840*time.Millisecond, samples.gaps[1].TimerInterval)
	assert.True(t, samples.gaps[1].State.Exposed)
	// Preserve the largest timer delay when further ticks extend the same gap.
	samples.add(start.Add(time.Second+8*time.Millisecond), start, 32, state)
	assert.Equal(t, 840*time.Millisecond, samples.gaps[1].TimerInterval)
	assert.Equal(t, time.Second+8*time.Millisecond, samples.gaps[1].RenderAge)
}

func TestPacingExcludesStartupAndBoundsStorage(t *testing.T) {
	start := time.Unix(100, 0)
	var samples pacingSamples
	samples.add(start, time.Time{}, 0, windowState{})
	samples.add(start.Add(time.Second), start, 30, windowState{})
	assert.Empty(t, samples.gaps)
	for i := 0; i < 100; i++ {
		samples.add(start.Add(time.Duration(i+2)*time.Second), start, uint64(i+31), windowState{})
	}
	assert.Len(t, samples.gaps, 64)
}
