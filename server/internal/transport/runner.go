// Package transport carries the CSIL-Events message dispatcher over raw TCP
// (native clients) and WebSocket (browsers). Both wrap their socket in a
// csilgen-transport FrameCarrier and feed event frames to
// messages.Dispatcher.HandleFrame; the carrier differs, the rest does not.
package transport

import (
	"context"

	txp "github.com/catalystcommunity/csilgen/transports/go"

	"github.com/catalystcommunity/piler/server/internal/messages"
)

// runConn drives one connection's lifecycle: the CSIL-Events handshake, then a
// single writer goroutine (so the socket is never written concurrently), a read
// loop dispatching inbound event frames, and disconnect cleanup via
// onDisconnect (the world drops the actor; the next tick snapshot reflects the
// departure).
//
// carrier moves length-/message-delimited frames in both directions.
// closeSocket forces the reader to unblock when the writer fails.
func runConn(
	ctx context.Context,
	conn *messages.Conn,
	d *messages.Dispatcher,
	carrier txp.FrameCarrier,
	closeSocket func(),
	onDisconnect func(connID uint64),
) {
	// Handshake first: read $hello, reply $hello-ack. Done synchronously before
	// the writer goroutine starts, so the ack write can't race the outbound queue.
	if err := messages.ServerHandshake(carrier); err != nil {
		closeSocket()
		return
	}

	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for {
			select {
			case <-conn.Done():
				return
			case frame := <-conn.Out():
				if err := carrier.SendFrame(frame); err != nil {
					closeSocket() // unblock the reader
					return
				}
			}
		}
	}()

	for {
		frame, err := carrier.RecvFrame()
		if err != nil || frame == nil { // error or clean end of stream
			break
		}
		d.HandleFrame(ctx, conn, frame)
	}

	onDisconnect(conn.ID)
	conn.Close()
	<-writerDone
	closeSocket()
}
