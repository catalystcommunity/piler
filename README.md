# piler

A game engine and server for a tiled, room-based online world — think *The
Realm Online*'s social/MMO feel, rendered like *Final Fantasy VI*'s tiled
rooms (discrete rooms you move between) rather than a single contiguous
scene walked in cardinal directions.

> **Status: pre-alpha, proof-of-concept flow working.** A minimal vertical
> slice proves the information/state path end to end: a CSIL/CBOR RPC
> contract, a Go server with PostgreSQL state, a Rust core client compiled
> to WASM, and a vanilla-TS web harness. You can join a room, move at
> sub-tile precision, chat, and read room state back — over raw TCP and
> over WebSocket. See [Quickstart](#quickstart).

## What this is

- A **Go server** (`server/`) holding authoritative, long-term world state
  in PostgreSQL (inventory, characters, rooms, placed objects).
- A **Rust core client** (`coreclient/`) — the platform-agnostic game
  client: world state, input model, and rendering into an abstract
  framebuffer/bitbuffer. Compiles to native (for tests) and to WASM.
- A **web client** (`webclient/`) — a browser host that loads the WASM
  core and blits its bitbuffer to a `<canvas>`, bridging keyboard/mouse
  input and the WebSocket transport. A future native client will wrap the
  same core the same way.

The API/RPC contract and data models are defined in **CSIL** (CBOR Service
Interface Language, see [`catalystcommunity/csilgen`](../csilgen)) and
generate both Go server stubs and the Rust client types from one source of
truth.

## Layout

| Path             | Language | Role                                                        |
| ---------------- | -------- | ----------------------------------------------------------- |
| `csil/`          | CSIL     | API + data-model definitions; source of truth (regenerates all) |
| `server/`        | Go       | Authoritative world server, CSIL RPC over TCP + WebSocket   |
| `coredb/`        | Go/SQL   | Embedded goose migrations (rooms, players, chat)            |
| `coreclient/`    | Rust     | Platform-agnostic game client core → native + WASM          |
| `webclient/`     | Rust     | WASM wrapper exposing the client API to JS                  |
| `webclient/web/` | TS       | Vanilla-TS vite harness: loads WASM, owns the WebSocket     |
| `docs/`          | —        | Architecture, positioning model, transport/auth, testing, roadmap |
| `tools.sh`       | bash     | Local dev workflow (postgres + server + vite + smoke)       |

## Quickstart

Assuming you've cloned this repo, you need:

- **Docker** — runs PostgreSQL in a container
- **Go** 1.26+
- **Rust** / cargo, plus **wasm-pack** and the `wasm32-unknown-unknown`
  target (`rustup target add wasm32-unknown-unknown`, or your distro's
  equivalent)
- **Node** + npm — the web harness

`csilgen` is only needed to *regenerate* the bindings after editing the CSIL
contract; the generated code is checked in, so you don't need it just to run.

### Run the dev stack

```bash
cp .env.dev.example .env.dev     # local config — the defaults work as-is
./tools.sh dev                   # postgres + migrations + WASM build + server + vite
```

`./tools.sh dev` brings everything up and tails the server/vite logs:

- **web client** — <http://localhost:5173>
- **server** — WebSocket on `:6080` (`/ws`), raw TCP on `:6081`
- **postgres** — in the `piler-postgres-dev` container (data persists in a volume)

Open **<http://localhost:5173>**, enter a name to join, then:

- **move** — `WASD`/arrows, or drag (touch) as an 8-way stick
- **chat** — `Enter` (or tap), type, `Enter` to send
- **firework** — `Space` (or double-tap) — broadcast to everyone in the room
- **bots** — type `/demo` (or `/demo 5`, 3–8) in chat to toggle demo bots

`Ctrl-C` stops tailing the logs (the servers keep running). Stop everything with:

```bash
./tools.sh dev-down              # stops server, vite, postgres (keeps the db volume)
```

### Connect from another device (phone, tablet, another machine)

The vite harness binds `0.0.0.0`, so from a device on the same network browse to
`http://<this-machine-ip>:5173`. The client derives the server host from the page
URL, so it automatically reaches the server's `:6080` on the same machine — just
make sure your firewall allows ports **5173** and **6080**.

### TCP smoke check (no browser)

```bash
./tools.sh smoke                 # join → move → say → get-room-state over raw TCP
```

The browser (WebSocket) and `smoke` (TCP) paths share the same authoritative
world state. After editing `csil/piler.csil`, run `./tools.sh regen` to
regenerate the Go/Rust/TS bindings (needs `csilgen` on PATH).

## Transport & auth (planned)

- Native clients talk **TCP**; browsers talk **WebSocket** (we have a Go
  websocket server in `catalystcommunity/websocks` to bridge).
- Authentication via **LinkKeys** (`catalystcommunity/linkkeys`), under
  active development — we adopt capabilities as they land.
- Payloads are **CBOR**, framed per the CSIL service definitions.

## Versioning

Client and server are versioned **independently** with semver, against a
**versioned API**. The CSIL contract carries the API version; client and
server each advertise compatibility.

## Testing philosophy

Tests are split on the **client/server boundary**. The server is the
trust boundary: we write API-level tests against it and **never trust a
client** — alternative third-party clients are expected and encouraged.
Client-side tests cover the core's logic and rendering independently.

## Building blocks in this org

- `csilgen` — the CSIL toolchain (parser, validator, code generators).
- `longhouse` — reference Go CSIL service + generated clients (the server
  here follows its `POST`-style RPC + CBOR patterns, adapted to TCP/WS).
- `linkkeys` — authentication / identity.
- `websocks` — Go WebSocket server for the browser transport.

See [`docs/`](docs/) for details and [`CLAUDE.md`](CLAUDE.md) for working
notes and conventions.

## License

Licensed under the Apache License, Version 2.0 — see [`LICENSE`](LICENSE).
