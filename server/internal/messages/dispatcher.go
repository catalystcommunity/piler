// Package messages is the transport-agnostic message core, built on the
// standardized CSIL-Events transport (csilgen's csil-events-transport.md). The
// wire profile (compact or verbose) is chosen at startup; the dispatcher decodes
// each frame to a profile-independent (name, payload) form (codec.go) and routes
// World operations to handlers by name, while Conn pushes events back. The
// protocol is pure event-push — handlers react by pushing events through the
// Conn (to the sender and/or, via the world's roster, the room) rather than
// returning a correlated response. TCP and WebSocket transports both feed frames
// to HandleFrame after the $hello handshake (see handshake.go).
//
// Op names ↔ ordinals live in ops.go and mirror the @wire-id annotations in
// csil/piler.csil (the source of truth).
//
// Presence and identity are NOT tracked here: the world package keys its actors
// by Conn.ID and binds identity at join. There is no auth or session yet
// (LinkKeys later — it will ride Hello.auth in the handshake).
package messages

import (
	"bytes"
	"context"
	"errors"
	"log"

	"github.com/fxamacker/cbor/v2"

	txp "github.com/catalystcommunity/csilgen/transports/go"
)

// Handler reacts to one client message. payload is the (untagged) CBOR payload
// carried by the event. It pushes any resulting events via the Conn and returns
// an error only for failures worth reporting to the sender (an *Error becomes
// an "error" event; anything else is logged as internal).
type Handler func(ctx context.Context, c *Conn, payload []byte) error

// Dispatcher routes a client→server operation (by name) to its Handler.
type Dispatcher struct {
	handlers map[string]Handler
}

func New() *Dispatcher {
	return &Dispatcher{handlers: map[string]Handler{}}
}

// Register binds a handler to an operation name (one of ops.go's inbound names).
// Re-registering panics — duplicates indicate a wiring bug worth catching at
// startup.
func (d *Dispatcher) Register(op string, h Handler) {
	if _, exists := d.handlers[op]; exists {
		panic("messages: duplicate registration for op " + op)
	}
	d.handlers[op] = h
}

// HandleFrame decodes one inbound CSIL-Events frame (in the active profile) and
// dispatches it. Control events are handled in-band (heartbeat, close); World
// operations route to their handler by name. Errors are reported back to the
// sender as an "error" event; the connection stays open.
func (d *Dispatcher) HandleFrame(ctx context.Context, c *Conn, frame []byte) {
	in, err := decodeInbound(appProfile, frame)
	if err != nil {
		c.PushError(400, "invalid event envelope: "+err.Error())
		return
	}
	if in.control {
		d.handleControl(c, in.name, in.payload)
		return
	}
	if in.name == "" {
		c.PushError(404, "unknown operation")
		return
	}
	h := d.handlers[in.name]
	if h == nil {
		c.PushError(404, "unknown operation: "+in.name)
		return
	}
	if err := h(ctx, c, in.payload); err != nil {
		var rerr *Error
		if errors.As(err, &rerr) {
			c.PushError(rerr.Code, rerr.Message)
			return
		}
		log.Printf("messages: handler for %q returned unstructured error: %v", in.name, err)
		c.PushError(500, "internal error")
	}
}

// handleControl services the CSIL-Events control plane after the handshake:
// answer $ping with $pong (echoing the nonce), honor $close, log a peer $error.
func (d *Dispatcher) handleControl(c *Conn, name string, payload []byte) {
	switch name {
	case txp.PingName:
		hb, err := txp.DecodeHeartbeat(payload)
		if err != nil {
			return
		}
		pong, err := txp.Heartbeat{Nonce: hb.Nonce}.Encode()
		if err != nil {
			return
		}
		c.pushControl(txp.PongName, pong)
	case txp.PongName:
		// Liveness reply to our own $ping; nothing to do at this scale.
	case txp.CloseName:
		c.Close()
	case txp.ErrorName:
		log.Printf("messages: peer reported a transport $error on conn %d", c.ID)
	default:
		// Unknown / out-of-phase control op (e.g. a late $hello): ignore.
	}
}

// Decode unmarshals an event's CBOR payload into v. An empty payload is a no-op
// (the canonical zero-value payload). Decode failures are caller-visible
// BadRequests since the payload shape is the caller's concern.
func Decode(payload []byte, v any) error {
	if len(payload) == 0 {
		return nil
	}
	if err := cbor.NewDecoder(bytes.NewReader(payload)).Decode(v); err != nil {
		return BadRequest("invalid CBOR payload: " + err.Error())
	}
	return nil
}

// --- payload encoding shared across the package ---

var encMode = func() cbor.EncMode {
	m, err := cbor.CanonicalEncOptions().EncMode()
	if err != nil {
		panic("messages: building CBOR enc mode: " + err.Error())
	}
	return m
}()

// EncodeEvent builds a server→client World event frame (in the active profile)
// carrying a tag-24 CBOR payload, encoded once so the caller can SendRaw the
// same bytes to many connections.
func EncodeEvent(event string, payload any) []byte {
	frame, err := encodeAppFrame(appProfile, event, encode(payload))
	if err != nil {
		log.Printf("messages: event encode failed: %v", err)
		return []byte{}
	}
	return frame
}

// encode marshals v to canonical CBOR, returning a non-nil slice. A nil value
// encodes to an empty byte string (used for payload-less events).
func encode(v any) []byte {
	if v == nil {
		return []byte{}
	}
	b, err := encMode.Marshal(v)
	if err != nil {
		log.Printf("messages: CBOR encode failed: %v", err)
		return []byte{}
	}
	return b
}
