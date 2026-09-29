// Package scene defines toolkit-neutral retained vector rendering data.
// It has no Qt, cgo, window-system, or native GPU dependencies. Producers own
// scene construction; after publication all slices and resource data are immutable.
package scene

import (
	"fmt"
	"math"
)

// Vertex separates a world/tile-local anchor from a screen-space pixel offset.
// This lets backends keep label geometry resident during camera zoom/rotation.
type Vertex struct {
	X, Y             float32
	OffsetX, OffsetY float32
	U, V             float32
}

// Affine maps local coordinates to logical viewport pixels, in row-major order:
// x = M11*x + M12*y + DX; y = M21*x + M22*y + DY.
type Affine struct{ M11, M12, DX, M21, M22, DY float32 }

type Kind uint8

const (
	Solid Kind = iota
	Image
	Pattern
	SDFHalo
	SDFFill
	// Dashed is Solid geometry whose fragments are kept only on the dashes of
	// Material.Dashes. Vertex U carries the distance along the line.
	Dashed
)

// Material contains drawing semantics rather than a toolkit-specific shader or
// pipeline handle. Colors are straight-alpha RGBA in [0,1]. Texture 0 is white.
type Material struct {
	Kind                           Kind
	Texture                        uint64
	Color                          [4]float32
	FontScale, HaloWidth, HaloBlur float32
	PatternSize, PatternPhase      [2]float32
	MapAligned                     bool
	// OffsetScale multiplies vertex pixel offsets; zero means one. Extruded
	// lines store unit directions as offsets and their half width in logical
	// pixels here, so one resident mesh serves every evaluated line width.
	OffsetScale float32 `json:",omitempty"`
	// Dashes and DashUnit apply to Dashed materials. Dashes alternates dash and
	// gap lengths in multiples of DashUnit, which is the line width in the unit
	// of vertex U, the distance along the line from its first point. A fragment
	// is drawn where its distance modulo the pattern length lies in a dash. The
	// pattern does not change with the line width, so a resident mesh serves
	// every evaluated width.
	Dashes   [4]float32 `json:",omitzero"`
	DashUnit float32    `json:",omitempty"`
}

// OffsetVertex is a Vertex without a texture coordinate, which reads as zero.
type OffsetVertex struct {
	X, Y             float32
	OffsetX, OffsetY float32
}

// PositionVertex is a Vertex with a position only. Its pixel offset and texture
// coordinate read as zero.
type PositionVertex struct{ X, Y float32 }

// Layout names the vertex section of a mesh that a draw reads.
type Layout uint8

const (
	// FullLayout reads Mesh.Vertices.
	FullLayout Layout = iota
	// OffsetLayout reads Mesh.Offsets.
	OffsetLayout
	// PositionLayout reads Mesh.Positions.
	PositionLayout
)

// Bytes is the packed size of one vertex of the layout, zero if it is unknown.
func (l Layout) Bytes() int {
	switch l {
	case FullLayout:
		return 24
	case OffsetLayout:
		return 16
	case PositionLayout:
		return 8
	}
	return 0
}

// Mesh holds up to three vertex sections. Vertices carries every attribute.
// Offsets and Positions leave out attributes that are zero for all of their
// vertices, which are most vertices of a basemap. A draw names its section in
// Draw.Layout, and its indices or vertex range count from the start of that
// section. A backend packs the sections into one buffer, in the order Vertices,
// Offsets, Positions.
type Mesh struct {
	ID, Revision uint64
	Vertices     []Vertex
	Offsets      []OffsetVertex   `json:",omitempty"`
	Positions    []PositionVertex `json:",omitempty"`
	// Optional uint32 indices. Draw.First/Count address this array when set,
	// otherwise they address the section of the draw. Neither representation
	// reorders draws.
	Indices []uint32 `json:",omitempty"`
}

// Len is the number of vertices in the section of a layout.
func (m Mesh) Len(layout Layout) int {
	switch layout {
	case FullLayout:
		return len(m.Vertices)
	case OffsetLayout:
		return len(m.Offsets)
	case PositionLayout:
		return len(m.Positions)
	}
	return 0
}

// At returns vertex index of a section with every attribute. It panics like a
// slice when the index is out of range.
func (m Mesh) At(layout Layout, index int) Vertex {
	switch layout {
	case OffsetLayout:
		v := m.Offsets[index]
		return Vertex{X: v.X, Y: v.Y, OffsetX: v.OffsetX, OffsetY: v.OffsetY}
	case PositionLayout:
		v := m.Positions[index]
		return Vertex{X: v.X, Y: v.Y}
	}
	return m.Vertices[index]
}

