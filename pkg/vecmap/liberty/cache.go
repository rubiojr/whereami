package liberty

import (
	"sync"

	"github.com/rubiojr/whereami/pkg/vecmap/sprite"
)

const maxCacheEntries = 512

type imageCache struct {
	mutex   sync.Mutex
	entries map[string]sprite.Image
	order   []string
}

func (c *imageCache) get(key string) (sprite.Image, bool) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	value, exists := c.entries[key]
	return value, exists
}

func (c *imageCache) put(key string, value sprite.Image) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]sprite.Image, maxCacheEntries)
	}
	if _, exists := c.entries[key]; exists {
		c.entries[key] = value
		return
	}
	if len(c.order) >= maxCacheEntries {
		delete(c.entries, c.order[0])
		copy(c.order, c.order[1:])
		c.order = c.order[:len(c.order)-1]
	}
	c.entries[key] = value
	c.order = append(c.order, key)
}
