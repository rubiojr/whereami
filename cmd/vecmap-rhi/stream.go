//go:build vecmap_rhi

package main

import (
	"fmt"

	rhi "github.com/rubiojr/whereami/internal/qtrhi"
	"github.com/rubiojr/whereami/internal/vecmaprhi"
	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
)

type streamStatus struct {
	Generation, BatchFailures uint64
	Ready                     bool
	Err                       error
}

type nativeEpoch struct {
	generation uint64
	renderer   *vecmaprhi.BatchRenderer
	pending    *retained.Packet
	current    *scene.Scene
	dead       bool
}

// viewerStream is render-thread owned. The fixed document uses the same transform
// slots across every generation, so the latest camera transforms are also valid
// while Current is old/empty. Live target changes need their own frame mapping.
type viewerStream struct {
	worker  *retained.Worker
	target  *scene.Scene
	epoch   *nativeEpoch
	observe func(vecmaprhi.Stats)
	report  func(streamStatus)
	status  streamStatus
}

func (s *viewerStream) sync(item *rhi.QQuickItem, old *rhi.QSGNode, camera scene.Frame) *rhi.QSGNode {
	select {
	case <-s.worker.Done():
		s.status.Err = retained.ErrClosed
		s.report(s.status)
		return old
	default:
	}
	if old == nil || s.epoch.dead {
		if old != nil {
			old.Delete()
		}
		return s.recreate(item, camera)
	}
	e := s.epoch
	if e.pending != nil {
		return old
	} // coalesce camera changes until completion
	var batch *retained.Batch
	if packet, ok := s.worker.Next(); ok && packet.Generation == e.generation {
		if packet.Err != nil {
			s.status.Err = packet.Err
			s.acknowledge(e, packet, false)
		} else {
			e.current, batch = packet.Current, packet.Batch
			e.pending = &packet
		}
	}
	camera.Scene = e.current
	if err := e.renderer.Sync(camera, batch); err != nil {
		s.status.Err = err
		e.dead = true
	} else if e.pending != nil && batch == nil {
		s.acknowledge(e, *e.pending, true)
		e.pending = nil
	}
	s.status.Ready = e.current == s.target && !e.dead
	s.report(s.status)
	return e.renderer.Node.QSGNode
}

func (s *viewerStream) recreate(item *rhi.QQuickItem, camera scene.Frame) *rhi.QSGNode {
	generation, err := s.worker.Restart(s.target)
	if err != nil {
		s.status.Err = err
		s.report(s.status)
		return nil
	}
	e := &nativeEpoch{generation: generation}
	s.epoch = e
	s.status.Generation, s.status.Ready = generation, false
	e.renderer = vecmaprhi.NewBatchRenderer(item, s.observe, func(result vecmaprhi.BatchResult) {
		s.complete(e, result)
	})
	// Do not consume work during node replacement. Start empty and allow the
	// previous node's DeleteLater objects to reach endFrame before new uploads.
	camera.Scene = nil
	if err := e.renderer.Sync(camera, nil); err != nil {
		s.status.Err = err
	}
	s.report(s.status)
	return e.renderer.Node.QSGNode
}

func (s *viewerStream) complete(e *nativeEpoch, result vecmaprhi.BatchResult) {
	if s.epoch != e {
		return
	} // callbacks from a retired native namespace
	if result.Reset {
		e.dead, e.pending = true, nil
		s.status.Ready = false
	} else if e.pending == nil || e.pending.Batch == nil || e.pending.Batch.Ticket != result.Ticket {
		s.status.Err = fmt.Errorf("native acknowledgement does not match outstanding packet")
		e.dead = true
	} else {
		if !result.Success {
			s.status.BatchFailures++
		}
		s.acknowledge(e, *e.pending, result.Success)
		e.pending = nil
	}
	s.report(s.status)
}

func (s *viewerStream) acknowledge(e *nativeEpoch, packet retained.Packet, success bool) {
	if !s.worker.Acknowledge(e.generation, packet.Sequence, success) {
		s.status.Err = fmt.Errorf("upload acknowledgement mailbox unavailable")
		e.dead = true
	}
}
