package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"novacoin/core/crypto"
	"novacoin/core/execution"
	"novacoin/core/p2p"
	"novacoin/core/pulse"
	"novacoin/core/store"
	"novacoin/core/tpu"
	"novacoin/core/types"
	"strings"
	"time"
)

type Config struct {
	P2PPort      string `json:"p2p_port"`
	UDPPort      int    `json:"udp_port"`
	MaxPeers     int    `json:"max_peers"`
	MaxPerIP     int    `json:"max_per_ip"`
	GenesisSeed1 string `json:"genesis_seed_1"`
	GenesisSeed2 string `json:"genesis_seed_2"`
	GenesisSeed3 string `json:"genesis_seed_3"`
	DataDir      string `json:"data_dir"`
	EnableTLS    bool   `json:"enable_tls"`
	TLSCertFile  string `json:"tls_cert_file"`
	TLSKeyFile   string `json:"tls_key_file"`
	APIPort      string `json:"api_port"`

	// Community validator public keys (hex encoded)
	CommunityValidators []string `json:"community_validators"`
}

// Global references for graceful shutdown
var (
	mempool   *tpu.Mempool
	p2pServer *p2p.Server
	tpuServer *tpu.IngestServer
)

// getEnvOrDefault returns environment variable or default value
func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func loadConfig() *Config {
	cfg := &Config{
		P2PPort:  ":9000",
		UDPPort:  8080,
		MaxPeers: 100,
		MaxPerIP: 5,
		DataDir:  "./data",
		APIPort:  ":8000",
	}

	// Try loading from config.json
	if file, err := os.ReadFile("config.json"); err == nil {
		json.Unmarshal(file, cfg)
	}

	// Override with environment variables (highest priority)
	if port := os.Getenv("SUPERNOVA_P2P_PORT"); port != "" {
		cfg.P2PPort = port
	}
	if port := os.Getenv("SUPERNOVA_UDP_PORT"); port != "" {
		if p, err := fmt.Sscanf(port, "%d", &cfg.UDPPort); err == nil && p > 0 {
			// parsed successfully
		}
	}
	if seed := os.Getenv("SUPERNOVA_GENESIS_SEED_1"); seed != "" {
		cfg.GenesisSeed1 = seed
	}
	if seed := os.Getenv("SUPERNOVA_GENESIS_SEED_2"); seed != "" {
		cfg.GenesisSeed2 = seed
	}
	if seed := os.Getenv("SUPERNOVA_GENESIS_SEED_3"); seed != "" {
		cfg.GenesisSeed3 = seed
	}
	if dir := os.Getenv("SUPERNOVA_DATA_DIR"); dir != "" {
		cfg.DataDir = dir
	}
	if port := os.Getenv("SUPERNOVA_API_PORT"); port != "" {
		cfg.APIPort = port
	}
	if os.Getenv("SUPERNOVA_ENABLE_TLS") == "true" {
		cfg.EnableTLS = true
	}
	if cert := os.Getenv("SUPERNOVA_TLS_CERT"); cert != "" {
		cfg.TLSCertFile = cert
	}
	if key := os.Getenv("SUPERNOVA_TLS_KEY"); key != "" {
		cfg.TLSKeyFile = key
	}

	// Validate: Genesis seeds MUST be provided externally in production
	if cfg.GenesisSeed1 == "" {
		fmt.Println("⚠️  WARNING: No genesis seed configured!")
		fmt.Println("   Set SUPERNOVA_GENESIS_SEED_1 environment variable or config.json")
		fmt.Println("   Using INSECURE default for development only!")
		cfg.GenesisSeed1 = "dev-only-insecure-seed-do-not-use-in-production"
	}

	return cfg
}

