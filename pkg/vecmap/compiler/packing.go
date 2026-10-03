package compiler

import (
	"errors"
	"math"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/glyph"
	"github.com/rubiojr/whereami/pkg/vecmap/placement"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
)

// MaxSceneElements retains the fixture's combined geometry, halo/fill and icon
// ceiling. It counts draw elements and also bounds unique vertex scratch sizes.
const MaxSceneElements = MaxTriangles*3 + glyph.MaxPreparedLayouts*glyph.MaxTextRunes*12 + placement.MaxSymbols*6
const MaxSceneTextures = 16_384

var (
	ErrPackingInput  = errors.New("invalid scene packing input")
	ErrPackingClosed = errors.New("scene builder is finalized")
)

// SceneBuilder packs one retained mesh, first-seen logical textures and ordered
// draws. Use NewSceneBuilder; the zero value is invalid. Methods latch the first
// error and then perform no further preparation. Finish returns no partial scene.
// Texture pixel slices are borrowed immutably; geometry inputs are copied. The
// builder is single-owner and must not be copied or used concurrently.
type SceneBuilder struct {
	mesh sections
	// dynamic receives zoom-dependent geometry when split; see Split.
	dynamic sections
	// symbols receives icon and text quads with resident symbols; see
	// ResidentSymbols.
	symbols   sections
	split     bool
	resident  bool
	compact   bool
	packed    bool
	class     meshClass
	result    scene.Scene
	textures  map[string]uint64
	indexed   bool
	limit     int
	err       error
	closed    bool
	breakDraw bool
	drawLimit int
	// borrowed is a StableMesh from an earlier build, published as is, and
	// stablePlan the plan its draws come from; see FragmentBuilder.Borrow.
	borrowed   *scene.Mesh
	stablePlan *StablePlan
	// labelVertices holds one label's transformed vertices until they are packed.
	labelVertices []scene.Vertex
	// halos are where symbolLayer packed each candidate's halo, by index.
	halos []textRange
}

// NewSceneBuilder selects the output topology. Zero maximumElements selects the
// existing fixture ceiling; positive limits may only lower it.
func NewSceneBuilder(indexed bool, maximumElements int) *SceneBuilder {
	if maximumElements == 0 {
		maximumElements = MaxSceneElements
	}
	b := &SceneBuilder{indexed: indexed, limit: maximumElements, textures: make(map[string]uint64)}
	if maximumElements < 0 || maximumElements > MaxSceneElements {
		b.err = geometry.ErrGeometryLimit
	}
	b.mesh = newSections(indexed, maximumElements)
	b.dynamic = newSections(indexed, maximumElements)
	b.symbols = newSections(indexed, maximumElements)
	return b
}

// sections builds the vertex sections of one mesh, one per scene.Layout. Only
// full is used unless the builder packs compact or packed vertices.
type sections struct {
	full            geometry.Builder[scene.Vertex]
	offsets         geometry.Builder[scene.OffsetVertex]
	positions       geometry.Builder[scene.PositionVertex]
	packedPositions geometry.Builder[scene.PackedPositionVertex]
	packedOffsets   geometry.Builder[scene.PackedOffsetVertex]
	packedDashed    geometry.Builder[scene.PackedDashedVertex]
}

func newSections(indexed bool, maximumElements int) sections {
	return sections{full: geometry.NewBuilder[scene.Vertex](indexed, maximumElements),
		offsets:         geometry.NewBuilder[scene.OffsetVertex](indexed, maximumElements),
		positions:       geometry.NewBuilder[scene.PositionVertex](indexed, maximumElements),
		packedPositions: geometry.NewBuilder[scene.PackedPositionVertex](indexed, maximumElements),
		packedOffsets:   geometry.NewBuilder[scene.PackedOffsetVertex](indexed, maximumElements),
		packedDashed:    geometry.NewBuilder[scene.PackedDashedVertex](indexed, maximumElements)}
}

