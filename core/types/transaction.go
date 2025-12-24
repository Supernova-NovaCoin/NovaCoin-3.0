package types

import (
	"bytes"
	"encoding/binary"
)

type TxType uint8

const (
	TxTransfer   TxType = 0
	TxStake      TxType = 1
	TxUnstake    TxType = 2
	TxDelegate   TxType = 3
	TxWithdraw   TxType = 4
	TxGrant      TxType = 5 // Genesis -> User (Locked)
	TxBuyLicense TxType = 6 // User -> Network (Burned/Locked)
)

// Transaction is the fundamental unit of value transfer.
// It matches the structure expected by the Execution engine.
type Transaction struct {
	Type   TxType
	From   [32]byte
	To     [32]byte
	Amount uint64
	Fee    uint64
	Nonce  uint64
	Sig    []byte
}

// Serialize returns the bytes to be signed.
// Format: Type + From + To + Amount + Fee + Nonce
func (tx *Transaction) SerializeForSigning() []byte {
	var buf bytes.Buffer
	buf.WriteByte(byte(tx.Type))
	buf.Write(tx.From[:])
	buf.Write(tx.To[:])

	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, tx.Amount)
	buf.Write(b)

	binary.BigEndian.PutUint64(b, tx.Fee)
	buf.Write(b)

	binary.BigEndian.PutUint64(b, tx.Nonce)
	buf.Write(b)

	return buf.Bytes()
}
