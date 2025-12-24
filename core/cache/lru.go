package cache

import (
	"container/list"
	"sync"
)

// LRU is a thread-safe LRU cache with configurable max size
type LRU[K comparable, V any] struct {
	maxSize int
	items   map[K]*list.Element
	order   *list.List
	mu      sync.RWMutex
}

type entry[K comparable, V any] struct {
	key   K
	value V
}

// NewLRU creates a new LRU cache with the given max size
func NewLRU[K comparable, V any](maxSize int) *LRU[K, V] {
	return &LRU[K, V]{
		maxSize: maxSize,
		items:   make(map[K]*list.Element),
		order:   list.New(),
	}
}

// Get retrieves a value from the cache
func (c *LRU[K, V]) Get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.order.MoveToFront(elem)
		return elem.Value.(*entry[K, V]).value, true
	}
	var zero V
	return zero, false
}

// Set adds or updates a value in the cache
func (c *LRU[K, V]) Set(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.order.MoveToFront(elem)
		elem.Value.(*entry[K, V]).value = value
		return
	}

	// Add new entry
	e := &entry[K, V]{key: key, value: value}
	elem := c.order.PushFront(e)
	c.items[key] = elem

	// Evict if over capacity
	for c.order.Len() > c.maxSize {
		oldest := c.order.Back()
		if oldest != nil {
			c.order.Remove(oldest)
			delete(c.items, oldest.Value.(*entry[K, V]).key)
		}
	}
}

// Delete removes a value from the cache
func (c *LRU[K, V]) Delete(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.order.Remove(elem)
		delete(c.items, key)
	}
}

// Len returns the number of items in the cache
func (c *LRU[K, V]) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.order.Len()
}

// Keys returns all keys in the cache (most recent first)
func (c *LRU[K, V]) Keys() []K {
	c.mu.RLock()
	defer c.mu.RUnlock()

	keys := make([]K, 0, c.order.Len())
	for elem := c.order.Front(); elem != nil; elem = elem.Next() {
		keys = append(keys, elem.Value.(*entry[K, V]).key)
	}
	return keys
}

// BoundedSlice is a thread-safe slice with a maximum size (FIFO eviction)
type BoundedSlice[T any] struct {
	items   []T
	maxSize int
	mu      sync.RWMutex
}

// NewBoundedSlice creates a bounded slice
func NewBoundedSlice[T any](maxSize int) *BoundedSlice[T] {
	return &BoundedSlice[T]{
		items:   make([]T, 0, maxSize),
		maxSize: maxSize,
	}
}

// Append adds an item, evicting oldest if at capacity
func (s *BoundedSlice[T]) Append(item T) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.items) >= s.maxSize {
		// Shift left and add at end
		copy(s.items, s.items[1:])
		s.items[len(s.items)-1] = item
	} else {
		s.items = append(s.items, item)
	}
}

// GetAll returns a copy of all items
func (s *BoundedSlice[T]) GetAll() []T {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]T, len(s.items))
	copy(result, s.items)
	return result
}

// GetLast returns the last N items (most recent)
func (s *BoundedSlice[T]) GetLast(n int) []T {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if n > len(s.items) {
		n = len(s.items)
	}
	start := len(s.items) - n
	result := make([]T, n)
	copy(result, s.items[start:])
	return result
}

// Len returns the current number of items
func (s *BoundedSlice[T]) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.items)
}

// BoundedMap is a thread-safe map where each key has a bounded slice of values
type BoundedMap[K comparable, V any] struct {
	data       map[K]*BoundedSlice[V]
	perKeyMax  int
	totalKeys  int
	maxKeys    int
	keyOrder   []K // Track insertion order for key eviction
	mu         sync.RWMutex
}

// NewBoundedMap creates a bounded map with limits per key and total keys
func NewBoundedMap[K comparable, V any](perKeyMax, maxKeys int) *BoundedMap[K, V] {
	return &BoundedMap[K, V]{
		data:      make(map[K]*BoundedSlice[V]),
		perKeyMax: perKeyMax,
		maxKeys:   maxKeys,
		keyOrder:  make([]K, 0, maxKeys),
	}
}

// Append adds a value for a key
func (m *BoundedMap[K, V]) Append(key K, value V) {
	m.mu.Lock()
	defer m.mu.Unlock()

	slice, exists := m.data[key]
	if !exists {
		// Evict oldest key if at capacity
		if len(m.keyOrder) >= m.maxKeys {
			oldestKey := m.keyOrder[0]
			delete(m.data, oldestKey)
			m.keyOrder = m.keyOrder[1:]
		}

		slice = NewBoundedSlice[V](m.perKeyMax)
		m.data[key] = slice
		m.keyOrder = append(m.keyOrder, key)
	}

	slice.Append(value)
}

// Get returns all values for a key
func (m *BoundedMap[K, V]) Get(key K) []V {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if slice, ok := m.data[key]; ok {
		return slice.GetAll()
	}
	return nil
}

// Len returns the number of keys
func (m *BoundedMap[K, V]) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.data)
}
