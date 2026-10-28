package render

import (
	"container/list"
	"sync"
)

// A few pond sizes can retain all 20 varieties × 16 headings × 8 tail
// frames. Byte limits also bound large terminal-cell sizes. Oversize entries
// are rendered normally, but are not retained.
const koiSpriteEntries = 3 * 20 * 16 * 8
const koiSpriteBytes = 128 << 20
const koiRingEntries = 256 // Eight complete 32-frame clicks.
const koiRingBytes = 16 << 20
const koiTerminalEntries = koiSpriteEntries + koiRingEntries
const koiTerminalBytes = koiSpriteBytes + koiRingBytes

type koiCacheItem[K comparable, V any] struct {
	key   K
	value V
	size  int
}
type koiCache[K comparable, V any] struct {
	mu                   sync.Mutex
	items                map[K]*list.Element
	order                list.List
	bytes                int
	maxEntries, maxBytes int
}

func (c *koiCache[K, V]) getOrCreate(key K, create func() (V, int)) V {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el := c.items[key]; el != nil {
		c.order.MoveToFront(el)
		return el.Value.(koiCacheItem[K, V]).value
	}
	value, size := create()
	if size > c.maxBytes {
		return value
	}
	if c.items == nil {
		c.items = make(map[K]*list.Element)
	}
	for len(c.items) >= c.maxEntries || c.bytes+size > c.maxBytes {
		el := c.order.Back()
		item := el.Value.(koiCacheItem[K, V])
		delete(c.items, item.key)
		c.bytes -= item.size
		c.order.Remove(el)
	}
	c.items[key] = c.order.PushFront(koiCacheItem[K, V]{key, value, size})
	c.bytes += size
	return value
}

// Range is also used by retention tests; readers never observe partial inserts.
func (c *koiCache[K, V]) Range(fn func(any, any) bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, el := range c.items {
		if !fn(key, el.Value.(koiCacheItem[K, V]).value) {
			return
		}
	}
}
