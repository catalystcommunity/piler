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

- **Contract**: `csil/piler.csil` defines the domain types — Position (tile +
  fixed-point sub + layer), Player, ChatMessage, RoomState (carries
  `field_w`/`field_h`), request payloads — and the message-passing envelopes
  (see below). The live PoC wire model is **correlation-free event-push**, not
  request/response: clients send `ClientMessage`s (kinds `join`, `move`, `say`,
  `check-name`, `firework`) and the server pushes `ServerMessage`s (events
  `welcome`, `tick`, `chat`, `name-availability`, `firework`, `error`). There
  is no `GetRoomState` — room state arrives in `welcome` and per-tick `tick`
  snapshots. The spec also carries a `WorldService` request/response block kept
  **only for the future native client** (it generates a Rust `WorldClient` and
  an unused Go interface) — it is not the live protocol.
- **Field bounds**: movement is clamped server-side to `[0, FieldW] x
  [0, FieldH]` sub-units (config; default 48000×27000 = 1920×1080 at
  40 px/tile). Players spawn at field center. The field size rides in
  RoomState so the client sizes/draws the play area.
- **Avatars**: `coreclient::identicon(seed)` makes a deterministic
  GitHub-style 5×5 mirrored identicon from a seed (player_id today, a
  LinkKeys id later), exposed to JS as `avatar(seed)`; the harness draws it
  on the field and in the player list. Pure + unit-tested in coreclient.
- **Wire envelope**: `ClientMessage{kind, body}` (in) / `ServerMessage{event,
  body}` (out), generated from CSIL into `server/internal/csil` and
  `coreclient/src/csil/types.rs`. `body` is modelled as an opaque CBOR `bytes`
  string (not a nested typed union) so the dispatcher routes by `kind`/`event`
  without a nested-bytes encoding mismatch across the Go/Rust/TS CBOR libs;
  handlers decode `body` into the matching payload type themselves. There is no
  request/response id correlation and no hand-written `envelope.rs`.
- **Dispatcher** (`internal/rpc`) is transport-agnostic:
  `HandleMessage(ctx, *Conn, frame)` routes one inbound `ClientMessage` by kind
  and pushes any resulting events back through the `Conn` (no return value).
  Presence/identity is not tracked here — the `world` package keys its actors
  by `Conn.ID` and binds identity at `join` (no auth yet; LinkKeys later).
- **Transports** (`internal/transport`): raw **TCP** = 4-byte big-endian
  length prefix + CBOR frame; **WebSocket** via `websocks` `MessageConn`
  (binary frames). Both just move bytes to `HandleMessage` via the shared
  `runConn` lifecycle.
- **Store** (`internal/store`): pgx pool; `coredb` goose migrations
  (rooms/players/chat). `world` package holds the rules + sub-tile move
  carry math (floored division), with API-level tests using a fake store.
- **Client**: `coreclient` is pure (`build_*` request frames, `apply_frame`
  updates state) and native-tested. `webclient` is the thin wasm-bindgen `App`
  wrapper over `coreclient::App` — forwards input (`keyDown`/`pointerDown`/
  `setText`), tunnels CBOR bytes (`receive`/`drainOutbound`), advances a frame
  (`render`), and exposes the RGBA framebuffer (`framePtr`/`frameLen`) for the
  host to blit. `webclient/web` is the vanilla-TS vite harness that owns the
  WebSocket and blits the framebuffer.
- **Smoke**: `./tools.sh smoke` (or `server smoke`) runs join→move→say over
  TCP, asserting the `welcome`/`tick`/`chat` pushes come back — the
  works-today end-to-end check.

### Gotchas worth remembering

- `websocks` **now has** `MessageConn` (ReadMessage/WriteMessage, binary
  frames) on `main` — the WS path is live. piler asserts it structurally
  in `internal/transport/ws.go` so it compiles regardless.
- **Framebuffer boundary, not state objects**: the core renders into an RGBA
  framebuffer that the host blits with `putImageData`; no Rust struct is
  serialized to JS, so there is no snake_case/`BigInt` coercion on this path.
  The host only tunnels opaque CBOR byte frames. The generated camelCase TS
  types (`webclient/web/src/api`) are for a future JSON path, not this one.
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
