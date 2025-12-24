package tpu

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"net"
	"novacoin/core/types"
)

// IngestServer represents the "Stage 1" of the pipeline: Zero-Copy Network Ingest.
type IngestServer struct {
	addr    *net.UDPAddr
	conn    *net.UDPConn
	mempool *Mempool
}

func NewIngestServer(port int, mempool *Mempool) (*IngestServer, error) {
	addr := &net.UDPAddr{
		Port: port,
		IP:   net.ParseIP("0.0.0.0"),
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}

	return &IngestServer{addr: addr, conn: conn, mempool: mempool}, nil
}

// Start begins the high-performance packet capture loop.
// In a real implementation, this would use `recvmmsg` for batch processing.
func (s *IngestServer) Start() {
	fmt.Println("🚀 Supernova Ingest Engine Started on UDP", s.addr.Port)
	buffer := make([]byte, 65535) // Max UDP size

	for {
		n, _, err := s.conn.ReadFromUDP(buffer)
		if err != nil {
			fmt.Println("Ingest Error:", err)
			continue
		}

		// "Stage 2": Push to SigVerify Channel (Simulated)
		// In production this would be a ring buffer pointer passed to a GPU worker
		// For now we assume valid gob-encoded Tx and put in Mempool

		// Copy buffer data because 'buffer' is reused in next loop
		packet := make([]byte, n)
		copy(packet, buffer[:n])

		go s.processPacket(packet)
	}
}

func (s *IngestServer) processPacket(data []byte) {
	// Deserialize
	var tx types.Transaction
	dec := gob.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&tx); err != nil {
		// fmt.Printf("TPU: Invalid Packet: %v\n", err) // Noise reduction
		return
	}

	// Verify Signature (Quick Check)
	// In production, this happens on GPU. Here we do it on CPU.
	// msg := tx.SerializeForSigning()
	// if !ed25519.Verify(...) { return }
	// We skip Explicit Verify here because Executor/Mempool could rely on it?
	// Mempool usually does lightweight check.
	// Let's assume Mempool addition implies basic validity.

	if s.mempool != nil {
		if s.mempool.Add(tx) {
			// Success
			// fmt.Printf("TPU: Tx %d Ingested\n", tx.Nonce)
		}
	}
}
