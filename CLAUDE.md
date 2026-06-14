# piler — working notes for Claude

A game engine + server for a tiled, room-based online world. Realm-Online
social/MMO sensibility, FF6-style discrete tiled rooms (not one walked
scene). Built iteratively — do not over-build ahead of the current step.

## Hard rules (from the maintainer)

- **Never commit or push.** The maintainer arbitrates all git. You may
  stage/prepare on request, but the human runs git.
- **Tests are mandatory**, split on the **client/server boundary**. The
  server is the trust boundary: API-level tests there, and we *never trust
  a client*. Third-party clients are expected and encouraged. The core
  client is tested natively, independent of any host.
- **Semver, separate for client and server, against a versioned API.**
- WASM is **Rust**, not Go (Go WASM only if it ever becomes trivially
  easy — default to Rust).
- No external game/display engine (no Bevy etc.). We build the rendering
  path ourselves down to a bitbuffer.

## Architecture

- `server/` — Go. Authoritative world state in PostgreSQL (inventory,
  characters, rooms, placed objects). Speaks CSIL RPC over **TCP**
  (native clients) and **WebSocket** (browsers, via
  `catalystcommunity/websocks`). CBOR payloads. Auth via
  `catalystcommunity/linkkeys` (LinkKeys, under active development —
  adopt as capabilities land).
- `coreclient/` — Rust lib crate. Platform-agnostic game client: world
  state, input model, rendering into an abstract framebuffer/bitbuffer.
  No host/wasm-specific deps so it stays native-testable. Both the web
  host and a future native client wrap this same core.
- `webclient/` — Rust (wasm-bindgen) + **minimal** JS/HTML host. Loads the
  WASM core, blits its bitbuffer to a `<canvas>`, forwards keyboard/mouse
  input, and carries the WebSocket transport. Keep JS glue thin — logic
  lives in the core. A `<canvas>` is opaque to the a11y tree, so the core
  exposes a dev/test-only `window.__piler` debug API (read state + inject
  input intents) for automation; it asserts no authority. In **production
  builds the debug interface is compiled out entirely** — gated behind a
  Cargo feature (off by default) so the wasm carries no debug symbols, and
  the production DOM never installs `window.__piler`. Absent, not merely
  hidden. See `docs/testing.md`.
- `csil/` — (coming) CSIL service + data-model definitions; source of
  truth generating Go server stubs and Rust client types.
- `docs/` — architecture, positioning model, transport/auth, roadmap.

## PoC implementation (the current vertical slice)

Proves the information/state flow end to end. Build/run with `./tools.sh`.

- **Contract**: `csil/piler.csil` → service `World` with methods `Join`,
  `MovePlayer`, `GetRoomState`, `Say`. Domain types: Position (tile +
  fixed-point sub + layer), Player, ChatMessage, RoomState (carries
  `field_w`/`field_h`), request types.
- **Field bounds**: movement is clamped server-side to `[0, FieldW] x
  [0, FieldH]` sub-units (config; default 48000×27000 = 1920×1080 at
  40 px/tile). Players spawn at field center. The field size rides in
  RoomState so the client sizes/draws the play area.
- **Avatars**: `coreclient::identicon(seed)` makes a deterministic
  GitHub-style 5×5 mirrored identicon from a seed (player_id today, a
  LinkKeys id later), exposed to JS as `avatar(seed)`; the harness draws it
  on the field and in the player list. Pure + unit-tested in coreclient.
- **Wire envelope** (NOT in CSIL — hand-written in Go `internal/rpc`, Rust
  `coreclient/src/envelope.rs`, and implicitly via the WASM): `Request{id,
  service, method, body}` / `Response{id, ok, body, error_code,
  error_message}`. `body` is the opaque CBOR-encoded payload so the
  dispatcher routes without knowing payload types. Kept out of CSIL so
  `body` stays a true CBOR byte string across Go/Rust/TS CBOR libs.
- **Dispatcher** (`internal/rpc`) is transport-agnostic: `HandleFrame(ctx,
  session, bytes) -> bytes`. A per-connection `Session` binds identity at
  `Join` (no auth yet; LinkKeys later).
- **Transports** (`internal/transport`): raw **TCP** = 4-byte big-endian
  length prefix + CBOR frame; **WebSocket** via `websocks` `MessageConn`
  (binary frames). Both just move bytes to `HandleFrame`.
- **Store** (`internal/store`): pgx pool; `coredb` goose migrations
  (rooms/players/chat). `world` package holds the rules + sub-tile move
  carry math (floored division), with API-level tests using a fake store.
- **Client**: `coreclient` is pure (`build_*` request frames, `apply_frame`
  updates state) and native-tested. `webclient` is the wasm-bindgen wrapper
  exposing `join/moveBy/say/getRoomState/applyFrame/state`. `webclient/web`
  is the vanilla-TS vite harness that owns the WebSocket and renders state.
- **Smoke**: `./tools.sh smoke` (or `server smoke`) runs join→move→say→
  get-room-state over TCP — the works-today end-to-end check.

### Gotchas worth remembering

- `websocks` **now has** `MessageConn` (ReadMessage/WriteMessage, binary
  frames) on `main` — the WS path is live. piler asserts it structurally
  in `internal/transport/ws.go` so it compiles regardless.
- **serde_wasm_bindgen** serializes Rust structs with **snake_case** field
  names and **BigInt** for i64/u64. The web harness reads snake_case keys
  and `Number()`-coerces positions. The generated camelCase TS types
  (`webclient/web/src/api`) are for a future JSON path, not this one.
- **csilgen quirks** (all worked around in the spec): `move` is a reserved
  word in Rust → use `move-player`; `text` can't be a field *name* (it's
  the type keyword) → use `message`; `.size(..)` on a `text` field makes
  the Rust type `serde_json::Value` instead of `String` → omit it and
  validate in handlers; `@receive-only` makes Rust `skip_serializing` the
  field (hides it from the client's own state view).

## CSIL

CSIL = CBOR Service Interface Language; toolchain in
`~/repos/catalystcommunity/csilgen` (`csilgen` is on PATH here). Reference
usage: `~/repos/catalystcommunity/longhouse` (Go server + generated TS
client, `csil/longhouse.csil` + `csil/regenerate.sh` as source of truth)
and `corndogs`. We mirror longhouse's CBOR-RPC pattern, adapted from its
HTTP entry point to TCP/WebSocket framing.

`csilgen generate --input <f>.csil --target go    --output server/...`
`csilgen generate --input <f>.csil --target rust  --output coreclient/...`

## Positioning model (design intent)

Discrete **rooms** of **tiles**. Objects are **layered** and live at any
tile **and sub-tile** coordinate — a character may stand mid-tile, a desk
may sit 3/10 of a tile in, etc. See `docs/positioning.md`. Working plan:
integer tile coords + fixed-point sub-tile offset + a z/layer for stacking.

## Toolchain present on this machine

- Go 1.26.3 (`/usr/bin/go`).
- Rust/Cargo 1.95 (Arch system rust; **no rustup**). `wasm32-unknown-unknown`
  target and `wasm-pack` are **not** installed yet — needed before WASM
  builds. Plan to use `cargo build --target wasm32-unknown-unknown` +
  `wasm-bindgen`, or install wasm-pack, when we reach the web build step.
- `csilgen` on PATH at `~/.local/bin/csilgen`.

## Scaling (much later)

Different worlds on different servers, with player hand-off between them.
Design choices should not preclude this, but don't build it yet.

## Chat

The world has chat. (Design later; route over the same CSIL transport.)
