package p2p

import (
	"bytes"
	"encoding/gob"
	"log"
	"net"
	"novacoin/core/execution"
	"novacoin/core/pulse"
	"novacoin/core/staking"
	"sync"
)

const (
	ProtocolVersion = 1
	GenesisHash     = "0000000000000000" // Placeholder, should match real genesis
)

// Server manages all P2P connections and protocol logic.
type Server struct {
	Transport  *Transport
	Peers      map[string]*Peer
	KnownPeers map[string]bool // Set of known peer addresses

	DAG      *pulse.VertexStore      // Link to the DAG
	State    *execution.StateManager // Link to State for staking check
	Executor *execution.Executor     // Link to Executor for rewards
	// Security / DDoS Protection
	ConnCount map[string]int // Count of connections per IP
	MaxPeers  int            // Total max peers
	MaxPerIP  int            // Max peers per IP

	lock sync.RWMutex
	Quit chan struct{}
}

// NewServer creates a new P2P server instance.
func NewServer(addr string, maxPeers int, dag *pulse.VertexStore, state *execution.StateManager, exec *execution.Executor) *Server {
	return &Server{
		Transport:  NewTransport(addr),
		Peers:      make(map[string]*Peer),
		KnownPeers: make(map[string]bool),
		ConnCount:  make(map[string]int),
		MaxPeers:   maxPeers,
		MaxPerIP:   5, // Strict per-IP Limit (Hardcoded for now)

		DAG:      dag,
		State:    state,
		Executor: exec,
		Quit:     make(chan struct{}),
	}
}

// Start initializes the transport and starts the accept loop.
func (s *Server) Start() error {
	if err := s.Transport.Listen(); err != nil {
		return err
	}
	log.Printf("P2P Server listening on %s", s.Transport.ListenAddr)

	go s.acceptLoop()

	return nil
}

// acceptLoop handles incoming connections.
func (s *Server) acceptLoop() {
	for {
		select {
		case <-s.Quit:
			return
		default:
			conn, err := s.Transport.Accept()
			if err != nil {
				log.Printf("P2P Accept error: %v", err)
				continue
			}

			// DDoS Check 1: Max Total Peers
			s.lock.RLock()
			total := len(s.Peers)
			s.lock.RUnlock()
			if total >= s.MaxPeers {
				log.Printf("⚠️ DDoS Protection: Dropped conn from %s (Max Peers Reached)", conn.RemoteAddr())
				conn.Close()
				continue
			}

			// DDoS Check 2: Max Per IP
			ip, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
			s.lock.Lock()
			count := s.ConnCount[ip]
			if count >= s.MaxPerIP {
				s.lock.Unlock()
				log.Printf("⚠️ DDoS Protection: Dropped conn from %s (Rate Limit Exceeded)", ip)
				conn.Close()
				continue
			}
			s.ConnCount[ip]++
			s.lock.Unlock()

			go s.handleConn(conn, false)
		}
	}
}

// Connect dial a remote peer and adds it to the network.
func (s *Server) Connect(addr string) error {
	conn, err := s.Transport.Dial(addr)
	if err != nil {
		return err
	}
	go s.handleConn(conn, true)
	return nil
}

