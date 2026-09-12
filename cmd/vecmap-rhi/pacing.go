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
	State                    windowState
}

// This is benchmark instrumentation, not a source of presentation timestamps.
// Qt's frameSwapped signal denotes queued presentation and can be delivered late.
type pacingSamples struct {
	lastTick               time.Time
	intervals              []time.Duration
	ticks, active, exposed int
	gaps                   []pacingGap
}

func (p *pacingSamples) add(now, lastRender time.Time, frame uint64, state windowState) {
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
	gap := pacingGap{Frame: frame, TimerInterval: interval, RenderAge: age, State: state}
	if len(p.gaps) > 0 && p.gaps[len(p.gaps)-1].Frame == frame {
		previous := &p.gaps[len(p.gaps)-1]
		previous.TimerInterval = max(previous.TimerInterval, gap.TimerInterval)
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
	for _, gap := range p.gaps {
		fmt.Printf("pacing_gap frame=%d timer=%s render_age=%s visible=%t active=%t exposed=%t swaps=%d\n", gap.Frame, gap.TimerInterval, gap.RenderAge, gap.State.Visible, gap.State.Active, gap.State.Exposed, gap.State.Swaps)
	}
}
