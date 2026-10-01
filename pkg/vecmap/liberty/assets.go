// Package liberty supplies the pinned OpenFreeMap Liberty assets without Qt.
// Returned layers and sprite pixels are shared immutable data.
package liberty

import (
	_ "embed"
	"fmt"
	"slices"
	"sync"

	"github.com/rubiojr/whereami/pkg/vecmap/sprite"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
)

//go:generate go run ../cmd/libertystylegen

//go:embed liberty_style.json
var styleJSON []byte

//go:embed liberty_sprite.json
var spriteJSON []byte

//go:embed liberty_sprite.png
var spritePNG []byte

var pinned = assets{styleData: styleJSON, indexData: spriteJSON, pngData: spritePNG}

type assets struct {
	styleData, indexData, pngData []byte
	styleOnce                     sync.Once
	layers                        []style.CompiledLayer
	styleErr                      error
	spriteOnce                    sync.Once
	atlas                         *sprite.Atlas
	spriteErr                     error
	cache                         imageCache
}

// Layers prepares the embedded style once. The slice and all reachable maps and
// expressions are immutable borrows, shared by all callers. Errors are cached.
func Layers() ([]style.CompiledLayer, error) { return pinned.compiledLayers() }

func (a *assets) compiledLayers() ([]style.CompiledLayer, error) {
	a.styleOnce.Do(func() {
		a.layers, a.styleErr = style.Parse(a.styleData)
		if a.styleErr != nil {
			a.styleErr = fmt.Errorf("prepare embedded Liberty style: %w", a.styleErr)
		}
	})
	return a.layers, a.styleErr
}

func (a *assets) loadSprites() error {
	a.spriteOnce.Do(func() {
		var err error
		a.atlas, err = sprite.Decode(a.indexData, a.pngData)
		if err != nil {
			a.spriteErr = fmt.Errorf("load Liberty sprites: %w", err)
		}
	})
	return a.spriteErr
}

// Files returns copies of the pinned style, sprite index and sprite atlas, so
// another renderer can load exactly the assets vecmap compiles.
func Files() (styleData, spriteIndex, spriteAtlas []byte) {
	return slices.Clone(styleJSON), slices.Clone(spriteJSON), slices.Clone(spritePNG)
}

// SpriteEntry returns a value snapshot of pinned sprite metrics. False means
// missing or unavailable. Decoding is synchronous and performed once, without I/O.
func SpriteEntry(name string) (sprite.Entry, bool) { return pinned.entry(name) }

func (a *assets) entry(name string) (sprite.Entry, bool) {
	if a.loadSprites() != nil {
		return sprite.Entry{}, false
	}
	entry, ok := a.atlas.Entries[name]
	return entry, ok
}

// Sprite returns an immutable prepared crop, sharing cached pixels. Preparation
// is synchronous; call on a preparation worker. The 512-entry FIFO cache preserves
// four-decimal opacity keys: nearby opacities can reuse the first cached image.
// Concurrent misses may prepare twice. Eviction never invalidates borrowed pixels.
// False means missing/unavailable sprite or invalid preparation input.
func Sprite(name string, color style.Color, opacity float64) (sprite.Image, bool) {
	return pinned.image(name, color, opacity)
}

func (a *assets) image(name string, color style.Color, opacity float64) (sprite.Image, bool) {
	entry, exists := a.entry(name)
	if !exists || entry.Width <= 0 || entry.Height <= 0 || entry.PixelRatio <= 0 {
		return sprite.Image{}, false
	}
	key := fmt.Sprintf("%s/%d/%d/%d/%d/%.4f", name, color.Red, color.Green, color.Blue, color.Alpha, opacity)
	if cached, exists := a.cache.get(key); exists {
		return cached, true
	}
	prepared, ok := sprite.Prepare(a.atlas.Pixels, entry, color, opacity)
	if !ok {
		return sprite.Image{}, false
	}
	a.cache.put(key, prepared)
	return prepared, true
}
