package scene

import (
	"fmt"

	"github.com/rubiojr/whereami/pkg/vecmap/view"
)

// TileSpace identifies the tile and world copy used by a transform slot.
type TileSpace struct {
	Tile view.TileID
	Wrap int
}

// Validate checks a captured scene and its transform metadata before rendering.
func (d *Document) Validate() error {
	if err := d.Scene.Validate(); err != nil {
		return err
	}
	if d.Width <= 0 || d.Height <= 0 || d.Width > 8192 || d.Height > 8192 {
		return fmt.Errorf("invalid viewport")
	}
	count := len(d.Transforms)
	if len(d.TileSpaces) > 0 {
		if d.Camera == nil {
			return fmt.Errorf("tile spaces require a camera")
		}
		count = len(d.TileSpaces)
		for _, space := range d.TileSpaces {
			if !space.Tile.Valid() {
				return fmt.Errorf("invalid tile space")
			}
		}
	}
	for _, transform := range d.Transforms {
		for _, v := range [...]float32{transform.M11, transform.M12, transform.DX, transform.M21, transform.M22, transform.DY} {
			if !finite(v) {
				return fmt.Errorf("non-finite transform")
			}
		}
	}
	for _, draw := range d.Scene.Draws {
		if draw.Transform >= count {
			return fmt.Errorf("missing transform")
		}
	}
	return nil
}

// Document is a portable, offline rendering fixture. It can be consumed by any
// backend without importing the Qt-bound vecmap package that produced it.
type Document struct {
	Scene         Scene
	Transforms    []Affine
	Width, Height int
	Labels        int
	MissingFonts  []string
	Source        string
	Camera        *view.Camera `json:",omitempty"`
	TileSpaces    []TileSpace  `json:",omitempty"`
}

// FrameAt reprojects tile spaces without changing any retained scene resources.
// Documents without geographic metadata retain their original affine transforms.
func (d *Document) FrameAt(camera view.Camera) Frame {
	if len(d.TileSpaces) == 0 {
		return Frame{Scene: &d.Scene, Transforms: d.Transforms}
	}
	camera = camera.Normalized()
	transforms := make([]Affine, len(d.TileSpaces))
	for i, space := range d.TileSpaces {
		t := view.TileTransform(camera, space.Tile, space.Wrap)
		transforms[i] = Affine{M11: float32(t.M11), M12: float32(t.M12), DX: float32(t.DX), M21: float32(t.M21), M22: float32(t.M22), DY: float32(t.DY)}
	}
	return Frame{Scene: &d.Scene, Transforms: transforms}
}
