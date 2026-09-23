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
	mesh      geometry.Builder[scene.Vertex]
	result    scene.Scene
	textures  map[string]uint64
	indexed   bool
	limit     int
	err       error
	closed    bool
	breakDraw bool
	drawLimit int
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
	b.mesh = geometry.NewBuilder[scene.Vertex](indexed, maximumElements)
	return b
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
	if vertices > b.limit || count > b.limit-b.mesh.Count() || (b.indexed && vertices > b.limit-len(b.mesh.Vertices)) {
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
	first := b.mesh.Count()
	if b.indexed {
		vertices := make([]scene.Vertex, len(mesh.Vertices))
		for i, point := range mesh.Vertices {
			vertices[i] = scene.Vertex{X: float32(point.X), Y: float32(point.Y)}
		}
		b.err = b.mesh.Append(vertices, mesh.Indices)
	} else if mesh.Indices != nil {
		for _, index := range mesh.Indices {
			point := mesh.Vertices[index]
			b.mesh.Vertices = append(b.mesh.Vertices, scene.Vertex{X: float32(point.X), Y: float32(point.Y)})
		}
	} else {
		for _, point := range mesh.Vertices {
			b.mesh.Vertices = append(b.mesh.Vertices, scene.Vertex{X: float32(point.X), Y: float32(point.Y)})
		}
	}
	b.draw(first, material, clip)
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
	first := b.mesh.Count()
	sin, cos := math.Sincos(angle)
	for i := 0; i < len(vertices); i += 4 {
		vertex := geometry.TextVertex{X: vertices[i], Y: vertices[i+1], U: vertices[i+2], V: vertices[i+3]}
		b.mesh.Vertices = append(b.mesh.Vertices, geometry.TransformTextVertex(anchor, vertex, offset, sin, cos))
	}
	b.draw(first, material, [4]float32{})
}

// IndexedText accepts known topology in either output mode (including icon quads
// in expanded scenes), reusing the original transformed scratch buffer and builder.
func (b *SceneBuilder) IndexedText(anchor geometry.Point, vertices []geometry.TextVertex, indices []uint32, offset geometry.Point, angle float64, material scene.Material) {
	if !b.check(len(vertices), indices) {
		return
	}
	first := b.mesh.Count()
	sin, cos := math.Sincos(angle)
	packed := make([]scene.Vertex, len(vertices))
	for i, vertex := range vertices {
		packed[i] = geometry.TransformTextVertex(anchor, vertex, offset, sin, cos)
	}
	b.err = b.mesh.Append(packed, indices)
	b.draw(first, material, [4]float32{})
}

func (b *SceneBuilder) draw(first int, material scene.Material, clip [4]float32) {
	count := b.mesh.Count() - first
	if b.err != nil || count == 0 {
		return
	}
	draw := scene.Draw{Mesh: 1, First: uint32(first), Count: uint32(count), Material: material, Clip: clip}
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
	if allowEmpty && b.mesh.Count() == 0 {
		b.result = scene.Scene{} // no empty mesh or unused atlas allocation
	} else {
		b.result.Meshes = []scene.Mesh{{ID: 1, Revision: 1, Vertices: b.mesh.Vertices, Indices: b.mesh.Indices}}
	}
	if err := b.result.Validate(); err != nil {
		b.err = err
		return nil, err
	}
	return &b.result, nil
}