// handleConn shakes hands and registers the peer.
func (s *Server) handleConn(conn net.Conn, outbound bool) {
	peer := NewPeer(conn, outbound)

	// 1. Send Handshake
	hsData := HandshakeData{
		Version:     ProtocolVersion,
		NodeID:      "node-1", // TODO: proper ID
		GenesisHash: GenesisHash,
		Height:      0, // TODO: get from DAG
	}

	payload, err := encodeHandshake(hsData)
	if err != nil {
		log.Printf("Failed to encode handshake: %v", err)
		conn.Close()
		return
	}

	if err := peer.Send(Message{Type: MsgHandshake, Payload: payload}); err != nil {
		log.Printf("Failed to send handshake: %v", err)
		conn.Close()
		return
	}

	// 2. Wait for Handshake Reply
	msg, err := peer.Read()
	if err != nil {
		log.Printf("Failed to read handshake: %v", err)
		conn.Close()
		return
	}

	if msg.Type != MsgHandshake {
		log.Printf("Expected handshake, got %d", msg.Type)
		conn.Close()
		return
	}

	var remoteHS HandshakeData
	if err := decodeHandshake(msg.Payload, &remoteHS); err != nil {
		log.Printf("Invalid handshake payload: %v", err)
		conn.Close()
		return
	}

	if remoteHS.GenesisHash != GenesisHash {
		log.Printf("Incompatible genesis: %s", remoteHS.GenesisHash)
		conn.Close()
		return
	}

	log.Printf("Handshake success with %s (Ver: %d)", conn.RemoteAddr(), remoteHS.Version)

	s.AddPeer(peer)

	// Start read loop
	go s.readLoop(peer)

	// Discovery: Ask for more peers
	s.SendGetAddr(peer)

	// Sync: Ask for DAG history
	s.SendGetDAG(peer)
}

func (s *Server) readLoop(p *Peer) {
	defer s.RemovePeer(p.Conn.RemoteAddr().String())
	for {
		msg, err := p.Read()
		if err != nil {
			log.Printf("Peer disconnected: %v", err)
			return
		}
		s.handleMessage(p, msg)
	}
}

func (s *Server) handleMessage(p *Peer, msg Message) {
	switch msg.Type {
	case MsgTx:
		// TODO: Forward to TPU or Mempool
		log.Printf("Received TX from %s", p.Conn.RemoteAddr())
	case MsgBlock:
		s.handleBlock(p, msg.Payload)
	case MsgGetAddr:
		s.handleGetAddr(p)
	case MsgAddr:
		s.handleAddr(p, msg.Payload)
	case MsgGetDAG:
		s.handleGetDAG(p)
	case MsgDAG:
		s.handleDAG(p, msg.Payload)
	default:
		log.Printf("Unknown message type: %d", msg.Type)
	}
}

// handleBlock processes incoming vertices.
func (s *Server) handleBlock(p *Peer, payload []byte) {
	var v pulse.Vertex
	dec := gob.NewDecoder(bytes.NewReader(payload))
	if err := dec.Decode(&v); err != nil {
		log.Printf("Failed to decode block from %s: %v", p.Conn.RemoteAddr(), err)
		return
	}

	log.Printf("Received Vertex %s from %s", v.Hash.String(), p.Conn.RemoteAddr())

	// Validate Block (Proof-of-Stake)
	if s.State != nil {
		if err := staking.ValidateBlock(&v, s.State); err != nil {
			log.Printf("⚠️ Block Rejected from %s: %v", p.Conn.RemoteAddr(), err)
			return
		}
	}

	// Add to local DAG
	if s.DAG != nil {
		s.DAG.AddVertex(&v)

		// Apply Block Reward (Coinbase)
		if s.Executor != nil {
			s.Executor.ApplyBlockReward(v.Author)
		}

		// TODO: Re-broadcast if valid and new?
	}
}

// Broadcast sends a message to all connected peers.
func (s *Server) Broadcast(msg Message) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	for _, peer := range s.Peers {
		go func(p *Peer) {
			if err := p.Send(msg); err != nil {
				log.Printf("Failed to broadcast to %s: %v", p.Conn.RemoteAddr(), err)
			}
		}(peer)
	}
}

// BroadcastBlock encodes and broadcasts a vertex.
func (s *Server) BroadcastBlock(v *pulse.Vertex) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(v); err != nil {
		log.Printf("Failed to encode block for broadcast: %v", err)
		return
	}
	s.Broadcast(Message{Type: MsgBlock, Payload: buf.Bytes()})
}

