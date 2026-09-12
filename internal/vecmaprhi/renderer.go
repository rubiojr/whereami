//go:build vecmap_rhi

// Package vecmaprhi adapts toolkit-neutral retained scenes to Qt's render thread.
package vecmaprhi

import (
	_ "embed"
	"fmt"
	"runtime"
	"slices"
	"time"
	"unsafe"

	qt "github.com/mappu/miqt/qt6"
	rhi "github.com/rubiojr/whereami/internal/qtrhi"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
)

//go:embed shaders/map.vert.qsb
var vertexShader []byte

//go:embed shaders/map.frag.qsb
var fragmentShader []byte

const uniformBytes = 176

type gpuMesh struct {
	buffer   *rhi.QRhiBuffer
	indices  *rhi.QRhiBuffer
	revision uint64
}

func (m gpuMesh) release() {
	if m.indices != nil {
		m.indices.Delete()
	}
	if m.buffer != nil {
		m.buffer.Delete()
	}
}

type gpuTexture struct {
	texture  *rhi.QRhiTexture
	bindings [2]*rhi.QRhiShaderResourceBindings
	revision uint64
}

// Stats is sampled on the render thread after a frame. Native byte counts are
// tracked explicitly; Go allocation measurements do not include GPU resources.
type Stats struct {
	Frames, Draws, MeshUploads, TextureUploads, UploadedBytes uint64
	LiveMeshes, LiveTextures                                  int
	PrepareTime, SubmitTime                                   time.Duration
	GPUTime                                                   time.Duration
	Error                                                     string
	Backend, Device                                           string
}

type Renderer struct {
	Node       *rhi.QSGRenderNode
	window     *rhi.QQuickWindow
	context    *rhi.QRhi
	frame      scene.Frame
	resident   *scene.Scene
	meshes     map[uint64]gpuMesh
	textures   map[uint64]gpuTexture
	samplers   [2]*rhi.QRhiSampler
	uniform    *rhi.QRhiBuffer
	uniforms   []float32
	stride     int
	pipelines  [2]*rhi.QRhiGraphicsPipeline
	passFormat []uint32
	samples    int
	stats      Stats
	observe    func(Stats)
	ready      bool
}

// New is called from updatePaintNode. Qt owns the returned node; its generated
// destruction callback releases resources on the render thread before Qt exits.
func New(item *rhi.QQuickItem, observe func(Stats)) *Renderer {
	r := &Renderer{Node: rhi.NewQSGRenderNode(), window: rhi.UnsafeNewQQuickItem(item.UnsafePointer()).Window(), observe: observe}
	r.Node.OnPrepare(func(func()) { r.prepare() })
	r.Node.OnRender(r.render)
	r.Node.OnReleaseResources(func(func()) { r.release() })
	r.Node.OnFlags(func(func() rhi.QSGRenderNode__RenderingFlag) rhi.QSGRenderNode__RenderingFlag {
		return rhi.QSGRenderNode__NoExternalRendering | rhi.QSGRenderNode__DepthAwareRendering
	})
	r.Node.OnChangedStates(func(func() rhi.QSGRenderNode__StateFlag) rhi.QSGRenderNode__StateFlag {
		return rhi.QSGRenderNode__ViewportState | rhi.QSGRenderNode__ScissorState
	})
	rhi.OnDestroyed(r.Node.UnsafePointer(), func() {
		r.release()
		if r.observe != nil {
			r.observe(r.stats)
		}
	})
	return r
}

// Sync only copies the frame header. Its scene and transform storage must remain
// immutable until the next updatePaintNode synchronization point.
func (r *Renderer) Sync(frame scene.Frame) { r.frame = frame }

func (r *Renderer) fail(err error) { r.ready = false; r.stats.Error = err.Error() }

