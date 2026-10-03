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

// Packed layouts store positions in int16 PositionUnits per tile-local unit and
// offsets in int16 OffsetUnits per unit, half the bytes of float32. MVT
// coordinates lie on a 1/16 grid at extent 4096, so positions within ±1024 units
// are exact; extruded lines' offsets are unit directions with miters up to 4,
// within 1/8192 of their value. Both units are powers of two, so reading a
// packed vertex back as float32 is exact.
const (
	PositionUnits = 32
	OffsetUnits   = 4096
)

// PackedPositionVertex is a PositionVertex in PositionUnits.
type PackedPositionVertex struct{ X, Y int16 }

// PackedOffsetVertex is an OffsetVertex with its position in PositionUnits and
// its pixel offset in OffsetUnits.
type PackedOffsetVertex struct{ X, Y, OffsetX, OffsetY int16 }

// PackedDashedVertex is a PackedOffsetVertex with texture coordinate U, the
// distance along a dashed line, which a dash period needs at full precision.
// V reads as zero.
type PackedDashedVertex struct {
	X, Y, OffsetX, OffsetY int16
	U                      float32
}

// PackPosition quantizes a position to PositionUnits, reporting false outside
// the int16 range.
func PackPosition(x, y float64) (int16, int16, bool) { return pack(x, y, PositionUnits) }

// PackOffset quantizes an offset to OffsetUnits, reporting false outside the
// int16 range.
func PackOffset(x, y float64) (int16, int16, bool) { return pack(x, y, OffsetUnits) }

func pack(x, y, units float64) (int16, int16, bool) {
	x, y = math.Round(x*units), math.Round(y*units)
	if !(x >= math.MinInt16 && x <= math.MaxInt16 && y >= math.MinInt16 && y <= math.MaxInt16) {
		return 0, 0, false
	}
	return int16(x), int16(y), true
}

// Layout names the vertex section of a mesh that a draw reads.
type Layout uint8

const (
	// FullLayout reads Mesh.Vertices.
	FullLayout Layout = iota
	// OffsetLayout reads Mesh.Offsets.
	OffsetLayout
	// PositionLayout reads Mesh.Positions.
	PositionLayout
	// PackedPositionLayout reads Mesh.PackedPositions.
	PackedPositionLayout
	// PackedOffsetLayout reads Mesh.PackedOffsets.
	PackedOffsetLayout
	// PackedDashedLayout reads Mesh.PackedDashed.
	PackedDashedLayout
	// LayoutCount is the number of layouts.
	LayoutCount
)

// Valid reports whether l names a vertex section.
func (l Layout) Valid() bool { return l < LayoutCount }

// Packed reports whether the layout stores int16 positions and offsets.
func (l Layout) Packed() bool { return l >= PackedPositionLayout && l < LayoutCount }

// Bytes is the packed size of one vertex of the layout, zero if it is unknown.
func (l Layout) Bytes() int {
	switch l {
	case FullLayout:
		return 24
	case OffsetLayout:
		return 16
	case PositionLayout:
		return 8
	case PackedPositionLayout:
		return 4
	case PackedOffsetLayout:
		return 8
	case PackedDashedLayout:
		return 12
	}
	return 0
}

// Mesh holds up to six vertex sections, one per Layout. Vertices carries every
// attribute. Offsets and Positions leave out attributes that are zero for all
// of their vertices, which are most vertices of a basemap, and the packed
// sections store them in half the bytes. A draw names its section in
// Draw.Layout, and its indices or vertex range count from the start of that
// section. A backend packs the sections into one buffer, in Layout order.
type Mesh struct {
	ID, Revision    uint64
	Vertices        []Vertex
	Offsets         []OffsetVertex         `json:",omitempty"`
	Positions       []PositionVertex       `json:",omitempty"`
	PackedPositions []PackedPositionVertex `json:",omitempty"`
	PackedOffsets   []PackedOffsetVertex   `json:",omitempty"`
	PackedDashed    []PackedDashedVertex   `json:",omitempty"`
	// Optional uint32 indices. Draw.First/Count address this array when set,
	// otherwise they address the section of the draw. Neither representation
	// reorders draws.
	Indices []uint32 `json:",omitempty"`
	// ShortIndices are uint16 indices in place of Indices, in half the bytes.
	// Draw.First/Count address them as they do Indices; each draw's indices
	// count from its Draw.Base, so a section longer than 65,536 vertices is
	// drawn in segments. A mesh has at most one of Indices and ShortIndices.
	ShortIndices []uint16 `json:",omitempty"`
}

