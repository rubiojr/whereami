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
	samples.add(start, start, 31, state, 0)
	// The GUI is responsive but no new rendering arrives.
	for i := 1; i <= 20; i++ {
		samples.add(start.Add(time.Duration(i)*8*time.Millisecond), start, 31, state, 0)
	}
	require.Len(t, samples.gaps, 1)
	assert.Equal(t, 8*time.Millisecond, samples.gaps[0].TimerInterval)
	assert.Equal(t, 160*time.Millisecond, samples.gaps[0].RenderAge)
	// Both event delivery and rendering stall, even while Qt says exposed.
	samples.add(start.Add(time.Second), start, 32, state, 0)
	require.Len(t, samples.gaps, 2)
	assert.Equal(t, 840*time.Millisecond, samples.gaps[1].TimerInterval)
	assert.True(t, samples.gaps[1].State.Exposed)
	// Preserve the largest timer delay when further ticks extend the same gap.
	samples.add(start.Add(time.Second+8*time.Millisecond), start, 32, state, 0)
	assert.Equal(t, 840*time.Millisecond, samples.gaps[1].TimerInterval)
	assert.Equal(t, time.Second+8*time.Millisecond, samples.gaps[1].RenderAge)
}

func TestPacingExcludesStartupAndBoundsStorage(t *testing.T) {
	start := time.Unix(100, 0)
	var samples pacingSamples
	samples.add(start, time.Time{}, 0, windowState{}, 0)
	samples.add(start.Add(time.Second), start, 30, windowState{}, 0)
	assert.Empty(t, samples.gaps)
	for i := 0; i < 100; i++ {
		samples.add(start.Add(time.Duration(i+2)*time.Second), start, uint64(i+31), windowState{}, 0)
	}
	assert.Len(t, samples.gaps, 64)
}

func TestPacingAttributesGapsToSwapchainWaits(t *testing.T) {
	start := time.Unix(100, 0)
	state := windowState{Visible: true, Exposed: true}
	var samples pacingSamples
	samples.add(start, start, 31, state, 0)
	// An occluded Xwayland window: the render thread waits a second for a buffer.
	samples.add(start.Add(time.Second), start, 31, state, time.Second)
	samples.add(start.Add(time.Second+8*time.Millisecond), start, 31, state, 0)
	// A stall the swapchain does not explain.
	samples.add(start.Add(2*time.Second), start.Add(time.Second), 32, state, 2*time.Millisecond)
	require.Len(t, samples.gaps, 2)
	assert.Equal(t, time.Second, samples.gaps[0].SwapchainWait, "keep the longest wait across a gap's ticks")
	assert.Equal(t, 2*time.Millisecond, samples.gaps[1].SwapchainWait)
}

func TestSwapchainWaitsTimeBeginAndEndFrame(t *testing.T) {
	start := time.Unix(100, 0)
	var waits swapchainWaits
	frame := func(at time.Time, begin, render, end time.Duration, sync bool) {
		waits.beforeFrameBegin(at)
		waits.frameBegun(at.Add(begin))
		if sync {
			// Rendering follows synchronization; only the first signal ends the wait.
			waits.frameBegun(at.Add(begin + time.Millisecond))
		}
		waits.afterRendering(at.Add(begin + render))
		waits.afterFrameEnd(at.Add(begin + render + end))
	}
	for i := range 30 {
		frame(start.Add(time.Duration(i)*time.Second), 5*time.Second, 0, 0, true)
	}
	assert.Empty(t, waits.begin, "startup frames are not sampled")
	assert.Equal(t, 5*time.Second, waits.takeLongest(), "startup waits still explain gaps")
	assert.Zero(t, waits.takeLongest())
	frame(start.Add(time.Minute), time.Second, 3*time.Millisecond, time.Millisecond, true)
	frame(start.Add(2*time.Minute), 2*time.Millisecond, 3*time.Millisecond, 700*time.Millisecond, false)
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Millisecond}, waits.begin)
	assert.Equal(t, []time.Duration{time.Millisecond, 700 * time.Millisecond}, waits.end)
	assert.Equal(t, time.Second, waits.takeLongest())
	// A failed beginFrame emits afterFrameEnd without rendering signals.
	waits.beforeFrameBegin(start.Add(3 * time.Minute))
	waits.afterFrameEnd(start.Add(3*time.Minute + time.Second))
	waits.frameBegun(start.Add(4 * time.Minute))
	assert.Len(t, waits.begin, 2)
	assert.Len(t, waits.end, 2)
	assert.Equal(t, uint64(33), waits.frames)
}
