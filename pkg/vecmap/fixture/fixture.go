// Package fixture compiles the pinned offline Madrid scene without Qt or cgo.
// It reuses the shared map engine; it is not a live tile scheduler.
package fixture

import (
	"crypto/sha256"
	"fmt"

	"github.com/rubiojr/whereami/pkg/vecmap/compiler"
	"github.com/rubiojr/whereami/pkg/vecmap/glyph"
	"github.com/rubiojr/whereami/pkg/vecmap/liberty"
	"github.com/rubiojr/whereami/pkg/vecmap/mvt"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
)

const TileSHA256 = "5007c887f3c99a2c0737b9a3afdf3813ef3e1ce939a63aa09a8407b7c3770c79"

// Tile returns the fixed canonical source tile by value.
func Tile() view.TileID { return view.TileID{X: 250, Y: 193, Z: 9} }

type Options struct {
	// DirectIndexed retains topology during construction, without IndexMesh.
	DirectIndexed bool
}

// Result owns its immutable scene metadata/geometry and missing-font list. Sprite
// pixels borrow the shared Liberty cache. Intermediate source features, candidates
// and layouts are not retained. Limits reports MVT resource degradation, if any.
type Result struct {
	Scene        *scene.Scene
	Camera       view.Camera
	Labels       int
	MissingFonts []string
	Limits       []mvt.LayerLimits
}

// GlyphLoader supplies range 0-255 PBFs keyed by exact font-stack identity. It runs
// once after successful tile/style preparation, with sorted unique font requests.
// Loading, aggregate font bytes/count and I/O scheduling belong to the caller.
type GlyphLoader func(fontStacks []string) (map[string][]byte, error)

// Compile uses caller-provided glyph ranges. Missing fonts are reported without
// substitution or fetching. Inputs must remain immutable during this call.
func Compile(data []byte, ranges map[string][]byte, options Options) (*Result, error) {
	return compile(data, ranges, nil, options)
}

// CompileWithGlyphLoader invokes load synchronously on the caller's goroutine;
// use a preparation worker, not a render callback. Nil means no glyph ranges.
// Every error returns nil output, including loader, layout and packing errors.
func CompileWithGlyphLoader(data []byte, load GlyphLoader, options Options) (*Result, error) {
	return compile(data, nil, load, options)
}

func compile(data []byte, ranges map[string][]byte, load GlyphLoader, options Options) (*Result, error) {
	if len(data) > mvt.MaxTileBytes {
		return nil, mvt.ErrTileResourceLimit
	}
	if actual := fmt.Sprintf("%x", sha256.Sum256(data)); actual != TileSHA256 {
		return nil, fmt.Errorf("tile checksum mismatch: got %s", actual)
	}
	source, err := mvt.DecodeTile(data, options.DirectIndexed)
	if err != nil {
		return nil, err
	}
	layers, err := liberty.Layers()
	if err != nil {
		return nil, err
	}
	prepared, err := prepareTile(source.Layers, layers, options.DirectIndexed)
	if err != nil {
		return nil, err
	}
	if load != nil {
		ranges, err = load(compiler.FontStacks(textRequests(prepared.symbols)))
		if err != nil {
			return nil, fmt.Errorf("load fixture glyphs: %w", err)
		}
	}
	result, err := prepared.finish(ranges, options.DirectIndexed)
	if err != nil {
		return nil, err
	}
	result.Limits = source.Limits
	return result, nil
}

func (p *preparedTile) finish(ranges map[string][]byte, indexed bool) (*Result, error) {
	fonts, err := compiler.DecodeFontRanges(ranges, 0)
	if err != nil {
		return nil, err
	}
	layouts, missing, err := compiler.PrepareTextLayouts(textRequests(p.symbols), fonts)
	if err != nil {
		return nil, err
	}
	atlas, _, err := glyph.BuildRetainedAtlas(glyph.LayoutGlyphs(layouts), nil)
	if err != nil {
		return nil, err
	}
	renderable, err := glyph.PrepareLayouts(layouts, atlas, indexed)
	if err != nil {
		return nil, err
	}
	camera := view.NewCamera(view.TileCoordinate(Tile(), view.ScreenPoint{X: 128, Y: 128}), 10, 0, 512, 512)
	// Collision uses metric layouts, including those omitted by atlas coverage.
	accepted, err := selectSymbols(camera, p.symbols, layouts)
	if err != nil {
		return nil, err
	}
	packing := compiler.NewSceneBuilder(indexed, 0)
	var atlasID uint64
	if len(renderable) > 0 {
		atlasID = packing.GlyphAtlas(atlas)
	}
	labels := p.pack(packing, renderable, accepted, atlasID)
	output, err := packing.Finish()
	if err != nil {
		return nil, err
	}
	return &Result{Scene: output, Camera: camera, Labels: labels, MissingFonts: missing}, nil
}

// Frame changes camera transforms only. Style visibility/widths and collision
// remain frozen at zoom 10 in the original 512x512 placement viewport.
func (r *Result) Frame(camera view.Camera) scene.Frame {
	t := view.TileTransform(camera, Tile(), 0)
	return scene.Frame{Scene: r.Scene, DevicePixelRatio: 1, Transforms: []scene.Affine{{M11: float32(t.M11), M12: float32(t.M12), DX: float32(t.DX), M21: float32(t.M21), M22: float32(t.M22), DY: float32(t.DY)}}}
}
