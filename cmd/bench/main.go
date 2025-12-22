package main

import (
	"fmt"
	"net"
	"time"
)

func main() {
	target := "127.0.0.1:8080"
	fmt.Printf("🚀 Warp Speed Spammer Targeting %s\n", target)

	conn, err := net.Dial("udp", target)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	packet := []byte("PULSE_DATA_PAYLOAD_EXAMPLE_SIG_VERIFY_ME")

	start := time.Now()
	count := 0

	for {
		_, err := conn.Write(packet)
		if err != nil {
			fmt.Println("Error:", err)
			break
		}
		count++

		if count%10000 == 0 {
			duration := time.Since(start).Seconds()
			fmt.Printf("\r🔥 RPS: %.2f | Total: %d", float64(count)/duration, count)
		}

		if count >= 1_000_000 {
			break
		}
	}
	fmt.Println("\n✅ Test Complete.")
}
