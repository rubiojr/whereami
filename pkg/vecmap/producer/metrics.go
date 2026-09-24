package producer

import "time"

// PhaseTime is bounded aggregate wall time for completed owner-observed work.
// Loading overlaps across workers; its sum is not CPU time or elapsed runtime.
// Preparation/build/selection include failed or discarded attempts.
type PhaseTime struct {
	Count          uint64
	Total, Maximum time.Duration
}

func (p *PhaseTime) observe(duration time.Duration) {
	p.Count++
	p.Total += duration
	p.Maximum = max(p.Maximum, duration)
}

// CacheUsage splits the logical cache charge. Raw includes the response and
// decoded-string backing allowance; Fragments counts owned payload plus Set's
// metadata copies. Profiles counts distinct borrowed style snapshots. Input asset
// profiles have a separate bounded handoff reservation; BuildOwned retains none.
type CacheUsage struct{ Raw, Prepared, Fragments, Profiles uint64 }

func (c CacheUsage) Total() uint64 { return c.Raw + c.Prepared + c.Fragments + c.Profiles }
