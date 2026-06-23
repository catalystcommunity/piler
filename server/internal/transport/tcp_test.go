package transport

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"

	txp "github.com/catalystcommunity/csilgen/transports/go"

	"github.com/catalystcommunity/piler/server/internal/csil"
	"github.com/catalystcommunity/piler/server/internal/messages"
)

// clientHandshake performs the initiator side of the CSIL-Events handshake over
// a carrier: send a (verbose) $hello offering both profiles, expect $hello-ack,
// and return the negotiated profile.
func clientHandshake(t *testing.T, carrier txp.FrameCarrier) txp.Profile {
	t.Helper()
	hello, err := txp.Hello{
		Versions: []uint64{txp.VERSION},
		Profiles: []string{txp.ProfileCompact.String(), txp.ProfileVerbose.String()},
	}.Encode()
	if err != nil {
		t.Fatalf("encode hello: %v", err)
	}
	frame, err := txp.NewVerboseEvent(nil, txp.HelloName, hello).Encode(txp.ProfileVerbose)
	if err != nil {
		t.Fatalf("encode hello frame: %v", err)
	}
	if err := carrier.SendFrame(frame); err != nil {
		t.Fatalf("send hello: %v", err)
	}
	ackFrame, err := carrier.RecvFrame()
	if err != nil {
		t.Fatalf("recv ack: %v", err)
	}
	ev, err := txp.DecodeEvent(ackFrame, txp.ProfileVerbose)
	if err != nil {
		t.Fatalf("decode ack: %v", err)
	}
	if ev.Event == nil || *ev.Event != txp.HelloAckName {
		t.Fatalf("first server frame = %v, want %s", ev.Event, txp.HelloAckName)
	}
	ack, err := txp.DecodeHelloAck(ev.Payload)
	if err != nil {
		t.Fatalf("decode ack payload: %v", err)
	}
	p, ok := txp.ParseProfile(ack.Profile)
	if !ok {
		t.Fatalf("unknown negotiated profile %q", ack.Profile)
	}
	return p
}

// A $hello handshake then a "check-name" op produce a "name-availability" event
// back — the round trip through the real stream carrier + handshake + dispatch,
// in whatever profile the server negotiated (default compact).
func TestTCPHandshakeThenRoundTrip(t *testing.T) {
	d := messages.New()
	d.Register("check-name", func(_ context.Context, c *messages.Conn, _ []byte) error {
		c.PushEvent("name-availability", csil.NameAvailability{Name: "x", Available: true})
		return nil
	})

	client, server := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveTCPConn(ctx, server, d, func(uint64) {})

	_ = client.SetDeadline(time.Now().Add(2 * time.Second))
	carrier := txp.NewStreamCarrier(client)
	profile := clientHandshake(t, carrier)

	// Send check-name in the negotiated profile.
	body, err := cbor.Marshal(csil.CheckNameRequest{Name: "x"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	checkOp, _ := messages.OutboundOp("name-availability") // op 1; check-name shares it
	var req txp.Event
	if profile == txp.ProfileVerbose {
		req = txp.NewVerboseEvent(nil, "check-name", body)
	} else {
		req = txp.NewCompactEvent(messages.WorldServiceOrd, checkOp, body)
	}
	reqFrame, err := req.Encode(profile)
	if err != nil {
		t.Fatalf("encode req: %v", err)
	}
	if err := carrier.SendFrame(reqFrame); err != nil {
		t.Fatalf("send req: %v", err)
	}

	respFrame, err := carrier.RecvFrame()
	if err != nil {
		t.Fatalf("recv resp: %v", err)
	}
	ev, err := txp.DecodeEvent(respFrame, profile)
	if err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	if profile == txp.ProfileVerbose {
		if ev.Event == nil || *ev.Event != "name-availability" {
			t.Fatalf("resp = %v, want name-availability", ev.Event)
		}
	} else {
		want, _ := messages.OutboundOp("name-availability")
		if ev.OpOrd == nil || *ev.OpOrd != want {
			t.Fatalf("resp op = %v, want %d (name-availability)", ev.OpOrd, want)
		}
	}
}

// A zero-length first frame is not a valid $hello: the handshake rejects it and
// the connection is torn down, rather than looping or allocating on
// attacker-supplied lengths.
func TestTCPZeroLengthFrameClosesConn(t *testing.T) {
	d := messages.New()
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
		t.Fatal("expected connection to close on a zero-length first frame")
	}
}