func (r *Renderer) prepare() {
	start := time.Now()
	defer func() { r.stats.PrepareTime = time.Since(start) }()
	r.ready = false
	if r.frame.Scene == nil {
		return
	}
	if r.context == nil && qt.QLibraryInfo_Version().ToString() != rhi.QtVersion {
		r.fail(fmt.Errorf("RHI binding requires Qt %s", rhi.QtVersion))
		return
	}
	for _, draw := range r.frame.Scene.Draws {
		if draw.Transform >= len(r.frame.Transforms) {
			r.fail(fmt.Errorf("missing transform %d", draw.Transform))
			return
		}
	}
	context := r.window.Rhi()
	if context == nil {
		r.fail(fmt.Errorf("no Qt RHI graphics context"))
		return
	}
	if r.context == nil || r.context.UnsafePointer() != context.UnsafePointer() {
		if err := r.initialize(context); err != nil {
			r.release()
			r.fail(err)
			return
		}
	}
	updates := context.NextResourceUpdateBatch()
	if err := r.prepareResources(updates); err != nil {
		updates.Release()
		r.release()
		r.fail(err)
		return
	}
	if err := r.preparePipelines(); err != nil {
		updates.Release()
		r.release()
		r.fail(err)
		return
	}
	projection := qt.NewQMatrix4x47(r.Node.ProjectionMatrix())
	projection.OperatorMultiplyAssign(r.Node.Matrix())
	matrix := unsafe.Slice(projection.ConstData(), 16)
	pixelRatio := float32(r.window.EffectiveDevicePixelRatio())
	opacity := float32(r.Node.InheritedOpacity())
	for index, draw := range r.frame.Scene.Draws {
		u := r.uniforms[index*r.stride/4:][:uniformBytes/4]
		clear(u)
		copy(u, matrix)
		t := r.frame.Transforms[draw.Transform]
		u[16], u[17], u[18] = t.M11, t.M12, t.DX
		if draw.Material.MapAligned {
			u[19] = 1
		}
		u[20], u[21], u[22] = t.M21, t.M22, t.DY
		copy(u[24:28], draw.Material.Color[:])
		copy(u[28:32], draw.Clip[:])
		u[32], u[33], u[34], u[35] = float32(draw.Material.Kind), draw.Material.FontScale, draw.Material.HaloWidth, draw.Material.HaloBlur
		copy(u[36:38], draw.Material.PatternSize[:])
		copy(u[38:40], draw.Material.PatternPhase[:])
		u[40], u[41] = pixelRatio, opacity
	}
	projection.Delete()
	if len(r.uniforms) > 0 {
		updates.UpdateDynamicBuffer(r.uniform, 0, uint32(len(r.uniforms)*4), unsafe.Pointer(unsafe.SliceData(r.uniforms)))
		runtime.KeepAlive(r.uniforms)
	}
	r.Node.CommandBuffer().ResourceUpdate(updates)
	r.stats.LiveMeshes = len(r.meshes)
	r.stats.LiveTextures = len(r.textures)
	r.ready = true
	r.stats.Error = ""
}

func (r *Renderer) initialize(context *rhi.QRhi) error {
	r.release()
	r.context = context
	r.stride = context.UbufAligned(uniformBytes)
	r.meshes = make(map[uint64]gpuMesh)
	r.textures = make(map[uint64]gpuTexture)
	r.stats.Backend = context.BackendName()
	info := context.DriverInfo()
	r.stats.Device = string(info.DeviceName())
	info.Delete()
	for index, mode := range []rhi.QRhiSampler__AddressMode{rhi.QRhiSampler__ClampToEdge, rhi.QRhiSampler__Repeat} {
		r.samplers[index] = context.NewSampler(rhi.QRhiSampler__Linear, rhi.QRhiSampler__Linear, rhi.QRhiSampler__None, mode, mode)
		if !r.samplers[index].Create() {
			return fmt.Errorf("create sampler")
		}
	}
	return nil
}

func (r *Renderer) prepareResources(updates *rhi.QRhiResourceUpdateBatch) error {
	s := r.frame.Scene
	needed := max(1, len(s.Draws)) * r.stride
	if r.uniform == nil || int(r.uniform.Size()) < needed {
		for id, texture := range r.textures {
			for _, binding := range texture.bindings {
				if binding != nil {
					binding.Delete()
				}
			}
			texture.bindings = [2]*rhi.QRhiShaderResourceBindings{}
			r.textures[id] = texture
		}
		if r.uniform != nil {
			r.uniform.Delete()
		}
		r.uniform = r.context.NewBuffer(rhi.QRhiBuffer__Dynamic, rhi.QRhiBuffer__UniformBuffer, uint32(needed))
		if !r.uniform.Create() {
			return fmt.Errorf("create uniform buffer")
		}
	}
	length := len(s.Draws) * r.stride / 4
	if cap(r.uniforms) < length {
		r.uniforms = make([]float32, length)
	} else {
		r.uniforms = r.uniforms[:length]
	}
	if r.resident != s {
		if err := r.syncMeshes(s, updates); err != nil {
			return err
		}
		if err := r.syncTextures(s, updates); err != nil {
			return err
		}
		r.resident = s
	}
	return r.prepareBindings()
}

