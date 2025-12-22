package p2p

import (
	"net"
)

// Transport represents the networking layer (TCP).
type Transport struct {
	ListenAddr string
	listener   net.Listener
}

// NewTransport creates a new TCP transport.
func NewTransport(addr string) *Transport {
	return &Transport{
		ListenAddr: addr,
	}
}

// Listen starts accepting incoming TCP connections.
func (t *Transport) Listen() error {
	l, err := net.Listen("tcp", t.ListenAddr)
	if err != nil {
		return err
	}
	t.listener = l
	return nil
}

// Accept returns the next incoming connection.
func (t *Transport) Accept() (net.Conn, error) {
	return t.listener.Accept()
}

// Close stops the listener.
func (t *Transport) Close() error {
	return t.listener.Close()
}

// Dial creates a connection to a remote address.
func (t *Transport) Dial(addr string) (net.Conn, error) {
	return net.Dial("tcp", addr)
}
