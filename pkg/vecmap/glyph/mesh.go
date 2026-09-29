package glyph

import (
	"errors"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
)

var ErrLayoutGeometry = errors.New("invalid glyph layout geometry")

// LayoutMesh owns exactly one representation: Expanded contains packed XYUV
// triangle vertices, or Vertices/Indices retain direct indexed quad topology.
// This keeps legacy packed consumers from needing a second conversion buffer.
type LayoutMesh struct {
	Expanded []float32
	Vertices []geometry.TextVertex
	Indices  []uint32
	// Unit marks positions built at scale one (EmSize logical pixels per em)
	// instead of the layout's Scale. The consumer multiplies them by Scale,
	// normally per draw, so one mesh serves every evaluated text size.
	Unit bool
}

// FitsAtlas checks complete drawable-glyph coverage, not rectangle/mesh validity.
// Bitmap-free glyphs need no atlas rectangle. Inputs are borrowed synchronously.
func FitsAtlas(layout *TextLayout, atlas *Atlas) bool {
	if layout == nil || atlas == nil || len(layout.Glyphs) > MaxTextRunes {
		return false
	}
	for _, positioned := range layout.Glyphs {
		if len(positioned.Glyph.Bitmap) == 0 {
			continue
		}
		if _, exists := atlas.Positions[positioned.Key]; !exists {
			return false
		}
	}
	return true
}

// BuildLayoutMesh reuses the original padding, UV arithmetic and triangle order.
// Missing atlas entries/bitmap-free glyphs are skipped; call FitsAtlas first when
// a label must be complete. Nil layout/atlas yields an empty mesh. Errors return
// no partial output and leave inputs untouched. Output owns all geometry buffers.
func BuildLayoutMesh(layout *TextLayout, atlas *Atlas, indexed bool) (LayoutMesh, error) {
	if layout == nil || atlas == nil {
		return LayoutMesh{}, nil
	}
	if err := validateMeshInput(layout, atlas); err != nil {
		return LayoutMesh{}, err
	}
	if indexed {
		mesh := geometry.NewBuilder[geometry.TextVertex](true, len(layout.Glyphs)*6)
		if err := emitLayoutQuads(layout, atlas, func(quad [4]geometry.TextVertex) error {
			return mesh.Quad(quad[0], quad[1], quad[2], quad[3])
		}); err != nil {
			return LayoutMesh{}, err
		}
		return LayoutMesh{Vertices: mesh.Vertices, Indices: mesh.Indices}, nil
	}
	vertices := make([]float32, 0, len(layout.Glyphs)*6*4)
	if err := emitLayoutQuads(layout, atlas, func(quad [4]geometry.TextVertex) error {
		for _, index := range [...]int{0, 1, 2, 0, 2, 3} {
			vertex := quad[index]
			vertices = append(vertices, vertex.X, vertex.Y, vertex.U, vertex.V)
		}
		return nil
	}); err != nil {
		return LayoutMesh{}, err
	}
	return LayoutMesh{Expanded: vertices}, nil
}

// EmitLayoutQuads is the streaming form for custom geometry sinks. Prior sink
// calls are not undone on a later validation/sink error. Nil layout/atlas emits
// nothing; otherwise a nil sink is invalid. Neither inputs nor sink are retained.
func EmitLayoutQuads(layout *TextLayout, atlas *Atlas, emit func([4]geometry.TextVertex) error) error {
	if layout == nil || atlas == nil {
		return nil
	}
	if emit == nil {
		return ErrLayoutGeometry
	}
	if err := validateMeshInput(layout, atlas); err != nil {
		return err
	}
	return emitLayoutQuads(layout, atlas, emit)
}

// BuildUnitLayoutMesh is BuildLayoutMesh at scale one, marked Unit: the mesh does
// not depend on the evaluated text size. The layout's own Scale is still
// validated. A Scale that float32 cannot carry as a positive finite factor has no
// unit form, so that layout keeps its baked mesh and the existing failure rules.
func BuildUnitLayoutMesh(layout *TextLayout, atlas *Atlas, indexed bool) (LayoutMesh, error) {
	if layout == nil || atlas == nil {
		return LayoutMesh{}, nil
	}
	if scale := float32(layout.Scale); !(scale > 0) || !finite(float64(scale)) {
		return BuildLayoutMesh(layout, atlas, indexed)
	}
	unit := TextLayout{Glyphs: layout.Glyphs, Scale: 1}
	mesh, err := BuildLayoutMesh(&unit, atlas, indexed)
	if err != nil {
		return LayoutMesh{}, err
	}
	mesh.Unit = true
	return mesh, nil
}

func validateMeshInput(layout *TextLayout, atlas *Atlas) error {
	// Check before capacity arithmetic or traversing caller-provided layouts.
	if len(layout.Glyphs) > MaxTextRunes {
		return geometry.ErrGeometryLimit
	}
	if len(layout.Glyphs) == 0 {
		return nil
	}
	if !finite(layout.Scale) || layout.Scale < 0 {
		return ErrLayoutGeometry
	}
	if atlas.Width <= 0 || atlas.Height <= 0 || atlas.Width > MaxAtlasSize || atlas.Height > MaxAtlasSize {
		return ErrLayoutGeometry
	}
	return nil
}

func emitLayoutQuads(layout *TextLayout, atlas *Atlas, emit func([4]geometry.TextVertex) error) error {
	for _, positioned := range layout.Glyphs {
		rectangle, exists := atlas.Positions[positioned.Key]
		if !exists || len(positioned.Glyph.Bitmap) == 0 {
			continue
		}
		if !validAtlasRect(rectangle, atlas.Width, atlas.Height) {
			return ErrLayoutGeometry
		}
		scale := layout.Scale
		x1 := (positioned.X + float64(positioned.Glyph.Left) - AtlasPadding) * scale
		y1 := (positioned.Y - float64(positioned.Glyph.Top) - AtlasPadding) * scale
		x2 := x1 + float64(rectangle.Width)*scale
		y2 := y1 + float64(rectangle.Height)*scale
		u1 := float64(rectangle.X) / float64(atlas.Width)
		v1 := float64(rectangle.Y) / float64(atlas.Height)
		u2 := float64(rectangle.X+rectangle.Width) / float64(atlas.Width)
		v2 := float64(rectangle.Y+rectangle.Height) / float64(atlas.Height)
		quad := geometry.TextQuad(x1, y1, x2, y2, u1, v1, u2, v2)
		for _, vertex := range quad {
			if !finite(float64(vertex.X)) || !finite(float64(vertex.Y)) || !finite(float64(vertex.U)) || !finite(float64(vertex.V)) {
				return ErrLayoutGeometry
			}
		}
		if err := emit(quad); err != nil {
			return err
		}
	}
	return nil
}

func validAtlasRect(rect Rect, width, height int) bool {
	// Subtract only after validating origins, avoiding int overflow on 386.
	return rect.X >= 0 && rect.Y >= 0 && rect.X <= width && rect.Y <= height &&
		rect.Width > 0 && rect.Height > 0 && rect.Width <= width-rect.X && rect.Height <= height-rect.Y
}
