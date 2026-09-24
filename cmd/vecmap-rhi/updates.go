//go:build vecmap_rhi

package main

import (
	"fmt"
	"os"
	"time"

	"github.com/rubiojr/whereami/pkg/vecmap/producer"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
)

// sampleUpdate runs on the GUI timer. Producers have already prepared documents;
// this only samples mailboxes and submits bounded camera metadata.
func (o benchmarkOptions) sampleUpdate(initial scene.Document, target *scene.Document, elapsed time.Duration) (streamUpdate, error) {
	traceTime := elapsed.Seconds()
	if o.live != nil && o.duration > 0 {
		traceTime = min(traceTime, o.duration.Seconds())
	}
	camera := traceCamera(initial, traceTime, o.animate)
	if o.live != nil {
		if camera.geographic == nil {
			return streamUpdate{}, producer.ErrInput
		}
		if err := o.live.update(*camera.geographic); err != nil {
			return streamUpdate{}, err
		}
		target = o.live.bridge.Target()
	}
	if o.feed != nil {
		target = o.feed.latest.Load()
		select {
		case err := <-o.feed.errors:
			fmt.Fprintf(os.Stderr, "scene reload: %v (keeping previous target)\n", err)
		default:
		}
	}
	return streamUpdate{target: target, camera: camera}, nil
}

// finish checks actual publication rather than acknowledging work on a timer.
// Live traces freeze at duration and get a bounded settlement tail before error.
func (o benchmarkOptions) finish(elapsed time.Duration, status streamStatus, target *scene.Document) (bool, error) {
	if o.duration <= 0 || elapsed < o.duration {
		return false, nil
	}
	ready := status.Ready && status.Current == target
	if o.live != nil && (!o.live.ready() || !ready) {
		if elapsed < o.duration+10*time.Second {
			return false, nil
		}
		s := o.live.p.Status()
		return true, fmt.Errorf("live producer did not settle: pending=%d failed=%d last_error=%q", s.Pending, s.Failed, s.LastError)
	}
	if !ready {
		return true, fmt.Errorf("scene uploads did not become ready before the duration elapsed")
	}
	return true, nil
}
