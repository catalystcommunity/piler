package transport

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"

	websocks "github.com/catalystcommunity/websocks/v1"

	txp "github.com/catalystcommunity/csilgen/transports/go"

	"github.com/catalystcommunity/piler/server/internal/messages"
)

// wsBinaryMessage is the WebSocket binary opcode (RFC 6455 0x2), matching
// websocks' BinaryMessage. Declared locally so this compiles regardless.
const wsBinaryMessage = 2

// wsMessageConn is the message-oriented interface piler needs from a websocks
// connection to carry binary CBOR. Go satisfies interfaces structurally, so
// this asserts once websocks' connection implements these methods.
type wsMessageConn interface {
	ReadMessage() (messageType int, payload []byte, err error)
	WriteMessage(messageType int, payload []byte) error
}

// wsCarrier adapts a websocks binary-message connection to the CSIL-Events
// FrameCarrier seam: one binary WS message is one CSIL-Events frame (the
// WebSocket carrier per csil-events-transport.md §4 — no length prefix; the WS
// message framing supplies the boundaries).
type wsCarrier struct{ mc wsMessageConn }

func (w wsCarrier) SendFrame(b []byte) error {
	return w.mc.WriteMessage(wsBinaryMessage, b)
}

func (w wsCarrier) RecvFrame() ([]byte, error) {
	_, payload, err := w.mc.ReadMessage()
	if err != nil {
		return nil, err
	}
	return payload, nil
}

// WSHandler upgrades to WebSocket (via websocks) and runs the same connection
// lifecycle as TCP over a binary-message carrier.
func WSHandler(ctx context.Context, d *messages.Dispatcher, onDisconnect func(uint64)) http.Handler {
	return websocks.NewHandler(func(nc net.Conn) error {
		mc, ok := nc.(wsMessageConn)
		if !ok {
			log.Printf("piler: websocks connection lacks binary MessageConn support; " +
				"the browser transport needs binary frames (see websocks/BINARY_FRAMES_PROMPT.md). Closing.")
			return errors.New("websocks binary message support unavailable")
		}

		conn := messages.NewConn()
		var carrier txp.FrameCarrier = wsCarrier{mc: mc}
		runConn(ctx, conn, d, carrier, func() { _ = nc.Close() }, onDisconnect)
		return nil
	})
}
