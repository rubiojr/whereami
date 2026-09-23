//go:build vecmap_rhi

package vecmaprhi

import (
	"fmt"
	"slices"

	rhi "github.com/rubiojr/whereami/internal/qtrhi"
	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
)

// BatchResult is delivered on the render thread. Enqueue it to the Planner owner;
// callbacks must not block or reenter the renderer. Reset invalidates this entire
// renderer/Planner namespace, including earlier successful tickets. Recreate both
// and ignore messages from the old instance; Reset is NOT a failed-batch ack.
type BatchResult struct {
	Ticket         uint64
	Success, Reset bool
	Err            error
}

// BatchRenderer executes one worker-planned batch per synchronization. It never
// calls Planner methods or scans geometry/pixels for validation. All methods are
// render-thread-only (Sync runs in updatePaintNode while Qt blocks the GUI thread).
// Use a dedicated Planner with initially empty residency for each instance.
type BatchRenderer struct {
	r                         *Renderer
	Node                      *rhi.QSGRenderNode
	notify                    func(BatchResult)
	pending                   *retained.Batch
	lastTicket                uint64
	executed, submitted, dead bool
	batchError                error
	meshFactory               func(scene.Mesh) (gpuMesh, error)
	textureFactory            func(scene.Texture) (gpuTexture, error)
}

// NewBatchRenderer installs a native end-of-frame acknowledgement callback. The
// current correctness path drains QRhi for each batch, including failed allocation
// cleanup. It is deliberately conservative and may stall the render thread.
// The caller must keep requesting frames until notify runs: submission is observed
// at afterFrameEnd and the drain takes place in the following prepare callback.
func NewBatchRenderer(item *rhi.QQuickItem, observe func(Stats), notify func(BatchResult)) *BatchRenderer {
	r := newRenderer(item, observe)
	b := &BatchRenderer{r: r, Node: r.Node, notify: notify, meshFactory: r.createMesh, textureFactory: r.createTexture}
	r.executor = b
	r.Node.OnPrepare(func(func()) { b.prepare() })
	// The signal connection lives until the window dies; sever its Go reference
	// when the node dies so repeated node recreation cannot retain scene payloads.
	live := b
	r.window.OnAfterFrameEnd(func() {
		if live != nil {
			live.afterFrame()
		}
	})
	r.onDestroy = func() { b.invalidate(fmt.Errorf("render node destroyed")); live = nil }
	return b
}

// Sync borrows a validated immutable frame for Planner.Current(), never its
// unfinished target. Keep the current scene's own transform slots while uploading
// replacements. Publish the new frame and its retirement batch together. A nil
// scene clears selection. Batch descriptors must come unmodified from Planner.Next;
// metadata is copied here, payloads remain immutable borrows through completion.
// A nil batch updates only the selected frame (including camera-only changes).
func (b *BatchRenderer) Sync(frame scene.Frame, batch *retained.Batch) error {
	if b.dead {
		return fmt.Errorf("batch renderer requires recreation")
	}
	if b.pending != nil {
		return retained.ErrBusy
	}
	if batch != nil {
		if batch.Ticket == 0 || batch.Ticket <= b.lastTicket {
			return retained.ErrTicket
		}
		copy := *batch
		copy.Uploads, copy.Releases = slices.Clone(batch.Uploads), slices.Clone(batch.Releases)
		b.pending = &copy
		b.lastTicket = batch.Ticket
	}
	if frame.Scene == nil {
		frame.Scene = &scene.Scene{}
	}
	b.r.Sync(frame)
	return nil
}

func (r *Renderer) requireResident(s *scene.Scene) error {
	for _, m := range s.Meshes {
		if _, ok := r.meshes[resourceKey{m.ID, m.Revision}]; !ok {
			return fmt.Errorf("unready mesh %d/%d", m.ID, m.Revision)
		}
	}
	for _, t := range s.Textures {
		if _, ok := r.textures[resourceKey{t.ID, t.Revision}]; !ok {
			return fmt.Errorf("unready texture %d/%d", t.ID, t.Revision)
		}
	}
	return nil
}

func (b *BatchRenderer) prepare() {
	if b.dead {
		return
	}
	if b.submitted {
		b.complete()
		if b.dead {
			return
		}
	}
	b.r.prepare()
	if b.dead {
		b.r.ready = false
		return
	}
	if !b.r.ready {
		b.invalidate(fmt.Errorf("prepare: %s", b.r.stats.Error))
		return
	}
	if b.pending == nil || b.executed {
		return
	}
	b.executed = true
	if len(b.pending.Releases) > 0 {
		b.batchError = b.releaseBatch(b.pending)
	} else {
		b.batchError = b.uploadBatch(b.pending)
	}
	b.r.stats.LiveMeshes, b.r.stats.LiveTextures = len(b.r.meshes), len(b.r.textures)
}