// Count is the number of draw elements in every section.
func (s *sections) Count() int {
	n := 0
	for layout := range scene.LayoutCount {
		n += s.count(layout)
	}
	return n
}

func (s *sections) vertices() int {
	return len(s.full.Vertices) + len(s.offsets.Vertices) + len(s.positions.Vertices) +
		len(s.packedPositions.Vertices) + len(s.packedOffsets.Vertices) + len(s.packedDashed.Vertices)
}

// count is the number of draw elements in the section of a layout.
func (s *sections) count(layout scene.Layout) int {
	switch layout {
	case scene.OffsetLayout:
		return s.offsets.Count()
	case scene.PositionLayout:
		return s.positions.Count()
	case scene.PackedPositionLayout:
		return s.packedPositions.Count()
	case scene.PackedOffsetLayout:
		return s.packedOffsets.Count()
	case scene.PackedDashedLayout:
		return s.packedDashed.Count()
	}
	return s.full.Count()
}

// indices are the indices of the section of a layout.
func (s *sections) indices(layout scene.Layout) []uint32 {
	switch layout {
	case scene.OffsetLayout:
		return s.offsets.Indices
	case scene.PositionLayout:
		return s.positions.Indices
	case scene.PackedPositionLayout:
		return s.packedPositions.Indices
	case scene.PackedOffsetLayout:
		return s.packedOffsets.Indices
	case scene.PackedDashedLayout:
		return s.packedDashed.Indices
	}
	return s.full.Indices
}

// reserve makes room in the section of a layout.
func (s *sections) reserve(layout scene.Layout, vertices, indices int) {
	switch layout {
	case scene.OffsetLayout:
		s.offsets.Reserve(vertices, indices)
	case scene.PositionLayout:
		s.positions.Reserve(vertices, indices)
	case scene.PackedPositionLayout:
		s.packedPositions.Reserve(vertices, indices)
	case scene.PackedOffsetLayout:
		s.packedOffsets.Reserve(vertices, indices)
	case scene.PackedDashedLayout:
		s.packedDashed.Reserve(vertices, indices)
	default:
		s.full.Reserve(vertices, indices)
	}
}

// mesh publishes the sections with buffers of their exact length, and the
// position of each section's indices in the shared index buffer.
func (s *sections) mesh(id uint64) (scene.Mesh, [scene.LayoutCount]uint32) {
	s.full.Compact()
	s.offsets.Compact()
	s.positions.Compact()
	s.packedPositions.Compact()
	s.packedOffsets.Compact()
	s.packedDashed.Compact()
	mesh := scene.Mesh{ID: id, Revision: 1, Vertices: s.full.Vertices, Offsets: s.offsets.Vertices, Positions: s.positions.Vertices,
		PackedPositions: s.packedPositions.Vertices, PackedOffsets: s.packedOffsets.Vertices, PackedDashed: s.packedDashed.Vertices,
		Indices: s.full.Indices}
	var first [scene.LayoutCount]uint32
	total := 0
	for layout := range scene.LayoutCount {
		first[layout] = uint32(total)
		total += len(s.indices(layout))
	}
	if total > len(s.full.Indices) {
		mesh.Indices = make([]uint32, 0, total)
		for layout := range scene.LayoutCount {
			mesh.Indices = append(mesh.Indices, s.indices(layout)...)
		}
	}
	// The scene a builder returns keeps the builder alive. Let go of the
	// sections' own index buffers, which the shared one has replaced.
	*s = sections{}
	return mesh, first
}

// StableMesh, DynamicMesh and SymbolMesh are the fixed scene-local mesh IDs of a
// builder using Split or ResidentSymbols. They never change meaning between
// builds of one fragment, so a retained store can compare each mesh with its
// predecessor.
const (
	StableMesh  = 1
	DynamicMesh = 2
	SymbolMesh  = 3
)

