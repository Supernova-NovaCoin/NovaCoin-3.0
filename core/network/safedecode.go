package network

import (
	"bytes"
	"encoding/gob"
	"errors"
	"io"
)

// Network safety constants
const (
	// MaxMessageSize is the maximum size of any network message (10 MB)
	MaxMessageSize = 10 * 1024 * 1024

	// MaxTxSize is the maximum size of a single transaction (64 KB)
	MaxTxSize = 64 * 1024

	// MaxBlockSize is the maximum size of a block/vertex (5 MB)
	MaxBlockSize = 5 * 1024 * 1024

	// MaxPeersResponse is the maximum peers in an addr response
	MaxPeersResponse = 100

	// MaxDAGSyncVertices is the maximum vertices in a DAG sync
	MaxDAGSyncVertices = 1000
)

// Common errors
var (
	ErrMessageTooLarge = errors.New("message exceeds maximum size limit")
	ErrDecodeFailed    = errors.New("failed to decode message")
)

// LimitedReader wraps a reader with a size limit
type LimitedReader struct {
	R io.Reader
	N int64 // max bytes remaining
}

func (l *LimitedReader) Read(p []byte) (n int, err error) {
	if l.N <= 0 {
		return 0, ErrMessageTooLarge
	}
	if int64(len(p)) > l.N {
		p = p[0:l.N]
	}
	n, err = l.R.Read(p)
	l.N -= int64(n)
	return
}

// SafeDecode decodes a gob message with size limit protection
func SafeDecode(data []byte, maxSize int, v interface{}) error {
	if len(data) > maxSize {
		return ErrMessageTooLarge
	}

	reader := &LimitedReader{
		R: bytes.NewReader(data),
		N: int64(maxSize),
	}

	dec := gob.NewDecoder(reader)
	if err := dec.Decode(v); err != nil {
		return ErrDecodeFailed
	}

	return nil
}

// SafeDecodeTransaction decodes a transaction with size limits
func SafeDecodeTransaction(data []byte, v interface{}) error {
	return SafeDecode(data, MaxTxSize, v)
}

// SafeDecodeBlock decodes a block/vertex with size limits
func SafeDecodeBlock(data []byte, v interface{}) error {
	return SafeDecode(data, MaxBlockSize, v)
}

// SafeDecodeMessage decodes a general network message with size limits
func SafeDecodeMessage(data []byte, v interface{}) error {
	return SafeDecode(data, MaxMessageSize, v)
}

// ValidatePayloadSize checks if payload is within acceptable limits
func ValidatePayloadSize(data []byte, maxSize int) error {
	if len(data) > maxSize {
		return ErrMessageTooLarge
	}
	return nil
}
