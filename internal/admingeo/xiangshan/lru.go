package xiangshan

import (
	"container/list"
	"sync"
)

type slabCache struct {
	mu       sync.Mutex
	maxBytes int64
	bytes    int64
	items    map[uint32]*list.Element
	lru      list.List
}

type cacheEntry struct {
	key  uint32
	data []byte
}

func newSlabCache(maxBytes int64) *slabCache {
	if maxBytes == 0 {
		return nil
	}
	return &slabCache{maxBytes: maxBytes, items: make(map[uint32]*list.Element)}
}

func (c *slabCache) get(key uint32) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.lru.MoveToFront(element)
	return element.Value.(*cacheEntry).data, true
}

func (c *slabCache) put(key uint32, data []byte) {
	size := int64(len(data))
	if size > c.maxBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if element, ok := c.items[key]; ok {
		entry := element.Value.(*cacheEntry)
		c.bytes -= int64(len(entry.data))
		entry.data = data
		c.bytes += size
		c.lru.MoveToFront(element)
	} else {
		element := c.lru.PushFront(&cacheEntry{key: key, data: data})
		c.items[key] = element
		c.bytes += size
	}
	for c.bytes > c.maxBytes {
		oldest := c.lru.Back()
		if oldest == nil {
			break
		}
		entry := oldest.Value.(*cacheEntry)
		c.bytes -= int64(len(entry.data))
		delete(c.items, entry.key)
		c.lru.Remove(oldest)
	}
}
