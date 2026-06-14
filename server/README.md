# server

The authoritative Go world server. Owns all long-term state in PostgreSQL
and is the only trusted party (see [../docs/architecture.md](../docs/architecture.md)).

- Speaks the CSIL/CBOR RPC contract over **TCP** (native clients) and
  **WebSocket** (browsers, via `catalystcommunity/websocks`).
- Auth via **LinkKeys** (`catalystcommunity/linkkeys`).
- State (characters, inventory, rooms, placed objects) in PostgreSQL.

Module: `github.com/catalystcommunity/piler/server`.

## Status

Scaffolding only — `main.go` is a placeholder that does not yet serve
anything. Generated CSIL stubs and the transport layer come in roadmap
steps 1–2.

## Layout (intended)

```
server/
├── main.go            # entry point (placeholder)
├── cmd/               # additional binaries (migrations, tooling)
├── internal/          # server internals (transport, handlers, state)
└── (generated CSIL Go stubs land here once csil/ exists)
```

## Tests

API-level tests live with the server and enforce game rules at the trust
boundary. We never rely on a client to uphold a rule. Run with `go test
./...`.
