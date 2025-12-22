package p2p

import (
	"encoding/gob"
	"net"
	"sync"
	"time"
)

// Peer represents a connected node in the network.
type Peer struct {
	Conn     net.Conn
	Outbound bool      // True if we initiated the connection
	Stats    PeerStats // Verification: Metrics
	Encoder  *gob.Encoder
	Decoder  *gob.Decoder
	wg       sync.WaitGroup
	quit     chan struct{}
}

// PeerStats tracks metrics for a peer.
type PeerStats struct {
	BytesReceived uint64
	BytesSent     uint64
	LastSeen      time.Time
}

// NewPeer creates a new peer from a connection.
func NewPeer(conn net.Conn, outbound bool) *Peer {
	return &Peer{
		Conn:     conn,
		Outbound: outbound,
		Encoder:  gob.NewEncoder(conn),
		Decoder:  gob.NewDecoder(conn),
		quit:     make(chan struct{}),
		Stats: PeerStats{
			LastSeen: time.Now(),
		},
	}
}

// Send transmits a message to the peer.
func (p *Peer) Send(msg Message) error {
	p.Stats.BytesSent += uint64(len(msg.Payload)) // Approx
	return p.Encoder.Encode(msg)
}

// Read waits for the next message from the peer.
func (p *Peer) Read() (Message, error) {
	var msg Message
	err := p.Decoder.Decode(&msg)
	if err == nil {
		p.Stats.BytesReceived += uint64(len(msg.Payload)) // Approx
		p.Stats.LastSeen = time.Now()
	}
	return msg, err
}

// Close disconnects the peer.
func (p *Peer) Close() {
	close(p.quit)
	p.Conn.Close()
	p.wg.Wait()
}
