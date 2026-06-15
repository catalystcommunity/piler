package transport

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"

	"github.com/catalystcommunity/piler/server/internal/csil"
	"github.com/catalystcommunity/piler/server/internal/rpc"
)

// writeFramed sends a 4-byte big-endian length prefix + payload.
func writeFramed(t *testing.T, w io.Writer, payload []byte) {
	t.Helper()
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if _, err := w.Write(payload); err != nil {
		t.Fatalf("write payload: %v", err)
	}
}

// readFramed reads one length-prefixed frame.
func readFramed(t *testing.T, r io.Reader) []byte {
	t.Helper()
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		t.Fatalf("read header: %v", err)
	}
	n := binary.BigEndian.Uint32(hdr[:])
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		t.Fatalf("read payload: %v", err)
	}
	return buf
}

// A length-prefixed CBOR ClientMessage in produces a length-prefixed
// ServerMessage out — the round trip through the real framing closures.
func TestTCPFramingRoundTrip(t *testing.T) {
	d := rpc.New()
	d.Register("ping", func(_ context.Context, c *rpc.Conn, _ []byte) error {
		c.PushEvent("pong", csil.NameAvailability{Name: "x", Available: true})
		return nil
	})

	client, server := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveTCPConn(ctx, server, d, func(uint64) {})

	msg, err := cbor.Marshal(csil.ClientMessage{Kind: "ping"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	_ = client.SetDeadline(time.Now().Add(2 * time.Second))
	writeFramed(t, client, msg)

	var sm csil.ServerMessage
	if err := cbor.Unmarshal(readFramed(t, client), &sm); err != nil {
		t.Fatalf("decode server message: %v", err)
	}
	if sm.Event != "pong" {
		t.Fatalf("event = %q, want \"pong\"", sm.Event)
	}
}

// A zero-length frame is out of range: the reader rejects it and the
// connection is torn down (the socket closes), rather than looping or
// allocating on attacker-supplied lengths.
func TestTCPZeroLengthFrameClosesConn(t *testing.T) {
	d := rpc.New()
	client, server := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	closed := make(chan struct{})
	go func() {
		serveTCPConn(ctx, server, d, func(uint64) {})
		close(closed)
	}()

	_ = client.SetDeadline(time.Now().Add(2 * time.Second))
	var hdr [4]byte // length 0
	if _, err := client.Write(hdr[:]); err != nil {
		t.Fatalf("write zero header: %v", err)
	}

	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("expected connection to close on a zero-length frame")
	}
}
