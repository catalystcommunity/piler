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
  characters, rooms, placed objects). Speaks the standardized **CSIL-Events**
  transport (from csilgen's `transports/go`) over **TCP** (native clients) and
  **WebSocket** (browsers, via `catalystcommunity/websocks`). CBOR payloads.
  Auth via `catalystcommunity/linkkeys` (LinkKeys, under active development —
  adopt as capabilities land; the credential will ride the events `$hello`).
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
  (see below). The live wire model is **correlation-free event-push** carried by
  the standardized **CSIL-Events** transport, not request/response: clients send
  intent events (`join`, `move`, `say`, `check-name`, `firework`) and the server
  pushes events (`welcome`, `tick`, `chat`, `name-availability`, `firework`,
  `error`). There is no `GetRoomState` — room state arrives in `welcome` and
  per-tick `tick` snapshots. The spec models this as the `@wire-id`'d `World`
  CSIL-Events service (op ordinals are the contract's source of truth);
  generation is **types-only** (`*-typesonly`) since piler hand-routes by those
  ordinals rather than using a generated router/client.
- **Field bounds**: movement is clamped server-side to `[0, FieldW] x
  [0, FieldH]` sub-units (config; default 48000×27000 = 1920×1080 at
  40 px/tile). Players spawn at field center. The field size rides in
  RoomState so the client sizes/draws the play area.
- **Avatars**: `coreclient::identicon(seed)` makes a deterministic
  GitHub-style 5×5 mirrored identicon from a seed (player_id today, a
  LinkKeys id later), exposed to JS as `avatar(seed)`; the harness draws it
  on the field and in the player list. Pure + unit-tested in coreclient.
- **Wire envelope**: the csilgen **CSIL-Events** `CsilEvent`, in either profile
  (compact `[service_ord, op_ord, payload]` or verbose `{event, payload}`),
  chosen by `PILER_WIRE_PROFILE` (default compact). `payload` is the tag-24 CBOR
  of one typed payload (generated into `server/internal/csil` and
  `coreclient/src/csil/types.rs`). The `World` service is ordinal 1; op ordinals
  mirror the spec's `@wire-id`s (`server/internal/messages/ops.go`,
  `coreclient/src/client.rs`). The library encodes/decodes the envelope; piler
  only supplies the inner payload bytes, so there's no nested-bytes mismatch
  across the Go/Rust/TS CBOR libs. The old hand-rolled
  `ClientMessage`/`ServerMessage` types are gone.
- **Dispatcher** (`internal/messages`) is carrier- and profile-agnostic:
  `HandleFrame(ctx, *Conn, frame)` decodes a `CsilEvent` (codec.go resolves both
  profiles to a name-keyed form) and routes World ops to handlers by name,
  pushing resulting events back through the `Conn` (no return value). Control
  events (`$ping`→`$pong`, `$close`) are serviced in-band. The
  `$hello`/`$hello-ack` handshake (`handshake.go`) runs once per connection
  before any app event — always verbose — and fixes the profile (the server
  offers only its configured one). Presence/identity is not tracked here — the
  `world` package keys actors by `Conn.ID` and binds identity at `join` (no auth
  yet; LinkKeys will ride `$hello`'s `auth`).
- **Transports** (`internal/transport`): both wrap their socket in a csilgen
  `FrameCarrier` and feed frames to `HandleFrame` via the shared `runConn`
  lifecycle (handshake → writer goroutine → read loop). **TCP** uses the
  library's `StreamCarrier` (4-byte big-endian length prefix + CBOR);
  **WebSocket** uses a tiny `wsCarrier` over `websocks` `MessageConn` (one
  binary message = one frame).
- **Store** (`internal/store`): pgx pool; `coredb` goose migrations
  (rooms/players/chat). `world` package holds the rules + sub-tile move
  carry math (floored division), with API-level tests using a fake store.
- **Client**: `coreclient` is pure (`build_*` event frames, `apply_frame`
  updates state) and native-tested; it depends on the csilgen `csilgen-transport`
  Rust crate for the same envelope codec the server uses. It speaks both profiles
  and adopts whichever the `$hello-ack` announces. `App::new` queues a `$hello` as
  the first outbound frame and **withholds app frames until the ack** (so none is
  sent in the wrong profile); `apply_frame` answers `$ping` with `build_pong`.
  `webclient` is the thin wasm-bindgen `App` wrapper over
  `coreclient::App` — forwards input (`keyDown`/`pointerDown`/`setText`), tunnels
  CBOR bytes (`receive`/`drainOutbound`), advances a frame (`render`), and
  exposes the RGBA framebuffer (`framePtr`/`frameLen`) for the host to blit.
  `webclient/web` is the vanilla-TS vite harness that owns the WebSocket and
  blits the framebuffer (no protocol logic — the `$hello` rides out as the first
  drained frame once the socket opens).
- **Smoke**: `./tools.sh smoke` (or `server smoke`) does the `$hello` handshake
  then runs join→move→say over TCP, asserting the `welcome`/`tick`/`chat` pushes
  come back — the works-today end-to-end check.

### Gotchas worth remembering

- **Standardized transport deps** come from the csilgen repo (treated as a
  normal internet dependency, not a filesystem path). Go: the module is
  `github.com/catalystcommunity/csilgen/transports/go` (required at a
  pseudo-version; `go get …@main`). Rust: `csilgen-transport` is a git
  dependency on `coreclient` (`cargo add --git`). The library is
  bring-your-own-carrier, so it pulls in no socket code and compiles to wasm32.
- **The handshake is mandatory.** No application event flows before
  `$hello`/`$hello-ack`. Any client (browser, native, smoke) must send `$hello`
  first; the server's `runConn` does the accept side before its read loop. A
  client that skips it gets its first frame rejected.
- **Two profiles, name-based routing.** `PILER_WIRE_PROFILE` (default `compact`,
  or `verbose`) sets the post-handshake profile; the handshake is always verbose
  and the client follows the server. Internally everything routes by event
  *name* (codec.go maps name↔wire per profile). The ordinal tables for compact
  live in `server/internal/messages/ops.go` and `coreclient/src/client.rs` and
  **must stay in sync with the `@wire-id`s in `csil/piler.csil`** — csilgen
  validates `@wire-id` and uses it for breaking-change detection but its
  generators don't emit ordinal constants (so generation is types-only). Ops are
  kept separate where they're separate (`tick` is its own server push, not a
  reply to `move`). The broadcast path encodes a frame once and `SendRaw`s it to
  the room — fine since every connection shares the one configured profile.
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
- Rust/Cargo 1.95 (Arch system rust; **no rustup**). The
  `wasm32-unknown-unknown` target is installed (system rust ships it).
  `./tools.sh build-wasm` builds the client into `webclient/web/wasm/` (which is
  **gitignored — we don't commit build artifacts**). It uses `wasm-pack` if
  present, else falls back to `cargo build --target wasm32 + wasm-bindgen` (cargo
  alone only emits a raw `.wasm`; wasm-bindgen generates the JS bindings the host
  imports). If neither tool is installed it errors with the exact
  `cargo install` command (pinned to the wasm-bindgen version in `Cargo.lock`).
  Both `wasm-pack` and `wasm-bindgen` (0.2.125) are installed under `~/.cargo/bin`.
- `csilgen` on PATH (`~/.cargo/bin` + `~/.local/bin`); must be a build new enough
  to parse `@wire-id` (it's on csilgen `main`; rebuild via
  `cargo install --path crates/csilgen-cli` if `csilgen validate` rejects
  `@wire-id`).

## Scaling (much later)

Different worlds on different servers, with player hand-off between them.
Design choices should not preclude this, but don't build it yet.

## Chat

The world has chat. (Design later; route over the same CSIL transport.)