func (b *BatchRenderer) uploadBatch(batch *retained.Batch) error {
	s := &scene.Scene{}
	for _, resource := range batch.Uploads {
		switch resource.Version.Kind {
		case retained.MeshResource:
			s.Meshes = append(s.Meshes, resource.Mesh)
		case retained.TextureResource:
			s.Textures = append(s.Textures, resource.Texture)
		default:
			return retained.ErrInput
		}
	}
	stage, err := b.r.allocateResources(s, b.meshFactory, b.textureFactory)
	if err != nil {
		return err
	}
	updates := b.r.context.NextResourceUpdateBatch()
	b.r.recordResources(stage, updates)
	b.r.Node.CommandBuffer().ResourceUpdate(updates)
	return nil
}

func (b *BatchRenderer) releaseBatch(batch *retained.Batch) error {
	if len(batch.Uploads) != 0 {
		return retained.ErrInput
	}
	// Check the entire batch before scheduling any destruction. Selection is
	// already updated by prepare; rejecting a stale frame leaves everything intact.
	selected := make(map[retained.Version]bool)
	for _, m := range b.r.frame.Scene.Meshes {
		selected[retained.Version{Kind: retained.MeshResource, ID: m.ID, Revision: m.Revision}] = true
	}
	for _, t := range b.r.frame.Scene.Textures {
		selected[retained.Version{Kind: retained.TextureResource, ID: t.ID, Revision: t.Revision}] = true
	}
	seen := make(map[retained.Version]bool)
	for _, v := range batch.Releases {
		if selected[v] || seen[v] || !b.contains(v) {
			return retained.ErrInput
		}
		seen[v] = true
	}
	for _, v := range batch.Releases {
		key := resourceKey{v.ID, v.Revision}
		if v.Kind == retained.MeshResource {
			b.r.meshes[key].retire()
			delete(b.r.meshes, key)
		} else {
			b.r.deleteTexture(b.r.textures[key])
			delete(b.r.textures, key)
		}
	}
	return nil
}

func (b *BatchRenderer) contains(v retained.Version) bool {
	if v.ID == 0 {
		return false
	}
	key := resourceKey{v.ID, v.Revision}
	switch v.Kind {
	case retained.MeshResource:
		_, ok := b.r.meshes[key]
		return ok
	case retained.TextureResource:
		_, ok := b.r.textures[key]
		return ok
	default:
		return false
	}
}

func (b *BatchRenderer) afterFrame() {
	if b.dead || !b.executed {
		return
	}
	context := b.r.context
	// afterFrameEnd is emitted on the rendering thread after endFrame. Never
	// substitute afterRendering, which precedes native submission.
	if context == nil || context.IsRecordingFrame() || context.IsDeviceLost() {
		b.invalidate(fmt.Errorf("frame did not complete on a healthy context"))
		return
	}
	b.submitted = true
}

// Drain in the next prepare, outside any render pass. In Qt 6.11.2 OpenGL
// finish() outside a frame does nothing; beginFrame first processes deferred
// deletions and finish() inside the frame then actually calls glFinish.
func (b *BatchRenderer) complete() {
	context := b.r.window.Rhi()
	if context == nil || b.r.context == nil || context.UnsafePointer() != b.r.context.UnsafePointer() || !context.IsRecordingFrame() {
		b.invalidate(fmt.Errorf("native completion context changed"))
		return
	}
	if context.IsDeviceLost() || context.Finish() != rhi.QRhi__FrameOpSuccess || context.IsDeviceLost() {
		b.invalidate(fmt.Errorf("native completion drain failed"))
		return
	}
	result := BatchResult{Ticket: b.pending.Ticket, Success: b.batchError == nil, Err: b.batchError}
	b.pending, b.batchError, b.executed = nil, nil, false
	b.submitted = false
	if b.notify != nil {
		b.notify(result)
	}
}

func (b *BatchRenderer) invalidate(err error) {
	if b.dead {
		return
	}
	b.dead = true
	b.r.ready = false
	b.pending = nil
	if b.notify != nil {
		b.notify(BatchResult{Reset: true, Err: err})
	}
}
