package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/gob"
	"encoding/hex"
	"flag"
	"fmt"
	"net"

	"os"
	"os/signal"
	"syscall"

	"novacoin/core/execution"
	"novacoin/core/p2p"
	"novacoin/core/pulse"
	"novacoin/core/store"
	"novacoin/core/tpu"
	"novacoin/core/types"

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

	// 3. Initialize Mempool
	// 3. Initialize Mempool
	mempool := tpu.NewMempool(state)

	// 4. Initialize TPU (Ingest)
	// We need to pass mempool to ingestion so UDP txs go there
	tpuServer, err := tpu.NewIngestServer(*udpPort, mempool)
	if err != nil {
		panic(err)
	}

	// 5. "Big Bang": Genesis Allocation (Multi-Validator)
	// Validator 1: Singapore (Dev Key)
	genSeed1 := make([]byte, 32)
	copy(genSeed1, []byte("supernova-genesis-seed-key-12345"))
	genPub1 := ed25519.NewKeyFromSeed(genSeed1).Public().(ed25519.PublicKey)
	var genID1 [32]byte
	copy(genID1[:], genPub1)

	// Validator 2: Mumbai
	genSeed2, _ := hex.DecodeString("3456dd1fde79a7e4da4c22897cff695db252aba429ea715b68a2b8e6aaf78fdb")
	genPub2 := ed25519.NewKeyFromSeed(genSeed2).Public().(ed25519.PublicKey)
	var genID2 [32]byte
	copy(genID2[:], genPub2)

	// Validator 3: USA
	// Validator 3: USA
	genSeed3, _ := hex.DecodeString("acc7c020b65d1d18f63b1e8cbabec25f1a755006759ced445c06c4c8bbb9be32")
	genPub3 := ed25519.NewKeyFromSeed(genSeed3).Public().(ed25519.PublicKey)
	var genID3 [32]byte
	copy(genID3[:], genPub3)

	// User Miner 1: Germany
	gemPubHex := "809677ea09986593d3dedbeb3b6f5d1fe66855ef4a3ea28a9ba64c05cdad7076"
	gemPubBytes, _ := hex.DecodeString(gemPubHex)
	var genID4 [32]byte
	copy(genID4[:], gemPubBytes)

	// User Miner 2: UK
	ukPubHex := "0810a0748d15669e791a23828b013e775223ab124672fb39a6d07be5a66e258b"
	ukPubBytes, _ := hex.DecodeString(ukPubHex)
	var genID5 [32]byte
	copy(genID5[:], ukPubBytes)

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
	p2pServer := p2p.NewServer(*p2pPort, *maxPeers, nodeID, dag, state, executor, mempool)

	// Start the Engine components
	go tpuServer.Start()

	go func() {
		if err := p2pServer.Start(); err != nil {
			panic(err)
		}
	}()

	// 6. Start API Server (Background)
	go startExplorerAPI(dag, state, mempool, p2pServer)

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

				// 1. Get Txs from Mempool
				txs := mempool.GetBatch(100)

				// 2. Serialize Payload
				var payloadBuf bytes.Buffer
				if len(txs) > 0 {
					if err := gob.NewEncoder(&payloadBuf).Encode(txs); err != nil {
						fmt.Printf("Miner Error encoding txs: %v\n", err)
					}
				} else {
					payloadBuf.Write([]byte("mined-block")) // Empty block (keep alive)
				}

				// Create new vertex (Signed)
				v := pulse.NewVertex(tips, minerPub, minerPriv, payloadBuf.Bytes())
				dag.AddVertex(v)
				fmt.Printf("⛏️  Mined new Vertex: %s (Parents: %d, Txs: %d)\n", v.Hash.String()[:8], len(v.Parents), len(txs))

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
	fmt.Println("\n🛑 Stopping Supernova...")
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
