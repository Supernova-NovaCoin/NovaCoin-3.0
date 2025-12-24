package main

import (
	"bytes"
	"encoding/gob"
	"encoding/hex"
	"flag"
	"fmt"
	"net"
	"os"

	"novacoin/core/crypto"
	"novacoin/core/types"
)

func main() {
	keygenCmd := flag.NewFlagSet("keygen", flag.ExitOnError)
	sendCmd := flag.NewFlagSet("send", flag.ExitOnError)

	sendPriv := sendCmd.String("priv", "", "Private Key (Hex)")
	sendTo := sendCmd.String("to", "", "Receiver Address (Hex)")
	sendAmount := sendCmd.Uint64("amount", 0, "Amount to send")
	sendNonce := sendCmd.Uint64("nonce", 1, "Nonce")

	if len(os.Args) < 2 {
		fmt.Println("Usage: wallet <command> [args]")
		fmt.Println("Commands: keygen, send")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "keygen":
		keygenCmd.Parse(os.Args[2:])
		runKeygen()
	case "send":
		sendCmd.Parse(os.Args[2:])
		runSend(*sendPriv, *sendTo, *sendAmount, *sendNonce)
	default:
		fmt.Println("Unknown command")
		os.Exit(1)
	}
}

func runKeygen() {
	kp, err := crypto.GenerateKeyPair()
	if err != nil {
		fmt.Println("Error generating key:", err)
		return
	}

	fmt.Println("🔑 New Supernova Wallet Generated")
	fmt.Printf("Public Address: %x\n", kp.PublicKey)
	fmt.Printf("Private Key:    %x\n", kp.PrivateKey)
	fmt.Println("⚠️  SAVE YOUR PRIVATE KEY! IT CANNOT BE RECOVERED.")
}

func runSend(privKeyHex, toHex string, amount, nonce uint64) {
	if privKeyHex == "" || toHex == "" || amount == 0 {
		fmt.Println("Usage: wallet send -priv <KEY> -to <ADDR> -amount <AMT>")
		return
	}

	// 1. Decode Keys
	privBytes, _ := hex.DecodeString(privKeyHex)
	toBytes, _ := hex.DecodeString(toHex)

	// Reconstruct KeyPair (Simplified for prototype: we assume pubkey is derived or known)
	// For sending, we just need the private key to sign.
	// But transaction structure needs 'From'. In production, derive pub from priv.
	// Here we will mock 'From' derived if library allows or just use a placeholder
	// Actually ed25519 private key includes public key.

	kp := crypto.KeyPair{PrivateKey: privBytes}
	// Ed25519 private key is 64 bytes: 32 bytes seed + 32 bytes pubkey
	if len(privBytes) == 64 {
		kp.PublicKey = privBytes[32:]
	} else {
		fmt.Println("Invalid Private Key Length")
		return
	}

	var fromAddr [32]byte
	var toAddr [32]byte
	copy(fromAddr[:], kp.PublicKey)
	copy(toAddr[:], toBytes)

	// 2. Create Transaction
	tx := types.Transaction{
		From:   fromAddr,
		To:     toAddr,
		Amount: amount,
		Nonce:  nonce,
	}

	// 3. Sign
	sig := kp.Sign(tx.SerializeForSigning())
	tx.Sig = sig

	// 4. Serialize for Network (Simple JSON or Gob for now, or just raw struct)
	// For this prototype, we'll just send the raw struct via Gob

	// 5. Broadcast (UDP Client)
	sendToNetwork(tx)
}

func sendToNetwork(tx types.Transaction) {
	conn, err := net.Dial("udp", "127.0.0.1:8080")
	if err != nil {
		fmt.Println("Error connecting to node:", err)
		return
	}
	defer conn.Close()

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(tx); err != nil {
		fmt.Println("Error encoding tx:", err)
		return
	}

	conn.Write(buf.Bytes())
	fmt.Println("✅ Transaction Signed & Broadcasted (Gob Binary)!")
	fmt.Printf("Tx Hash: %x\n", tx.Sig)
}
