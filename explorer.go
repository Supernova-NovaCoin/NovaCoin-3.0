package main

import (
	"bytes"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"novacoin/core/execution"
	"novacoin/core/p2p"
	"novacoin/core/pulse"
	"novacoin/core/tpu"
	"novacoin/core/types"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// JSONVertex is a frontend-friendly representation of a Block/Vertex
type JSONVertex struct {
	Hash      string   `json:"hash"`
	Timestamp int64    `json:"timestamp"`
	Author    string   `json:"author"`
	Parents   []string `json:"parents"`
	Round     uint64   `json:"round"`
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

func startExplorerAPI(dag *pulse.VertexStore, state *execution.StateManager, mempool *tpu.Mempool, p2pServer *p2p.Server) {
	// WebSocket Hub
	clients := make(map[*websocket.Conn]bool)
	broadcast := make(chan *JSONVertex)

	// Indexer (Simple In-Memory)
	history := make(map[string][]TxRecord)        // Address -> Txs
	txIndex := make(map[string]types.Transaction) // TxHash -> Tx (For Search)
	blockIndex := make(map[string]*pulse.Vertex)  // Hash -> Vertex (Fast Lookup)

	// Rebuild Index from DAG
	all := dag.GetAllVertices()
	for _, v := range all {
		auth := hex.EncodeToString(v.Author[:])
		blockIndex[v.Hash.String()] = v

		// 1. Mining Reward
		history[auth] = append(history[auth], TxRecord{
			Hash: v.Hash.String(),
			Type: "MINING",
			From: "SYSTEM",
			To:   auth,
			Amt:  100, // Fixed Reward (TODO: Calculate dynamic)
			Time: v.Timestamp,
		})

		// 2. Checks for real Txs in Payload (if any)
		var txs []types.Transaction
		if err := gob.NewDecoder(bytes.NewReader(v.TxPayload)).Decode(&txs); err == nil {
			for _, tx := range txs {
				txHash := hex.EncodeToString(tx.Sig) // Use Sig as Hash
				txIndex[txHash] = tx

				from := hex.EncodeToString(tx.From[:])
				to := hex.EncodeToString(tx.To[:]) // If 0000.. it's system/burn
				rec := TxRecord{
					Hash: txHash,
					Type: "TRANSFER", // Todo: Map type
					From: from,
					To:   to,
					Amt:  float64(tx.Amount) / 1_000_000,
					Time: v.Timestamp,
				}
				history[from] = append(history[from], rec)
				if to != "0000000000000000000000000000000000000000000000000000000000000000" {
					history[to] = append(history[to], rec)
				}
			}
		} else {
			// Try single tx decode for backward compatibility or miner empty block
			// Miner empty block is just []byte("mined-block") which fails decode
		}
	}

	// Subscribe to DAG events
	dag.OnNewVertex = func(v *pulse.Vertex) {
		// Update Index
		blockIndex[v.Hash.String()] = v

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
				txIndex[txHash] = tx

				from := hex.EncodeToString(tx.From[:])
				to := hex.EncodeToString(tx.To[:])
				rec := TxRecord{
					Hash: txHash,
					Type: "TRANSFER",
					From: from,
					To:   to,
					Amt:  float64(tx.Amount) / 1_000_000,
					Time: v.Timestamp,
				}
				history[from] = append(history[from], rec)
				if to != "0000000000000000000000000000000000000000000000000000000000000000" {
					history[to] = append(history[to], rec)
				}
			}
		}

		// Reward (Approx currently 500 + 50% fees)
		// We use static 500 for display
		reward := 500.0 + (float64(fees)/2)/1_000_000

		jv := toJSONVertex(v, reward, txCount)
		broadcast <- jv

		// Mining Record
		auth := hex.EncodeToString(v.Author[:])
		history[auth] = append(history[auth], TxRecord{
			Hash: v.Hash.String(),
			Type: "MINING",
			From: "SYSTEM",
			To:   auth,
			Amt:  reward,
			Time: v.Timestamp,
		})
	}

	// Broadcaster
	go func() {
		for {
			jv := <-broadcast
			// jv is already structured, WriteJSON handles marshaling
			// For now, let's send the object directly or use a type field
			// The React app expects: "TX:..." or object?
			// Existing React app parsed "TX:" string. We will update React to handle JSON.

			for client := range clients {
				err := client.WriteJSON(jv)
				if err != nil {
					client.Close()
					delete(clients, client)
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
		clients[ws] = true
	})

	// 2. REST API: Recent Blocks
	mux.HandleFunc("/api/blocks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")

		recent := dag.GetRecentVertices()
		var response []JSONVertex
		for _, v := range recent {
			// Calc stats on fly (duplicate logic, but fine for MVP)
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
		json.NewEncoder(w).Encode(response)
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
		recs := history[addrStr]

		// Sort desc
		// (Skipped for brevity, frontend can sort)

		resp := AccountResp{
			Address: addrStr,
			Balance: float64(bal) / 1_000_000,
			Stake:   float64(stake) / 1_000_000,
			History: recs,
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

		// Accept JSON for Web Clients (easier than Gob for JS)
		// We need a JSON-friendly Tx struct or custom unmarshal?
		// Types.Transaction has byte arrays usually [32]byte, which JSON marshals as base64 string.
		// Let's assume the client sends a compatible JSON structure.
		var tx types.Transaction
		if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
			http.Error(w, "invalid tx json", http.StatusBadRequest)
			return
		}

		// Validate (Basic)
		// Signature check happens in Mempool/Ingest usually, or we can simply trust passing to Mempool which validates duplicates.
		// Real system would do full SigVerify here too.

		if mempool != nil {
			if success := mempool.Add(tx); success {
				json.NewEncoder(w).Encode(map[string]string{"status": "ok", "hash": hex.EncodeToString(tx.Sig)})
			} else {
				http.Error(w, "tx rejected (duplicate or pool full)", http.StatusServiceUnavailable)
			}
		} else {
			http.Error(w, "mempool offline", http.StatusServiceUnavailable)
		}
	})

	// 6. REST API: Block Details
	mux.HandleFunc("/api/block", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")
		hash := r.URL.Query().Get("hash")
		if v, ok := blockIndex[hash]; ok {
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
		if _, ok := blockIndex[q]; ok {
			result["type"] = "block"
			result["value"] = q
		} else if _, ok := txIndex[q]; ok {
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
	})

	log.Println("🌍 Explorer API running on :8000")
	http.ListenAndServe(":8000", mux)
}

type TxRecord struct {
	Hash string  `json:"hash"`
	Type string  `json:"type"`
	From string  `json:"from"`
	To   string  `json:"to"`
	Amt  float64 `json:"amount"`
	Time int64   `json:"time"`
}

type AccountResp struct {
	Address string     `json:"address"`
	Balance float64    `json:"balance"`
	Stake   float64    `json:"stake"`
	History []TxRecord `json:"history"`
}

func toJSONVertex(v *pulse.Vertex, reward float64, txCount int) *JSONVertex {
	parents := make([]string, len(v.Parents))
	for i, p := range v.Parents {
		parents[i] = p.String()
	}
	return &JSONVertex{
		Hash:      v.Hash.String(),
		Timestamp: v.Timestamp,
		Author:    hex.EncodeToString(v.Author[:]),
		Parents:   parents,
		Round:     v.Round,
		// Stats
		Reward:  reward,
		Size:    len(v.TxPayload), // Roughly payload size
		TxCount: txCount,
	}
}
