package p2p

import "encoding/gob"

// MessageType represents the type of P2P message.
type MessageType uint8

const (
	MsgHandshake MessageType = 0x01
	MsgBlock     MessageType = 0x02
	MsgTx        MessageType = 0x03
	MsgGetAddr   MessageType = 0x04
	MsgAddr      MessageType = 0x05
	MsgGetDAG    MessageType = 0x06
	MsgDAG       MessageType = 0x07
)

// Message holds the type and payload of a P2P message.
type Message struct {
	Type    MessageType
	Payload []byte
}

// HandshakeData is the payload for MsgHandshake.
type HandshakeData struct {
	Version     uint32
	NodeID      string
	GenesisHash string
	Height      uint64
}

// AddrData is the payload for MsgAddr.
type AddrData struct {
	Addrs []string // List of peer addresses (e.g. "1.2.3.4:9000")
}

// DAGData is the payload for MsgDAG.
type DAGData struct {
	Vertices [][]byte // Gob-encoded vertices
}

func init() {
	gob.Register(HandshakeData{})
	gob.Register(AddrData{})
	gob.Register(DAGData{})
	// Register other types for Gob if interfaces are involved,
	// though straight structs usually work fine if fields are exported.
}
