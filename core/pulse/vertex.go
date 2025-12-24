package pulse

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
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
	Timestamp  int64
	Parents    []Hash   // References to previous tips of the DAG
	Author     [32]byte // Ed25519 Public Key of the validator
	TxPayload  []byte   // In a real impl, this would be a Merkle Root or list of Tx Hashes
	MerkleRoot [32]byte // Cryptographic proof of TxPayload integrity

	// Consensus Data
	Round     uint64 // The logical round this vertex typically belongs to
	Signature []byte // Signature of the Author
	Hash      Hash   // Self hash
}

func NewVertex(parents []Hash, author ed25519.PublicKey, privKey ed25519.PrivateKey, payload []byte, merkleRoot [32]byte) *Vertex {
	v := &Vertex{
		Timestamp:  time.Now().UnixNano(), // Genesis/Miner creates time, but Executor must use it
		Parents:    parents,
		Author:     [32]byte(author),
		TxPayload:  payload,
		MerkleRoot: merkleRoot,
		Round:      0,
	}
	// Sort parents for deterministic hashing? For now, we assume miner provided order is canonical.
	v.Hash = v.ComputeHash()
	v.Signature = ed25519.Sign(privKey, v.Hash[:])
	return v
}

func (v *Vertex) ComputeHash() Hash {
	// Secure Serialization using Binary Write (Avoids string conversion issues)
	h := sha256.New()
	h.Write(v.Author[:])
	h.Write(v.TxPayload)
	h.Write(v.MerkleRoot[:])
	binary.Write(h, binary.BigEndian, v.Timestamp)
	for _, p := range v.Parents {
		h.Write(p[:])
	}
	var res Hash
	copy(res[:], h.Sum(nil))
	return res
}
