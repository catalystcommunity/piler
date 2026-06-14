package transport

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log"
	"net"

	"github.com/catalystcommunity/piler/server/internal/rpc"
)

// maxFrameBytes bounds a single frame (1 MiB) so a malformed length prefix
// can't make us allocate unboundedly.
const maxFrameBytes = 1 << 20

// ServeTCP listens on addr and serves the length-prefixed CBOR framing: each
// frame is a 4-byte big-endian length followed by that many CBOR bytes (a
// ClientMessage in, a ServerMessage out). Blocks until the listener fails or
// ctx is cancelled. onDisconnect is called with a connection's id when it ends.
func ServeTCP(ctx context.Context, addr string, d *rpc.Dispatcher, onDisconnect func(uint64)) error {
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

func serveTCPConn(ctx context.Context, nc net.Conn, d *rpc.Dispatcher, onDisconnect func(uint64)) {
	conn := rpc.NewConn()

	var lenBuf [4]byte
	readFrame := func() ([]byte, error) {
		if _, err := io.ReadFull(nc, lenBuf[:]); err != nil {
			return nil, err
		}
		n := binary.BigEndian.Uint32(lenBuf[:])
		if n == 0 || n > maxFrameBytes {
			return nil, errors.New("frame length out of range")
		}
		buf := make([]byte, n)
		if _, err := io.ReadFull(nc, buf); err != nil {
			return nil, err
		}
		return buf, nil
	}
	writeFrame := func(payload []byte) error {
		if len(payload) > maxFrameBytes {
			return errors.New("outbound frame exceeds max size")
		}
		var hdr [4]byte
		binary.BigEndian.PutUint32(hdr[:], uint32(len(payload)))
		if _, err := nc.Write(hdr[:]); err != nil {
			return err
		}
		_, err := nc.Write(payload)
		return err
	}

	runConn(ctx, conn, d, readFrame, writeFrame, func() { _ = nc.Close() }, onDisconnect)
}
