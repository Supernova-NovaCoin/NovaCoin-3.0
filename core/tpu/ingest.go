package tpu

import (
	"crypto/ed25519"
	"fmt"
	"net"
	"novacoin/core/network"
	"novacoin/core/types"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// Rate limiting constants
const (
	MaxTxPerAddressPerMinute = 10
	RateLimitWindowSeconds   = 60
)

// Worker pool constants
const (
	PacketQueueSize    = 10000           // Buffered channel size
	MinWorkers         = 4               // Minimum workers
	MaxWorkers         = 64              // Maximum workers
	PacketReadTimeout  = 100 * time.Millisecond
	RateLimitCleanup   = 5 * time.Minute // Cleanup stale rate limit entries
)

// IngestServer represents the "Stage 1" of the pipeline: Zero-Copy Network Ingest.
// Uses a worker pool to prevent unbounded goroutine creation.
type IngestServer struct {
	addr    *net.UDPAddr
	conn    *net.UDPConn
	mempool *Mempool

	// Worker pool
	packetQueue chan []byte
	numWorkers  int
	wg          sync.WaitGroup
	shutdown    chan struct{}
	running     atomic.Bool

	// Rate limiting: tracks tx count per address
	rateLimiter map[[32]byte]*rateLimitEntry
	rateMu      sync.RWMutex

	// Metrics
	packetsReceived  atomic.Uint64
	packetsDropped   atomic.Uint64
	packetsProcessed atomic.Uint64
	packetsInvalid   atomic.Uint64
}

type rateLimitEntry struct {
	count     int
	resetTime time.Time
}

// IngestStats contains metrics for monitoring
type IngestStats struct {
	PacketsReceived  uint64 `json:"packetsReceived"`
	PacketsDropped   uint64 `json:"packetsDropped"`
	PacketsProcessed uint64 `json:"packetsProcessed"`
	PacketsInvalid   uint64 `json:"packetsInvalid"`
	QueueLength      int    `json:"queueLength"`
	QueueCapacity    int    `json:"queueCapacity"`
	NumWorkers       int    `json:"numWorkers"`
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

	// Calculate optimal worker count based on CPU
	numWorkers := runtime.NumCPU() * 2
	if numWorkers < MinWorkers {
		numWorkers = MinWorkers
	}
	if numWorkers > MaxWorkers {
		numWorkers = MaxWorkers
	}

	server := &IngestServer{
		addr:        addr,
		conn:        conn,
		mempool:     mempool,
		packetQueue: make(chan []byte, PacketQueueSize),
		numWorkers:  numWorkers,
		shutdown:    make(chan struct{}),
		rateLimiter: make(map[[32]byte]*rateLimitEntry),
	}

	return server, nil
}

// Start begins the high-performance packet capture loop with worker pool.
func (s *IngestServer) Start() {
	if s.running.Swap(true) {
		return // Already running
	}

	fmt.Printf("🚀 Supernova Ingest Engine Started on UDP %d (Workers: %d, Queue: %d)\n",
		s.addr.Port, s.numWorkers, PacketQueueSize)

	// Start worker pool
	for i := 0; i < s.numWorkers; i++ {
		s.wg.Add(1)
		go s.worker(i)
	}

	// Start rate limit cleanup goroutine
	s.wg.Add(1)
	go s.rateLimitCleanupLoop()

	// Main receive loop
	buffer := make([]byte, 65535) // Max UDP size

	for {
		select {
		case <-s.shutdown:
			fmt.Println("📥 Ingest: Shutdown signal received")
			return
		default:
		}

		// Set read deadline to allow checking shutdown
		s.conn.SetReadDeadline(time.Now().Add(PacketReadTimeout))

		n, _, err := s.conn.ReadFromUDP(buffer)
		if err != nil {
			// Check if it's a timeout (expected during shutdown check)
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			// Check if we're shutting down
			select {
			case <-s.shutdown:
				return
			default:
				fmt.Println("Ingest Error:", err)
				continue
			}
		}

		s.packetsReceived.Add(1)

		// Quick size validation BEFORE queuing (zero cost rejection)
		if n > network.MaxTxSize {
			s.packetsDropped.Add(1)
			continue
		}

		// Copy buffer data because 'buffer' is reused
		packet := make([]byte, n)
		copy(packet, buffer[:n])

		// Non-blocking queue - DROP if full (zero cost)
		select {
		case s.packetQueue <- packet:
			// Queued successfully
		default:
			// Queue full - DROP packet immediately
			s.packetsDropped.Add(1)
		}
	}
}

// worker processes packets from the queue
func (s *IngestServer) worker(id int) {
	defer s.wg.Done()

	for {
		select {
		case <-s.shutdown:
			return
		case packet, ok := <-s.packetQueue:
			if !ok {
				return // Channel closed
			}
			s.processPacket(packet)
		}
	}
}

// rateLimitCleanupLoop periodically cleans up expired rate limit entries
func (s *IngestServer) rateLimitCleanupLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(RateLimitCleanup)
	defer ticker.Stop()

	for {
		select {
		case <-s.shutdown:
			return
		case <-ticker.C:
			s.cleanupRateLimits()
		}
	}
}