// Indexed reports whether draws of the mesh address indices.
func (m Mesh) Indexed() bool { return len(m.Indices) > 0 || len(m.ShortIndices) > 0 }

// IndexCount is the number of indices, of either width.
func (m Mesh) IndexCount() int { return len(m.Indices) + len(m.ShortIndices) }

// Index is index i of the mesh, of either width, without a draw's Base. It
// panics like a slice when i is out of range.
func (m Mesh) Index(i int) uint32 {
	if m.ShortIndices != nil {
		return uint32(m.ShortIndices[i])
	}
	return m.Indices[i]
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
	case PackedPositionLayout:
		return len(m.PackedPositions)
	case PackedOffsetLayout:
		return len(m.PackedOffsets)
	case PackedDashedLayout:
		return len(m.PackedDashed)
	}
	return 0
}

// largestSection is the length of the mesh's longest vertex section.
func (m Mesh) largestSection() int {
	largest := 0
	for layout := range LayoutCount {
		largest = max(largest, m.Len(layout))
	}
	return largest
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
	case PackedPositionLayout:
		v := m.PackedPositions[index]
		return Vertex{X: float32(v.X) / PositionUnits, Y: float32(v.Y) / PositionUnits}
	case PackedOffsetLayout:
		v := m.PackedOffsets[index]
		return Vertex{X: float32(v.X) / PositionUnits, Y: float32(v.Y) / PositionUnits,
			OffsetX: float32(v.OffsetX) / OffsetUnits, OffsetY: float32(v.OffsetY) / OffsetUnits}
	case PackedDashedLayout:
		v := m.PackedDashed[index]
		return Vertex{X: float32(v.X) / PositionUnits, Y: float32(v.Y) / PositionUnits,
			OffsetX: float32(v.OffsetX) / OffsetUnits, OffsetY: float32(v.OffsetY) / OffsetUnits, U: v.U}
	}
	return m.Vertices[index]
}

// SectionOffset is the byte at which the section of a layout starts in the
// packed vertex buffer. Every section starts 4-byte aligned.
func (m Mesh) SectionOffset(layout Layout) uint64 {
	var offset uint64
	for l := range min(layout, LayoutCount) {
		offset += uint64(m.Len(l)) * uint64(l.Bytes())
	}
	return offset
}

// VertexBytes is the packed size of the vertex buffer.
func (m Mesh) VertexBytes() uint64 { return m.SectionOffset(LayoutCount) }

// IndexBytes is the size of the index buffer.
func (m Mesh) IndexBytes() uint64 { return uint64(len(m.Indices))*4 + uint64(len(m.ShortIndices))*2 }

// BufferBytes is the packed size of the vertex and optional index buffers.
func (m Mesh) BufferBytes() uint64 { return m.VertexBytes() + m.IndexBytes() }

// Validate checks resource bounds and every vertex before GPU upload. Indices
// are checked against the largest section here and against the section and
// Base of each draw by Scene.Validate.
func (m Mesh) Validate() error {
	if err := m.validateSize(); err != nil {
		return err
	}
	return m.validateContent()
}

func (m Mesh) validateSize() error {
	if m.ID == 0 || m.largestSection() == 0 || m.VertexBytes() > 1<<31-1 || len(m.Indices) > (1<<31-1)/4 ||
		len(m.ShortIndices) > (1<<31-1)/2 || len(m.Indices) > 0 && len(m.ShortIndices) > 0 {
		return fmt.Errorf("invalid mesh %d", m.ID)
	}
	return nil
}