func (r *Renderer) syncMeshes(s *scene.Scene, updates *rhi.QRhiResourceUpdateBatch) error {
	desired := make(map[uint64]bool, len(s.Meshes))
	for _, mesh := range s.Meshes {
		desired[mesh.ID] = true
		if old, exists := r.meshes[mesh.ID]; exists {
			if old.revision == mesh.Revision {
				continue
			}
			old.release()
			delete(r.meshes, mesh.ID)
		}
		gpu, err := r.uploadMesh(mesh, updates)
		if err != nil {
			return err
		}
		r.meshes[mesh.ID] = gpu
		r.stats.MeshUploads++
		r.stats.UploadedBytes += mesh.BufferBytes()
	}
	for id, mesh := range r.meshes {
		if !desired[id] {
			mesh.release()
			delete(r.meshes, id)
		}
	}
	return nil
}

func (r *Renderer) uploadMesh(mesh scene.Mesh, updates *rhi.QRhiResourceUpdateBatch) (gpuMesh, error) {
	gpu := gpuMesh{revision: mesh.Revision}
	gpu.buffer = r.context.NewBuffer(rhi.QRhiBuffer__Immutable, rhi.QRhiBuffer__VertexBuffer, uint32(len(mesh.Vertices)*24))
	if !gpu.buffer.Create() {
		gpu.release()
		return gpuMesh{}, fmt.Errorf("create mesh %d", mesh.ID)
	}
	// Create both buffers before queuing uploads, so failure cannot leave an
	// update batch referencing a resource released by this function.
	if len(mesh.Indices) > 0 {
		gpu.indices = r.context.NewBuffer(rhi.QRhiBuffer__Immutable, rhi.QRhiBuffer__IndexBuffer, uint32(len(mesh.Indices)*4))
		if !gpu.indices.Create() {
			gpu.release()
			return gpuMesh{}, fmt.Errorf("create indices for mesh %d", mesh.ID)
		}
		updates.UploadStaticBuffer3(gpu.indices, unsafe.Pointer(unsafe.SliceData(mesh.Indices)))
		runtime.KeepAlive(mesh.Indices)
	}
	updates.UploadStaticBuffer3(gpu.buffer, unsafe.Pointer(unsafe.SliceData(mesh.Vertices)))
	runtime.KeepAlive(mesh.Vertices)
	return gpu, nil
}

func (r *Renderer) syncTextures(s *scene.Scene, updates *rhi.QRhiResourceUpdateBatch) error {
	textures := make(map[uint64]bool, len(s.Textures)+1)
	textures[0] = true
	if _, exists := r.textures[0]; !exists {
		if err := r.uploadTexture(updates, scene.Texture{Width: 1, Height: 1, RGBA: []byte{255, 255, 255, 255}}); err != nil {
			return err
		}
	}
	for _, texture := range s.Textures {
		textures[texture.ID] = true
		if old, exists := r.textures[texture.ID]; exists {
			if old.revision == texture.Revision {
				continue
			}
			r.deleteTexture(old)
			delete(r.textures, texture.ID)
		}
		if err := r.uploadTexture(updates, texture); err != nil {
			return err
		}
	}
	for id, texture := range r.textures {
		if !textures[id] {
			r.deleteTexture(texture)
			delete(r.textures, id)
		}
	}
	return nil
}

func (r *Renderer) prepareBindings() error {
	for id, texture := range r.textures {
		for index, sampler := range r.samplers {
			if texture.bindings[index] != nil {
				continue
			}
			uniform := rhi.QRhiShaderResourceBinding_UniformBufferWithDynamicOffset(0, rhi.QRhiShaderResourceBinding__VertexStage|rhi.QRhiShaderResourceBinding__FragmentStage, r.uniform, uniformBytes)
			sampled := rhi.QRhiShaderResourceBinding_SampledTexture(1, rhi.QRhiShaderResourceBinding__FragmentStage, texture.texture, sampler)
			bindings := r.context.NewShaderResourceBindings()
			bindings.SetBindings([]rhi.QRhiShaderResourceBinding{*uniform, *sampled})
			uniform.Delete()
			sampled.Delete()
			if !bindings.Create() {
				bindings.Delete()
				return fmt.Errorf("create texture bindings")
			}
			texture.bindings[index] = bindings
			r.textures[id] = texture
		}
	}
	return nil
}

