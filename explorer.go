package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"novacoin/core/cache"
	"novacoin/core/execution"
	"novacoin/core/p2p"
	"novacoin/core/pulse"
	"novacoin/core/tpu"
	"novacoin/core/types"
	"novacoin/core/wallet"
	"strconv"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

// splitTrim splits a string by separator and trims whitespace
func splitTrim(s, sep string) []string {
	parts := strings.Split(s, sep)
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// verifyEd25519Signature verifies an Ed25519 signature
func verifyEd25519Signature(publicKey, message, signature []byte) bool {
	if len(publicKey) != ed25519.PublicKeySize {
		return false
	}
	if len(signature) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(publicKey, message, signature)
}

// Explorer cache limits
const (
	MaxHistoryPerAddress = 1000  // Max tx history per address
	MaxAddressesTracked  = 10000 // Max addresses with history
	MaxTxIndexSize       = 50000 // Max transactions in index
	MaxBlockIndexSize    = 10000 // Max blocks in index
)

// Global wallet manager for API
var walletManager *wallet.Manager

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// JSONVertex is a frontend-friendly representation of a Block/Vertex
type JSONVertex struct {
	Hash       string   `json:"hash"`
	Timestamp  int64    `json:"timestamp"`
	Author     string   `json:"author"`
	Parents    []string `json:"parents"`
	Rounds     uint64   `json:"round"`
	MerkleRoot string   `json:"merkleRoot"`
	// Etherscan-like Details
	Reward  float64 `json:"reward"`
	Size    int     `json:"size"`
	TxCount int     `json:"tx_count"`
}

type Stats struct {
	TotalBlocks int `json:"totalBlocks"`
	TPS         int `json:"tps"`
	Validators  int `json:"validators"`
	Latency     int `json:"latency"`
}

func startExplorerAPI(dag *pulse.VertexStore, state *execution.StateManager, mempool *tpu.Mempool, p2pServer *p2p.Server, cfg *Config) {
	// Initialize wallet manager
	walletManager = wallet.NewManager("./wallets")

	// WebSocket Hub
	var clientsMu sync.Mutex
	clients := make(map[*websocket.Conn]bool)
	broadcast := make(chan *JSONVertex)

	// Indexer with bounded caches (prevents memory exhaustion)
	var mu sync.RWMutex
	history := cache.NewBoundedMap[string, TxRecord](MaxHistoryPerAddress, MaxAddressesTracked)
	txIndex := cache.NewLRU[string, types.Transaction](MaxTxIndexSize)
	blockIndex := cache.NewLRU[string, *pulse.Vertex](MaxBlockIndexSize)

	// Rebuild Index from DAG
	all := dag.GetAllVertices()
	for _, v := range all {
		auth := hex.EncodeToString(v.Author[:])
		blockIndex.Set(v.Hash.String(), v)

		// 1. Mining Reward
		history.Append(auth, TxRecord{
			Hash:       v.Hash.String(),
			Type:       "MINING",
			From:       "SYSTEM",
			To:         auth,
			Amount:     100, // Fixed Reward (TODO: Calculate dynamic)
			Fee:        0,
			Nonce:      0,
			Signature:  "",
			BlockHash:  v.Hash.String(),
			BlockRound: v.Round,
			Time:       v.Timestamp / 1_000_000,
		})

		// 2. Checks for real Txs in Payload (if any)
		var txs []types.Transaction
		if err := gob.NewDecoder(bytes.NewReader(v.TxPayload)).Decode(&txs); err == nil {
			for _, tx := range txs {
				txHash := hex.EncodeToString(tx.Sig) // Use Sig as Hash
				txIndex.Set(txHash, tx)

				from := hex.EncodeToString(tx.From[:])
				to := hex.EncodeToString(tx.To[:]) // If 0000.. it's system/burn
				rec := TxRecord{
					Hash:       txHash,
					Type:       getTxTypeString(tx.Type),
					From:       from,
					To:         to,
					Amount:     float64(tx.Amount) / 1_000_000,
					Fee:        float64(tx.Fee) / 1_000_000,
					Nonce:      tx.Nonce,
					Signature:  txHash, // Using hash as sig for now
					BlockHash:  v.Hash.String(),
					BlockRound: v.Round,
					Time:       v.Timestamp / 1_000_000,
				}
				history.Append(from, rec)
				if to != "0000000000000000000000000000000000000000000000000000000000000000" {
					history.Append(to, rec)
				}
			}
		}
	}

	// Subscribe to DAG events
	dag.OnNewVertex = func(v *pulse.Vertex) {
		mu.Lock()
		// Update Index
		blockIndex.Set(v.Hash.String(), v)

		// 1. Calculate Reward & Stats
		var txs []types.Transaction
		txCount := 0
		var fees uint64 = 0
		if err := gob.NewDecoder(bytes.NewReader(v.TxPayload)).Decode(&txs); err == nil {
			txCount = len(txs)
			for _, tx := range txs {
				fees += tx.Fee

				// Index Tx
				txHash := hex.EncodeToString(tx.Sig)
				txIndex.Set(txHash, tx)

				from := hex.EncodeToString(tx.From[:])
				to := hex.EncodeToString(tx.To[:])
				rec := TxRecord{
					Hash:       txHash,
					Type:       getTxTypeString(tx.Type),
					From:       from,
					To:         to,
					Amount:     float64(tx.Amount) / 1_000_000,
					Fee:        float64(tx.Fee) / 1_000_000,
					Nonce:      tx.Nonce,
					Signature:  txHash,
					BlockHash:  v.Hash.String(),
					BlockRound: v.Round,
					Time:       v.Timestamp / 1_000_000,
				}
				history.Append(from, rec)
				if to != "0000000000000000000000000000000000000000000000000000000000000000" {
					history.Append(to, rec)
				}
			}
		}

		// Reward (Approx currently 500 + 50% fees)
		reward := 500.0 + (float64(fees)/2)/1_000_000

		// Mining Record
		auth := hex.EncodeToString(v.Author[:])
		history.Append(auth, TxRecord{
			Hash:       v.Hash.String(),
			Type:       "MINING",
			From:       "SYSTEM",
			To:         auth,
			Amount:     reward,
			Fee:        0,
			Nonce:      0,
			Signature:  "",
			BlockHash:  v.Hash.String(),
			BlockRound: v.Round,
			Time:       v.Timestamp / 1_000_000,
		})
		mu.Unlock()

		jv := toJSONVertex(v, reward, txCount)
		broadcast <- jv
	}

	// Broadcaster
	go func() {
		for {
			jv := <-broadcast
			// jv is already structured, WriteJSON handles marshaling
			// For now, let's send the object directly or use a type field
			// The React app expects: "TX:..." or object?
			// Existing React app parsed "TX:" string. We will update React to handle JSON.

			clientsMu.Lock()
			var peers []*websocket.Conn
			for client := range clients {
				peers = append(peers, client)
			}
			clientsMu.Unlock()

			for _, client := range peers {
				err := client.WriteJSON(jv)
				if err != nil {
					client.Close()
					clientsMu.Lock()
					delete(clients, client)
					clientsMu.Unlock()
				}
			}
		}
	}()

	mux := http.NewServeMux()

	// 0. Root Handler (Health Check)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Supernova Explorer API Active 🚀\nEndpoints: /api/stats, /api/blocks, /api/account?addr=..., /ws"))
	})

	// 1. WebSocket Endpoint
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println("WS Upgrade error:", err)
			return
		}
		clientsMu.Lock()
		clients[ws] = true
		clientsMu.Unlock()
	})

	// 2. REST API: Recent Blocks (with pagination)
	mux.HandleFunc("/api/blocks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")

		// Pagination parameters
		page := 1
		limit := 20
		if p := r.URL.Query().Get("page"); p != "" {
			if v, err := strconv.Atoi(p); err == nil && v > 0 {
				page = v
			}
		}
		if l := r.URL.Query().Get("limit"); l != "" {
			if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 100 {
				limit = v
			}
		}

		all := dag.GetAllVertices()
		total := len(all)

		// Sort by timestamp descending (most recent first)
		// Vertices are already sorted, but let's ensure
		start := (page - 1) * limit
		end := start + limit
		if start > total {
			start = total
		}
		if end > total {
			end = total
		}

		// Reverse to get most recent first
		reversed := make([]*pulse.Vertex, total)
		for i, v := range all {
			reversed[total-1-i] = v
		}

		paginated := reversed[start:end]

		var response []JSONVertex
		for _, v := range paginated {
			var txs []types.Transaction
			txCount := 0
			var fees uint64 = 0
			if err := gob.NewDecoder(bytes.NewReader(v.TxPayload)).Decode(&txs); err == nil {
				txCount = len(txs)
				for _, tx := range txs {
					fees += tx.Fee
				}
			}
			reward := 500.0 + (float64(fees)/2)/1_000_000
			response = append(response, *toJSONVertex(v, reward, txCount))
		}

		// Return paginated response with metadata
		result := map[string]interface{}{
			"blocks":     response,
			"page":       page,
			"limit":      limit,
			"total":      total,
			"totalPages": (total + limit - 1) / limit,
		}
		json.NewEncoder(w).Encode(result)
	})

	// 3. REST API: Stats
	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {

		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")

		// Simple stats implementation
		all := dag.GetAllVertices()
		stats := Stats{
			TotalBlocks: len(all),
			TPS:         1500, // Placeholder or calculated
			Validators:  5,
			Latency:     50,
		}
		json.NewEncoder(w).Encode(stats)
	})

	// 4. REST API: Account Details
	mux.HandleFunc("/api/account", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")

		addrStr := r.URL.Query().Get("addr")
		if addrStr == "" {
			http.Error(w, "addr required", http.StatusBadRequest)
			return
		}

		// Decode Hex Address
		b, err := hex.DecodeString(addrStr)
		if err != nil || len(b) != 32 {
			http.Error(w, "invalid address hex", http.StatusBadRequest)
			return
		}
		var addr [32]byte
		copy(addr[:], b)

		bal := state.GetBalance(addr)
		stake := state.GetStake(addr)
		mu.RLock()
		recs := history.Get(addrStr)
		mu.RUnlock()

		// Sort desc
		// (Skipped for brevity, frontend can sort)

		// Get Extended State
		grant := state.GetGrantStake(addr)
		unbonding, _ := state.GetUnbonding(addr)

		// Get Delegations (This is inefficient because State doesn't expose "Get All Delegations for Delegator" easily without iteration?
		// Actually, I added Delegations map to Account struct in state.go!
		// But StateManager.GetAccount is internal.
		// I need to use `state.GetDelegation` but that is for specific validator.
		// In `state.go`, I added `Delegations map[[32]byte]uint64` to `Account`.
		// But `GetDelegation` gets one.
		// I should rely on the fact that `state.Accounts` is accessible via mutex or add `GetAllDelegations`.
		// `state.Accounts` is public but mutex protected.

		delegations := make(map[string]float64)
		state.IterateDelegations(addr, func(validator [32]byte, amount uint64) {
			delegations[hex.EncodeToString(validator[:])] = float64(amount) / 1_000_000
		})

		resp := AccountResp{
			Address:     addrStr,
			Balance:     float64(bal) / 1_000_000,
			Stake:       float64(stake) / 1_000_000,
			GrantStake:  float64(grant) / 1_000_000,
			Unbonding:   float64(unbonding) / 1_000_000,
			Delegations: delegations,
			History:     recs,
		}
		json.NewEncoder(w).Encode(resp)
	})

	// 5. REST API: Submit Transaction (Broadcast)
	mux.HandleFunc("/api/tx", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}

		var tx types.Transaction
		if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
			http.Error(w, `{"error":"invalid tx json"}`, http.StatusBadRequest)
			return
		}

		// SECURITY: Validate signature before accepting
		if len(tx.Sig) != 64 {
			http.Error(w, `{"error":"invalid signature length"}`, http.StatusBadRequest)
			return
		}

		// Verify Ed25519 signature
		msg := tx.SerializeForSigning()
		if !verifyEd25519Signature(tx.From[:], msg, tx.Sig) {
			http.Error(w, `{"error":"invalid signature"}`, http.StatusBadRequest)
			return
		}

		// Validate minimum fee
		if tx.Fee < tpu.MinFee {
			http.Error(w, `{"error":"fee too low, minimum is 1000 nanoNVN"}`, http.StatusBadRequest)
			return
		}

		// Validate amount > 0 for transfers
		if tx.Type == types.TxTransfer && tx.Amount == 0 {
			http.Error(w, `{"error":"transfer amount must be greater than 0"}`, http.StatusBadRequest)
			return
		}

		if mempool != nil {
			if success := mempool.Add(tx); success {
				json.NewEncoder(w).Encode(map[string]string{"status": "ok", "hash": hex.EncodeToString(tx.Sig[:32])})
			} else {
				http.Error(w, `{"error":"tx rejected (duplicate, invalid nonce, or pool full)"}`, http.StatusServiceUnavailable)
			}
		} else {
			http.Error(w, `{"error":"mempool offline"}`, http.StatusServiceUnavailable)
		}
	})

	// 5.5. REST API: Get Transaction Details
	mux.HandleFunc("/api/transaction", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")
		hash := r.URL.Query().Get("hash")

		// 1. Check Index
		mu.RLock()
		tx, ok := txIndex.Get(hash)
		mu.RUnlock()

		if ok {
			// Construct response
			from := hex.EncodeToString(tx.From[:])
			to := hex.EncodeToString(tx.To[:])

			// Look up full record from history for timestamp
			var fullRec TxRecord
			mu.RLock()
			recs := history.Get(from)
			for _, r := range recs {
				if r.Hash == hash {
					fullRec = r // Found full record with block info
					break
				}
			}
			mu.RUnlock()

			// If we found it in history, return it (it has block info)
			if fullRec.Hash != "" {
				json.NewEncoder(w).Encode(fullRec)
				return
			}

			// Fallback: If not found in history (unlikely if in txIndex, but possible if mempool/orphan?)
			// Or maybe it's a receive only? Check To address history?
			// For now, construct partial if missing
			resp := TxRecord{
				Hash:      hash,
				Type:      getTxTypeString(tx.Type),
				From:      from,
				To:        to,
				Amount:    float64(tx.Amount) / 1_000_000,
				Fee:       float64(tx.Fee) / 1_000_000,
				Nonce:     tx.Nonce,
				Signature: hash,
				Time:      0, // Unknown if not in block history
			}
			json.NewEncoder(w).Encode(resp)
		} else {
			http.Error(w, "tx not found", http.StatusNotFound)
		}
	})

	// Initialize finality tracker
	finalityTracker := pulse.NewFinalityTracker(dag, nil)

	// Subscribe to finality tracking on new vertices
	originalOnNewVertex := dag.OnNewVertex
	dag.OnNewVertex = func(v *pulse.Vertex) {
		finalityTracker.RecordVertex(v)
		if originalOnNewVertex != nil {
			originalOnNewVertex(v)
		}
	}

	// 6. REST API: Block Details
	mux.HandleFunc("/api/block", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")
		hash := r.URL.Query().Get("hash")
		mu.RLock()
		v, ok := blockIndex.Get(hash)
		mu.RUnlock()

		if ok {
			// Calc stats on fly for now
			var txs []types.Transaction
			txCount := 0
			var fees uint64 = 0
			if err := gob.NewDecoder(bytes.NewReader(v.TxPayload)).Decode(&txs); err == nil {
				txCount = len(txs)
				for _, tx := range txs {
					fees += tx.Fee
				}
			}
			reward := 500.0 + (float64(fees)/2)/1_000_000

			json.NewEncoder(w).Encode(toJSONVertex(v, reward, txCount))
		} else {
			http.Error(w, "block not found", http.StatusNotFound)
		}
	})

	// 6.5. REST API: Block Finality Status
	mux.HandleFunc("/api/block/finality", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")
		hashStr := r.URL.Query().Get("hash")

		if hashStr == "" {
			http.Error(w, "hash required", http.StatusBadRequest)
			return
		}

		// Parse hash
		hashBytes, err := hex.DecodeString(hashStr)
		if err != nil || len(hashBytes) != 32 {
			http.Error(w, "invalid hash", http.StatusBadRequest)
			return
		}

		var hash pulse.Hash
		copy(hash[:], hashBytes)

		status := finalityTracker.GetFinalityStatus(hash)
		if status == nil {
			http.Error(w, "block not found", http.StatusNotFound)
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"hash":           hashStr,
			"confirmations":  status.Confirmations,
			"age":            status.Age,
			"validatorCount": status.ValidatorCount,
			"isFinalized":    status.IsFinalized,
			"progress":       status.Progress,
			"config": map[string]interface{}{
				"minConfirmations": 6,
				"minAge":           30,
				"minStakePercent":  66,
			},
		})
	})

	// 7. REST API: Network Peers
	mux.HandleFunc("/api/peers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")

		if p2pServer == nil {
			http.Error(w, "p2p server unavailable", http.StatusServiceUnavailable)
			return
		}

		p2pServer.PeersMutex.RLock()
		defer p2pServer.PeersMutex.RUnlock()

		type PeerInfo struct {
			Addr    string `json:"addr"`
			NodeID  string `json:"nodeID"`
			Latency int    `json:"latency"`
		}
		var list []PeerInfo
		for addr, p := range p2pServer.Peers {
			list = append(list, PeerInfo{
				Addr:    addr,
				NodeID:  p.NodeID,
				Latency: 10 + int(r.ContentLength)%50, // Fake latency for now
			})
		}
		json.NewEncoder(w).Encode(list)
	})

	// 7.5. REST API: Peer Reputation
	mux.HandleFunc("/api/peers/reputation", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")

		if p2pServer == nil {
			http.Error(w, "p2p server unavailable", http.StatusServiceUnavailable)
			return
		}

		stats := p2pServer.GetReputationStats()
		reps := p2pServer.GetPeerReputations()

		type RepInfo struct {
			NodeID        string `json:"nodeId"`
			Score         int    `json:"score"`
			ValidBlocks   int    `json:"validBlocks"`
			InvalidBlocks int    `json:"invalidBlocks"`
			ValidTxs      int    `json:"validTxs"`
			InvalidTxs    int    `json:"invalidTxs"`
			IsBanned      bool   `json:"isBanned"`
			Status        string `json:"status"`
		}

		var repList []RepInfo
		for _, rep := range reps {
			status := "normal"
			if rep.IsBanned {
				status = "banned"
			} else if rep.Score >= 75 {
				status = "trusted"
			} else if rep.Score < 0 {
				status = "suspicious"
			}

			repList = append(repList, RepInfo{
				NodeID:        rep.NodeID,
				Score:         rep.Score,
				ValidBlocks:   rep.ValidBlocks,
				InvalidBlocks: rep.InvalidBlocks,
				ValidTxs:      rep.ValidTxs,
				InvalidTxs:    rep.InvalidTxs,
				IsBanned:      rep.IsBanned,
				Status:        status,
			})
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"stats": stats,
			"peers": repList,
		})
	})

	// 7.6. REST API: Slashing Records
	mux.HandleFunc("/api/slashing", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")

		if p2pServer == nil || p2pServer.Slasher == nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"records": []interface{}{},
				"total":   0,
			})
			return
		}

		records := p2pServer.Slasher.GetAllSlashRecords()

		type SlashInfo struct {
			Validator   string `json:"validator"`
			Offense     string `json:"offense"`
			Amount      float64 `json:"amount"`
			Timestamp   int64  `json:"timestamp"`
			JailedUntil int64  `json:"jailedUntil"`
		}

		var slashList []SlashInfo
		for _, r := range records {
			offenseStr := "UNKNOWN"
			switch r.Offense {
			case 0:
				offenseStr = "DOUBLE_SIGN"
			case 1:
				offenseStr = "DOWNTIME"
			case 2:
				offenseStr = "INVALID_BLOCK"
			}

			slashList = append(slashList, SlashInfo{
				Validator:   hex.EncodeToString(r.Validator[:]),
				Offense:     offenseStr,
				Amount:      float64(r.Amount) / 1_000_000,
				Timestamp:   r.Timestamp,
				JailedUntil: r.JailedUntil,
			})
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"records": slashList,
			"total":   len(slashList),
		})
	})

	// 8. REST API: Mempool
	mux.HandleFunc("/api/mempool", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")

		if mempool == nil {
			json.NewEncoder(w).Encode([]string{})
			return
		}

		pend := mempool.GetAll()
		json.NewEncoder(w).Encode(pend)
	})

	// 9. REST API: Search
	mux.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")
		q := r.URL.Query().Get("q")

		result := map[string]string{"type": "none", "value": ""}

		// 1. Check Block
		mu.RLock()
		_, isBlock := blockIndex.Get(q)
		_, isTx := txIndex.Get(q)
		mu.RUnlock()

		if isBlock {
			result["type"] = "block"
			result["value"] = q
		} else if isTx {
			// 2. Check Tx
			result["type"] = "tx"
			result["value"] = q
		} else {
			// 3. Check Address (Length 64 hex)
			if len(q) == 64 {
				// Assume address
				result["type"] = "address"
				result["value"] = q
			}
		}
		json.NewEncoder(w).Encode(result)
	})

	// 10. REST API: Mempool Stats
	mux.HandleFunc("/api/mempool/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")

		if mempool == nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"txCount":   0,
				"totalFees": 0,
				"minFee":    0,
				"maxFee":    0,
				"avgFee":    0,
			})
			return
		}

		stats := mempool.GetStats()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"txCount":   stats.TxCount,
			"totalFees": float64(stats.TotalFees) / 1_000_000,
			"minFee":    float64(stats.MinFee) / 1_000_000,
			"maxFee":    float64(stats.MaxFee) / 1_000_000,
			"avgFee":    float64(stats.AvgFee) / 1_000_000,
		})
	})

	// ============ WALLET API ENDPOINTS ============

	// Production CORS handler - restrict origins for wallet APIs
	// In production, set SUPERNOVA_ALLOWED_ORIGINS environment variable
	// Example: SUPERNOVA_ALLOWED_ORIGINS=https://wallet.supernova.io,https://app.supernova.io
	allowedOrigins := make(map[string]bool)
	if origins := getEnvOrDefault("SUPERNOVA_ALLOWED_ORIGINS", ""); origins != "" {
		for _, origin := range splitTrim(origins, ",") {
			allowedOrigins[origin] = true
		}
	}

	corsHandler := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// If allowed origins configured, validate
			if len(allowedOrigins) > 0 {
				if allowedOrigins[origin] {
					w.Header().Set("Access-Control-Allow-Origin", origin)
				} else {
					// For non-browser requests or same-origin, allow
					if origin == "" {
						w.Header().Set("Access-Control-Allow-Origin", "*")
					}
					// Else: no CORS header = browser will block
				}
			} else {
				// Development mode: allow all (with warning logged once)
				w.Header().Set("Access-Control-Allow-Origin", "*")
			}

			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Max-Age", "86400") // Cache preflight for 24h

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			next(w, r)
		}
	}

	// 11. Wallet: Create new wallet
	mux.HandleFunc("/api/wallet/create", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"POST required"}`, http.StatusMethodNotAllowed)
			return
		}

		var req wallet.CreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		if len(req.Password) < 8 {
			http.Error(w, `{"error":"password must be at least 8 characters"}`, http.StatusBadRequest)
			return
		}

		resp, err := walletManager.Create(req.Password, req.Label)
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(resp)
	}))

	// 12. Wallet: Import from mnemonic
	mux.HandleFunc("/api/wallet/import", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"POST required"}`, http.StatusMethodNotAllowed)
			return
		}

		var req wallet.ImportRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		if len(req.Mnemonic) != 12 {
			http.Error(w, `{"error":"mnemonic must be 12 words"}`, http.StatusBadRequest)
			return
		}

		if len(req.Password) < 8 {
			http.Error(w, `{"error":"password must be at least 8 characters"}`, http.StatusBadRequest)
			return
		}

		resp, err := walletManager.Import(req.Mnemonic, req.Password, req.Label)
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
			return
		}

		json.NewEncoder(w).Encode(resp)
	}))

	// 13. Wallet: List all wallets
	mux.HandleFunc("/api/wallet/list", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		wallets := walletManager.List()
		json.NewEncoder(w).Encode(wallets)
	}))

	// 14. Wallet: Get wallet info
	mux.HandleFunc("/api/wallet/info", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		address := r.URL.Query().Get("address")
		if address == "" {
			http.Error(w, `{"error":"address required"}`, http.StatusBadRequest)
			return
		}

		info, err := walletManager.Get(address)
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusNotFound)
			return
		}

		// Also get balance from state
		var addr [32]byte
		addrBytes, _ := hex.DecodeString(address)
		copy(addr[:], addrBytes)

		balance := state.GetBalance(addr)
		stake := state.GetStake(addr)
		grant := state.GetGrantStake(addr)
		nonce := state.GetNonce(addr)

		result := map[string]interface{}{
			"wallet":  info,
			"balance": float64(balance) / 1_000_000,
			"stake":   float64(stake) / 1_000_000,
			"grant":   float64(grant) / 1_000_000,
			"nonce":   nonce,
		}
		json.NewEncoder(w).Encode(result)
	}))

	// 15. Wallet: Unlock
	mux.HandleFunc("/api/wallet/unlock", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"POST required"}`, http.StatusMethodNotAllowed)
			return
		}

		var req wallet.UnlockRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		if err := walletManager.Unlock(req.Address, req.Password); err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusUnauthorized)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"status": "unlocked"})
	}))

	// 16. Wallet: Lock
	mux.HandleFunc("/api/wallet/lock", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"POST required"}`, http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Address string `json:"address"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		if err := walletManager.Lock(req.Address); err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusNotFound)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"status": "locked"})
	}))

	// 17. Wallet: Send transaction (simplified)
	mux.HandleFunc("/api/wallet/send", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"POST required"}`, http.StatusMethodNotAllowed)
			return
		}

		var req wallet.SendRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		// Unlock if password provided
		if req.Password != "" {
			if err := walletManager.Unlock(req.From, req.Password); err != nil {
				http.Error(w, `{"error":"unlock failed: `+err.Error()+`"}`, http.StatusUnauthorized)
				return
			}
		}

		// Get current nonce
		var fromAddr [32]byte
		fromBytes, _ := hex.DecodeString(req.From)
		copy(fromAddr[:], fromBytes)
		nonce := state.GetNonce(fromAddr) + 1

		// Create and sign transaction
		tx, err := walletManager.CreateTransaction(req.From, req.To, req.Amount, req.Fee, nonce, types.TxTransfer)
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
			return
		}

		// Add to mempool
		if mempool != nil {
			if !mempool.Add(*tx) {
				http.Error(w, `{"error":"transaction rejected by mempool"}`, http.StatusServiceUnavailable)
				return
			}
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
			"txHash": hex.EncodeToString(tx.Sig[:32]),
			"nonce":  nonce,
		})
	}))

	// 18. Wallet: Sign transaction (for advanced users)
	mux.HandleFunc("/api/wallet/sign", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"POST required"}`, http.StatusMethodNotAllowed)
			return
		}

		var req wallet.SignRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		resp, err := walletManager.Sign(req.Address, &req.Tx)
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
			return
		}

		json.NewEncoder(w).Encode(resp)
	}))

	// 19. Wallet: Export private key (dangerous - requires password confirmation)
	mux.HandleFunc("/api/wallet/export", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"POST required"}`, http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Address  string `json:"address"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		// Must unlock first
		if err := walletManager.Unlock(req.Address, req.Password); err != nil {
			http.Error(w, `{"error":"invalid password"}`, http.StatusUnauthorized)
			return
		}

		privKey, err := walletManager.ExportPrivateKey(req.Address)
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{
			"privateKey": privKey,
			"warning":    "NEVER share this key. Anyone with it can steal your funds.",
		})
	}))

	// 20. Wallet: Delete (requires password)
	mux.HandleFunc("/api/wallet/delete", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"POST required"}`, http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Address  string `json:"address"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		if err := walletManager.Delete(req.Address, req.Password); err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusUnauthorized)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
	}))

	// 21. Wallet: Quick create (no encryption, for demo/testing)
	mux.HandleFunc("/api/wallet/quick", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"POST required"}`, http.StatusMethodNotAllowed)
			return
		}

		kp, address, err := wallet.QuickCreate()
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{
			"address":    address,
			"publicKey":  hex.EncodeToString(kp.PublicKey),
			"privateKey": hex.EncodeToString(kp.PrivateKey),
			"warning":    "This wallet is NOT encrypted. For production, use /api/wallet/create",
		})
	}))

	// Start server with optional TLS
	if cfg.EnableTLS && cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
		log.Printf("🔒 Explorer API running on %s with TLS", cfg.APIPort)
		log.Println("📱 Wallet API endpoints available at /api/wallet/*")
		if err := http.ListenAndServeTLS(cfg.APIPort, cfg.TLSCertFile, cfg.TLSKeyFile, mux); err != nil {
			log.Printf("TLS Server error: %v", err)
		}
	} else {
		log.Printf("🌍 Explorer API running on %s", cfg.APIPort)
		log.Println("📱 Wallet API endpoints available at /api/wallet/*")
		if cfg.EnableTLS {
			log.Println("⚠️  TLS enabled but cert/key not configured - falling back to HTTP")
		}
		http.ListenAndServe(cfg.APIPort, mux)
	}
}

