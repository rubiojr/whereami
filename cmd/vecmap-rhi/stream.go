//go:build vecmap_rhi

package main

import (
	"errors"
	"fmt"

	rhi "github.com/rubiojr/whereami/internal/qtrhi"
	"github.com/rubiojr/whereami/internal/vecmaprhi"
	"github.com/rubiojr/whereami/pkg/vecmap/producer"
	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
)

type streamStatus struct {
	Generation, BatchFailures uint64
	Current                   *scene.Document
	Ready                     bool
	Err                       error
}

type nativeEpoch struct {
	generation uint64
	renderer   *vecmaprhi.BatchRenderer
	pending    *retained.PacketWithData[*scene.Document]
	current    *scene.Document
	dead       bool
}

// viewerStream is render-thread owned. Documents carry immutable scene-local
// transform mappings; camera state is independent and applies to Current, not the
// unfinished target. The worker carries the mapping through publication.
type viewerStream struct {
	worker  *retained.WorkerWithData[*scene.Document]
	target  *scene.Document
	epoch   *nativeEpoch
	observe func(vecmaprhi.Stats)
	report  func(streamStatus)
	status  streamStatus
	bridge  *producer.Bridge
}

func (s *viewerStream) sync(item *rhi.QQuickItem, old *rhi.QSGNode, update streamUpdate) *rhi.QSGNode {
	select {
	case <-s.worker.Done():
		s.status.Err = retained.ErrClosed
		s.report(s.status)
		return old
	default:
	}
	if errors.Is(s.status.Err, vecmaprhi.ErrNativeCompletion) || errors.Is(s.status.Err, vecmaprhi.ErrUnsupportedCompletion) {
		return old
	}
	if old == nil || s.epoch.dead {
		if old != nil {
			old.Delete()
		}
		s.target = update.target
		return s.recreate(item, update.camera)
	}
	e := s.epoch
	if s.target != update.target {
		s.target = update.target
		s.status.Ready = false
		if !s.worker.SetTargetWithData(e.generation, documentScene(s.target), s.target) {
			s.status.Err = fmt.Errorf("target mailbox unavailable")
			e.dead = true
		}
		s.report(s.status)
	}
	if e.pending != nil {
		if e.pending.Batch != nil || !e.renderer.FrameSelected() {
			return old
		}
		s.acknowledge(e, *e.pending, true)
		e.pending = nil
	} // coalesce camera changes until completion
	var batch *retained.Batch
	if packet, ok := s.worker.Next(); ok && packet.Generation == e.generation {
		if s.bridge != nil && !s.bridge.Consumed(packet) {
			s.status.Err = fmt.Errorf("live Current mailbox rejected packet")
			e.dead = true
			s.report(s.status)
			return old
		}
		if packet.Err != nil {
			s.status.Err = packet.Err
			s.acknowledge(e, packet, false)
		} else {
			e.current, batch = packet.CurrentData, packet.Batch
			e.pending = &packet
		}
	}
	if err := e.renderer.Sync(update.camera.frame(e.current), batch); err != nil {
		s.status.Err = err
		e.dead = true
	} else if e.pending != nil && batch == nil && e.renderer.FrameSelected() {
		s.acknowledge(e, *e.pending, true)
		e.pending = nil
	}
	s.status.Ready = e.current == s.target && !e.dead && e.renderer.Initialized()
	s.status.Current = e.current
	s.report(s.status)
	return e.renderer.Node.QSGNode
}

func (s *viewerStream) recreate(item *rhi.QQuickItem, camera streamCamera) *rhi.QSGNode {
	generation, err := s.worker.RestartWithData(documentScene(s.target), s.target)
	if err != nil {
		s.status.Err = err
		s.report(s.status)
		return nil
	}
	e := &nativeEpoch{generation: generation}
	s.epoch = e
	s.status.Generation, s.status.Ready = generation, false
	s.status.Current = nil
	if s.bridge != nil && !s.bridge.Restarted(generation) {
		s.status.Err = fmt.Errorf("live Current reset rejected")
		s.report(s.status)
		return nil
	}
	e.renderer = vecmaprhi.NewBatchRenderer(item, s.observe, func(result vecmaprhi.BatchResult) {
		s.complete(e, result)
	})
	// Do not consume work during node replacement. Start empty and allow the
	// previous node's DeleteLater objects to reach endFrame before new uploads.
	if err := e.renderer.Sync(camera.frame(nil), nil); err != nil {
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
		if errors.Is(result.Err, vecmaprhi.ErrNativeCompletion) || errors.Is(result.Err, vecmaprhi.ErrUnsupportedCompletion) {
			s.status.Err = result.Err
		}
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

func (s *viewerStream) acknowledge(e *nativeEpoch, packet retained.PacketWithData[*scene.Document], success bool) {
	if !s.worker.Acknowledge(e.generation, packet.Sequence, success) {
		s.status.Err = fmt.Errorf("upload acknowledgement mailbox unavailable")
		e.dead = true
	} else if s.bridge != nil && !s.bridge.Completed(packet, success) {
		s.status.Err = fmt.Errorf("live completion mailbox rejected packet")
		e.dead = true
	}
}

func documentScene(document *scene.Document) *scene.Scene {
	if document == nil {
		return nil
	}
	return &document.Scene
}