// SectionOffset is the byte at which the section of a layout starts in the
// packed vertex buffer.
func (m Mesh) SectionOffset(layout Layout) uint64 {
	switch layout {
	case OffsetLayout:
		return uint64(len(m.Vertices)) * 24
	case PositionLayout:
		return uint64(len(m.Vertices))*24 + uint64(len(m.Offsets))*16
	}
	return 0
}

// VertexBytes is the packed size of the vertex buffer.
func (m Mesh) VertexBytes() uint64 {
	return uint64(len(m.Vertices))*24 + uint64(len(m.Offsets))*16 + uint64(len(m.Positions))*8
}

// BufferBytes is the packed size of the vertex and optional index buffers.
func (m Mesh) BufferBytes() uint64 { return m.VertexBytes() + uint64(len(m.Indices))*4 }

// Validate checks resource bounds and every vertex before GPU upload. Indices
// are checked against the largest section here and against the section of each
// draw by Scene.Validate.
func (m Mesh) Validate() error {
	count := uint64(len(m.Vertices)) + uint64(len(m.Offsets)) + uint64(len(m.Positions))
	if m.ID == 0 || count == 0 || m.VertexBytes() > 1<<31-1 || len(m.Indices) > (1<<31-1)/4 {
		return fmt.Errorf("invalid mesh %d", m.ID)
	}
	for _, v := range m.Vertices {
		for _, f := range [...]float32{v.X, v.Y, v.OffsetX, v.OffsetY, v.U, v.V} {
			if !finite(f) {
				return fmt.Errorf("non-finite vertex in mesh %d", m.ID)
			}
		}
	}
	for _, v := range m.Offsets {
		for _, f := range [...]float32{v.X, v.Y, v.OffsetX, v.OffsetY} {
			if !finite(f) {
				return fmt.Errorf("non-finite vertex in mesh %d", m.ID)
			}
		}
	}
	for _, v := range m.Positions {
		if !finite(v.X) || !finite(v.Y) {
			return fmt.Errorf("non-finite vertex in mesh %d", m.ID)
		}
	}
	largest := max(len(m.Vertices), len(m.Offsets), len(m.Positions))
	for _, index := range m.Indices {
		if uint64(index) >= uint64(largest) {
			return fmt.Errorf("index out of bounds in mesh %d", m.ID)
		}
	}
	return nil
}

// Texture is tightly packed, unpremultiplied RGBA8 data. Pixel storage and atlas
// layout belong to Go; native texture handles belong exclusively to a backend.
type Texture struct {
	ID, Revision  uint64
	Width, Height int
	RGBA          []byte
}

type Draw struct {
	Mesh uint64
	// First and Count are vertex or index elements according to the mesh.
	First, Count uint32
	Transform    int
	Material     Material
	// Clip is a local-coordinate rectangle [left,top,right,bottom]. A zero
	// rectangle disables clipping. It never clips screen-space label offsets.
	Clip [4]float32
	// Layout is the vertex section of the mesh this draw reads.
	Layout Layout `json:",omitempty"`
}

type Scene struct {
	Meshes   []Mesh
	Textures []Texture
	Draws    []Draw
}

// Frame references retained geometry. Updating transforms does not change scene
// identity and must not cause geometry or texture uploads in a backend.
type Frame struct {
	Scene            *Scene
	Transforms       []Affine
	DevicePixelRatio float32
}

// Validate runs before publishing a scene, off the GUI/render thread.
func (s *Scene) Validate() error {
	if s == nil {
		return fmt.Errorf("nil scene")
	}
	meshes, err := validateMeshes(s.Meshes)
	if err != nil {
		return err
	}
	textures, err := validateTextures(s.Textures)
	if err != nil {
		return err
	}
	for index, draw := range s.Draws {
		if err := draw.validate(meshes, textures); err != nil {
			return fmt.Errorf("draw %d: %w", index, err)
		}
	}
	return nil
}

func validateMeshes(values []Mesh) (map[uint64]*Mesh, error) {
	meshes := make(map[uint64]*Mesh, len(values))
	for i := range values {
		mesh := &values[i]
		if err := mesh.Validate(); err != nil {
			return nil, err
		}
		if _, exists := meshes[mesh.ID]; exists {
			return nil, fmt.Errorf("duplicate mesh %d", mesh.ID)
		}
		meshes[mesh.ID] = mesh
	}
	return meshes, nil
}

