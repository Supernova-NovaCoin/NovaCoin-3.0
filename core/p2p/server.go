package p2p

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"log"
	"net"
	"novacoin/core/execution"
	"novacoin/core/network"
	"novacoin/core/pulse"
	"novacoin/core/staking"
	"novacoin/core/tpu"
	"novacoin/core/types"
	"sync"
)

const (
	ProtocolVersion = 1
)

// ComputeGenesisHash computes a deterministic genesis hash from network parameters
func ComputeGenesisHash() string {
	// Create a deterministic genesis hash from chain parameters
	data := []byte("supernova-mainnet-v1-genesis-2024")
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:16]) // Use first 16 bytes
}

// GenesisHash is computed at init time
var GenesisHash = ComputeGenesisHash()

// Server manages all P2P connections and protocol logic.
type Server struct {
	Transport  *Transport
	Peers      map[string]*Peer
	KnownPeers map[string]bool // Set of known peer addresses
	NodeID     string          // Unique ID of this node (Public Key Hex)

	DAG      *pulse.VertexStore      // Link to the DAG
	State    *execution.StateManager // Link to State for staking check
	Executor *execution.Executor     // Link to Executor for rewards
	Mempool  *tpu.Mempool            // Link to Mempool for Tx storage
	Slasher  *staking.Slasher        // Link to Slasher for double-sign detection

	// Security / DDoS Protection
	ConnCount  map[string]int     // Count of connections per IP
	MaxPeers   int                // Total max peers
	MaxPerIP   int                // Max peers per IP
	Reputation *ReputationManager // Peer reputation tracking

	PeersMutex sync.RWMutex
	Quit       chan struct{}
}

