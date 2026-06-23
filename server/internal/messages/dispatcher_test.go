package messages

import (
	"context"
	"errors"
	"testing"

	txp "github.com/catalystcommunity/csilgen/transports/go"

	"github.com/catalystcommunity/piler/server/internal/csil"
)

// inboundOpOrd finds the compact op ordinal for a client→server handler name.
func inboundOpOrd(t *testing.T, name string) uint64 {
	t.Helper()
	for op, n := range inboundOp {
		if n == name {
			return op
		}
	}
	t.Fatalf("no inbound op for %q", name)
	return 0
}

// clientFrame builds a client→server frame for handler `name` in the active
// profile (verbose: by name; compact: by op ordinal).
func clientFrame(t *testing.T, name string, payload []byte) []byte {
	t.Helper()
	var ev txp.Event
	if appProfile == txp.ProfileVerbose {
		ev = txp.NewVerboseEvent(nil, name, payload)
	} else {
		ev = txp.NewCompactEvent(WorldServiceOrd, inboundOpOrd(t, name), payload)
	}
	f, err := ev.Encode(appProfile)
	if err != nil {
		t.Fatalf("encode client frame: %v", err)
	}
	return f
}

// decodeServer resolves a server→client frame to its event name + payload across
// profiles (covers both World events and control ops).
func decodeServer(t *testing.T, b []byte) (string, []byte) {
	t.Helper()
	ev, err := txp.DecodeEvent(b, appProfile)
	if err != nil {
		t.Fatalf("decode server frame: %v", err)
	}
	if appProfile == txp.ProfileVerbose {
		if ev.Event == nil {
			t.Fatal("verbose frame missing name")
		}
		return *ev.Event, ev.Payload
	}
	if ev.ServiceOrd != nil && *ev.ServiceOrd == txp.ControlServiceOrd {
		return controlName[*ev.OpOrd], ev.Payload
	}
	for name, op := range outboundOp {
		if ev.OpOrd != nil && *ev.OpOrd == op {
			return name, ev.Payload
		}
	}
	t.Fatalf("unknown server op ordinal %v", ev.OpOrd)
	return "", nil
}

// drainErr pulls the single queued frame off a connection and decodes it as the
// "error" event, returning the code.
func drainErr(t *testing.T, c *Conn) int64 {
	t.Helper()
	select {
	case b := <-c.Out():
		name, payload := decodeServer(t, b)
		if name != "error" {
			t.Fatalf("event = %q, want \"error\"", name)
		}
		var e csil.ErrorEvent
		if err := Decode(payload, &e); err != nil {
			t.Fatalf("decode error event: %v", err)
		}
		return e.Code
	default:
		t.Fatal("expected an error frame, got none")
		return 0
	}
}

// A malformed envelope (not even valid CBOR) is rejected with a 400 rather
// than crashing the connection — the untrusted-bytes surface must be safe.
func TestHandleFrameRejectsGarbageEnvelope(t *testing.T) {
	d := New()
	c := NewConn()
	d.HandleFrame(context.Background(), c, []byte{0xff, 0x00, 0x13, 0x37})
	if code := drainErr(t, c); code != 400 {
		t.Fatalf("garbage envelope code = %d, want 400", code)
	}
}

// A well-formed frame for an op no handler claims is a 404.
func TestHandleFrameUnknownOp(t *testing.T) {
	d := New()
	c := NewConn()
	// An ordinal/name with no handler.
	var frame []byte
	if appProfile == txp.ProfileVerbose {
		frame, _ = txp.NewVerboseEvent(nil, "nope", nil).Encode(appProfile)
	} else {
		frame, _ = txp.NewCompactEvent(WorldServiceOrd, 99, nil).Encode(appProfile)
	}
	d.HandleFrame(context.Background(), c, frame)
	if code := drainErr(t, c); code != 404 {
		t.Fatalf("unknown op code = %d, want 404", code)
	}
}

// A handler's structured *Error maps to its code; an unstructured error is
// masked as an internal 500 (its detail stays server-side).
func TestHandleFrameErrorMapping(t *testing.T) {
	d := New()
	d.Register("join", func(context.Context, *Conn, []byte) error {
		return BadRequest("nope")
	})
	d.Register("move", func(context.Context, *Conn, []byte) error {
		return errors.New("leaky internal detail")
	})

	c := NewConn()
	d.HandleFrame(context.Background(), c, clientFrame(t, "join", nil))
	if code := drainErr(t, c); code != 400 {
		t.Fatalf("structured error code = %d, want 400", code)
	}

	d.HandleFrame(context.Background(), c, clientFrame(t, "move", nil))
	if code := drainErr(t, c); code != 500 {
		t.Fatalf("unstructured error code = %d, want 500", code)
	}
}

// A handler that succeeds pushes no error frame.
func TestHandleFrameOKPushesNoError(t *testing.T) {
	d := New()
	d.Register("join", func(context.Context, *Conn, []byte) error { return nil })
	c := NewConn()
	d.HandleFrame(context.Background(), c, clientFrame(t, "join", nil))
	select {
	case b := <-c.Out():
		t.Fatalf("expected no frame on success, got %d bytes", len(b))
	default:
	}
}

// A $ping control frame is answered in-band with a $pong echoing the nonce.
func TestHandleFramePingPongsNonce(t *testing.T) {
	d := New()
	c := NewConn()

	ping, err := txp.Heartbeat{Nonce: 99}.Encode()
	if err != nil {
		t.Fatalf("encode ping: %v", err)
	}
	frame, err := encodeControlFrame(appProfile, txp.PingName, ping)
	if err != nil {
		t.Fatalf("encode ping frame: %v", err)
	}
	d.HandleFrame(context.Background(), c, frame)

	select {
	case b := <-c.Out():
		name, payload := decodeServer(t, b)
		if name != txp.PongName {
			t.Fatalf("event = %q, want %s", name, txp.PongName)
		}
		hb, err := txp.DecodeHeartbeat(payload)
		if err != nil {
			t.Fatalf("decode pong payload: %v", err)
		}
		if hb.Nonce != 99 {
			t.Fatalf("pong nonce = %d, want 99", hb.Nonce)
		}
	default:
		t.Fatal("expected a $pong frame, got none")
	}
}

// The dispatch path round-trips under BOTH wire profiles: a client→server "join"
// reaches the handler, and the server's "welcome" push decodes back to its name.
func TestRoundTripBothProfiles(t *testing.T) {
	saved := appProfile
	defer SetProfile(saved)

	for _, p := range []txp.Profile{txp.ProfileCompact, txp.ProfileVerbose} {
		SetProfile(p)
		d := New()
		d.Register("join", func(_ context.Context, c *Conn, _ []byte) error {
			c.PushEvent("welcome", csil.NameAvailability{Name: "ok", Available: true})
			return nil
		})
		c := NewConn()
		d.HandleFrame(context.Background(), c, clientFrame(t, "join", nil))
		select {
		case b := <-c.Out():
			if name, _ := decodeServer(t, b); name != "welcome" {
				t.Fatalf("[%s] event = %q, want \"welcome\"", p, name)
			}
		default:
			t.Fatalf("[%s] expected a welcome frame", p)
		}
	}
}
