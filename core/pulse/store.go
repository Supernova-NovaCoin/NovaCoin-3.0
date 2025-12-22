package pulse

import (
	"bytes"
	"encoding/gob"
	"log"
	"novacoin/core/store"
	"sync"

	"github.com/dgraph-io/badger/v4"
)

type VertexStore struct {
	// Cache for recent vertices (Hot storage)
	Vertices map[Hash]*Vertex
	Tips     map[Hash]bool
	mu       sync.RWMutex
}

func NewVertexStore() *VertexStore {
	vs := &VertexStore{
		Vertices: make(map[Hash]*Vertex),
		Tips:     make(map[Hash]bool),
	}
	vs.LoadTips()
	return vs
}

// AddVertex inserts a new vertex into the DAG and persists it.
func (vs *VertexStore) AddVertex(v *Vertex) {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	// 1. Update In-Memory Cache
	vs.Vertices[v.Hash] = v
	vs.Tips[v.Hash] = true
	for _, parentHash := range v.Parents {
		delete(vs.Tips, parentHash)
	}

	// 2. Persist to Disk
	if store.DB != nil {
		err := store_DB_Update(func(txn *badger.Txn) error {
			// Encode Vertex
			var buf bytes.Buffer
			if err := gob.NewEncoder(&buf).Encode(v); err != nil {
				return err
			}
			// Save v:{hash}
			key := append([]byte("v:"), v.Hash[:]...)
			if err := txn.Set(key, buf.Bytes()); err != nil {
				return err
			}

			// Save Tips
			return vs.saveTipsTxn(txn)
		})
		if err != nil {
			log.Printf("Failed to persist vertex: %v", err)
		}
	}
}

func (vs *VertexStore) saveTipsTxn(txn *badger.Txn) error {
	var tips []Hash
	for h := range vs.Tips {
		tips = append(tips, h)
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(tips); err != nil {
		return err
	}
	return txn.Set([]byte("meta:tips"), buf.Bytes())
}

func (vs *VertexStore) LoadTips() {
	if store.DB == nil {
		return
	}

	err := store.DB.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("meta:tips"))
		if err != nil {
			return err
		}

		return item.Value(func(val []byte) error {
			var tips []Hash
			if err := gob.NewDecoder(bytes.NewReader(val)).Decode(&tips); err != nil {
				return err
			}
			vs.mu.Lock()
			for _, t := range tips {
				vs.Tips[t] = true
			}
			vs.mu.Unlock()
			return nil
		})
	})
	if err != nil && err != badger.ErrKeyNotFound {
		log.Printf("Failed to load tips: %v", err)
	}
}

// Helper to access the global DB from core/store
func store_DB_Update(fn func(txn *badger.Txn) error) error {
	if store.DB == nil {
		return nil
	}
	return store.DB.Update(fn)
}

// GetTips returns the current "Wavefront" of the DAG.
func (vs *VertexStore) GetTips() []Hash {
	vs.mu.RLock()
	defer vs.mu.RUnlock()

	var tips []Hash
	for hash := range vs.Tips {
		tips = append(tips, hash)
	}
	return tips
}

// GetAllVertices returns all vertices in the store.
// WARNING: In production, this must be batched/paginated.
func (vs *VertexStore) GetAllVertices() []*Vertex {
	vs.mu.RLock()
	defer vs.mu.RUnlock()

	var all []*Vertex
	for _, v := range vs.Vertices {
		all = append(all, v)
	}
	return all
}