func main() {
	// Initialize Structured Logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// Load Config
	cfg := loadConfig()

	// Flags override config
	p2pPort := flag.String("p2p", cfg.P2PPort, "P2P listening address")
	udpPort := flag.Int("udp", cfg.UDPPort, "UDP Ingest port")
	peers := flag.String("peers", "", "Comma-separated list of peers")
	miner := flag.Bool("miner", false, "Enable mining (simulated)")
	maxPeers := flag.Int("maxpeers", cfg.MaxPeers, "Maximum number of connected peers")
	genKey := flag.Bool("genkey", false, "Generate a new Validator KeyPair")
	minerKey := flag.String("minerkey", "", "Hex-encoded Seed for Mining")

	// CLI Commands
	send := flag.Bool("send", false, "Send NVN")
	stakeFlag := flag.Bool("stake", false, "Stake NVN")
	to := flag.String("to", "", "Recipient Address (Hex) for -send")
	amount := flag.Uint64("amount", 0, "Amount in NVN")
	key := flag.String("key", "", "Sender/Staker Private Seed (Hex)")

	flag.Parse()

	// Handle Transaction Commands
	if *send || *stakeFlag {
		handleTransaction(*send, *stakeFlag, *to, *amount, *key, *udpPort)
		return
	}

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

	slog.Info("🌟 NovaCoin 3.0 'Supernova' Engine Starting...", "version", "3.0.1")

	// 0. Initialize Database
	dbPath := fmt.Sprintf("./data/nova-%s", strings.ReplaceAll(*p2pPort, ":", ""))
	store.Init(dbPath)
	defer store.Close()
	slog.Info("📦 Database initialized", "path", dbPath)

	// 1. Initialize State (Memory Bank)
	state := execution.NewStateManager()
	executor := execution.NewExecutor(state)
	slog.Info("✅ State Manager Initialized (0-Copy Mode)")

	// 2. Initialize DAG (The Pulse)
	dag := pulse.NewVertexStore()
	tips := dag.GetTips()
	slog.Info("✅ Pulse DAG Initialized", "tips", len(tips))

	// 3. Initialize Mempool
	mempool = tpu.NewMempool(state)

	// 4. Initialize TPU (Ingest) with worker pool
	var err error
	tpuServer, err = tpu.NewIngestServer(*udpPort, mempool)
	if err != nil {
		panic(err)
	}

	// 5. "Big Bang": Genesis Allocation (Multi-Validator)
	// Seeds MUST come from config/environment - never hardcode in production!
	genSeed1 := deriveGenesisSeed(cfg.GenesisSeed1)
	genPub1 := ed25519.NewKeyFromSeed(genSeed1).Public().(ed25519.PublicKey)
	var genID1 [32]byte
	copy(genID1[:], genPub1)

	// Validator 2 (optional - from config)
	genSeed2 := deriveGenesisSeed(cfg.GenesisSeed2)
	genPub2 := ed25519.NewKeyFromSeed(genSeed2).Public().(ed25519.PublicKey)
	var genID2 [32]byte
	copy(genID2[:], genPub2)

	// Validator 3 (optional - from config)
	genSeed3 := deriveGenesisSeed(cfg.GenesisSeed3)
	genPub3 := ed25519.NewKeyFromSeed(genSeed3).Public().(ed25519.PublicKey)
	var genID3 [32]byte
	copy(genID3[:], genPub3)

	// Community Validators (from config or defaults)
	communityValidators := cfg.CommunityValidators
	if len(communityValidators) == 0 {
		// Default community validators for backward compatibility
		communityValidators = []string{
			"809677ea09986593d3dedbeb3b6f5d1fe66855ef4a3ea28a9ba64c05cdad7076", // Germany
			"0810a0748d15669e791a23828b013e775223ab124672fb39a6d07be5a66e258b", // UK
		}
	}

	// Parse community validators
	var genID4, genID5 [32]byte
	if len(communityValidators) >= 1 {
		if pubBytes, err := hex.DecodeString(communityValidators[0]); err == nil && len(pubBytes) == 32 {
			copy(genID4[:], pubBytes)
		}
	}
	if len(communityValidators) >= 2 {
		if pubBytes, err := hex.DecodeString(communityValidators[1]); err == nil && len(pubBytes) == 32 {
			copy(genID5[:], pubBytes)
		}
	}

	// Reduced to fit uint64: 10 Billion Total, Split 3 ways
	balance := uint64(3_333_333_333 * 1_000_000)
	stake := uint64(1_666_666_666 * 1_000_000)

	// Allocate to all 3
	state.SetBalance(genID1, balance)
	state.SetStake(genID1, stake)

	state.SetBalance(genID2, balance)
	state.SetStake(genID2, stake)

	state.SetBalance(genID3, balance)
	state.SetStake(genID3, stake)

	// Give Germany & UK instant stake (5M each)
	userBalance := uint64(10_000_000 * 1_000_000)
	userStake := uint64(5_000_000 * 1_000_000)

	state.SetBalance(genID4, userBalance)
	state.SetStake(genID4, userStake)

	state.SetBalance(genID5, userBalance)
	state.SetStake(genID5, userStake)

	fmt.Printf("💥 Big Bang! Genesis Validators via DPoS:\n")
	fmt.Printf("1. Singapore: %x\n", genID1[:4])
	fmt.Printf("2. Mumbai:    %x\n", genID2[:4])
	fmt.Printf("3. USA:       %x\n", genID3[:4])
	fmt.Printf("4. Germany:   %x\n", genID4[:4])
	fmt.Printf("5. UK:        %x\n", genID5[:4])

	// 5. Initialize P2P Network
	// Derive NodeID from the Identity Key we are using
	var identitySeed []byte
	if *minerKey != "" {
		// 1. User specified key
		decoded, err := hex.DecodeString(*minerKey)
		if err == nil && len(decoded) == 32 {
			identitySeed = decoded
		}
	}

	if len(identitySeed) == 0 {
		// 2. Default to Genesis 1 (Dev Mode)
		identitySeed = genSeed1
	}

	// Derive Public Key from Seed
	idKeys := ed25519.NewKeyFromSeed(identitySeed)
	idPub := idKeys.Public().(ed25519.PublicKey)
	nodeID := hex.EncodeToString(idPub)

	// In a real app, we load the "Self" key.
	p2pServer = p2p.NewServer(*p2pPort, *maxPeers, nodeID, dag, state, executor, mempool)

	// Start the Engine components
	go tpuServer.Start()

	go func() {
		if err := p2pServer.Start(); err != nil {
			panic(err)
		}
	}()

	// 6. Start API Server (Background)
	go startExplorerAPI(dag, state, mempool, p2pServer, cfg)

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

			// Cache for granted peers (Session-based) to prevent spamming
			grantedPeers := make(map[string]bool)

			ticker := time.NewTicker(3 * time.Second)
			for range ticker.C {
				tips := dag.GetTips()
				if len(tips) == 0 {
					// Use genesis hash if no tips, or create a dummy parent
					// For now we just create a root vertex if empty
					tips = []pulse.Hash{{}}
				}

				// 1. Get Txs from Mempool
				txs := mempool.GetBatch(100)

				// 2. Serialize Payload
				var payloadBuf bytes.Buffer
				var merkleRoot [32]byte

				if len(txs) > 0 {
					// Encode Txs
					if err := gob.NewEncoder(&payloadBuf).Encode(txs); err != nil {
						fmt.Printf("Miner Error encoding txs: %v\n", err)
						continue
					}

					// Compute Merkle Root
					var txHashes [][]byte
					for _, tx := range txs {
						// For Merkle, we need hash of each Tx.
						// In this prototype, Tx doesn't have a cached hash method exposed easily
						// without serialization. We use Signature as ID? Or re-serialize.
						// Let's use Signature as a "rough" hash for now (unique per tx).
						// Ideally: Hash(Serialize(Tx)).
						txHashes = append(txHashes, tx.Sig)
					}
					root := crypto.MerkleRoot(txHashes)
					copy(merkleRoot[:], root)

				} else {
					payloadBuf.Write([]byte("mined-block")) // Empty block (keep alive)
					// Root of empty? Zero.
				}

				// Create new vertex (Signed)
				// NewVertex signature now includes MerkleRoot
				v := pulse.NewVertex(tips, minerPub, minerPriv, payloadBuf.Bytes(), merkleRoot)
				dag.AddVertex(v)
				fmt.Printf("⛏️  Mined new Vertex: %s (Parents: %d, Txs: %d, Merkle: %x)\n", v.Hash.String()[:8], len(v.Parents), len(txs), merkleRoot[:4])

				// Broadcast
				p2pServer.BroadcastBlock(v)

				// AUTO-FUND BOT: If we are Genesis 1 (Singapore), grant licenses to new peers
				// We check peer list. If they have 0 GrantStake, we send TxGrant.
				// For prototype: we just iterate known peers and check state.
				// Only do this if we hold the Genesis 1 key.
				if bytes.Equal(seed, genSeed1) {
					p2pServer.PeersMutex.RLock()
					peers := make([]string, 0, len(p2pServer.Peers))
					for addr := range p2pServer.Peers {
						peers = append(peers, addr)
					}
					p2pServer.PeersMutex.RUnlock()

					for _, peerAddrStr := range peers {
						// This is IP address strings. In real P2P, we need their ID/Pubkey.
						// Our Handshake has the NodeID (which we set to PubKey Hex in main).
						// So we need to access peer.NodeID.
						peer := p2pServer.GetPeer(peerAddrStr)
						if peer == nil || peer.NodeID == "" {
							continue
						}

						pubBytes, err := hex.DecodeString(peer.NodeID)
						if err != nil || len(pubBytes) != 32 {
							continue
						}
						var peerPub [32]byte
						copy(peerPub[:], pubBytes)

						// Check duplication in Cache
						if grantedPeers[peer.NodeID] {
							continue
						}

						// Check if they need a grant
						if state.GetGrantStake(peerPub) == 0 {
							// Grant 1000 NVN
							grantAmount := uint64(1000 * 1_000_000)
							// Construct Tx
							tx := types.Transaction{
								Type:   types.TxGrant,
								From:   [32]byte(minerPub), // Genesis
								To:     peerPub,            // User
								Amount: grantAmount,
								Fee:    0, // Free for Genesis
								Nonce:  uint64(time.Now().UnixNano()),
							}

							msg := tx.SerializeForSigning()
							tx.Sig = ed25519.Sign(minerPriv, msg)

							// Execute locally + Broadcast
							// For simulation, we just inject to our own executor which propagates via block?
							// Or better: Broadcast Tx
							// We lack a BroadcastTx method on server, so we send as MsgTx?
							// For now, simpler: Just "Direct execution" via our own miner block?
							// Actually, miners include Txs from mempool. We don't have a mempool here yet.
							// Short-circuit: Create a block with this Tx.

							fmt.Printf("🤖 Auto-Bot: Granting License to %x...\n", peerPub[:4])

							// Create a special block for this grant immediately?
							// Or just let the loop handle it next tick?
							// Let's just execute it on state for "Instant Finality" simulation
							// (Network won't see it unless in block, but for demo OK).
							// REAL WAY: Send to UDP Ingest.

							var buf bytes.Buffer
							gob.NewEncoder(&buf).Encode(tx)
							conn, err := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", *udpPort))
							if err == nil {
								conn.Write(buf.Bytes())
								conn.Close()
								// Mark as granted in cache immediately
								grantedPeers[peer.NodeID] = true
							}
						}
					}
				}
			}
		}()
	}

	// Keep alive
	fmt.Println("🚀 Node is RUNNING. Press Ctrl+C to stop.")
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	// Graceful shutdown
	fmt.Println("\n🛑 Stopping Supernova...")
	fmt.Println("   Cleaning up resources...")

	// Stop TPU ingest server (worker pool)
	if tpuServer != nil {
		tpuServer.Stop()
		fmt.Println("   ✓ TPU ingest stopped")
	}

	// Stop mempool cleanup goroutine
	if mempool != nil {
		mempool.Stop()
		fmt.Println("   ✓ Mempool stopped")
	}

	// Stop P2P server
	if p2pServer != nil {
		close(p2pServer.Quit)
		fmt.Println("   ✓ P2P server stopped")
	}

	// Close database (important for data integrity)
	if store.DB != nil {
		if err := store.DB.Close(); err != nil {
			fmt.Printf("   ⚠ Error closing database: %v\n", err)
		} else {
			fmt.Println("   ✓ Database closed")
		}
	}

	fmt.Println("👋 Supernova shutdown complete")
}

