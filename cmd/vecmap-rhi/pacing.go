//go:build vecmap_rhi

package main

import (
	"fmt"
	"slices"
	"time"
)

type windowState struct {
	Visible, Active, Exposed bool
	Swaps                    int
}

type pacingGap struct {
	Frame                    uint64
	TimerInterval, RenderAge time.Duration
	SwapchainWait            time.Duration
	State                    windowState
}

// swapchainGap is the shortest swapchain wait that accounts for a pacing gap.
const swapchainGap = 100 * time.Millisecond

// This is benchmark instrumentation, not a source of presentation timestamps.
// Qt's frameSwapped signal denotes queued presentation and can be delivered late.
type pacingSamples struct {
	lastTick               time.Time
	intervals              []time.Duration
	ticks, active, exposed int
	gaps                   []pacingGap
}

// add records one GUI timer tick. wait is the longest swapchain wait the render
// thread finished since the previous tick.
func (p *pacingSamples) add(now, lastRender time.Time, frame uint64, state windowState, wait time.Duration) {
	p.ticks++
	if state.Active {
		p.active++
	}
	if state.Exposed {
		p.exposed++
	}
	interval := time.Duration(0)
	if !p.lastTick.IsZero() {
		interval = now.Sub(p.lastTick)
		if len(p.intervals) < 60000 {
			p.intervals = append(p.intervals, interval)
		}
	}
	p.lastTick = now
	if lastRender.IsZero() || frame <= 30 {
		return
	}
	age := now.Sub(lastRender)
	if interval < 100*time.Millisecond && age < 100*time.Millisecond {
		return
	}
	gap := pacingGap{Frame: frame, TimerInterval: interval, RenderAge: age, SwapchainWait: wait, State: state}
	if len(p.gaps) > 0 && p.gaps[len(p.gaps)-1].Frame == frame {
		previous := &p.gaps[len(p.gaps)-1]
		previous.TimerInterval = max(previous.TimerInterval, gap.TimerInterval)
		previous.SwapchainWait = max(previous.SwapchainWait, gap.SwapchainWait)
		if gap.RenderAge > previous.RenderAge {
			previous.RenderAge = gap.RenderAge
			previous.State = gap.State
		}
		return
	}
	if len(p.gaps) < 64 {
		p.gaps = append(p.gaps, gap)
	}
}

func (p *pacingSamples) report() {
	fmt.Printf("timer_ticks=%d active_ticks=%d exposed_ticks=%d\n", p.ticks, p.active, p.exposed)
	if len(p.intervals) > 0 {
		values := slices.Clone(p.intervals)
		slices.Sort(values)
		fmt.Printf("timer_interval p50=%s p95=%s p99=%s\n", values[len(values)/2], values[len(values)*95/100], values[len(values)*99/100])
	}
	swapchain := 0
	for _, gap := range p.gaps {
		if gap.SwapchainWait >= swapchainGap {
			swapchain++
		}
		fmt.Printf("pacing_gap frame=%d timer=%s render_age=%s swapchain_wait=%s visible=%t active=%t exposed=%t swaps=%d\n", gap.Frame, gap.TimerInterval, gap.RenderAge, gap.SwapchainWait, gap.State.Visible, gap.State.Active, gap.State.Exposed, gap.State.Swaps)
	}
	fmt.Printf("pacing_gaps=%d swapchain_gaps=%d\n", len(p.gaps), swapchain)
}

// swapchainWaits times the render thread's QRhi::beginFrame and endFrame calls
// from the QQuickWindow signals around them. When the window system withholds
// buffers, Vulkan blocks in beginFrame's image acquisition and OpenGL in
// endFrame's buffer swap. Xwayland does this for an occluded or minimized window,
// releasing one buffer per second. The first 30 frames are not sampled.
type swapchainWaits struct {
	frames               uint64
	frameBegan, rendered time.Time
	begin, end           []time.Duration
	longest              time.Duration
}

func (s *swapchainWaits) beforeFrameBegin(now time.Time) { s.frameBegan = now }

// frameBegun ends a beginFrame wait. Qt emits beforeSynchronizing, or
// beforeRendering for a frame without sync, after beginFrame returns.
func (s *swapchainWaits) frameBegun(now time.Time) {
	if s.frameBegan.IsZero() {
		return
	}
	s.record(&s.begin, now.Sub(s.frameBegan))
	s.frameBegan = time.Time{}
}

func (s *swapchainWaits) afterRendering(now time.Time) { s.rendered = now }

// afterFrameEnd ends an endFrame wait. Qt also emits it after a failed
// beginFrame, which then has no wait to report.
func (s *swapchainWaits) afterFrameEnd(now time.Time) {
	if !s.rendered.IsZero() {
		s.record(&s.end, now.Sub(s.rendered))
	}
	s.frameBegan, s.rendered = time.Time{}, time.Time{}
	s.frames++
}

func (s *swapchainWaits) record(values *[]time.Duration, wait time.Duration) {
	s.longest = max(s.longest, wait)
	if s.frames >= 30 && len(*values) < 60000 {
		*values = append(*values, wait)
	}
}

// takeLongest returns the longest wait since the previous call.
func (s *swapchainWaits) takeLongest() time.Duration {
	longest := s.longest
	s.longest = 0
	return longest
}

func (s *swapchainWaits) report() {
	for _, phase := range []struct {
		name   string
		values []time.Duration
	}{{"frame_begin_wait", s.begin}, {"frame_end_wait", s.end}} {
		if len(phase.values) == 0 {
			continue
		}
		values := slices.Clone(phase.values)
		slices.Sort(values)
		fmt.Printf("%s samples=%d p50=%s p95=%s p99=%s max=%s\n", phase.name, len(values), values[len(values)/2], values[len(values)*95/100], values[len(values)*99/100], values[len(values)-1])
	}
}
