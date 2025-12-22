package main

import (
	"fmt"
	"net"
	"net/http"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func main() {
	// UDP Connection to Supernova Node
	udpAddr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:8080")
	udpConn, _ := net.DialUDP("udp", nil, udpAddr)
	defer udpConn.Close()

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		fmt.Println("Web Client Connected")

		for {
			_, msg, err := ws.ReadMessage()
			if err != nil {
				break
			}

			// Forward to Pulse Engine via UDP
			fmt.Printf("Bridge: Forwarding %d bytes to Supernova\n", len(msg))
			udpConn.Write(msg)

			// Ack back to Web
			ws.WriteMessage(websocket.TextMessage, []byte("✅ Sent to Supernova"))
		}
	})

	fmt.Println("🌉 Supernova Gateway running on :3000")
	http.ListenAndServe(":3000", nil)
}
