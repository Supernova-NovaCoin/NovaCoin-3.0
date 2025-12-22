package pulse

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"
)

// Hash is a 32-byte identifier
type Hash [32]byte

func (h Hash) String() string {
	return hex.EncodeToString(h[:])
}

// Vertex replaces the traditional "Block".
// It is a node in the DAG that points to multiple previous vertices.
type Vertex struct {
	Timestamp int64
	Parents   []Hash   // References to previous tips of the DAG
	Author    [32]byte // Ed25519 Public Key of the validator
	TxPayload []byte   // In a real impl, this would be a Merkle Root or list of Tx Hashes

	// Consensus Data
	Round     uint64 // The logical round this vertex typically belongs to
	Signature []byte // Signature of the Author
	Hash      Hash   // Self hash
}

func NewVertex(parents []Hash, author ed25519.PublicKey, privKey ed25519.PrivateKey, payload []byte) *Vertex {
	v := &Vertex{
		Timestamp: time.Now().UnixNano(),
		Parents:   parents,
		Author:    [32]byte(author),
		TxPayload: payload,
		Round:     0, // To be calculated based on parents
	}
	v.Hash = v.ComputeHash()
	v.Signature = ed25519.Sign(privKey, v.Hash[:])
	return v
}

func (v *Vertex) ComputeHash() Hash {
	// Simple serialization for hashing (Prototype)
	// In production, use high-performance serialization (e.g., Cap'n Proto)
	record := string(v.Author[:]) + string(v.TxPayload) + strconv.FormatInt(v.Timestamp, 10)
	for _, p := range v.Parents {
		record += p.String()
	}
	return sha256.Sum256([]byte(record))
}
