package transport

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"

	websocks "github.com/catalystcommunity/websocks/v1"

	"github.com/catalystcommunity/piler/server/internal/rpc"
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

// WSHandler upgrades to WebSocket (via websocks) and runs the same message
// loop as TCP, framed per WebSocket binary message instead of length-prefixed.
func WSHandler(ctx context.Context, d *rpc.Dispatcher, onDisconnect func(uint64)) http.Handler {
	return websocks.NewHandler(func(nc net.Conn) error {
		mc, ok := nc.(wsMessageConn)
		if !ok {
			log.Printf("piler: websocks connection lacks binary MessageConn support; " +
				"the browser transport needs binary frames (see websocks/BINARY_FRAMES_PROMPT.md). Closing.")
			return errors.New("websocks binary message support unavailable")
		}

		conn := rpc.NewConn()
		readFrame := func() ([]byte, error) {
			_, payload, err := mc.ReadMessage()
			return payload, err
		}
		writeFrame := func(payload []byte) error {
			return mc.WriteMessage(wsBinaryMessage, payload)
		}
		runConn(ctx, conn, d, readFrame, writeFrame, func() { _ = nc.Close() }, onDisconnect)
		return nil
	})
}
