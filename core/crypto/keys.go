package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
)

type KeyPair struct {
	PublicKey  ed25519.PublicKey
	PrivateKey ed25519.PrivateKey
}

func GenerateKeyPair() (*KeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &KeyPair{PublicKey: pub, PrivateKey: priv}, nil
}

func (kp *KeyPair) Sign(msg []byte) []byte {
	return ed25519.Sign(kp.PrivateKey, msg)
}

func Verify(pubKey []byte, msg []byte, sig []byte) bool {
	if len(pubKey) != ed25519.PublicKeySize {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(pubKey), msg, sig)
}

func (kp *KeyPair) Address() string {
	return hex.EncodeToString(kp.PublicKey)
}

// Quantum interface (Future proofing placeholder)
type QuantumSigner interface {
	SignQuantum(msg []byte) ([]byte, error)
}
