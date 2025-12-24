package wallet

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"novacoin/core/crypto"
	"novacoin/core/types"

	"golang.org/x/crypto/pbkdf2"
)

// Common errors
var (
	ErrWalletLocked      = errors.New("wallet is locked")
	ErrWalletNotFound    = errors.New("wallet not found")
	ErrInvalidPassword   = errors.New("invalid password")
	ErrInvalidMnemonic   = errors.New("invalid mnemonic phrase")
	ErrInsufficientFunds = errors.New("insufficient funds")
)

// Wallet represents a Supernova wallet with encrypted key storage
type Wallet struct {
	Address    string `json:"address"`
	PublicKey  string `json:"publicKey"`
	EncPrivKey []byte `json:"encryptedPrivateKey"` // AES-256-GCM encrypted
	Salt       []byte `json:"salt"`                // PBKDF2 salt
	CreatedAt  int64  `json:"createdAt"`
	Label      string `json:"label,omitempty"`

	// Runtime state (not persisted)
	privateKey ed25519.PrivateKey
	unlocked   bool
	mu         sync.RWMutex
}

// WalletInfo is a safe representation for API responses (no private data)
type WalletInfo struct {
	Address   string `json:"address"`
	PublicKey string `json:"publicKey"`
	Label     string `json:"label,omitempty"`
	CreatedAt int64  `json:"createdAt"`
	Unlocked  bool   `json:"unlocked"`
}

// CreateRequest is the request to create a new wallet
type CreateRequest struct {
	Password string `json:"password"`
	Label    string `json:"label,omitempty"`
}

// CreateResponse is the response after creating a wallet
type CreateResponse struct {
	Address   string   `json:"address"`
	PublicKey string   `json:"publicKey"`
	Mnemonic  []string `json:"mnemonic"` // 12-word seed phrase
	CreatedAt int64    `json:"createdAt"`
}

// ImportRequest is the request to import a wallet from mnemonic
type ImportRequest struct {
	Mnemonic []string `json:"mnemonic"`
	Password string   `json:"password"`
	Label    string   `json:"label,omitempty"`
}

// UnlockRequest is the request to unlock a wallet
type UnlockRequest struct {
	Address  string `json:"address"`
	Password string `json:"password"`
}

// SignRequest is the request to sign a transaction
type SignRequest struct {
	Address string            `json:"address"`
	Tx      types.Transaction `json:"transaction"`
}

// SignResponse is the response after signing
type SignResponse struct {
	TxHash    string `json:"txHash"`
	Signature string `json:"signature"`
	RawTx     string `json:"rawTx"` // Hex-encoded signed transaction
}

// SendRequest is a simplified send request
type SendRequest struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Amount   uint64 `json:"amount"`
	Fee      uint64 `json:"fee"`
	Password string `json:"password"` // Optional if wallet already unlocked
}