// meshClass is the kind of geometry being packed, which selects its mesh.
type meshClass uint8

const (
	stableClass meshClass = iota
	dynamicClass
	symbolClass
)

// Split routes geometry whose vertices depend on the evaluated style zoom
// (Dynamic primitives and, without ResidentSymbols, all symbol passes) to
// DynamicMesh, keeping fills, patterns and extruded lines in StableMesh. Draw
// order is unchanged: draws name their mesh. The element limit still bounds all
// meshes together. Call before packing; an unsplit builder packs everything into
// mesh one as before.
func (b *SceneBuilder) Split() {
	if b.unused() {
		b.split = true
	}
}

// ResidentSymbols routes every symbol pass to SymbolMesh and packs icon quads at
// icon size one, with the evaluated size in Material.OffsetScale. Text follows
// its layout: a glyph.LayoutMesh marked Unit is packed at EmSize pixels per em
// with the layout's Scale in OffsetScale. Anchors, rotation, atlas coordinates,
// draw order and provenance are unchanged, so a style-zoom change that leaves
// symbol layout alone changes draws only. A size that float32 cannot carry as a
// positive finite factor keeps its baked quad. Call before packing.
func (b *SceneBuilder) ResidentSymbols() {
	if b.unused() {
		b.resident = true
	}
}

// CompactVertices packs vertices without the attributes they do not use: fills
// and patterns as scene.PositionVertex and extruded lines as scene.OffsetVertex,
// in sections of the same meshes. Dashed lines, icons and text keep every
// attribute. Draws name their section in scene.Draw.Layout. Positions, draw
// order and materials are unchanged, so the rendered output is too. The backend
// must implement the sections. Call before packing.
func (b *SceneBuilder) CompactVertices() {
	if b.unused() {
		b.compact = true
	}
}

// PackedVertices packs fills, patterns, extruded and dashed lines in the
// int16 sections of scene.PackedPositionLayout, PackedOffsetLayout and
// PackedDashedLayout, half the bytes of float32. MVT positions are exact and
// directions within 1/8192; a primitive with a coordinate outside the packed
// range keeps float vertices, compact if CompactVertices is set. Icons and
// text keep every attribute. The backend must implement the packed sections.
// Call before packing.
func (b *SceneBuilder) PackedVertices() {
	if b.unused() {
		b.packed = true
	}
}

func (b *SceneBuilder) unused() bool {
	if !b.ready() {
		return false
	}
	if b.elements() != 0 {
		b.err = ErrPackingInput
		return false
	}
	return true
}

func (b *SceneBuilder) elements() int {
	elements, _ := b.borrowedCounts()
	return b.mesh.Count() + b.dynamic.Count() + b.symbols.Count() + elements
}

// borrowedCounts are the draw elements and vertices of a borrowed StableMesh.
func (b *SceneBuilder) borrowedCounts() (elements, vertices int) {
	if b.borrowed == nil {
		return 0, 0
	}
	for layout := range scene.LayoutCount {
		vertices += b.borrowed.Len(layout)
	}
	if b.indexed {
		return len(b.borrowed.Indices), vertices
	}
	return vertices, vertices
}

// target selects the mesh receiving the next geometry and its scene-local ID.
func (b *SceneBuilder) target() (*sections, uint64) {
	switch {
	case b.resident && b.class == symbolClass:
		return &b.symbols, SymbolMesh
	case b.split && b.class != stableClass:
		return &b.dynamic, DynamicMesh
	}
	return &b.mesh, StableMesh
}

// Reserve makes room for geometry that Geometry, Extruded or Dashed will pack,
// so packing it does not reallocate. dynamic selects the mesh that receives
// Dynamic geometry when split, and layout the section as Layout reports it. It
// is a hint and never an error: a builder that is not ready ignores it, and
// Finish publishes buffers of their exact length either way. Call after Split
// and CompactVertices.
func (b *SceneBuilder) Reserve(dynamic bool, layout scene.Layout, vertices, indices int) {
	if b.err != nil || b.closed || vertices < 0 || indices < 0 {
		return
	}
	target := &b.mesh
	if dynamic && b.split {
		target = &b.dynamic
	}
	target.reserve(layout, vertices, indices)
}

