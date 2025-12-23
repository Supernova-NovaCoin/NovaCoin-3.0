```go
package main

import (
	"bytes"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"novacoin/core/execution"
	"novacoin/core/pulse"
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
}

type Stats struct {
	TotalBlocks int `json:"totalBlocks"`
	TPS         int `json:"tps"`
	Validators  int `json:"validators"`
	Latency     int `json:"latency"`
}

func startExplorerAPI(dag *pulse.VertexStore, state *execution.StateManager) {
	// WebSocket Hub
	clients := make(map[*websocket.Conn]bool)
	broadcast := make(chan *JSONVertex)

	// Indexer (Simple In-Memory)
	history := make(map[string][]TxRecord)

	// Rebuild Index from DAG
	all := dag.GetAllVertices()
	for _, v := range all {
		auth := hex.EncodeToString(v.Author[:])
		// 1. Mining Reward
		history[auth] = append(history[auth], TxRecord{
			Hash: v.Hash.String(),
			Type: "MINING",
			From: "SYSTEM",
			To:   auth,
			Amt:  100, // Fixed Reward
			Time: v.Timestamp,
		})

		// 2. Checks for real Txs in Payload (if any)
		var tx types.Transaction
		if err := gob.NewDecoder(bytes.NewReader(v.TxPayload)).Decode(&tx); err == nil {
			from := hex.EncodeToString(tx.From[:])
			to := hex.EncodeToString(tx.To[:])
			rec := TxRecord{
				Hash: v.Hash.String(),
				Type: "TRANSFER",
				From: from,
				To:   to,
				Amt:  float64(tx.Amount) / 1_000_000,
				Time: v.Timestamp,
			}
			history[from] = append(history[from], rec)
			history[to] = append(history[to], rec)
		}
	}

	// Subscribe to DAG events
	dag.OnNewVertex = func(v *pulse.Vertex) {
		jv := toJSONVertex(v)
		broadcast <- jv

		// Update Index
		auth := hex.EncodeToString(v.Author[:])
		history[auth] = append(history[auth], TxRecord{
			Hash: v.Hash.String(),
			Type: "MINING",
			From: "SYSTEM",
			To:   auth,
			Amt:  100,
			Time: v.Timestamp,
		})

		// 2. Checks for real Txs in Payload (if any)
		var tx types.Transaction
		if err := gob.NewDecoder(bytes.NewReader(v.TxPayload)).Decode(&tx); err == nil {
			from := hex.EncodeToString(tx.From[:])
			to := hex.EncodeToString(tx.To[:])
			rec := TxRecord{
				Hash: v.Hash.String(),
				Type: "TRANSFER",
				From: from,
				To:   to,
				Amt:  float64(tx.Amount) / 1_000_000,
				Time: v.Timestamp,
			}
			history[from] = append(history[from], rec)
			history[to] = append(history[to], rec)
		}
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
			response = append(response, *toJSONVertex(v))
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

func toJSONVertex(v *pulse.Vertex) *JSONVertex {
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
	}
}
