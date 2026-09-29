package compiler

import (
	"fmt"
	"math"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/glyph"
	"github.com/rubiojr/whereami/pkg/vecmap/placement"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/sprite"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
)

// SpriteLookup supplies an already prepared immutable crop/tint image. Calls are
// synchronous; loading, caching and scheduling belong to the caller. False means
// unavailable, not a packing error. The image bytes may be retained by the scene.
type SpriteLookup func(name string, color style.Color, opacity float64) (sprite.Image, bool)

// Primitive selects solid/pattern material and tile clipping for retained geometry.
// Pattern phase uses the supplied canonical tile and world wrap; geometry stays
// tile-local. Missing patterns are skipped. Inputs follow CompileTile's contract.
func (b *SceneBuilder) Primitive(tile view.TileID, wrap int, primitive Primitive, lookup SpriteLookup) {
	b.primitive(tile, wrap, primitive, lookup)
}

func (b *SceneBuilder) primitive(tile view.TileID, wrap int, primitive Primitive, lookup SpriteLookup) [2]float64 {
	if !b.ready() {
		return [2]float64{}
	}
	var period [2]float64
	material := scene.Material{Color: PackedColor(primitive.Color)}
	if primitive.PatternName != "" {
		image, ok := b.sprite(lookup, primitive.PatternName, style.Color{Red: 255, Green: 255, Blue: 255, Alpha: 255}, 1)
		if !ok {
			return period
		}
		width, height := float64(image.Width)/image.PixelRatio*primitive.PatternScale, float64(image.Height)/image.PixelRatio*primitive.PatternScale
		period = [2]float64{width, height}
		x, y := view.PatternPhase(tile, wrap, width, height)
		material = scene.Material{Kind: scene.Pattern, Texture: b.Texture("pattern/"+primitive.PatternName, image.Width, image.Height, image.Pixels),
			Color: [4]float32{1, 1, 1, float32(primitive.Opacity)}, PatternSize: [2]float32{float32(width), float32(height)}, PatternPhase: [2]float32{float32(x), float32(y)}}
	}
	clip := [4]float32{0, 0, view.TileSize, view.TileSize}
	b.volatile = primitive.Dynamic
	if primitive.Directions != nil {
		b.Extruded(primitive.Mesh, primitive.Directions, primitive.HalfWidth, material, clip)
	} else {
		b.Geometry(primitive.Mesh, material, clip)
	}
	b.volatile = false
	return period
}

// RenderSymbol pairs evaluated paint with caller-owned collision acceptance and
// an optional prepared text mesh. Layout meshes must match the builder's output
// mode and have complete atlas coverage. The value borrows immutable layout data.
type RenderSymbol struct {
	Symbol   placement.Symbol
	Accepted placement.Accepted
	Layout   *glyph.PreparedLayout
}

// SymbolLayer emits icons, then all text halos, then all text fills. symbolAt must
// return the same immutable one-layer sequence on each of three traversals.
// Count includes rejected symbols and is capped at placement.MaxSymbols before
// preparation. Accessor work is caller-owned; zero count needs no accessor.
// No candidate/conversion slice is allocated. Return value counts accepted text
// fill labels, and is meaningful only if Finish succeeds. Errors latch on the
// builder; callers still use Finish for atomic scene publication.
func (b *SceneBuilder) SymbolLayer(count int, symbolAt func(int) RenderSymbol, atlas uint64, lookup SpriteLookup) int {
	return b.symbolLayer(count, symbolAt, func(_ int, kind scene.Kind, item RenderSymbol) bool {
		return b.symbolPass(kind, item, atlas, lookup)
	})
}

func (b *SceneBuilder) symbolLayer(count int, symbolAt func(int) RenderSymbol, emit func(int, scene.Kind, RenderSymbol) bool) int {
	if !b.ready() {
		return 0
	}
	if count < 0 || (count > 0 && symbolAt == nil) {
		b.err = ErrPackingInput
		return 0
	}
	if count > placement.MaxSymbols {
		b.err = geometry.ErrGeometryLimit
		return 0
	}
	labels := 0
	for _, kind := range [...]scene.Kind{scene.Image, scene.SDFHalo, scene.SDFFill} {
		for index := range count {
			if emit(index, kind, symbolAt(index)) {
				labels++
			}
			if b.err != nil {
				return labels
			}
		}
	}
	return labels
}