// BIP39-like word list (simplified - 256 words for demo, real BIP39 has 2048)
var wordList = []string{
	"abandon", "ability", "able", "about", "above", "absent", "absorb", "abstract",
	"absurd", "abuse", "access", "accident", "account", "accuse", "achieve", "acid",
	"acoustic", "acquire", "across", "act", "action", "actor", "actress", "actual",
	"adapt", "add", "addict", "address", "adjust", "admit", "adult", "advance",
	"advice", "aerobic", "affair", "afford", "afraid", "again", "age", "agent",
	"agree", "ahead", "aim", "air", "airport", "aisle", "alarm", "album",
	"alert", "alien", "all", "alley", "allow", "almost", "alone", "alpha",
	"already", "also", "alter", "always", "amateur", "amazing", "among", "amount",
	"anchor", "ancient", "anger", "angle", "angry", "animal", "ankle", "announce",
	"annual", "another", "answer", "antenna", "antique", "anxiety", "any", "apart",
	"apology", "appear", "apple", "approve", "april", "arch", "arctic", "area",
	"arena", "argue", "arm", "armed", "armor", "army", "around", "arrange",
	"arrest", "arrive", "arrow", "art", "artefact", "artist", "artwork", "ask",
	"aspect", "assault", "asset", "assist", "assume", "asthma", "athlete", "atom",
	"attack", "attend", "attitude", "attract", "auction", "audit", "august", "aunt",
	"author", "auto", "autumn", "average", "avocado", "avoid", "awake", "aware",
	"away", "awesome", "awful", "awkward", "axis", "baby", "bachelor", "bacon",
	"badge", "bag", "balance", "balcony", "ball", "bamboo", "banana", "banner",
	"bar", "barely", "bargain", "barrel", "base", "basic", "basket", "battle",
	"beach", "bean", "beauty", "because", "become", "beef", "before", "begin",
	"behave", "behind", "believe", "below", "belt", "bench", "benefit", "best",
	"betray", "better", "between", "beyond", "bicycle", "bid", "bike", "bind",
	"biology", "bird", "birth", "bitter", "black", "blade", "blame", "blanket",
	"blast", "bleak", "bless", "blind", "blood", "blossom", "blouse", "blue",
	"blur", "blush", "board", "boat", "body", "boil", "bomb", "bone",
	"bonus", "book", "boost", "border", "boring", "borrow", "boss", "bottom",
	"bounce", "box", "boy", "bracket", "brain", "brand", "brass", "brave",
	"bread", "breeze", "brick", "bridge", "brief", "bright", "bring", "brisk",
	"broccoli", "broken", "bronze", "broom", "brother", "brown", "brush", "bubble",
	"buddy", "budget", "buffalo", "build", "bulb", "bulk", "bullet", "bundle",
	"bunker", "burden", "burger", "burst", "bus", "business", "busy", "butter",
	"buyer", "buzz", "cabbage", "cabin", "cable", "cactus", "cage", "cake",
}

// Manager handles multiple wallets
type Manager struct {
	wallets  map[string]*Wallet // address -> wallet
	dataDir  string
	mu       sync.RWMutex
}

// NewManager creates a new wallet manager
func NewManager(dataDir string) *Manager {
	m := &Manager{
		wallets: make(map[string]*Wallet),
		dataDir: dataDir,
	}
	// Load existing wallets from disk
	m.loadWallets()
	return m
}

// loadWallets loads all wallets from the data directory
func (m *Manager) loadWallets() {
	if m.dataDir == "" {
		return
	}

	files, err := filepath.Glob(filepath.Join(m.dataDir, "wallet_*.json"))
	if err != nil {
		return
	}

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var w Wallet
		if err := json.Unmarshal(data, &w); err != nil {
			continue
		}
		m.wallets[w.Address] = &w
	}
}

// Create creates a new wallet with password encryption
func (m *Manager) Create(password, label string) (*CreateResponse, error) {
	// 1. Generate mnemonic (12 words)
	mnemonic := generateMnemonic(12)

	// 2. Derive seed from mnemonic
	seed := mnemonicToSeed(mnemonic)

	// 3. Generate key pair from seed
	privateKey := ed25519.NewKeyFromSeed(seed[:32])
	publicKey := privateKey.Public().(ed25519.PublicKey)

	// 4. Encrypt private key
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}

	encKey := deriveKey(password, salt)
	encPrivKey, err := encryptAESGCM(encKey, []byte(privateKey))
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt private key: %w", err)
	}

	// 5. Create wallet
	address := hex.EncodeToString(publicKey)
	wallet := &Wallet{
		Address:    address,
		PublicKey:  address,
		EncPrivKey: encPrivKey,
		Salt:       salt,
		CreatedAt:  time.Now().Unix(),
		Label:      label,
		privateKey: privateKey,
		unlocked:   true, // Newly created wallet is unlocked
	}

	// 6. Save wallet
	m.mu.Lock()
	m.wallets[address] = wallet
	m.mu.Unlock()

	if err := m.saveWallet(wallet); err != nil {
		return nil, fmt.Errorf("failed to save wallet: %w", err)
	}

	return &CreateResponse{
		Address:   address,
		PublicKey: address,
		Mnemonic:  mnemonic,
		CreatedAt: wallet.CreatedAt,
	}, nil
}

