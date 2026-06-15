package rpc

import (
	"context"
	"errors"
	"testing"

	"github.com/fxamacker/cbor/v2"

	"github.com/catalystcommunity/piler/server/internal/csil"
)

// drainErr pulls the single queued frame off a connection and decodes it as an
// "error" event, returning the code. Fails the test if the frame isn't an
// error event (the dispatcher's only reply to a bad inbound message).
func drainErr(t *testing.T, c *Conn) int64 {
	t.Helper()
	select {
	case b := <-c.Out():
		var sm csil.ServerMessage
		if err := cbor.Unmarshal(b, &sm); err != nil {
			t.Fatalf("decode server message: %v", err)
		}
		if sm.Event != "error" {
			t.Fatalf("event = %q, want \"error\"", sm.Event)
		}
		var e csil.ErrorEvent
		if err := cbor.Unmarshal(sm.Body, &e); err != nil {
			t.Fatalf("decode error event: %v", err)
		}
		return e.Code
	default:
		t.Fatal("expected an error frame, got none")
		return 0
	}
}

func frame(t *testing.T, kind string) []byte {
	t.Helper()
	b, err := cbor.Marshal(csil.ClientMessage{Kind: kind})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// A malformed envelope (not even valid CBOR) is rejected with a 400 rather
// than crashing the connection — the untrusted-bytes surface must be safe.
func TestHandleMessageRejectsGarbageEnvelope(t *testing.T) {
	d := New()
	c := NewConn()
	d.HandleMessage(context.Background(), c, []byte{0xff, 0x00, 0x13, 0x37})
	if code := drainErr(t, c); code != 400 {
		t.Fatalf("garbage envelope code = %d, want 400", code)
	}
}

// A well-formed message for a kind no handler claims is a 404.
func TestHandleMessageUnknownKind(t *testing.T) {
	d := New()
	c := NewConn()
	d.HandleMessage(context.Background(), c, frame(t, "no-such-kind"))
	if code := drainErr(t, c); code != 404 {
		t.Fatalf("unknown kind code = %d, want 404", code)
	}
}

// A handler's structured *Error maps to its code; an unstructured error is
// masked as an internal 500 (its detail stays server-side).
func TestHandleMessageErrorMapping(t *testing.T) {
	d := New()
	d.Register("bad", func(context.Context, *Conn, []byte) error {
		return BadRequest("nope")
	})
	d.Register("boom", func(context.Context, *Conn, []byte) error {
		return errors.New("leaky internal detail")
	})

	c := NewConn()
	d.HandleMessage(context.Background(), c, frame(t, "bad"))
	if code := drainErr(t, c); code != 400 {
		t.Fatalf("structured error code = %d, want 400", code)
	}

	d.HandleMessage(context.Background(), c, frame(t, "boom"))
	if code := drainErr(t, c); code != 500 {
		t.Fatalf("unstructured error code = %d, want 500", code)
	}
}

// A handler that succeeds pushes no error frame.
func TestHandleMessageOKPushesNoError(t *testing.T) {
	d := New()
	d.Register("ok", func(context.Context, *Conn, []byte) error { return nil })
	c := NewConn()
	d.HandleMessage(context.Background(), c, frame(t, "ok"))
	select {
	case b := <-c.Out():
		t.Fatalf("expected no frame on success, got %d bytes", len(b))
	default:
	}
}