// Layout is the vertex section that geometry with these attributes is packed
// into: a packed section with PackedVertices, and otherwise every attribute
// unless the builder packs compact vertices.
func (b *SceneBuilder) Layout(directions, distances bool) scene.Layout {
	switch {
	case b.packed && distances:
		return scene.PackedDashedLayout
	case b.packed && directions:
		return scene.PackedOffsetLayout
	case b.packed:
		return scene.PackedPositionLayout
	}
	return b.floatLayout(directions, distances)
}

// floatLayout is the section of geometry without PackedVertices.
func (b *SceneBuilder) floatLayout(directions, distances bool) scene.Layout {
	switch {
	case !b.compact || distances:
		return scene.FullLayout
	case directions:
		return scene.OffsetLayout
	}
	return scene.PositionLayout
}

// publish adds a mesh of exact buffers: growth slack would stay allocated, and
// charged, for as long as the scene is retained. It moves the draws of an
// indexed mesh from their section's indices to the shared index buffer.
func (b *SceneBuilder) publish(s *sections, id uint64) {
	mesh, first := s.mesh(id)
	b.result.Meshes = append(b.result.Meshes, mesh)
	if !b.indexed {
		return
	}
	for i := range b.result.Draws {
		if draw := &b.result.Draws[i]; draw.Mesh == id {
			draw.First += first[draw.Layout]
		}
	}
}

// offsetScale reports whether size can travel as Material.OffsetScale, where
// zero means one.
func offsetScale(size float64) (float32, bool) {
	scale := float32(size)
	return scale, scale > 0 && !math.IsInf(float64(scale), 0)
}

func (b *SceneBuilder) ready() bool {
	if b.err != nil {
		return false
	}
	if b.closed {
		b.err = ErrPackingClosed
		return false
	}
	if b.textures == nil {
		b.err = ErrPackingInput
		return false
	}
	return true
}

// Texture assigns IDs from one in first-use order. Repeated keys reuse the first
// image, without rechecking later metadata. Pixels remain caller-owned immutable
// storage through the returned scene's lifetime; no upload/copy happens here.
func (b *SceneBuilder) Texture(key string, width, height int, pixels []byte) uint64 {
	if !b.ready() {
		return 0
	}
	if id, exists := b.textures[key]; exists {
		return id
	}
	if width <= 0 || height <= 0 || width > 16384 || height > 16384 || int64(len(pixels)) != int64(width)*int64(height)*4 {
		b.err = ErrPackingInput
		return 0
	}
	if len(b.textures) >= MaxSceneTextures {
		b.err = geometry.ErrGeometryLimit
		return 0
	}
	id := uint64(len(b.textures) + 1)
	b.textures[key] = id
	b.result.Textures = append(b.result.Textures, scene.Texture{ID: id, Revision: 1, Width: width, Height: height, RGBA: pixels})
	return id
}

// GlyphAtlas converts the existing single-channel SDF to owned RGB distance bytes
// with opaque alpha. It uses the reserved logical key "glyph-atlas". Nil skips it.
func (b *SceneBuilder) GlyphAtlas(atlas *glyph.Atlas) uint64 {
	if !b.ready() || atlas == nil {
		return 0
	}
	if id, exists := b.textures["glyph-atlas"]; exists {
		return id
	}
	if atlas.Width <= 0 || atlas.Height <= 0 || atlas.Width > glyph.MaxAtlasSize || atlas.Height > glyph.MaxAtlasSize || len(atlas.Pixels) != atlas.Width*atlas.Height {
		b.err = ErrPackingInput
		return 0
	}
	pixels := make([]byte, len(atlas.Pixels)*4)
	for i, value := range atlas.Pixels {
		pixels[i*4], pixels[i*4+1], pixels[i*4+2], pixels[i*4+3] = value, value, value, 255
	}
	return b.Texture("glyph-atlas", atlas.Width, atlas.Height, pixels)
}