// cleanupRateLimits removes expired rate limit entries
func (s *IngestServer) cleanupRateLimits() {
	s.rateMu.Lock()
	defer s.rateMu.Unlock()

	now := time.Now()
	for addr, entry := range s.rateLimiter {
		if now.After(entry.resetTime) {
			delete(s.rateLimiter, addr)
		}
	}
}

func (s *IngestServer) processPacket(data []byte) {
	// Validate size again (defense in depth)
	if err := network.ValidatePayloadSize(data, network.MaxTxSize); err != nil {
		s.packetsInvalid.Add(1)
		return
	}

	// Deserialize with safe decoder
	var tx types.Transaction
	if err := network.SafeDecodeTransaction(data, &tx); err != nil {
		s.packetsInvalid.Add(1)
		return // Invalid packet format
	}

	// 1. Signature Verification (CRITICAL SECURITY CHECK)
	if len(tx.Sig) != ed25519.SignatureSize {
		s.packetsInvalid.Add(1)
		return // Invalid signature length
	}

	msg := tx.SerializeForSigning()
	if !ed25519.Verify(tx.From[:], msg, tx.Sig) {
		s.packetsInvalid.Add(1)
		return // Invalid signature - reject
	}

	// 2. Rate Limiting (DoS Protection) - atomic check and increment
	if !s.checkAndIncrementRateLimit(tx.From) {
		s.packetsDropped.Add(1)
		return // Rate limit exceeded
	}

	// 3. Add to Mempool
	if s.mempool != nil {
		if s.mempool.Add(tx) {
			s.packetsProcessed.Add(1)
		} else {
			s.packetsDropped.Add(1) // Rejected by mempool (duplicate, etc.)
		}
	}
}

// checkAndIncrementRateLimit atomically checks and increments rate limit
func (s *IngestServer) checkAndIncrementRateLimit(addr [32]byte) bool {
	s.rateMu.Lock()
	defer s.rateMu.Unlock()

	now := time.Now()
	entry, exists := s.rateLimiter[addr]

	if !exists || now.After(entry.resetTime) {
		// Create new entry or reset expired one
		s.rateLimiter[addr] = &rateLimitEntry{
			count:     1,
			resetTime: now.Add(RateLimitWindowSeconds * time.Second),
		}
		return true
	}

	if entry.count >= MaxTxPerAddressPerMinute {
		return false // Rate limit exceeded
	}

	entry.count++
	return true
}

// Stop gracefully shuts down the ingest server
func (s *IngestServer) Stop() {
	if !s.running.Swap(false) {
		return // Already stopped
	}

	fmt.Println("📥 Ingest: Stopping...")

	// Signal shutdown
	close(s.shutdown)

	// Close connection to unblock ReadFromUDP
	s.conn.Close()

	// Close packet queue
	close(s.packetQueue)

	// Wait for all workers to finish
	s.wg.Wait()

	fmt.Println("📥 Ingest: Stopped")
}

// GetStats returns current metrics
func (s *IngestServer) GetStats() IngestStats {
	return IngestStats{
		PacketsReceived:  s.packetsReceived.Load(),
		PacketsDropped:   s.packetsDropped.Load(),
		PacketsProcessed: s.packetsProcessed.Load(),
		PacketsInvalid:   s.packetsInvalid.Load(),
		QueueLength:      len(s.packetQueue),
		QueueCapacity:    cap(s.packetQueue),
		NumWorkers:       s.numWorkers,
	}
}

// Legacy compatibility methods (deprecated, kept for backward compatibility)

// checkRateLimit checks if the address has exceeded the rate limit.
// Deprecated: Use checkAndIncrementRateLimit instead
func (s *IngestServer) checkRateLimit(addr [32]byte) bool {
	s.rateMu.RLock()
	entry, exists := s.rateLimiter[addr]
	s.rateMu.RUnlock()

	now := time.Now()

	if !exists {
		return true // No entry = allowed
	}

	// Reset window if expired
	if now.After(entry.resetTime) {
		return true
	}

	return entry.count < MaxTxPerAddressPerMinute
}

// incrementRateLimit increments the rate limit counter for an address.
// Deprecated: Use checkAndIncrementRateLimit instead
func (s *IngestServer) incrementRateLimit(addr [32]byte) {
	s.rateMu.Lock()
	defer s.rateMu.Unlock()

	now := time.Now()
	entry, exists := s.rateLimiter[addr]

	if !exists || now.After(entry.resetTime) {
		// Create new entry or reset expired one
		s.rateLimiter[addr] = &rateLimitEntry{
			count:     1,
			resetTime: now.Add(RateLimitWindowSeconds * time.Second),
		}
		return
	}

	entry.count++
}
