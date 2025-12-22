package pulse

import (
	"sync"
)

type VertexStore struct {
	Vertices map[Hash]*Vertex
	Tips     map[Hash]bool // Set of hashes that are currently tips (no parents referencing them yet)
	mu       sync.RWMutex
}

func NewVertexStore() *VertexStore {
	return &VertexStore{
		Vertices: make(map[Hash]*Vertex),
		Tips:     make(map[Hash]bool),
	}
}

// AddVertex inserts a new vertex into the DAG and updates the tips.
// This is the core "Mesh" building logic.
func (store *VertexStore) AddVertex(v *Vertex) {
	store.mu.Lock()
	defer store.mu.Unlock()

	store.Vertices[v.Hash] = v
	store.Tips[v.Hash] = true

	// Remove parents from tips since they are now referenced
	for _, parentHash := range v.Parents {
		delete(store.Tips, parentHash)
	}
}

// GetTips returns the current "Wavefront" of the DAG.
// New vertices should reference these as parents.
func (store *VertexStore) GetTips() []Hash {
	store.mu.RLock()
	defer store.mu.RUnlock()

	var tips []Hash
	for hash := range store.Tips {
		tips = append(tips, hash)
	}
	return tips
}
