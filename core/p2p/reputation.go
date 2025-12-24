package p2p

import (
	"sync"
	"time"
)

// Reputation score constants
const (
	MaxReputation     = 100
	MinReputation     = -100
	InitialReputation = 50

	// Score adjustments
	ScoreValidBlock    = 5  // Received valid block
	ScoreInvalidBlock  = -20 // Received invalid block
	ScoreValidTx       = 1  // Received valid transaction
	ScoreInvalidTx     = -5  // Received invalid transaction
	ScoreFastResponse  = 2  // Quick response to requests
	ScoreSlowResponse  = -1  // Slow response
	ScoreGoodPeers     = 3  // Shared good peer addresses
	ScoreBadPeers      = -5  // Shared bad/unreachable peers
	ScoreTimeout       = -10 // Connection timeout
	ScoreDisconnect    = -3  // Unexpected disconnect
	ScoreProtocolError = -15 // Protocol violation

	// Thresholds
	BanThreshold         = -50  // Score below this = ban
	SuspiciousThreshold  = 0    // Score below this = suspicious
	TrustedThreshold     = 75   // Score above this = trusted

	// Ban duration
	BanDuration = 1 * time.Hour
)

// PeerReputation tracks a peer's behavior score
type PeerReputation struct {
	NodeID        string
	Address       string
	Score         int
	ValidBlocks   int
	InvalidBlocks int
	ValidTxs      int
	InvalidTxs    int
	Timeouts      int
	LastSeen      time.Time
	FirstSeen     time.Time
	BannedUntil   time.Time
	IsBanned      bool
}

// ReputationManager tracks peer reputations
type ReputationManager struct {
	peers   map[string]*PeerReputation // nodeID -> reputation
	banned  map[string]time.Time       // address -> ban expiry
	mu      sync.RWMutex
}

// NewReputationManager creates a new reputation manager
func NewReputationManager() *ReputationManager {
	rm := &ReputationManager{
		peers:  make(map[string]*PeerReputation),
		banned: make(map[string]time.Time),
	}

	// Start cleanup goroutine
	go rm.cleanupLoop()

	return rm
}

// cleanupLoop periodically removes expired bans
func (rm *ReputationManager) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		rm.cleanupExpiredBans()
	}
}

// cleanupExpiredBans removes expired bans
func (rm *ReputationManager) cleanupExpiredBans() {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	now := time.Now()
	for addr, expiry := range rm.banned {
		if now.After(expiry) {
			delete(rm.banned, addr)
		}
	}

	// Also unban peers
	for _, rep := range rm.peers {
		if rep.IsBanned && now.After(rep.BannedUntil) {
			rep.IsBanned = false
			rep.Score = 0 // Reset score on unban
		}
	}
}

// GetOrCreate returns existing reputation or creates new one
func (rm *ReputationManager) GetOrCreate(nodeID, address string) *PeerReputation {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if rep, ok := rm.peers[nodeID]; ok {
		rep.LastSeen = time.Now()
		return rep
	}

	rep := &PeerReputation{
		NodeID:    nodeID,
		Address:   address,
		Score:     InitialReputation,
		FirstSeen: time.Now(),
		LastSeen:  time.Now(),
	}
	rm.peers[nodeID] = rep
	return rep
}

// Get returns peer reputation if exists
func (rm *ReputationManager) Get(nodeID string) (*PeerReputation, bool) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	rep, ok := rm.peers[nodeID]
	return rep, ok
}

// AdjustScore modifies a peer's reputation score
func (rm *ReputationManager) AdjustScore(nodeID string, delta int) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	rep, ok := rm.peers[nodeID]
	if !ok {
		return
	}

	rep.Score += delta

	// Clamp to bounds
	if rep.Score > MaxReputation {
		rep.Score = MaxReputation
	}
	if rep.Score < MinReputation {
		rep.Score = MinReputation
	}

	// Auto-ban if below threshold
	if rep.Score <= BanThreshold && !rep.IsBanned {
		rep.IsBanned = true
		rep.BannedUntil = time.Now().Add(BanDuration)
		rm.banned[rep.Address] = rep.BannedUntil
	}
}

// RecordValidBlock records receipt of a valid block
func (rm *ReputationManager) RecordValidBlock(nodeID string) {
	rm.mu.Lock()
	if rep, ok := rm.peers[nodeID]; ok {
		rep.ValidBlocks++
	}
	rm.mu.Unlock()
	rm.AdjustScore(nodeID, ScoreValidBlock)
}

// RecordInvalidBlock records receipt of an invalid block
func (rm *ReputationManager) RecordInvalidBlock(nodeID string) {
	rm.mu.Lock()
	if rep, ok := rm.peers[nodeID]; ok {
		rep.InvalidBlocks++
	}
	rm.mu.Unlock()
	rm.AdjustScore(nodeID, ScoreInvalidBlock)
}