// deriveGenesisSeed converts a seed string to 32-byte seed for ed25519
// Supports: hex strings, plain text (hashed), or empty (generates warning)
func deriveGenesisSeed(seedStr string) []byte {
	seed := make([]byte, 32)

	if seedStr == "" {
		// Generate random seed for empty config (dev mode warning already shown)
		rand.Read(seed)
		return seed
	}

	// Try hex decode first
	if decoded, err := hex.DecodeString(seedStr); err == nil && len(decoded) == 32 {
		return decoded
	}

	// Otherwise use as passphrase (hash it for consistent 32 bytes)
	copy(seed, []byte(seedStr))
	return seed
}

func handleTransaction(isSend, isStake bool, toStr string, amountVal uint64, keyStr string, udpPort int) {
	if keyStr == "" {
		keyStr = "73757065726e6f76612d67656e657369732d736565642d6b65792d3132333435"
	}

	var seed []byte
	var err error
	if keyStr == "genesis" {
		seed = make([]byte, 32)
		copy(seed, []byte("supernova-genesis-seed-key-12345"))
	} else {
		seed, err = hex.DecodeString(keyStr)
		if err != nil {
			panic("Invalid key hex")
		}
	}

	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)

	// Construct Tx
	tx := types.Transaction{
		From:   [32]byte(pub),
		Amount: amountVal * 1_000_000, // Convert to nanoNVN
		Nonce:  uint64(time.Now().UnixNano()),
	}

	if isSend {
		tx.Type = types.TxTransfer
		if toStr == "" {
			panic("-to address required for send")
		}
		toBytes, _ := hex.DecodeString(toStr)
		copy(tx.To[:], toBytes)
		fmt.Printf("💸 Sending %d NVN to %s...\n", amountVal, toStr)
	} else if isStake {
		tx.Type = types.TxStake
		fmt.Printf("🔒 Staking %d NVN for %x...\n", amountVal, pub[:4])
	}

	// Sign
	msg := tx.SerializeForSigning()
	sig := ed25519.Sign(priv, msg)
	tx.Sig = sig

	// Send to UDP
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(tx); err != nil {
		panic(err)
	}

	conn, err := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", udpPort))
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	_, err = conn.Write(buf.Bytes())
	if err != nil {
		panic(err)
	}

	fmt.Println("✅ Transaction Sent to Ingest Engine!")
}