// Helpers
func encodeHandshake(data HandshakeData) ([]byte, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decodeHandshake(data []byte, out *HandshakeData) error {
	return gob.NewDecoder(bytes.NewReader(data)).Decode(out)
}

// SendGetAddr requests peers from a neighbor.
func (s *Server) SendGetAddr(p *Peer) {
	p.Send(Message{Type: MsgGetAddr})
}

// handleGetAddr responds with a list of known peers.
func (s *Server) handleGetAddr(p *Peer) {
	s.lock.RLock()
	var addrs []string
	for addr := range s.KnownPeers {
		addrs = append(addrs, addr)
		if len(addrs) >= 10 { // Limit response size
			break
		}
	}
	s.lock.RUnlock()

	data := AddrData{Addrs: addrs}
	var buf bytes.Buffer
	gob.NewEncoder(&buf).Encode(data)
	p.Send(Message{Type: MsgAddr, Payload: buf.Bytes()})
}

// handleAddr processes received peer addresses.
func (s *Server) handleAddr(p *Peer, payload []byte) {
	var data AddrData
	dec := gob.NewDecoder(bytes.NewReader(payload))
	if err := dec.Decode(&data); err != nil {
		return
	}

	s.lock.Lock()
	newPeers := 0
	for _, addr := range data.Addrs {
		if !s.KnownPeers[addr] {
			s.KnownPeers[addr] = true
			newPeers++
			// Active Discovery: Connect to them!
			go s.Connect(addr)
		}
	}
	s.lock.Unlock()

	if newPeers > 0 {
		log.Printf("Discovery: Received %d new peers from %s", newPeers, p.Conn.RemoteAddr())
	}
}

// SendGetDAG requests the full DAG from a peer.
func (s *Server) SendGetDAG(p *Peer) {
	p.Send(Message{Type: MsgGetDAG})
}

// handleGetDAG responds with the full DAG.
func (s *Server) handleGetDAG(p *Peer) {
	if s.DAG == nil {
		return
	}

	// Get all vertices using the new method
	vertices := s.DAG.GetAllVertices()

	// Encode them individually
	var encoded [][]byte
	for _, v := range vertices {
		var buf bytes.Buffer
		if err := gob.NewEncoder(&buf).Encode(v); err == nil {
			encoded = append(encoded, buf.Bytes())
		}
	}

	// Send MsgDAG
	data := DAGData{Vertices: encoded}
	var buf bytes.Buffer
	gob.NewEncoder(&buf).Encode(data)
	p.Send(Message{Type: MsgDAG, Payload: buf.Bytes()})
}

// handleDAG processes a batch of vertices.
func (s *Server) handleDAG(p *Peer, payload []byte) {
	var data DAGData
	dec := gob.NewDecoder(bytes.NewReader(payload))
	if err := dec.Decode(&data); err != nil {
		return
	}

	count := 0
	for _, vBytes := range data.Vertices {
		// handleBlock already does decoding + validation + persistence
		s.handleBlock(p, vBytes)
		count++
	}

	if count > 0 {
		log.Printf("🔥 Synced: Downloaded %d vertices from %s", count, p.Conn.RemoteAddr())
	}
}

// AddPeer safely adds a peer to the map.
func (s *Server) AddPeer(p *Peer) {
	s.lock.Lock()
	defer s.lock.Unlock()
	addr := p.Conn.RemoteAddr().String()
	s.Peers[addr] = p
	s.KnownPeers[addr] = true // Add to known list
}

// RemovePeer safely removes a peer.
func (s *Server) RemovePeer(addr string) {
	s.lock.Lock()
	defer s.lock.Unlock()
	if peer, ok := s.Peers[addr]; ok {
		peer.Close()
		delete(s.Peers, addr)

		// Decrement IP count
		ip, _, _ := net.SplitHostPort(addr)
		if s.ConnCount[ip] > 0 {
			s.ConnCount[ip]--
		}
	}
}