func (b *SceneBuilder) symbolPass(kind scene.Kind, item RenderSymbol, atlas uint64, lookup SpriteLookup) bool {
	// Label offsets follow the evaluated text and icon sizes.
	b.volatile = true
	defer func() { b.volatile = false }()
	if kind == scene.Image {
		if item.Accepted.Icon && item.Symbol.IconName != "" {
			b.icon(item.Symbol, lookup)
		}
	} else if item.Accepted.Text && item.Layout != nil {
		return b.text(item.Symbol, item.Layout, atlas, kind) && kind == scene.SDFFill
	}
	return false
}

func (b *SceneBuilder) sprite(lookup SpriteLookup, name string, color style.Color, opacity float64) (sprite.Image, bool) {
	if lookup == nil {
		return sprite.Image{}, false
	}
	image, ok := lookup(name, color, opacity)
	if !ok {
		return sprite.Image{}, false
	}
	if image.PixelRatio <= 0 || math.IsNaN(image.PixelRatio) || math.IsInf(image.PixelRatio, 0) {
		b.err = ErrPackingInput
		return sprite.Image{}, false
	}
	return image, true
}

func (b *SceneBuilder) icon(candidate placement.Symbol, lookup SpriteLookup) {
	image, ok := b.sprite(lookup, candidate.IconName, candidate.IconColor, candidate.IconOpacity)
	if !ok {
		return
	}
	width, height := float64(image.Width)/image.PixelRatio*candidate.IconSize, float64(image.Height)/image.PixelRatio*candidate.IconSize
	x, y := placement.AnchoredOrigin(candidate.IconAnchor, width, height)
	quad := geometry.TextQuad(x, y, x+width, y+height, 0, 0, 1, 1)
	key := fmt.Sprintf("icon/%s/%v/%g", candidate.IconName, candidate.IconColor, candidate.IconOpacity)
	material := scene.Material{Kind: scene.Image, Texture: b.Texture(key, image.Width, image.Height, image.Pixels), Color: [4]float32{1, 1, 1, 1}, MapAligned: !candidate.IconViewportAligned}
	b.IndexedText(candidate.Anchor, quad[:], []uint32{0, 1, 2, 0, 2, 3},
		geometry.Point{X: candidate.IconOffset.X * candidate.IconSize, Y: candidate.IconOffset.Y * candidate.IconSize},
		placement.RenderedSymbolAngle(candidate.IconLineAngle, candidate.IconRotate, candidate.IconViewportAligned), material)
}

func (b *SceneBuilder) text(candidate placement.Symbol, layout *glyph.PreparedLayout, atlas uint64, kind scene.Kind) bool {
	color := candidate.TextColor
	if kind == scene.SDFHalo {
		if candidate.HaloWidth <= 0 || candidate.HaloColor.Alpha == 0 {
			return false
		}
		color = candidate.HaloColor
	}
	offset := geometry.Point{X: candidate.TextOffset.X * candidate.TextSize, Y: candidate.TextOffset.Y * candidate.TextSize}
	angle := placement.RenderedSymbolAngle(candidate.LineAngle, candidate.TextRotate, candidate.ViewportAligned)
	material := scene.Material{Kind: kind, Texture: atlas, Color: PackedColor(color), FontScale: float32(layout.Scale), HaloWidth: float32(candidate.HaloWidth), HaloBlur: float32(candidate.HaloBlur), MapAligned: !candidate.ViewportAligned}
	if b.indexed {
		b.IndexedText(candidate.Anchor, layout.Vertices, layout.Indices, offset, angle, material)
	} else {
		b.ExpandedText(candidate.Anchor, layout.Expanded, offset, angle, material)
	}
	return true
}