// validateContent checks every vertex and index, in time proportional to the
// mesh.
func (m Mesh) validateContent() error {
	// The largest magnitude decides finiteness, without a branch per value.
	var largestMagnitude uint32
	for _, v := range m.Vertices {
		largestMagnitude = max(largestMagnitude, magnitude(v.X), magnitude(v.Y), magnitude(v.OffsetX), magnitude(v.OffsetY), magnitude(v.U), magnitude(v.V))
	}
	for _, v := range m.Offsets {
		largestMagnitude = max(largestMagnitude, magnitude(v.X), magnitude(v.Y), magnitude(v.OffsetX), magnitude(v.OffsetY))
	}
	for _, v := range m.Positions {
		largestMagnitude = max(largestMagnitude, magnitude(v.X), magnitude(v.Y))
	}
	// Packed positions and offsets are integers; only the distance is a float.
	for _, v := range m.PackedDashed {
		largestMagnitude = max(largestMagnitude, magnitude(v.U))
	}
	if largestMagnitude >= nonFinite {
		return fmt.Errorf("non-finite vertex in mesh %d", m.ID)
	}
	// Short indices count from the Base of each draw, which Scene.Validate
	// checks.
	largest := m.largestSection()
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
	// Base is the vertex of the section that index zero reads, for an
	// indexed mesh; see Mesh.ShortIndices. It is zero for an unindexed mesh.
	Base uint32 `json:",omitempty"`
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
func (s *Scene) Validate() error { return s.ValidateExcept(nil) }

// ValidateExcept is Validate without the vertex and index checks of the meshes
// for which checked reports true: immutable meshes that an earlier Validate
// accepted. Their sizes and IDs, and the draws that read them, are still
// checked. A nil checked validates every mesh.
func (s *Scene) ValidateExcept(checked func(*Mesh) bool) error {
	if s == nil {
		return fmt.Errorf("nil scene")
	}
	meshes, err := validateMeshes(s.Meshes, checked)
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

func validateMeshes(values []Mesh, checked func(*Mesh) bool) (map[uint64]*Mesh, error) {
	meshes := make(map[uint64]*Mesh, len(values))
	for i := range values {
		mesh := &values[i]
		if err := mesh.validateSize(); err != nil {
			return nil, err
		}
		if checked == nil || !checked(mesh) {
			if err := mesh.validateContent(); err != nil {
				return nil, err
			}
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
	if !exists || !draw.Layout.Valid() || draw.Count == 0 || draw.Count%3 != 0 || draw.Transform < 0 {
		return fmt.Errorf("invalid mesh range or transform")
	}
	section, elements := mesh.Len(draw.Layout), mesh.Len(draw.Layout)
	if mesh.Indexed() {
		elements = mesh.IndexCount()
	} else if draw.Base != 0 {
		return fmt.Errorf("vertex base of an unindexed mesh")
	}
	if uint64(draw.First)+uint64(draw.Count) > uint64(elements) {
		return fmt.Errorf("invalid mesh range or transform")
	}
	if uint64(draw.Base) >= uint64(section) && mesh.Indexed() {
		return fmt.Errorf("index out of bounds of its vertex section")
	}
	// Mesh.Validate bounds indices by the largest section, which is the section
	// of every draw of a mesh with one section. Short indices reach at most
	// 65,535 past their base.
	switch {
	case len(mesh.Indices) > 0 && (section < mesh.largestSection() || draw.Base != 0):
		if !inSection(mesh.Indices[draw.First:draw.First+draw.Count], draw.Base, section) {
			return fmt.Errorf("index out of bounds of its vertex section")
		}
	case len(mesh.ShortIndices) > 0 && uint64(draw.Base)+math.MaxUint16 >= uint64(section):
		if !inSection(mesh.ShortIndices[draw.First:draw.First+draw.Count], draw.Base, section) {
			return fmt.Errorf("index out of bounds of its vertex section")
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

// inSection reports whether indices counted from base lie in a section of
// that many vertices.
func inSection[T uint16 | uint32](indices []T, base uint32, section int) bool {
	var largest T
	for _, index := range indices {
		largest = max(largest, index)
	}
	return uint64(base)+uint64(largest) < uint64(section)
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

// nonFinite is the magnitude bits of infinity, the smallest of an infinity or
// NaN: all exponent bits set.
const nonFinite = 0x7f800000

// magnitude is f's bits without the sign. Magnitudes of finite values are below
// nonFinite.
func magnitude(f float32) uint32 { return math.Float32bits(f) &^ (1 << 31) }

func finite(f float32) bool { return magnitude(f) < nonFinite }

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
		if last.Mesh == draw.Mesh && last.Layout == draw.Layout && last.Base == draw.Base && last.Transform == draw.Transform && last.Material == draw.Material && last.Clip == draw.Clip && uint64(last.First)+uint64(last.Count) == uint64(draw.First) && uint64(last.Count)+uint64(draw.Count) <= math.MaxUint32 {
			last.Count += draw.Count
			return draws
		}
	}
	return append(draws, draw)
}