// Import imports a wallet from mnemonic phrase
func (m *Manager) Import(mnemonic []string, password, label string) (*WalletInfo, error) {
	// Validate mnemonic
	if len(mnemonic) != 12 {
		return nil, ErrInvalidMnemonic
	}

	for _, word := range mnemonic {
		found := false
		for _, w := range wordList {
			if strings.ToLower(word) == w {
				found = true
				break
			}
		}
		if !found {
			return nil, ErrInvalidMnemonic
		}
	}

	// Derive seed from mnemonic
	seed := mnemonicToSeed(mnemonic)

	// Generate key pair from seed
	privateKey := ed25519.NewKeyFromSeed(seed[:32])
	publicKey := privateKey.Public().(ed25519.PublicKey)

	// Encrypt private key
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}

	encKey := deriveKey(password, salt)
	encPrivKey, err := encryptAESGCM(encKey, []byte(privateKey))
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt private key: %w", err)
	}

	// Create wallet
	address := hex.EncodeToString(publicKey)
	wallet := &Wallet{
		Address:    address,
		PublicKey:  address,
		EncPrivKey: encPrivKey,
		Salt:       salt,
		CreatedAt:  time.Now().Unix(),
		Label:      label,
		privateKey: privateKey,
		unlocked:   true,
	}

	// Save wallet
	m.mu.Lock()
	m.wallets[address] = wallet
	m.mu.Unlock()

	if err := m.saveWallet(wallet); err != nil {
		return nil, fmt.Errorf("failed to save wallet: %w", err)
	}

	return wallet.Info(), nil
}

// Unlock decrypts and loads a wallet's private key into memory
func (m *Manager) Unlock(address, password string) error {
	m.mu.RLock()
	wallet, ok := m.wallets[address]
	m.mu.RUnlock()

	if !ok {
		return ErrWalletNotFound
	}

	wallet.mu.Lock()
	defer wallet.mu.Unlock()

	// Derive key and decrypt
	encKey := deriveKey(password, wallet.Salt)
	privKeyBytes, err := decryptAESGCM(encKey, wallet.EncPrivKey)
	if err != nil {
		return ErrInvalidPassword
	}

	wallet.privateKey = ed25519.PrivateKey(privKeyBytes)
	wallet.unlocked = true

	return nil
}

// Lock clears the private key from memory
func (m *Manager) Lock(address string) error {
	m.mu.RLock()
	wallet, ok := m.wallets[address]
	m.mu.RUnlock()

	if !ok {
		return ErrWalletNotFound
	}

	wallet.mu.Lock()
	defer wallet.mu.Unlock()

	wallet.privateKey = nil
	wallet.unlocked = false

	return nil
}

// Get returns wallet info (safe for API)
func (m *Manager) Get(address string) (*WalletInfo, error) {
	m.mu.RLock()
	wallet, ok := m.wallets[address]
	m.mu.RUnlock()

	if !ok {
		return nil, ErrWalletNotFound
	}

	return wallet.Info(), nil
}

// List returns all wallets (safe info only)
func (m *Manager) List() []*WalletInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var list []*WalletInfo
	for _, w := range m.wallets {
		list = append(list, w.Info())
	}
	return list
}

// Sign signs a transaction with the wallet's private key
func (m *Manager) Sign(address string, tx *types.Transaction) (*SignResponse, error) {
	m.mu.RLock()
	wallet, ok := m.wallets[address]
	m.mu.RUnlock()

	if !ok {
		return nil, ErrWalletNotFound
	}

	wallet.mu.RLock()
	defer wallet.mu.RUnlock()

	if !wallet.unlocked {
		return nil, ErrWalletLocked
	}

	// Set From address
	pubKey, _ := hex.DecodeString(wallet.PublicKey)
	copy(tx.From[:], pubKey)

	// Sign
	sigData := tx.SerializeForSigning()
	signature := ed25519.Sign(wallet.privateKey, sigData)
	tx.Sig = signature

	return &SignResponse{
		TxHash:    hex.EncodeToString(signature[:32]),
		Signature: hex.EncodeToString(signature),
		RawTx:     hex.EncodeToString(sigData),
	}, nil
}

