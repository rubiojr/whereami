//go:build vecmap_rhi

package vecmaprhi

import (
	"runtime"
	"unsafe"

	qt "github.com/mappu/miqt/qt6"
	rhi "github.com/rubiojr/whereami/internal/qtrhi"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
)

type resourceKey struct{ id, revision uint64 }
type drawResourceKeys struct{ mesh, texture resourceKey }

type stagedMesh struct {
	source scene.Mesh
	gpu    gpuMesh
}
type stagedTexture struct {
	source scene.Texture
	gpu    gpuTexture
}

// resourceStage owns only freshly allocated, unrecorded resources. Allocation
// failure can delete them immediately: no command batch refers to them yet.
type resourceStage struct {
	meshes   []stagedMesh
	textures []stagedTexture
}

func (s *resourceStage) discard() {
	for _, m := range s.meshes {
		m.gpu.release()
	}
	for _, t := range s.textures {
		t.gpu.texture.Delete()
	}
	s.meshes, s.textures = nil, nil
}

// allocateResources accepts validated immutable scenes on the render thread.
// Factories are synchronous and must clean up their own failed allocation.
// Success transfers the stage to recordResources; failure preserves all resident
// versions and records no upload commands. No extra geometry/pixel copy is made.
func (r *Renderer) allocateResources(s *scene.Scene, mesh func(scene.Mesh) (gpuMesh, error), texture func(scene.Texture) (gpuTexture, error)) (*resourceStage, error) {
	stage := &resourceStage{}
	for _, source := range s.Meshes {
		if _, exists := r.meshes[resourceKey{source.ID, source.Revision}]; exists {
			continue
		}
		gpu, err := mesh(source)
		if err != nil {
			stage.discard()
			return nil, err
		}
		stage.meshes = append(stage.meshes, stagedMesh{source, gpu})
	}
	if _, exists := r.textures[resourceKey{}]; !exists {
		white := scene.Texture{Width: 1, Height: 1, RGBA: []byte{255, 255, 255, 255}}
		gpu, err := texture(white)
		if err != nil {
			stage.discard()
			return nil, err
		}
		stage.textures = append(stage.textures, stagedTexture{white, gpu})
	}
	for _, source := range s.Textures {
		if _, exists := r.textures[resourceKey{source.ID, source.Revision}]; exists {
			continue
		}
		gpu, err := texture(source)
		if err != nil {
			stage.discard()
			return nil, err
		}
		stage.textures = append(stage.textures, stagedTexture{source, gpu})
	}
	return stage, nil
}

// recordResources consumes a successful stage and queues all uploads into one
// caller-owned update batch. The caller must submit that batch before drawing or
// cancel it before releasing the renderer. This is recorded, not acknowledged GPU
// residency; a future planner adapter must acknowledge only after submission.
func (r *Renderer) recordResources(stage *resourceStage, updates *rhi.QRhiResourceUpdateBatch) {
	for _, m := range stage.meshes {
		if m.gpu.indices != nil {
			updates.UploadStaticBuffer3(m.gpu.indices, unsafe.Pointer(unsafe.SliceData(m.source.Indices)))
		}
		// Sections are packed in the order of their layouts.
		if len(m.source.Vertices) > 0 {
			updates.UploadStaticBuffer(m.gpu.buffer, m.gpu.sections[scene.FullLayout], uint32(len(m.source.Vertices)*24), unsafe.Pointer(unsafe.SliceData(m.source.Vertices)))
		}
		if len(m.source.Offsets) > 0 {
			updates.UploadStaticBuffer(m.gpu.buffer, m.gpu.sections[scene.OffsetLayout], uint32(len(m.source.Offsets)*16), unsafe.Pointer(unsafe.SliceData(m.source.Offsets)))
		}
		if len(m.source.Positions) > 0 {
			updates.UploadStaticBuffer(m.gpu.buffer, m.gpu.sections[scene.PositionLayout], uint32(len(m.source.Positions)*8), unsafe.Pointer(unsafe.SliceData(m.source.Positions)))
		}
		r.meshes[resourceKey{m.source.ID, m.source.Revision}] = m.gpu
		r.stats.MeshUploads++
		r.stats.UploadedBytes += m.source.BufferBytes()
	}
	for _, t := range stage.textures {
		recordTexture(updates, t)
		r.textures[resourceKey{t.source.ID, t.source.Revision}] = t.gpu
		r.stats.TextureUploads++
		r.stats.UploadedBytes += uint64(len(t.source.RGBA))
	}
	runtime.KeepAlive(stage)
	stage.meshes, stage.textures = nil, nil
}

func recordTexture(updates *rhi.QRhiResourceUpdateBatch, t stagedTexture) {
	size := qt.NewQSize2(t.source.Width, t.source.Height)
	data := rhi.NewQRhiTextureSubresourceUploadDescription3(unsafe.Pointer(unsafe.SliceData(t.source.RGBA)), uint32(len(t.source.RGBA)))
	data.SetSourceSize(size)
	entry := rhi.NewQRhiTextureUploadEntry2(0, 0, data)
	description := rhi.NewQRhiTextureUploadDescription2(entry)
	updates.UploadTexture(t.gpu.texture, description)
	description.Delete()
	entry.Delete()
	data.Delete()
	size.Delete()
	runtime.KeepAlive(t.source.RGBA)
}

// selectResources changes the draw selection, then retires versions not needed
// by it. DeleteLater keeps wrappers alive through endFrame; QRhi defers native
// destruction further until in-flight GPU use is finished. This does not signal
// completed release to an upload planner or reclaim its residency budget yet.
func (r *Renderer) selectResources(s *scene.Scene) {
	r.selectDrawKeys(s)
	meshes := make(map[resourceKey]bool, len(s.Meshes))
	textures := map[resourceKey]bool{{}: true}
	for _, m := range s.Meshes {
		meshes[resourceKey{m.ID, m.Revision}] = true
	}
	for _, t := range s.Textures {
		textures[resourceKey{t.ID, t.Revision}] = true
	}
	for key, m := range r.meshes {
		if !meshes[key] {
			m.retire()
			delete(r.meshes, key)
		}
	}
	for key, t := range r.textures {
		if !textures[key] {
			r.deleteTexture(t)
			delete(r.textures, key)
		}
	}
}

func (r *Renderer) selectDrawKeys(s *scene.Scene) {
	meshes := make(map[uint64]resourceKey, len(s.Meshes))
	textures := make(map[uint64]resourceKey, len(s.Textures)+1)
	textures[0] = resourceKey{}
	for _, m := range s.Meshes {
		meshes[m.ID] = resourceKey{m.ID, m.Revision}
	}
	for _, t := range s.Textures {
		textures[t.ID] = resourceKey{t.ID, t.Revision}
	}
	r.drawKeys = make([]drawResourceKeys, len(s.Draws))
	for i, d := range s.Draws {
		r.drawKeys[i] = drawResourceKeys{meshes[d.Mesh], textures[d.Material.Texture]}
	}
}

func (m gpuMesh) retire() {
	if m.indices != nil {
		m.indices.DeleteLater()
	}
	if m.buffer != nil {
		m.buffer.DeleteLater()
	}
}