// PackedColor preserves the fixture's integer-to-float32 conversion order.
func PackedColor(c style.Color) [4]float32 {
	return [4]float32{float32(c.Red) / 255, float32(c.Green) / 255, float32(c.Blue) / 255, float32(c.Alpha) / 255}
}

func (b *SceneBuilder) check(vertices int, indices []uint32) bool {
	if !b.ready() {
		return false
	}
	count := vertices
	if indices != nil {
		count = len(indices)
	}
	_, borrowedVertices := b.borrowedCounts()
	used, usedVertices := b.elements(), b.mesh.vertices()+b.dynamic.vertices()+b.symbols.vertices()+borrowedVertices
	if vertices > b.limit || count > b.limit-used || (b.indexed && vertices > b.limit-usedVertices) {
		b.err = geometry.ErrGeometryLimit
		return false
	}
	if count%3 != 0 {
		b.err = ErrPackingInput
		return false
	}
	for _, index := range indices {
		if uint64(index) >= uint64(vertices) {
			b.err = ErrPackingInput
			return false
		}
	}
	return true
}

// Geometry packs tile-local XY geometry and appends/coalesces its ordered draw.
// Expanded output writes directly into its final buffer, avoiding a conversion
// slice. Indexed output preserves source topology through geometry.Builder.
func (b *SceneBuilder) Geometry(mesh geometry.Mesh, material scene.Material, clip [4]float32) {
	if !b.check(len(mesh.Vertices), mesh.Indices) {
		return
	}
	b.geometry(mesh, nil, nil, material, clip)
}

// Extruded packs width-independent line geometry: anchors as tile-local XY and
// unit directions as offsets. The material's OffsetScale supplies the half width
// in logical pixels and its offsets follow the map. directions parallels
// mesh.Vertices. The same vertices serve every evaluated width.
func (b *SceneBuilder) Extruded(mesh geometry.Mesh, directions []geometry.Point, halfWidth float64, material scene.Material, clip [4]float32) {
	if !b.ready() {
		return
	}
	b.extruded(mesh, directions, nil, halfWidth, material, clip)
}

func (b *SceneBuilder) extruded(mesh geometry.Mesh, directions []geometry.Point, distances []float64, halfWidth float64, material scene.Material, clip [4]float32) {
	if len(directions) != len(mesh.Vertices) || !(halfWidth > 0) || math.IsInf(halfWidth, 0) {
		b.err = ErrPackingInput
		return
	}
	if !b.check(len(mesh.Vertices), mesh.Indices) {
		return
	}
	material.MapAligned, material.OffsetScale = true, float32(halfWidth)
	b.geometry(mesh, directions, distances, material, clip)
}

// Dashed packs Extruded geometry with the distance along each path in vertex U
// and makes the material scene.Dashed: unit is the line width in tile units and
// pattern its dash array in multiples of it. distances parallels mesh.Vertices.
// The same vertices serve every evaluated width and every scale of the pattern.
func (b *SceneBuilder) Dashed(mesh geometry.Mesh, directions []geometry.Point, distances []float64, halfWidth, unit float64, pattern geometry.DashPattern, material scene.Material, clip [4]float32) {
	if !b.ready() {
		return
	}
	if len(distances) != len(mesh.Vertices) || !(unit > 0) || math.IsInf(unit, 0) || !(pattern[0] > 0) {
		b.err = ErrPackingInput
		return
	}
	material.Kind, material.DashUnit = scene.Dashed, float32(unit)
	for i, dash := range pattern {
		material.Dashes[i] = float32(dash)
	}
	// Extruded checks the remaining input before anything is packed.
	b.extruded(mesh, directions, distances, halfWidth, material, clip)
}