// CreateTransaction creates a signed transaction ready for broadcast
func (m *Manager) CreateTransaction(from, to string, amount, fee, nonce uint64, txType types.TxType) (*types.Transaction, error) {
	m.mu.RLock()
	wallet, ok := m.wallets[from]
	m.mu.RUnlock()

	if !ok {
		return nil, ErrWalletNotFound
	}

	wallet.mu.RLock()
	defer wallet.mu.RUnlock()

	if !wallet.unlocked {
		return nil, ErrWalletLocked
	}

	// Parse addresses
	fromBytes, err := hex.DecodeString(from)
	if err != nil || len(fromBytes) != 32 {
		return nil, errors.New("invalid from address")
	}

	toBytes, err := hex.DecodeString(to)
	if err != nil || len(toBytes) != 32 {
		return nil, errors.New("invalid to address")
	}

	var fromAddr, toAddr [32]byte
	copy(fromAddr[:], fromBytes)
	copy(toAddr[:], toBytes)

	tx := &types.Transaction{
		Type:   txType,
		From:   fromAddr,
		To:     toAddr,
		Amount: amount,
		Fee:    fee,
		Nonce:  nonce,
	}

	// Sign
	sigData := tx.SerializeForSigning()
	tx.Sig = ed25519.Sign(wallet.privateKey, sigData)

	return tx, nil
}

// saveWallet persists wallet to disk
func (m *Manager) saveWallet(w *Wallet) error {
	if m.dataDir == "" {
		return nil // In-memory only mode
	}

	// Ensure directory exists
	if err := os.MkdirAll(m.dataDir, 0700); err != nil {
		return err
	}

	// Save to file
	filename := filepath.Join(m.dataDir, fmt.Sprintf("wallet_%s.json", w.Address[:16]))
	data, err := json.MarshalIndent(w, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filename, data, 0600)
}

// Delete removes a wallet
func (m *Manager) Delete(address, password string) error {
	// Verify password first
	if err := m.Unlock(address, password); err != nil {
		return err
	}

	m.mu.Lock()
	delete(m.wallets, address)
	m.mu.Unlock()

	// Remove file
	if m.dataDir != "" {
		filename := filepath.Join(m.dataDir, fmt.Sprintf("wallet_%s.json", address[:16]))
		os.Remove(filename)
	}

	return nil
}

// ExportPrivateKey exports the private key (requires unlock)
func (m *Manager) ExportPrivateKey(address string) (string, error) {
	m.mu.RLock()
	wallet, ok := m.wallets[address]
	m.mu.RUnlock()

	if !ok {
		return "", ErrWalletNotFound
	}

	wallet.mu.RLock()
	defer wallet.mu.RUnlock()

	if !wallet.unlocked {
		return "", ErrWalletLocked
	}

	return hex.EncodeToString(wallet.privateKey), nil
}

// Info returns safe wallet info
func (w *Wallet) Info() *WalletInfo {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return &WalletInfo{
		Address:   w.Address,
		PublicKey: w.PublicKey,
		Label:     w.Label,
		CreatedAt: w.CreatedAt,
		Unlocked:  w.unlocked,
	}
}

// Utility functions

func generateMnemonic(wordCount int) []string {
	words := make([]string, wordCount)
	for i := 0; i < wordCount; i++ {
		idx := make([]byte, 1)
		rand.Read(idx)
		words[i] = wordList[int(idx[0])%len(wordList)]
	}
	return words
}

func mnemonicToSeed(mnemonic []string) [64]byte {
	phrase := strings.Join(mnemonic, " ")
	hash := sha256.Sum256([]byte(phrase))
	var seed [64]byte
	copy(seed[:32], hash[:])
	// Double hash for more entropy
	hash2 := sha256.Sum256(hash[:])
	copy(seed[32:], hash2[:])
	return seed
}

func deriveKey(password string, salt []byte) []byte {
	return pbkdf2.Key([]byte(password), salt, 100000, 32, sha256.New)
}

func encryptAESGCM(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func decryptAESGCM(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// QuickCreate creates a wallet without encryption (for testing/demo)
func QuickCreate() (*crypto.KeyPair, string, error) {
	kp, err := crypto.GenerateKeyPair()
	if err != nil {
		return nil, "", err
	}
	return kp, hex.EncodeToString(kp.PublicKey), nil
}
