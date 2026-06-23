package transport

import (
	"context"
	"log"
	"net"

	txp "github.com/catalystcommunity/csilgen/transports/go"

	"github.com/catalystcommunity/piler/server/internal/messages"
)

// maxFrameBytes bounds a single frame (1 MiB) so a malformed length prefix
// can't make us allocate unboundedly. (The carrier enforces this before
// allocating.)
const maxFrameBytes = 1 << 20

// ServeTCP listens on addr and serves the CSIL-Events byte-stream carrier: each
// frame is a 4-byte big-endian length followed by that many CBOR bytes (the
// standardized stream framing). Blocks until the listener fails or ctx is
// cancelled. onDisconnect is called with a connection's id when it ends.
func ServeTCP(ctx context.Context, addr string, d *messages.Dispatcher, onDisconnect func(uint64)) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	log.Printf("piler: TCP transport listening on %s", addr)

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		nc, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil // shutting down
			}
			return err
		}
		go serveTCPConn(ctx, nc, d, onDisconnect)
	}
}

func serveTCPConn(ctx context.Context, nc net.Conn, d *messages.Dispatcher, onDisconnect func(uint64)) {
	conn := messages.NewConn()
	// The standardized length-prefix stream carrier handles framing + the
	// max-frame guard; piler just moves the bytes.
	carrier := txp.NewStreamCarrierWithMaxFrame(nc, maxFrameBytes)
	runConn(ctx, conn, d, carrier, func() { _ = nc.Close() }, onDisconnect)
}