// NewServer creates a new P2P server instance.
func NewServer(addr string, maxPeers int, nodeID string, dag *pulse.VertexStore, state *execution.StateManager, exec *execution.Executor, mempool *tpu.Mempool) *Server {
	return &Server{
		Transport:  NewTransport(addr),
		Peers:      make(map[string]*Peer),
		KnownPeers: make(map[string]bool),
		NodeID:     nodeID,
		ConnCount:  make(map[string]int),
		MaxPeers:   maxPeers,
		MaxPerIP:   5, // Strict per-IP Limit (Hardcoded for now)
		Reputation: NewReputationManager(),
		Slasher:    staking.NewSlasher(nil), // Use default slashing config

		DAG:      dag,
		State:    state,
		Executor: exec,
		Mempool:  mempool,
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
			s.PeersMutex.RLock()
			total := len(s.Peers)
			s.PeersMutex.RUnlock()
			if total >= s.MaxPeers {
				log.Printf("⚠️ DDoS Protection: Dropped conn from %s (Max Peers Reached)", conn.RemoteAddr())
				conn.Close()
				continue
			}

			// DDoS Check 2: Max Per IP
			ip, _, _ := net.SplitHostPort(conn.RemoteAddr().String())

			// Reputation Check: Is this IP banned?
			if s.Reputation.IsAddressBanned(ip) {
				log.Printf("🚫 Reputation: Dropped conn from banned IP %s", ip)
				conn.Close()
				continue
			}

			s.PeersMutex.Lock()
			count := s.ConnCount[ip]
			if count >= s.MaxPerIP {
				s.PeersMutex.Unlock()
				log.Printf("⚠️ DDoS Protection: Dropped conn from %s (Rate Limit Exceeded)", ip)
				conn.Close()
				continue
			}
			s.ConnCount[ip]++
			s.PeersMutex.Unlock()

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
		NodeID:      s.NodeID,
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

	peer.NodeID = remoteHS.NodeID

	// Check if this peer is banned by NodeID
	if s.Reputation.IsBanned(peer.NodeID) {
		log.Printf("🚫 Reputation: Rejected banned peer %s", peer.NodeID)
		conn.Close()
		return
	}

	// Register peer with reputation system
	ip, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
	s.Reputation.GetOrCreate(peer.NodeID, ip)

	s.AddPeer(peer)

	// Start read loop
	go s.readLoop(peer)

	// Discovery: Ask for more peers
	s.SendGetAddr(peer)

	// Sync: Ask for DAG history
	s.SendGetDAG(peer)
}

func (s *Server) readLoop(p *Peer) {
	defer func() {
		s.Reputation.RecordDisconnect(p.NodeID)
		s.RemovePeer(p.Conn.RemoteAddr().String())
	}()
	for {
		msg, err := p.Read()
		if err != nil {
			log.Printf("Peer disconnected: %v", err)
			return
		}
		s.handleMessage(p, msg)
	}
}

// GetReputationStats returns peer reputation statistics
func (s *Server) GetReputationStats() ReputationStats {
	return s.Reputation.Stats()
}

// GetPeerReputations returns all peer reputations
func (s *Server) GetPeerReputations() []*PeerReputation {
	return s.Reputation.GetAllReputations()
}

func (s *Server) handleMessage(p *Peer, msg Message) {
	// Validate payload size first
	if err := network.ValidatePayloadSize(msg.Payload, network.MaxMessageSize); err != nil {
		log.Printf("⚠️ Message too large from %s: %d bytes", p.Conn.RemoteAddr(), len(msg.Payload))
		s.Reputation.RecordProtocolError(p.NodeID)
		return
	}

	switch msg.Type {
	case MsgTx:
		// Validate TX size
		if err := network.ValidatePayloadSize(msg.Payload, network.MaxTxSize); err != nil {
			log.Printf("⚠️ TX too large from %s", p.Conn.RemoteAddr())
			s.Reputation.RecordInvalidTx(p.NodeID)
			return
		}

		// Forward to Mempool with safe decoding
		var tx types.Transaction
		if err := network.SafeDecodeTransaction(msg.Payload, &tx); err != nil {
			log.Printf("Invalid Tx from %s: %v", p.Conn.RemoteAddr(), err)
			s.Reputation.RecordInvalidTx(p.NodeID)
			return
		}
		if s.Mempool != nil {
			if s.Mempool.Add(tx) {
				log.Printf("📥 Recv Tx %d from %s (Added to Mempool)", tx.Nonce, p.Conn.RemoteAddr())
				s.Reputation.RecordValidTx(p.NodeID)
			}
		}
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
	// Validate block size
	if err := network.ValidatePayloadSize(payload, network.MaxBlockSize); err != nil {
		log.Printf("⚠️ Block too large from %s: %d bytes", p.Conn.RemoteAddr(), len(payload))
		s.Reputation.RecordProtocolError(p.NodeID)
		return
	}

	var v pulse.Vertex
	if err := network.SafeDecodeBlock(payload, &v); err != nil {
		log.Printf("Failed to decode block from %s: %v", p.Conn.RemoteAddr(), err)
		s.Reputation.RecordProtocolError(p.NodeID)
		return
	}

	log.Printf("Received Vertex %s from %s", v.Hash.String(), p.Conn.RemoteAddr())

	// Check if we already have this vertex (skip if duplicate)
	if s.DAG != nil && s.DAG.HasVertex(v.Hash) {
		return // Already have it, don't process or rebroadcast
	}

	// Validate Block (Proof-of-Stake)
	if s.State != nil {
		if err := staking.ValidateBlock(&v, s.State); err != nil {
			log.Printf("⚠️ Block Rejected from %s: %v", p.Conn.RemoteAddr(), err)
			s.Reputation.RecordInvalidBlock(p.NodeID)
			return
		}
	}

	// Check for double-signing (slashing condition)
	if s.Slasher != nil {
		isDoubleSign, slashRecord := s.Slasher.RecordBlockSigned(&v)
		if isDoubleSign && slashRecord != nil {
			log.Printf("🚨 DOUBLE-SIGN DETECTED: Validator %x signed conflicting blocks at round %d",
				v.Author[:4], v.Round)

			// Apply slashing if we have state access
			if s.State != nil {
				stake := s.State.GetStake(v.Author)
				if stake > 0 {
					slashAmount, err := s.Slasher.Slash(v.Author, stake, staking.OffenseDoubleSign, v.Timestamp)
					if err == nil && slashAmount > 0 {
						// Reduce stake in state
						newStake := stake - slashAmount
						if newStake > stake { // Underflow check
							newStake = 0
						}
						s.State.SetStake(v.Author, newStake)
						log.Printf("⚠️ Slashed validator %x: %d NVN", v.Author[:4], slashAmount/1_000_000)
					}
				}
			}
			// Reject the duplicate block
			s.Reputation.RecordInvalidBlock(p.NodeID)
			return
		}
	}

	// Valid block - update reputation
	s.Reputation.RecordValidBlock(p.NodeID)

	// Add to local DAG
	if s.DAG != nil {
		s.DAG.AddVertex(&v)

		// Apply Block Reward (Coinbase)
		if s.Executor != nil {
			// Calculate fees from transactions in the block
			var fees uint64 = 0
			var txs []types.Transaction
			if err := gob.NewDecoder(bytes.NewReader(v.TxPayload)).Decode(&txs); err == nil {
				for _, tx := range txs {
					fees += tx.Fee
				}
			}
			s.Executor.ApplyBlockReward(v.Author, fees, v.Timestamp)
		}

		// Re-broadcast to other peers (excluding sender)
		s.RebroadcastBlock(&v, p.Conn.RemoteAddr().String())
	}
}

// RebroadcastBlock sends a block to all peers except the sender
func (s *Server) RebroadcastBlock(v *pulse.Vertex, excludeAddr string) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(v); err != nil {
		log.Printf("Failed to encode block for rebroadcast: %v", err)
		return
	}

	msg := Message{Type: MsgBlock, Payload: buf.Bytes()}

	s.PeersMutex.RLock()
	defer s.PeersMutex.RUnlock()

	for addr, peer := range s.Peers {
		if addr == excludeAddr {
			continue // Don't send back to sender
		}
		go func(p *Peer) {
			if err := p.Send(msg); err != nil {
				log.Printf("Failed to rebroadcast to %s: %v", p.Conn.RemoteAddr(), err)
			}
		}(peer)
	}
}

// Broadcast sends a message to all connected peers.
func (s *Server) Broadcast(msg Message) {
	s.PeersMutex.RLock()
	defer s.PeersMutex.RUnlock()
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
	s.PeersMutex.RLock()
	var addrs []string
	for addr := range s.KnownPeers {
		addrs = append(addrs, addr)
		if len(addrs) >= 10 { // Limit response size
			break
		}
	}
	s.PeersMutex.RUnlock()

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

	s.PeersMutex.Lock()
	newPeers := 0
	for _, addr := range data.Addrs {
		if !s.KnownPeers[addr] {
			s.KnownPeers[addr] = true
			newPeers++
			// Active Discovery: Connect to them!
			go s.Connect(addr)
		}
	}
	s.PeersMutex.Unlock()

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

// GetPeer safely retrieves a peer by address.
func (s *Server) GetPeer(addr string) *Peer {
	s.PeersMutex.RLock()
	defer s.PeersMutex.RUnlock()
	return s.Peers[addr]
}

// AddPeer safely adds a peer to the map.
func (s *Server) AddPeer(p *Peer) {
	s.PeersMutex.Lock()
	defer s.PeersMutex.Unlock()
	addr := p.Conn.RemoteAddr().String()
	s.Peers[addr] = p
	s.KnownPeers[addr] = true // Add to known list
}

// RemovePeer safely removes a peer.
func (s *Server) RemovePeer(addr string) {
	s.PeersMutex.Lock()
	defer s.PeersMutex.Unlock()
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