// RecordValidTx records receipt of a valid transaction
func (rm *ReputationManager) RecordValidTx(nodeID string) {
	rm.mu.Lock()
	if rep, ok := rm.peers[nodeID]; ok {
		rep.ValidTxs++
	}
	rm.mu.Unlock()
	rm.AdjustScore(nodeID, ScoreValidTx)
}

// RecordInvalidTx records receipt of an invalid transaction
func (rm *ReputationManager) RecordInvalidTx(nodeID string) {
	rm.mu.Lock()
	if rep, ok := rm.peers[nodeID]; ok {
		rep.InvalidTxs++
	}
	rm.mu.Unlock()
	rm.AdjustScore(nodeID, ScoreInvalidTx)
}

// RecordTimeout records a connection timeout
func (rm *ReputationManager) RecordTimeout(nodeID string) {
	rm.mu.Lock()
	if rep, ok := rm.peers[nodeID]; ok {
		rep.Timeouts++
	}
	rm.mu.Unlock()
	rm.AdjustScore(nodeID, ScoreTimeout)
}

// RecordDisconnect records an unexpected disconnect
func (rm *ReputationManager) RecordDisconnect(nodeID string) {
	rm.AdjustScore(nodeID, ScoreDisconnect)
}

// RecordProtocolError records a protocol violation
func (rm *ReputationManager) RecordProtocolError(nodeID string) {
	rm.AdjustScore(nodeID, ScoreProtocolError)
}

// IsBanned checks if a peer is banned
func (rm *ReputationManager) IsBanned(nodeID string) bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	if rep, ok := rm.peers[nodeID]; ok {
		if rep.IsBanned && time.Now().Before(rep.BannedUntil) {
			return true
		}
	}
	return false
}

// IsAddressBanned checks if an address is banned
func (rm *ReputationManager) IsAddressBanned(address string) bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	if expiry, ok := rm.banned[address]; ok {
		return time.Now().Before(expiry)
	}
	return false
}

// BanPeer manually bans a peer
func (rm *ReputationManager) BanPeer(nodeID string, duration time.Duration) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if rep, ok := rm.peers[nodeID]; ok {
		rep.IsBanned = true
		rep.BannedUntil = time.Now().Add(duration)
		rm.banned[rep.Address] = rep.BannedUntil
	}
}

// UnbanPeer manually unbans a peer
func (rm *ReputationManager) UnbanPeer(nodeID string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if rep, ok := rm.peers[nodeID]; ok {
		rep.IsBanned = false
		delete(rm.banned, rep.Address)
		rep.Score = InitialReputation // Reset to initial
	}
}

// IsTrusted checks if peer is trusted (high reputation)
func (rm *ReputationManager) IsTrusted(nodeID string) bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	if rep, ok := rm.peers[nodeID]; ok {
		return rep.Score >= TrustedThreshold
	}
	return false
}

// IsSuspicious checks if peer is suspicious (low reputation)
func (rm *ReputationManager) IsSuspicious(nodeID string) bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	if rep, ok := rm.peers[nodeID]; ok {
		return rep.Score < SuspiciousThreshold
	}
	return false
}

// GetAllReputations returns all peer reputations (for monitoring)
func (rm *ReputationManager) GetAllReputations() []*PeerReputation {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	var reps []*PeerReputation
	for _, rep := range rm.peers {
		// Return a copy
		repCopy := *rep
		reps = append(reps, &repCopy)
	}
	return reps
}

// GetBannedPeers returns all banned peers
func (rm *ReputationManager) GetBannedPeers() []string {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	var banned []string
	for addr := range rm.banned {
		banned = append(banned, addr)
	}
	return banned
}

// GetTrustedPeers returns all trusted peers
func (rm *ReputationManager) GetTrustedPeers() []string {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	var trusted []string
	for nodeID, rep := range rm.peers {
		if rep.Score >= TrustedThreshold && !rep.IsBanned {
			trusted = append(trusted, nodeID)
		}
	}
	return trusted
}

// Stats returns reputation system statistics
type ReputationStats struct {
	TotalPeers      int `json:"totalPeers"`
	TrustedPeers    int `json:"trustedPeers"`
	SuspiciousPeers int `json:"suspiciousPeers"`
	BannedPeers     int `json:"bannedPeers"`
	AverageScore    int `json:"averageScore"`
}

func (rm *ReputationManager) Stats() ReputationStats {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	stats := ReputationStats{
		TotalPeers: len(rm.peers),
	}

	totalScore := 0
	for _, rep := range rm.peers {
		totalScore += rep.Score
		if rep.Score >= TrustedThreshold {
			stats.TrustedPeers++
		}
		if rep.Score < SuspiciousThreshold {
			stats.SuspiciousPeers++
		}
		if rep.IsBanned {
			stats.BannedPeers++
		}
	}

	if stats.TotalPeers > 0 {
		stats.AverageScore = totalScore / stats.TotalPeers
	}

	return stats
}
