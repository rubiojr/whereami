package vecmap

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"sync"
)

//go:embed liberty_sprite.json
var libertySpriteJSON []byte

//go:embed liberty_sprite.png
var libertySpritePNG []byte

const (
	libertySpriteJSONSHA256 = "73e75e58d8c7bb62cc25d9d150660500552f4d04f6eac5efa4e236076773c356"
	libertySpritePNGSHA256  = "8996a519d218dc5f98015267709dae272a77bb74ef0ecc5a0992dcf276c1be4c"
)

type libertySpriteEntry struct {
	X          int     `json:"x"`
	Y          int     `json:"y"`
	Width      int     `json:"width"`
	Height     int     `json:"height"`
	PixelRatio float64 `json:"pixelRatio"`
	SDF        bool    `json:"sdf"`
}

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
		if err := json.Unmarshal(libertySpriteJSON, &libertySpriteIndex); err != nil {
			libertySpritesError = fmt.Errorf("decode Liberty sprite index: %w", err)
			return
		}
		decoded, err := png.Decode(bytes.NewReader(libertySpritePNG))
		if err != nil {
			libertySpritesError = fmt.Errorf("decode Liberty sprite atlas: %w", err)
			return
		}
		bounds := decoded.Bounds()
		libertySpriteAtlas = image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
		draw.Draw(libertySpriteAtlas, libertySpriteAtlas.Bounds(), decoded, bounds.Min, draw.Src)
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
	key := fmt.Sprintf("%s/%d/%d/%d/%d/%.4f", name, color.red, color.green, color.blue, color.alpha, opacity)
	if cached, exists := libertySpriteCache.get(key); exists {
		return cached, true
	}
	atlasWidth := int64(libertySpriteAtlas.Bounds().Dx())
	atlasHeight := int64(libertySpriteAtlas.Bounds().Dy())
	if entry.X < 0 || entry.Y < 0 || int64(entry.X)+int64(entry.Width) > atlasWidth ||
		int64(entry.Y)+int64(entry.Height) > atlasHeight {
		return libertySpriteImage{}, false
	}
	pixels := make([]byte, entry.Width*entry.Height*4)
	alphaScale := max(0, min(1, opacity))
	for y := range entry.Height {
		for x := range entry.Width {
			sourceOffset := (entry.Y+y)*libertySpriteAtlas.Stride + (entry.X+x)*4
			targetOffset := (y*entry.Width + x) * 4
			if entry.SDF {
				pixels[targetOffset] = byte(color.red)
				pixels[targetOffset+1] = byte(color.green)
				pixels[targetOffset+2] = byte(color.blue)
				pixels[targetOffset+3] = byte(math.Round(float64(libertySpriteAtlas.Pix[sourceOffset+3]) * alphaScale * float64(color.alpha) / 255))
				continue
			}
			copy(pixels[targetOffset:targetOffset+3], libertySpriteAtlas.Pix[sourceOffset:sourceOffset+3])
			pixels[targetOffset+3] = byte(math.Round(float64(libertySpriteAtlas.Pix[sourceOffset+3]) * alphaScale))
		}
	}
	result := libertySpriteImage{
		pixels:     pixels,
		width:      entry.Width,
		height:     entry.Height,
		pixelRatio: entry.PixelRatio,
	}
	libertySpriteCache.put(key, result)
	return result, true
}