func (r *Renderer) uploadTexture(updates *rhi.QRhiResourceUpdateBatch, source scene.Texture) error {
	size := qt.NewQSize2(source.Width, source.Height)
	defer size.Delete()
	texture := r.context.NewTexture(rhi.QRhiTexture__RGBA8, size)
	if !texture.Create() {
		texture.Delete()
		return fmt.Errorf("create texture %d", source.ID)
	}
	data := rhi.NewQRhiTextureSubresourceUploadDescription3(unsafe.Pointer(unsafe.SliceData(source.RGBA)), uint32(len(source.RGBA)))
	data.SetSourceSize(size)
	entry := rhi.NewQRhiTextureUploadEntry2(0, 0, data)
	description := rhi.NewQRhiTextureUploadDescription2(entry)
	updates.UploadTexture(texture, description)
	description.Delete()
	entry.Delete()
	data.Delete()
	runtime.KeepAlive(source.RGBA)
	r.textures[source.ID] = gpuTexture{texture: texture, revision: source.Revision}
	r.stats.TextureUploads++
	r.stats.UploadedBytes += uint64(len(source.RGBA))
	return nil
}

func (r *Renderer) preparePipelines() error {
	target := r.Node.RenderTarget()
	format := target.RenderPassDescriptor().SerializedFormat()
	samples := target.SampleCount()
	if !slices.Equal(format, r.passFormat) || samples != r.samples {
		for index, p := range r.pipelines {
			if p != nil {
				p.Delete()
				r.pipelines[index] = nil
			}
		}
		r.passFormat = format
		r.samples = samples
	}
	if r.pipelines[0] != nil && r.pipelines[1] != nil {
		return nil
	}
	vertex := rhi.QShader_FromSerialized(vertexShader)
	defer vertex.Delete()
	fragment := rhi.QShader_FromSerialized(fragmentShader)
	defer fragment.Delete()
	if !vertex.IsValid() || !fragment.IsValid() {
		return fmt.Errorf("invalid map shaders")
	}
	vs := rhi.NewQRhiShaderStage2(rhi.QRhiShaderStage__Vertex, vertex)
	defer vs.Delete()
	fs := rhi.NewQRhiShaderStage2(rhi.QRhiShaderStage__Fragment, fragment)
	defer fs.Delete()
	binding := rhi.NewQRhiVertexInputBinding2(24)
	defer binding.Delete()
	attributes := make([]rhi.QRhiVertexInputAttribute, 3)
	for i := range attributes {
		attributes[i] = *rhi.NewQRhiVertexInputAttribute2(0, i, rhi.QRhiVertexInputAttribute__Float2, uint32(i*8))
		defer attributes[i].Delete()
	}
	layout := rhi.NewQRhiVertexInputLayout()
	defer layout.Delete()
	layout.SetBindings([]rhi.QRhiVertexInputBinding{*binding})
	layout.SetAttributes(attributes)
	blend := rhi.NewQRhiGraphicsPipeline__TargetBlend()
	defer blend.Delete()
	blend.SetEnable(true)
	for index := range r.pipelines {
		if r.pipelines[index] != nil {
			continue
		}
		pipeline := r.context.NewGraphicsPipeline()
		pipeline.SetSampleCount(samples)
		pipeline.SetFlags(rhi.QRhiGraphicsPipeline__UsesScissor | rhi.QRhiGraphicsPipeline__UsesStencilRef)
		pipeline.SetShaderStages([]rhi.QRhiShaderStage{*vs, *fs})
		pipeline.SetVertexInputLayout(layout)
		pipeline.SetShaderResourceBindings(r.textures[0].bindings[0])
		pipeline.SetRenderPassDescriptor(target.RenderPassDescriptor())
		pipeline.SetTargetBlends([]rhi.QRhiGraphicsPipeline__TargetBlend{*blend})
		pipeline.SetDepthTest(false)
		pipeline.SetDepthWrite(false)
		if index == 1 {
			stencil := rhi.NewQRhiGraphicsPipeline__StencilOpState()
			stencil.SetCompareOp(rhi.QRhiGraphicsPipeline__Equal)
			pipeline.SetStencilTest(true)
			pipeline.SetStencilFront(stencil)
			pipeline.SetStencilBack(stencil)
			pipeline.SetStencilWriteMask(0)
			stencil.Delete()
		}
		if !pipeline.Create() {
			pipeline.Delete()
			return fmt.Errorf("create map pipeline")
		}
		r.pipelines[index] = pipeline
	}
	return nil
}

