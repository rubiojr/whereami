package liberty

import (
	"crypto/sha256"
	"fmt"
	"math"
	"strconv"
	"sync"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/sprite"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPinnedAssets(t *testing.T) {
	assert.Equal(t, "6010998863b4876911ac9a2d62c9a28d97c8877f6d20cd158b74808572257b60", fmt.Sprintf("%x", sha256.Sum256(styleJSON)))
	assert.Equal(t, "73e75e58d8c7bb62cc25d9d150660500552f4d04f6eac5efa4e236076773c356", fmt.Sprintf("%x", sha256.Sum256(spriteJSON)))
	assert.Equal(t, "8996a519d218dc5f98015267709dae272a77bb74ef0ecc5a0992dcf276c1be4c", fmt.Sprintf("%x", sha256.Sum256(spritePNG)))
	layers, err := Layers()
	require.NoError(t, err)
	require.Len(t, layers, 111)
	assert.Equal(t, "background", layers[0].ID)
	assert.Equal(t, "label_country_1", layers[110].ID)
	again, err := Layers()
	require.NoError(t, err)
	assert.Same(t, &layers[0], &again[0])

	entry, ok := SpriteEntry("airport")
	require.True(t, ok)
	assert.Len(t, pinned.atlas.Entries, 264)
	color := style.Color{Red: 1, Green: 2, Blue: 3, Alpha: 255}
	image, ok := Sprite("airport", color, 1)
	require.True(t, ok)
	want, ok := sprite.Prepare(pinned.atlas.Pixels, entry, color, 1)
	require.True(t, ok)
	assert.Equal(t, want, image)
	cached, ok := Sprite("airport", color, 1)
	require.True(t, ok)
	assert.Same(t, &image.Pixels[0], &cached.Pixels[0])
	_, ok = SpriteEntry("not-a-sprite")
	assert.False(t, ok)
	_, ok = Sprite("not-a-sprite", color, 1)
	assert.False(t, ok)
}

func TestAssetFailuresAreCached(t *testing.T) {
	a := assets{styleData: []byte("{"), indexData: []byte("{")}
	layers, err := a.compiledLayers()
	assert.Nil(t, layers)
	require.ErrorContains(t, err, "prepare embedded Liberty style")
	a.styleData = styleJSON
	_, again := a.compiledLayers()
	assert.Same(t, err, again)
	require.ErrorContains(t, a.loadSprites(), "load Liberty sprites")
	a.indexData, a.pngData = spriteJSON, spritePNG
	_, ok := a.entry("airport")
	assert.False(t, ok)
	_, ok = a.image("airport", style.Color{}, 1)
	assert.False(t, ok)
}

func TestOpacityCacheIdentity(t *testing.T) {
	a := assets{indexData: spriteJSON, pngData: spritePNG}
	color := style.Color{Alpha: 255}
	first, ok := a.image("airport", color, 0.50001)
	require.True(t, ok)
	nearby, ok := a.image("airport", color, 0.50002)
	require.True(t, ok)
	assert.Same(t, &first.Pixels[0], &nearby.Pixels[0])
	distinct, ok := a.image("airport", color, 0.6)
	require.True(t, ok)
	assert.NotSame(t, &first.Pixels[0], &distinct.Pixels[0])
	_, ok = a.image("airport", color, math.NaN())
	assert.False(t, ok)
	assert.Len(t, a.cache.entries, 2)
}

func TestCacheFIFOAndBorrowLifetime(t *testing.T) {
	var cache imageCache
	_, ok := cache.get("0")
	assert.False(t, ok)
	for i := range maxCacheEntries {
		cache.put(strconv.Itoa(i), sprite.Image{Pixels: []byte{byte(i)}})
	}
	replacement := sprite.Image{Pixels: []byte{42}}
	cache.put("0", replacement)
	borrow, ok := cache.get("0")
	require.True(t, ok)
	assert.Same(t, &replacement.Pixels[0], &borrow.Pixels[0])
	cache.put("next", sprite.Image{})
	_, ok = cache.get("0")
	assert.False(t, ok, "reads and replacements must not promote FIFO entries")
	assert.Equal(t, []byte{42}, borrow.Pixels)
	assert.Len(t, cache.entries, maxCacheEntries)
	assert.Len(t, cache.order, maxCacheEntries)
	assert.Equal(t, "1", cache.order[0])
}

func TestConcurrentColdAssets(t *testing.T) {
	a := assets{styleData: styleJSON, indexData: spriteJSON, pngData: spritePNG}
	var workers sync.WaitGroup
	for i := range 16 {
		workers.Go(func() {
			layers, err := a.compiledLayers()
			assert.NoError(t, err)
			assert.Len(t, layers, 111)
			for j := range 40 {
				image, ok := a.image("airport", style.Color{Red: i*40 + j, Alpha: 255}, 1)
				assert.True(t, ok)
				assert.NotEmpty(t, image.Pixels)
			}
		})
	}
	workers.Wait()
	assert.Len(t, a.cache.entries, maxCacheEntries)
	assert.Len(t, a.cache.order, maxCacheEntries)
}
