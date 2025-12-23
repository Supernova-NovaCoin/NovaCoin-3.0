package main

import (
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"novacoin/core/execution"
	"novacoin/core/pulse"

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

	// Subscribe to DAG events
	dag.OnNewVertex = func(v *pulse.Vertex) {
		jv := toJSONVertex(v)
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

	log.Println("🌍 Explorer API running on :8000")
	http.ListenAndServe(":8000", mux)
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