func (r *Renderer) render(state *rhi.QSGRenderNode__RenderState) {
	if !r.ready {
		if r.observe != nil {
			r.observe(r.stats)
		}
		return
	}
	start := time.Now()
	cb := r.Node.CommandBuffer()
	size := r.Node.RenderTarget().PixelSize()
	w, h := size.Width(), size.Height()
	size.Delete()
	viewport := rhi.NewQRhiViewport2(0, 0, float32(w), float32(h))
	cb.SetViewport(viewport)
	viewport.Delete()
	scissor := rhi.NewQRhiScissor2(0, 0, w, h)
	if state.ScissorEnabled() {
		rect := state.ScissorRect()
		scissor.Delete()
		scissor = rhi.NewQRhiScissor2(rect.X(), rect.Y(), rect.Width(), rect.Height())
		rect.Delete()
	}
	cb.SetScissor(scissor)
	scissor.Delete()
	pipeline := 0
	if state.StencilEnabled() {
		pipeline = 1
		cb.SetStencilRef(uint32(state.StencilValue()))
	}
	cb.SetGraphicsPipeline(r.pipelines[pipeline])
	var current uint64
	for index, draw := range r.frame.Scene.Draws {
		mesh := r.meshes[draw.Mesh]
		if current != draw.Mesh {
			binding := struct {
				First  *rhi.QRhiBuffer
				Second uint32
			}{mesh.buffer, 0}
			if mesh.indices != nil {
				cb.SetVertexInput4(0, 1, binding, mesh.indices, 0, rhi.QRhiCommandBuffer__IndexUInt32)
			} else {
				cb.SetVertexInput(0, 1, binding)
			}
			current = draw.Mesh
		}
		sampler := 0
		if draw.Material.Kind == scene.Pattern {
			sampler = 1
		}
		cb.SetShaderResources3(r.textures[draw.Material.Texture].bindings[sampler], 1, struct {
			First  int
			Second uint32
		}{0, uint32(index * r.stride)})
		if mesh.indices != nil {
			cb.DrawIndexed3(draw.Count, 1, draw.First)
		} else {
			cb.Draw3(draw.Count, 1, draw.First)
		}
	}
	r.stats.Frames++
	r.stats.Draws += uint64(len(r.frame.Scene.Draws))
	r.stats.SubmitTime = time.Since(start)
	r.stats.GPUTime = time.Duration(cb.LastCompletedGpuTime() * float64(time.Second))
	if r.observe != nil {
		r.observe(r.stats)
	}
}

func (r *Renderer) deleteTexture(texture gpuTexture) {
	for _, binding := range texture.bindings {
		if binding != nil {
			binding.Delete()
		}
	}
	texture.texture.Delete()
}

func (r *Renderer) release() {
	for _, pipeline := range r.pipelines {
		if pipeline != nil {
			pipeline.Delete()
		}
	}
	for _, texture := range r.textures {
		r.deleteTexture(texture)
	}
	for _, mesh := range r.meshes {
		mesh.release()
	}
	if r.uniform != nil {
		r.uniform.Delete()
	}
	for _, sampler := range r.samplers {
		if sampler != nil {
			sampler.Delete()
		}
	}
	r.pipelines = [2]*rhi.QRhiGraphicsPipeline{}
	r.textures = nil
	r.meshes = nil
	r.uniform = nil
	r.samplers = [2]*rhi.QRhiSampler{}
	r.context = nil
	r.resident = nil
	r.ready = false
	r.stats.LiveMeshes = 0
	r.stats.LiveTextures = 0
}