func validateTextures(values []Texture) (map[uint64]bool, error) {
	textures := map[uint64]bool{0: true}
	for _, texture := range values {
		if texture.ID == 0 || textures[texture.ID] {
			return nil, fmt.Errorf("duplicate/reserved texture %d", texture.ID)
		}
		if texture.Width <= 0 || texture.Height <= 0 || texture.Width > 16384 || texture.Height > 16384 || len(texture.RGBA) != texture.Width*texture.Height*4 {
			return nil, fmt.Errorf("invalid texture %d", texture.ID)
		}
		textures[texture.ID] = true
	}
	return textures, nil
}

func (draw Draw) validate(meshes map[uint64]*Mesh, textures map[uint64]bool) error {
	mesh, exists := meshes[draw.Mesh]
	if !exists || draw.Layout > PositionLayout || draw.Count == 0 || draw.Count%3 != 0 || draw.Transform < 0 {
		return fmt.Errorf("invalid mesh range or transform")
	}
	section, elements := mesh.Len(draw.Layout), mesh.Len(draw.Layout)
	if len(mesh.Indices) > 0 {
		elements = len(mesh.Indices)
	}
	if uint64(draw.First)+uint64(draw.Count) > uint64(elements) {
		return fmt.Errorf("invalid mesh range or transform")
	}
	// Mesh.Validate bounds indices by the largest section, which is the section
	// of every draw of a mesh with one section.
	if len(mesh.Indices) > 0 && section < max(len(mesh.Vertices), len(mesh.Offsets), len(mesh.Positions)) {
		for _, index := range mesh.Indices[draw.First : draw.First+draw.Count] {
			if uint64(index) >= uint64(section) {
				return fmt.Errorf("index out of bounds of its vertex section")
			}
		}
	}
	if !textures[draw.Material.Texture] {
		return fmt.Errorf("missing texture")
	}
	for _, v := range draw.Clip {
		if !finite(v) {
			return fmt.Errorf("non-finite clip")
		}
	}
	if draw.Clip != [4]float32{} && (draw.Clip[0] >= draw.Clip[2] || draw.Clip[1] >= draw.Clip[3]) {
		return fmt.Errorf("invalid clip")
	}
	return draw.Material.validate()
}

func (m Material) validate() error {
	if m.Kind > Dashed {
		return fmt.Errorf("invalid material kind")
	}
	if !validColor(m.Color) {
		return fmt.Errorf("invalid color")
	}
	for _, v := range [...]float32{m.FontScale, m.HaloWidth, m.HaloBlur, m.PatternSize[0], m.PatternSize[1], m.PatternPhase[0], m.PatternPhase[1], m.OffsetScale, m.Dashes[0], m.Dashes[1], m.Dashes[2], m.Dashes[3], m.DashUnit} {
		if !finite(v) {
			return fmt.Errorf("non-finite material")
		}
	}
	if m.OffsetScale < 0 {
		return fmt.Errorf("invalid offset scale")
	}
	if m.Kind == Pattern && (m.PatternSize[0] <= 0 || m.PatternSize[1] <= 0) {
		return fmt.Errorf("invalid pattern size")
	}
	if (m.Kind == SDFHalo || m.Kind == SDFFill) && (m.FontScale <= 0 || m.HaloWidth < 0 || m.HaloBlur < 0) {
		return fmt.Errorf("invalid SDF material")
	}
	if m.Kind == Dashed && (m.DashUnit <= 0 || m.Dashes[0] <= 0 || m.Dashes[1] < 0 || m.Dashes[2] < 0 || m.Dashes[3] < 0) {
		return fmt.Errorf("invalid dash pattern")
	}
	return nil
}

func finite(f float32) bool { return !math.IsNaN(float64(f)) && !math.IsInf(float64(f), 0) }

func validColor(color [4]float32) bool {
	for _, value := range color {
		if !finite(value) || value < 0 || value > 1 {
			return false
		}
	}
	return true
}

// AppendDraw coalesces adjacent compatible ranges without reordering transparent
// layers. Callers append geometry first and pass its range here.
func AppendDraw(draws []Draw, draw Draw) []Draw {
	if len(draws) > 0 {
		last := &draws[len(draws)-1]
		if last.Mesh == draw.Mesh && last.Layout == draw.Layout && last.Transform == draw.Transform && last.Material == draw.Material && last.Clip == draw.Clip && uint64(last.First)+uint64(last.Count) == uint64(draw.First) && uint64(last.Count)+uint64(draw.Count) <= math.MaxUint32 {
			last.Count += draw.Count
			return draws
		}
	}
	return append(draws, draw)
}
