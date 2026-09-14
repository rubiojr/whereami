package vecmap

import (
	_ "embed"
	"fmt"
	"image"
	"sync"

	"github.com/rubiojr/whereami/pkg/vecmap/sprite"
)

//go:embed liberty_sprite.json
var libertySpriteJSON []byte

//go:embed liberty_sprite.png
var libertySpritePNG []byte

const (
	libertySpriteJSONSHA256 = "73e75e58d8c7bb62cc25d9d150660500552f4d04f6eac5efa4e236076773c356"
	libertySpritePNGSHA256  = "8996a519d218dc5f98015267709dae272a77bb74ef0ecc5a0992dcf276c1be4c"
)

type libertySpriteEntry = sprite.Entry

type libertySpriteImage struct {
	pixels     []byte
	width      int
	height     int
	pixelRatio float64
}

const maxLibertySpriteCacheEntries = 512

type libertySpriteImageCache struct {
	mutex   sync.Mutex
	entries map[string]libertySpriteImage
	order   []string
}

func (c *libertySpriteImageCache) get(key string) (libertySpriteImage, bool) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	value, exists := c.entries[key]
	return value, exists
}

func (c *libertySpriteImageCache) put(key string, value libertySpriteImage) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]libertySpriteImage, maxLibertySpriteCacheEntries)
	}
	if _, exists := c.entries[key]; exists {
		c.entries[key] = value
		return
	}
	if len(c.order) >= maxLibertySpriteCacheEntries {
		delete(c.entries, c.order[0])
		copy(c.order, c.order[1:])
		c.order = c.order[:len(c.order)-1]
	}
	c.entries[key] = value
	c.order = append(c.order, key)
}

var (
	libertySpritesOnce  sync.Once
	libertySpriteAtlas  *image.NRGBA
	libertySpriteIndex  map[string]libertySpriteEntry
	libertySpritesError error
	libertySpriteCache  libertySpriteImageCache
)

func loadLibertySprites() error {
	libertySpritesOnce.Do(func() {
		atlas, err := sprite.Decode(libertySpriteJSON, libertySpritePNG)
		if err != nil {
			libertySpritesError = fmt.Errorf("load Liberty sprites: %w", err)
			return
		}
		libertySpriteAtlas = atlas.Pixels
		libertySpriteIndex = atlas.Entries
	})
	return libertySpritesError
}

func libertySprite(name string, color mapColor, opacity float64) (libertySpriteImage, bool) {
	if err := loadLibertySprites(); err != nil {
		return libertySpriteImage{}, false
	}
	entry, exists := libertySpriteIndex[name]
	if !exists || entry.Width <= 0 || entry.Height <= 0 || entry.PixelRatio <= 0 {
		return libertySpriteImage{}, false
	}
	key := fmt.Sprintf("%s/%d/%d/%d/%d/%.4f", name, color.Red, color.Green, color.Blue, color.Alpha, opacity)
	if cached, exists := libertySpriteCache.get(key); exists {
		return cached, true
	}
	prepared, ok := sprite.Prepare(libertySpriteAtlas, entry, color, opacity)
	if !ok {
		return libertySpriteImage{}, false
	}
	result := libertySpriteImage{
		pixels:     prepared.Pixels,
		width:      prepared.Width,
		height:     prepared.Height,
		pixelRatio: prepared.PixelRatio,
	}
	libertySpriteCache.put(key, result)
	return result, true
}