func (b *SceneBuilder) geometry(mesh geometry.Mesh, directions []geometry.Point, distances []float64, material scene.Material, clip [4]float32) {
	vertex := func(index int) scene.Vertex {
		point := mesh.Vertices[index]
		packed := scene.Vertex{X: float32(point.X), Y: float32(point.Y)}
		if directions != nil {
			packed.OffsetX, packed.OffsetY = float32(directions[index].X), float32(directions[index].Y)
		}
		if distances != nil {
			packed.U = float32(distances[index])
		}
		return packed
	}
	target, id := b.target()
	if id == StableMesh && b.borrowed != nil {
		b.err = ErrPackingInput // the borrowed StableMesh is complete
		return
	}
	layout := b.Layout(directions != nil, distances != nil)
	if layout.Packed() {
		first := target.count(layout)
		if b.packGeometry(target, layout, mesh, directions, distances) {
			b.draw(target, id, layout, first, material, clip)
			return
		}
		// A coordinate outside the packed range keeps float vertices.
		layout = b.floatLayout(directions != nil, distances != nil)
	}
	first := target.count(layout)
	switch layout {
	case scene.OffsetLayout:
		b.err = appendGeometry(&target.offsets, b.indexed, mesh, func(index int) scene.OffsetVertex {
			v := vertex(index)
			return scene.OffsetVertex{X: v.X, Y: v.Y, OffsetX: v.OffsetX, OffsetY: v.OffsetY}
		})
	case scene.PositionLayout:
		b.err = appendGeometry(&target.positions, b.indexed, mesh, func(index int) scene.PositionVertex {
			v := vertex(index)
			return scene.PositionVertex{X: v.X, Y: v.Y}
		})
	default:
		b.err = appendGeometry(&target.full, b.indexed, mesh, vertex)
	}
	b.draw(target, id, layout, first, material, clip)
}

// packGeometry packs a checked mesh into a packed section, reporting false,
// with nothing packed, if a position or direction lies outside its range.
func (b *SceneBuilder) packGeometry(target *sections, layout scene.Layout, mesh geometry.Mesh, directions []geometry.Point, distances []float64) bool {
	switch layout {
	case scene.PackedPositionLayout:
		vertices, ok := packVertices(mesh, nil, nil, func(x, y, _, _ int16, _ float32) scene.PackedPositionVertex {
			return scene.PackedPositionVertex{X: x, Y: y}
		})
		if ok {
			b.err = appendPacked(&target.packedPositions, b.indexed, mesh, vertices)
		}
		return ok
	case scene.PackedOffsetLayout:
		vertices, ok := packVertices(mesh, directions, nil, func(x, y, dx, dy int16, _ float32) scene.PackedOffsetVertex {
			return scene.PackedOffsetVertex{X: x, Y: y, OffsetX: dx, OffsetY: dy}
		})
		if ok {
			b.err = appendPacked(&target.packedOffsets, b.indexed, mesh, vertices)
		}
		return ok
	}
	vertices, ok := packVertices(mesh, directions, distances, func(x, y, dx, dy int16, u float32) scene.PackedDashedVertex {
		return scene.PackedDashedVertex{X: x, Y: y, OffsetX: dx, OffsetY: dy, U: u}
	})
	if ok {
		b.err = appendPacked(&target.packedDashed, b.indexed, mesh, vertices)
	}
	return ok
}

