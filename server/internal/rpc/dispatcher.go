// Package rpc is the transport-agnostic message core: a per-connection
// session, a connection hub for presence + broadcasts, and a dispatcher that
// routes inbound ClientMessages by kind. The protocol is pure message-passing
// — handlers react to a message by pushing events (to the sender and/or the
// room) rather than returning a correlated response. TCP and WebSocket
// transports both feed raw CBOR frames to HandleMessage.
package rpc

import (
	"bytes"
	"context"
	"errors"
	"log"

	"github.com/fxamacker/cbor/v2"

	"github.com/catalystcommunity/piler/server/internal/csil"
)

// Handler reacts to one client message. body is the CBOR payload for the
// message kind. It pushes any resulting events via the Conn / Hub and returns
// an error only for failures worth reporting to the sender (an *Error becomes
// an "error" event; anything else is logged as internal).
type Handler func(ctx context.Context, c *Conn, body []byte) error

// Dispatcher routes a message kind to its Handler.
type Dispatcher struct {
	handlers map[string]Handler
}

func New() *Dispatcher {
	return &Dispatcher{handlers: map[string]Handler{}}
}

// Register binds a handler to a message kind. Re-registering panics —
// duplicates indicate a wiring bug worth catching at startup.
func (d *Dispatcher) Register(kind string, h Handler) {
	if _, exists := d.handlers[kind]; exists {
		panic("rpc: duplicate registration for kind " + kind)
	}
	d.handlers[kind] = h
}

// HandleMessage decodes one inbound frame and dispatches it. Errors are
// reported back to the sender as an "error" event; the connection stays open.
func (d *Dispatcher) HandleMessage(ctx context.Context, c *Conn, frame []byte) {
	var msg csil.ClientMessage
	if err := cbor.Unmarshal(frame, &msg); err != nil {
		c.PushError(400, "invalid message envelope: "+err.Error())
		return
	}
	h := d.handlers[msg.Kind]
	if h == nil {
		c.PushError(404, "unknown message kind: "+msg.Kind)
		return
	}
	if err := h(ctx, c, msg.Body); err != nil {
		var rerr *Error
		if errors.As(err, &rerr) {
			c.PushError(int64(rerr.Code), rerr.Message)
			return
		}
		log.Printf("rpc: handler for %q returned unstructured error: %v", msg.Kind, err)
		c.PushError(500, "internal error")
	}
}

// Decode unmarshals a CBOR message body into v. An empty body is a no-op
// (the canonical zero-value payload). Decode failures are caller-visible
// BadRequests since the body shape is the caller's concern.
func Decode(body []byte, v any) error {
	if len(body) == 0 {
		return nil
	}
	if err := cbor.NewDecoder(bytes.NewReader(body)).Decode(v); err != nil {
		return BadRequest("invalid CBOR body: " + err.Error())
	}
	return nil
}

// --- CBOR encoding shared across the package ---

var encMode = func() cbor.EncMode {
	m, err := cbor.CanonicalEncOptions().EncMode()
	if err != nil {
		panic("rpc: building CBOR enc mode: " + err.Error())
	}
	return m
}()

// EncodeEvent builds a ServerMessage frame (event + CBOR payload), encoded
// once so the caller can SendRaw the same bytes to many connections.
func EncodeEvent(event string, payload any) []byte {
	return encode(csil.ServerMessage{Event: event, Body: encode(payload)})
}

// encode marshals v to canonical CBOR, returning a non-nil slice. A nil value
// encodes to an empty byte string (used for payload-less pushes).
func encode(v any) []byte {
	if v == nil {
		return []byte{}
	}
	b, err := encMode.Marshal(v)
	if err != nil {
		log.Printf("rpc: CBOR encode failed: %v", err)
		return []byte{}
	}
	return b
}
