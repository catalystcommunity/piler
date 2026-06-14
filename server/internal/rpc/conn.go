package rpc

import (
	"sync"
	"sync/atomic"

	"github.com/catalystcommunity/piler/server/internal/csil"
)

// outboundBuffer bounds a connection's pending writes. A slow consumer that
// fills it has frames dropped (rather than blocking the server/tick).
const outboundBuffer = 256

var idSeq atomic.Uint64

// NextID returns a process-unique id (>= 1). Connections and server-side
// bots draw from the same space so they never collide as actor keys.
func NextID() uint64 { return idSeq.Add(1) }

// Conn is one client connection. The transport runs a single writer goroutine
// draining Out(), so all writes to the socket are serialized — handler
// responses and the broadcast/tick path from any goroutine just enqueue here,
// which keeps concurrent sends race-free.
type Conn struct {
	ID uint64

	out       chan []byte
	done      chan struct{}
	closeOnce sync.Once
}

// NewConn creates a connection with a fresh unique ID.
func NewConn() *Conn {
	return &Conn{
		ID:   NextID(),
		out:  make(chan []byte, outboundBuffer),
		done: make(chan struct{}),
	}
}

// Out is the stream of encoded ServerMessage frames the transport writes.
func (c *Conn) Out() <-chan []byte { return c.out }

// Done is closed when the connection is shutting down.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Close signals shutdown (idempotent); the writer goroutine then exits and
// further sends become no-ops.
func (c *Conn) Close() { c.closeOnce.Do(func() { close(c.done) }) }

// SendRaw enqueues a pre-encoded frame. Never blocks: drops if the buffer is
// full or the connection is closing (out is never closed, so panic-free).
// Used by the broadcast/tick path, which encodes one frame and SendRaws it to
// many connections.
func (c *Conn) SendRaw(frame []byte) {
	select {
	case c.out <- frame:
	case <-c.done:
	default:
	}
}

// PushEvent encodes payload and enqueues a server push with the given event.
func (c *Conn) PushEvent(event string, payload any) {
	c.SendRaw(EncodeEvent(event, payload))
}

// PushError sends the "error" event to this connection.
func (c *Conn) PushError(code int64, message string) {
	c.PushEvent("error", csil.ErrorEvent{Code: code, Message: message})
}