type TxRecord struct {
	Hash       string  `json:"hash"`
	Type       string  `json:"type"`
	From       string  `json:"from"`
	To         string  `json:"to"`
	Amount     float64 `json:"amount"`
	Fee        float64 `json:"fee"`
	Nonce      uint64  `json:"nonce"`
	Signature  string  `json:"signature"`
	BlockHash  string  `json:"blockHash"`
	BlockRound uint64  `json:"blockRound"`
	Time       int64   `json:"timestamp"`
}

func getTxTypeString(t types.TxType) string {
	switch t {
	case types.TxTransfer:
		return "TRANSFER"
	case types.TxStake:
		return "STAKE"
	case types.TxUnstake:
		return "UNSTAKE"
	case types.TxDelegate:
		return "DELEGATE"
	case types.TxWithdraw:
		return "WITHDRAW"
	case types.TxGrant:
		return "GRANT_LICENSE"
	case types.TxBuyLicense:
		return "BUY_LICENSE"
	default:
		return "UNKNOWN"
	}
}

type AccountResp struct {
	Address     string             `json:"address"`
	Balance     float64            `json:"balance"`
	Stake       float64            `json:"stake"`
	GrantStake  float64            `json:"grantStake"`
	Unbonding   float64            `json:"unbonding"`
	Delegations map[string]float64 `json:"delegations"`
	History     []TxRecord         `json:"history"`
}

func toJSONVertex(v *pulse.Vertex, reward float64, txCount int) *JSONVertex {
	parents := make([]string, len(v.Parents))
	for i, p := range v.Parents {
		parents[i] = p.String()
	}
	return &JSONVertex{
		Hash:       v.Hash.String(),
		Timestamp:  v.Timestamp / 1_000_000, // Convert Nano to Milli for JS
		Author:     hex.EncodeToString(v.Author[:]),
		Parents:    parents,
		Rounds:     v.Round,
		MerkleRoot: hex.EncodeToString(v.MerkleRoot[:]),
		// Stats
		Reward:  reward,
		Size:    len(v.TxPayload), // Roughly payload size
		TxCount: txCount,
	}
}
