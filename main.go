package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"novacoin/core/execution"
	"novacoin/core/pulse"
	"novacoin/core/tpu"
)

func main() {
	fmt.Println("🌟 NovaCoin 3.0 'Supernova' Engine Starting...")

	// 1. Initialize State (Memory Bank)
	state := execution.NewStateManager()
	executor := execution.NewExecutor(state)
	fmt.Printf("✅ State Manager Initialized (0-Copy Mode). Executor: %v\n", executor)

	// 2. Initialize DAG (The Pulse)
	dag := pulse.NewVertexStore()
	fmt.Printf("✅ Pulse DAG Initialized. Tips: %d\n", len(dag.GetTips()))

	// 3. Initialize TPU (Ingest)
	tpuServer, err := tpu.NewIngestServer(8080)
	if err != nil {
		panic(err)
	}

	// Start the Engine components
	go tpuServer.Start()

	// "Big Bang": Genesis Allocation to 'You' (Simulated)
	var myKey [32]byte // Zero key for demo
	// Reduced to fit uint64: 10 Billion * 1 Million
	balance := uint64(10_000_000_000 * 1_000_000)
	state.SetBalance(myKey, balance)
	fmt.Printf("💥 Big Bang! Genesis Allocation: %d NVN to Validator 0\n", state.GetBalance(myKey)/1_000_000)

	// Keep alive
	fmt.Println("🚀 Node is RUNNING. Press Ctrl+C to stop.")
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	fmt.Println("\n🛑 Stopping Supernova...")
}