// packVertices quantizes a mesh's vertices, with directions and distances when
// set, reporting false if any lies outside the packed range.
func packVertices[T any](mesh geometry.Mesh, directions []geometry.Point, distances []float64, vertex func(x, y, dx, dy int16, u float32) T) ([]T, bool) {
	packed := make([]T, len(mesh.Vertices))
	for i, point := range mesh.Vertices {
		x, y, ok := scene.PackPosition(point.X, point.Y)
		var dx, dy int16
		if ok && directions != nil {
			dx, dy, ok = scene.PackOffset(directions[i].X, directions[i].Y)
		}
		if !ok {
			return nil, false
		}
		var u float32
		if distances != nil {
			u = float32(distances[i])
		}
		packed[i] = vertex(x, y, dx, dy, u)
	}
	return packed, true
}

// appendPacked packs quantized vertices with the mesh's topology, as
// appendGeometry does.
func appendPacked[T any](target *geometry.Builder[T], indexed bool, mesh geometry.Mesh, vertices []T) error {
	if indexed {
		return target.Append(vertices, mesh.Indices)
	}
	if mesh.Indices != nil {
		for _, index := range mesh.Indices {
			target.Vertices = append(target.Vertices, vertices[index])
		}
		return nil
	}
	target.Vertices = append(target.Vertices, vertices...)
	return nil
}

// appendGeometry packs a checked mesh. Expanded output writes directly into its
// final buffer; indexed output preserves source topology through the builder.
func appendGeometry[T any](target *geometry.Builder[T], indexed bool, mesh geometry.Mesh, vertex func(int) T) error {
	if indexed {
		vertices := make([]T, len(mesh.Vertices))
		for i := range mesh.Vertices {
			vertices[i] = vertex(i)
		}
		return target.Append(vertices, mesh.Indices)
	}
	if mesh.Indices != nil {
		for _, index := range mesh.Indices {
			target.Vertices = append(target.Vertices, vertex(int(index)))
		}
		return nil
	}
	for i := range mesh.Vertices {
		target.Vertices = append(target.Vertices, vertex(i))
	}
	return nil
}

// ExpandedText consumes packed XYUV triangles for an expanded builder only.
// Transform arithmetic preserves the original Sincos/TransformTextVertex order.
func (b *SceneBuilder) ExpandedText(anchor geometry.Point, vertices []float32, offset geometry.Point, angle float64, material scene.Material) {
	if !b.ready() {
		return
	}
	if b.indexed || len(vertices)%4 != 0 {
		b.err = ErrPackingInput
		return
	}
	if !b.check(len(vertices)/4, nil) {
		return
	}
	target, id := b.target()
	first := target.full.Count()
	sin, cos := math.Sincos(angle)
	for i := 0; i < len(vertices); i += 4 {
		vertex := geometry.TextVertex{X: vertices[i], Y: vertices[i+1], U: vertices[i+2], V: vertices[i+3]}
		target.full.Vertices = append(target.full.Vertices, geometry.TransformTextVertex(anchor, vertex, offset, sin, cos))
	}
	b.draw(target, id, scene.FullLayout, first, material, [4]float32{})
}

// IndexedText accepts known topology in either output mode (including icon quads
// in expanded scenes), reusing the original transformed scratch buffer and builder.
func (b *SceneBuilder) IndexedText(anchor geometry.Point, vertices []geometry.TextVertex, indices []uint32, offset geometry.Point, angle float64, material scene.Material) {
	if !b.check(len(vertices), indices) {
		return
	}
	target, id := b.target()
	first := target.full.Count()
	sin, cos := math.Sincos(angle)
	packed := b.labelVertices[:0]
	for _, vertex := range vertices {
		packed = append(packed, geometry.TransformTextVertex(anchor, vertex, offset, sin, cos))
	}
	b.labelVertices = packed
	b.err = target.full.Append(packed, indices)
	b.draw(target, id, scene.FullLayout, first, material, [4]float32{})
}

// draw records a range of one section. Its First counts from the start of that
// section until Finish places the sections' indices in one buffer.
func (b *SceneBuilder) draw(target *sections, id uint64, layout scene.Layout, first int, material scene.Material, clip [4]float32) {
	count := target.count(layout) - first
	if b.err != nil || count == 0 {
		return
	}
	b.appendDraw(scene.Draw{Mesh: id, First: uint32(first), Count: uint32(count), Material: material, Clip: clip, Layout: layout})
}

