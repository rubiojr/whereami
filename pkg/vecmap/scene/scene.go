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
}

type Mesh struct {
	ID, Revision uint64
	Vertices     []Vertex
	// Optional uint32 indices. Draw.First/Count address this array when set,
	// otherwise they address Vertices. Neither representation reorders draws.
	Indices []uint32 `json:",omitempty"`
}

// BufferBytes is the packed size of the vertex and optional index buffers.
func (m Mesh) BufferBytes() uint64 { return uint64(len(m.Vertices))*24 + uint64(len(m.Indices))*4 }

// Validate checks resource bounds and every referenced vertex before GPU upload.
func (m Mesh) Validate() error {
	if m.ID == 0 || len(m.Vertices) == 0 || len(m.Vertices) > (1<<31-1)/24 || len(m.Indices) > (1<<31-1)/4 {
		return fmt.Errorf("invalid mesh %d", m.ID)
	}
	for _, v := range m.Vertices {
		for _, f := range [...]float32{v.X, v.Y, v.OffsetX, v.OffsetY, v.U, v.V} {
			if !finite(f) {
				return fmt.Errorf("non-finite vertex in mesh %d", m.ID)
			}
		}
	}
	for _, index := range m.Indices {
		if uint64(index) >= uint64(len(m.Vertices)) {
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

func validateMeshes(values []Mesh) (map[uint64]int, error) {
	meshes := make(map[uint64]int, len(values))
	for _, mesh := range values {
		if err := mesh.Validate(); err != nil {
			return nil, err
		}
		if _, exists := meshes[mesh.ID]; exists {
			return nil, fmt.Errorf("duplicate mesh %d", mesh.ID)
		}
		meshes[mesh.ID] = len(mesh.Vertices)
		if len(mesh.Indices) > 0 {
			meshes[mesh.ID] = len(mesh.Indices)
		}
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

func (draw Draw) validate(meshes map[uint64]int, textures map[uint64]bool) error {
	n, exists := meshes[draw.Mesh]
	if !exists || draw.Count == 0 || draw.Count%3 != 0 || uint64(draw.First)+uint64(draw.Count) > uint64(n) || draw.Transform < 0 {
		return fmt.Errorf("invalid mesh range or transform")
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
	if m.Kind > SDFFill {
		return fmt.Errorf("invalid material kind")
	}
	if !validColor(m.Color) {
		return fmt.Errorf("invalid color")
	}
	for _, v := range [...]float32{m.FontScale, m.HaloWidth, m.HaloBlur, m.PatternSize[0], m.PatternSize[1], m.PatternPhase[0], m.PatternPhase[1], m.OffsetScale} {
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
		if last.Mesh == draw.Mesh && last.Transform == draw.Transform && last.Material == draw.Material && last.Clip == draw.Clip && uint64(last.First)+uint64(last.Count) == uint64(draw.First) && uint64(last.Count)+uint64(draw.Count) <= math.MaxUint32 {
			last.Count += draw.Count
			return draws
		}
	}
	return append(draws, draw)
}
