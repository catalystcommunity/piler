# Architecture

## Components

```
                         CSIL service + data-model defs (csil/)
                         source of truth, generates both sides
                                      │
              ┌───────────── generate ┴ generate ─────────────┐
              ▼                                                ▼
   ┌────────────────────┐                          ┌────────────────────┐
   │  server/  (Go)     │                          │ coreclient/ (Rust) │
   │  authoritative     │   CBOR over TCP / WS      │ game client core   │
   │  world state       │◀────────────────────────▶│ state + rendering  │
   │  PostgreSQL        │   (CSIL RPC framing)      │ → bitbuffer        │
   └────────────────────┘                          └─────────┬──────────┘
                                                             │ wrapped by
                                          ┌──────────────────┴───────────────┐
                                          ▼                                   ▼
                               ┌────────────────────┐            ┌────────────────────┐
                               │ webclient/         │            │ (future) native    │
                               │ WASM core + canvas │            │ client host        │
                               │ + WS transport     │            │ + TCP transport    │
                               └────────────────────┘            └────────────────────┘
```

## Trust boundary

The **server is authoritative and the only trusted party**. Clients send
intents (move, interact, chat, equip); the server validates, applies, and
broadcasts resulting state. We assume clients can be hostile or
home-grown — alternative clients are explicitly encouraged — so no
gameplay rule may live only on the client. This drives the test split:
API-level tests on the server enforce the rules; client tests cover
presentation and local prediction only.

## The client core vs. host split

`coreclient/` is platform-agnostic and renders into an **abstract
framebuffer/bitbuffer** rather than touching any real surface. It has no
WASM- or browser-specific dependencies so it can be unit-tested natively
and reused by any host.

A **host** (the `webclient/`, or a future native client) is thin: it
creates the surface (a `<canvas>` in the browser), pumps input events into
the core, calls the core's tick/render, and blits the resulting bitbuffer
to the surface. The host also owns the transport socket. This is the
"wrap the WASM app with the appropriate function interfaces to do
bitbuffering to an equivalent to a canvas" arrangement.

### Why bitbuffer, not draw calls

Rendering to an owned pixel/tile buffer keeps the core engine independent
of any display API and lets a custom (non-browser) client present the same
frames however it likes. The browser host's job reduces to `putImageData`
(or texture upload) per frame.

## Data & state

- **Long-term state** (inventory, characters, room contents, placed
  object positions) lives in **PostgreSQL**, owned by the server.
- **Transient state** (who is in a room right now, in-flight motion) is
  server-held and broadcast; clients keep a local mirror for rendering and
  may do short-horizon prediction, always reconciled to server truth.

## Code generation

CSIL definitions in `csil/` generate Go server stubs and Rust client
types. Run the (forthcoming) `csil/regenerate.sh` after any spec change —
generated code is checked in so diffs are reviewable, mirroring longhouse.

## Scaling (deferred)

The world is shardable: different worlds on different server processes,
with player hand-off between them. Keep room/world identifiers and the
session/auth model hand-off-friendly, but do not build orchestration yet.