func (b *SceneBuilder) appendDraw(draw scene.Draw) {
	if b.breakDraw {
		if len(b.result.Draws) >= b.drawLimit {
			b.err = geometry.ErrGeometryLimit
			return
		}
		b.result.Draws = append(b.result.Draws, draw)
		b.breakDraw = false
	} else {
		b.result.Draws = scene.AppendDraw(b.result.Draws, draw)
	}
}

// Finish validates and seals the builder, returning owned geometry/draw/texture
// metadata with borrowed immutable image bytes. It can be repeated until a caller
// attempts further mutation; mutating methods after Finish latch ErrPackingClosed
// without changing published data. IDs/revisions are one-scene-local, starting at 1.
func (b *SceneBuilder) Finish() (*scene.Scene, error) {
	return b.finish(false)
}

func (b *SceneBuilder) finish(allowEmpty bool) (*scene.Scene, error) {
	if b.err != nil {
		return nil, b.err
	}
	if b.closed {
		return &b.result, nil
	}
	if !b.ready() {
		return nil, b.err
	}
	b.closed = true
	if allowEmpty && b.elements() == 0 {
		b.result = scene.Scene{} // no empty mesh or unused atlas allocation
	} else if !b.split && !b.resident {
		b.publish(&b.mesh, StableMesh)
	} else {
		// A mesh without geometry is omitted; its ID keeps its meaning.
		for _, mesh := range []struct {
			sections *sections
			id       uint64
		}{{&b.mesh, StableMesh}, {&b.dynamic, DynamicMesh}, {&b.symbols, SymbolMesh}} {
			if mesh.id == StableMesh && b.borrowed != nil {
				b.result.Meshes = append(b.result.Meshes, *b.borrowed)
			} else if mesh.sections.Count() > 0 {
				b.publish(mesh.sections, mesh.id)
			}
		}
	}
	// A borrowed StableMesh passed these checks in the build it comes from.
	if err := b.result.ValidateExcept(func(mesh *scene.Mesh) bool { return b.borrowed != nil && mesh.ID == StableMesh }); err != nil {
		b.err = err
		return nil, err
	}
	return &b.result, nil
}

// borrowDraw draws a borrowed primitive from the range its run had in the
// build the StableMesh comes from, with the material checks of Extruded and
// Dashed.
func (b *SceneBuilder) borrowDraw(primitive Primitive, material scene.Material, clip [4]float32) {
	if b.stablePlan == nil || primitive.StableRun < 1 || primitive.StableRun > len(b.stablePlan.runs) {
		b.err = ErrPackingInput
		return
	}
	run := b.stablePlan.runs[primitive.StableRun-1]
	if !run.drawn {
		b.err = ErrStableMismatch // a pattern sprite now packs geometry
		return
	}
	switch primitive.borrowed {
	case dashedShape:
		if !(primitive.DashUnit > 0) || math.IsInf(primitive.DashUnit, 0) || !(primitive.Dashes[0] > 0) {
			b.err = ErrPackingInput
			return
		}
		material.Kind, material.DashUnit = scene.Dashed, float32(primitive.DashUnit)
		for i, dash := range primitive.Dashes {
			material.Dashes[i] = float32(dash)
		}
		fallthrough
	case extrudedShape:
		if !(primitive.HalfWidth > 0) || math.IsInf(primitive.HalfWidth, 0) {
			b.err = ErrPackingInput
			return
		}
		material.MapAligned, material.OffsetScale = true, float32(primitive.HalfWidth)
	}
	b.appendDraw(scene.Draw{Mesh: StableMesh, First: run.first, Count: run.count, Material: material, Clip: clip, Layout: run.layout})
}
