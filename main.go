package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"

	"os"
	"os/signal"
	"syscall"

	"novacoin/core/execution"
	"novacoin/core/p2p"
	"novacoin/core/pulse"
	"novacoin/core/store"
	"novacoin/core/tpu"

	"strings"
	"time"
)

func main() {
	p2pPort := flag.String("p2p", ":9000", "P2P listening address (e.g. :9000)")
	udpPort := flag.Int("udp", 8080, "UDP Ingest port")
	peers := flag.String("peers", "", "Comma-separated list of peers to connect to")
	miner := flag.Bool("miner", false, "Enable mining (simulated)")
	maxPeers := flag.Int("maxpeers", 100, "Maximum number of connected peers")
	genKey := flag.Bool("genkey", false, "Generate a new Validator KeyPair")
	minerKey := flag.String("minerkey", "", "Hex-encoded Seed for Mining (32 bytes)")
	flag.Parse()

	if *genKey {
		seed := make([]byte, 32)
		if _, err := rand.Read(seed); err != nil {
			panic(err)
		}
		priv := ed25519.NewKeyFromSeed(seed)
		pub := priv.Public().(ed25519.PublicKey)

		fmt.Println("🔑 New Validator Identity Generated")
		fmt.Printf("SEED (Save this!): %s\n", hex.EncodeToString(seed))
		fmt.Printf("ADDRESS (Public):  %x\n", pub)
		return
	}

	fmt.Println("🌟 NovaCoin 3.0 'Supernova' Engine Starting...")

	// 0. Initialize Database
	dbPath := fmt.Sprintf("./data/nova-%s", strings.ReplaceAll(*p2pPort, ":", ""))
	store.Init(dbPath)
	defer store.Close()
	fmt.Printf("📦 Database initialized at %s\n", dbPath)

	// 1. Initialize State (Memory Bank)

	state := execution.NewStateManager()
	executor := execution.NewExecutor(state)
	fmt.Printf("✅ State Manager Initialized (0-Copy Mode). Executor: %v\n", executor)

	// 2. Initialize DAG (The Pulse)
	dag := pulse.NewVertexStore()
	fmt.Printf("✅ Pulse DAG Initialized. Tips: %d\n", len(dag.GetTips()))

	// 3. Initialize TPU (Ingest)
	// 3. Initialize TPU (Ingest)
	tpuServer, err := tpu.NewIngestServer(*udpPort)
	if err != nil {
		panic(err)
	}

	// 4. "Big Bang": Genesis Allocation (MUST happen before P2P starts)
	genesisSeed := make([]byte, 32)
	copy(genesisSeed, []byte("supernova-genesis-seed-key-12345"))
	genesisPriv := ed25519.NewKeyFromSeed(genesisSeed)
	genesisPub := genesisPriv.Public().(ed25519.PublicKey)

	var genesisID [32]byte
	copy(genesisID[:], genesisPub)

	// Reduced to fit uint64: 10 Billion * 1 Million
	balance := uint64(10_000_000_000 * 1_000_000)
	stake := uint64(5_000_000 * 1_000_000)

	state.SetBalance(genesisID, balance)
	state.SetStake(genesisID, stake) // 5 Million NVN Staked

	fmt.Printf("💥 Big Bang! Genesis Validator: %x\n", genesisID[:4])
	fmt.Printf("💰 Allocation: %d NVN (Stake: %d)\n",
		state.GetBalance(genesisID)/1_000_000,
		state.GetStake(genesisID)/1_000_000)

	// 5. Initialize P2P Network
	p2pServer := p2p.NewServer(*p2pPort, *maxPeers, dag, state, executor)

	// Start the Engine components
	go tpuServer.Start()

	go func() {
		if err := p2pServer.Start(); err != nil {
			panic(err)
		}
	}()

	// Connect to peers
	if *peers != "" {
		peerList := strings.Split(*peers, ",")
		for _, p := range peerList {
			fmt.Printf("🔗 Connecting to peer: %s\n", p)
			if err := p2pServer.Connect(strings.TrimSpace(p)); err != nil {
				fmt.Printf("⚠️ Failed to connect to %s: %v\n", p, err)
			}
		}
	}

	// 5. Miner Simulation
	if *miner {
		go func() {

			// GENERATE OR LOAD MINER KEY
			seed := make([]byte, 32)

			if *minerKey != "" {
				// Use provided key
				decoded, err := hex.DecodeString(*minerKey)
				if err != nil || len(decoded) != 32 {
					panic("Invalid miner key: must be 32-byte hex string")
				}
				copy(seed, decoded)
				fmt.Println("🔑 Mining with User Key")
			} else {
				// Use Genesis Key (Dev Mode)
				copy(seed, []byte("supernova-genesis-seed-key-12345"))
				fmt.Println("⚠️  WARNING: Mining with Dev Key (UNSAFE). Use -minerkey to specify your own.")
			}

			minerPriv := ed25519.NewKeyFromSeed(seed)
			minerPub := minerPriv.Public().(ed25519.PublicKey)

			// Note: We already gave stake to this key below in the main init
			fmt.Printf("⛏️  Miner Started! ID: %x\n", minerPub[:4])

			ticker := time.NewTicker(3 * time.Second)
			for range ticker.C {
				tips := dag.GetTips()
				if len(tips) == 0 {
					// Use genesis hash if no tips, or create a dummy parent
					// For now we just create a root vertex if empty
					tips = []pulse.Hash{{}}
				}

				// Create new vertex (Signed)
				v := pulse.NewVertex(tips, minerPub, minerPriv, []byte("mined-block"))
				dag.AddVertex(v)
				fmt.Printf("⛏️  Mined new Vertex: %s (Parents: %d)\n", v.Hash.String()[:8], len(v.Parents))

				// Broadcast
				p2pServer.BroadcastBlock(v)
			}
		}()
	}

	// Keep alive
	fmt.Println("🚀 Node is RUNNING. Press Ctrl+C to stop.")
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	fmt.Println("\n🛑 Stopping Supernova...")
}
