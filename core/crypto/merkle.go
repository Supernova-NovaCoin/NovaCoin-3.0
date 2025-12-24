package crypto

import (
	"crypto/sha256"
)

// MerkleRoot computes the root hash of a list of transaction hashes.
// It uses a simple binary tree approach.
func MerkleRoot(txHashes [][]byte) []byte {
	if len(txHashes) == 0 {
		return []byte{}
	}
	if len(txHashes) == 1 {
		return txHashes[0]
	}

	// Double SHA256 is unnecessary for Ed25519-native, but we use SHA256 for standard structure.

	// 1. Ensure even number of leaves by duplicating last one if needed
	if len(txHashes)%2 != 0 {
		txHashes = append(txHashes, txHashes[len(txHashes)-1])
	}

	var nextLevel [][]byte

	for i := 0; i < len(txHashes); i += 2 {
		left := txHashes[i]
		right := txHashes[i+1]

		h := sha256.New()
		h.Write(left)
		h.Write(right)
		nextLevel = append(nextLevel, h.Sum(nil))
	}

	return MerkleRoot(nextLevel)
}
