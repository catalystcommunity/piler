# Roadmap (rough, iterative)

Not a commitment — a sketch of plausible ordering. We design each step
together before building it.

## 0. Scaffolding ✅

Repo layout, docs, component stubs.

## 1. Shared contract foundations ✅ (PoC)

- CSIL definitions in `csil/piler.csil`: Position, Player, ChatMessage,
  RoomState + a `World` service (join / move-player / get-room-state / say).
- `csil/regenerate.sh` wiring Go + Rust + TS generation.
- Position fixed-point `SUB` (default 1000) + a `layer` field chosen.

## 2. Server skeleton ✅ (PoC)

- Go server boots, runs goose migrations (`coredb`), connects PostgreSQL,
  serves the `World` service over **TCP and WebSocket**. API-level tests
  (fake store) + a TCP framing test. `smoke` subcommand for end-to-end.

## 3. Core client ✅ (PoC)

- `coreclient/` holds the player/room state, builds/applies CBOR RPC
  frames; native tests assert the logic without a server. (Bitbuffer
  rendering still to come — see step 6.)

## 4. Web host ✅ (PoC)

- WASM toolchain in use (`wasm32-unknown-unknown` + wasm-pack).
- `webclient/` wasm-bindgen wrapper + `webclient/web/` vanilla-TS vite
  harness; WebSocket transport via `websocks` (binary `MessageConn`).
- Still TODO: blit a bitbuffer to `<canvas>` (currently renders state as
  dots/text), and the dev/test `window.__piler` debug API behind a Cargo
  feature that is **off in release** — see [testing.md](testing.md).

## 5. Auth

- LinkKeys integration for both transports.

## 6. Gameplay loop

- Movement at sub-tile precision, interaction, object placement, presence.

## 7. Chat

## 8. Scale-out (deferred)

- Multiple worlds across servers, player hand-off.

Versioning is independent per client/server against a versioned API
throughout.
