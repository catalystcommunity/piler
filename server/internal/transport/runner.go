// Package transport carries the message dispatcher over raw TCP (native
// clients) and WebSocket (browsers). Both decode opaque CBOR frames and feed
// them to rpc.Dispatcher.HandleMessage; the framing differs, the rest does
// not.
package transport

import (
	"context"

	"github.com/catalystcommunity/piler/server/internal/rpc"
)

// runConn drives one connection's lifecycle: a single writer goroutine (so
// the socket is never written concurrently), a read loop dispatching inbound
// frames, and disconnect cleanup via onDisconnect (the world drops the actor;
// the next tick snapshot reflects the departure).
//
// readFrame returns the next inbound frame or an error to end the connection.
// writeFrame writes one outbound frame. closeSocket forces the reader to
// unblock when the writer fails.
func runConn(
	ctx context.Context,
	conn *rpc.Conn,
	d *rpc.Dispatcher,
	readFrame func() ([]byte, error),
	writeFrame func([]byte) error,
	closeSocket func(),
	onDisconnect func(connID uint64),
) {
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for {
			select {
			case <-conn.Done():
				return
			case frame := <-conn.Out():
				if err := writeFrame(frame); err != nil {
					closeSocket() // unblock the reader
					return
				}
			}
		}
	}()

	for {
		frame, err := readFrame()
		if err != nil {
			break
		}
		d.HandleMessage(ctx, conn, frame)
	}

	onDisconnect(conn.ID)
	conn.Close()
	<-writerDone
	closeSocket()
}
