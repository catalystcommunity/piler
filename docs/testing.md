# Testing

Tests split on the **client/server boundary**. The server is the trust
boundary; clients are untrusted and replaceable. Each side is tested on
its own terms.

## Server (the authority)

- **API-level tests** against the CSIL/CBOR contract: every gameplay rule
  is enforced here and asserted here. Movement legality, interaction
  range, inventory mutations, room transitions, chat moderation — all
  proven server-side.
- We **never** rely on a client to uphold a rule, so a hostile or
  home-grown client cannot be the reason a test passes. Tests should
  include "client sent something illegal" cases, not just happy paths.
- Run with `go test ./...` in `server/`.

## Core client (presentation + local logic)

- `coreclient` is platform-agnostic and native-testable — no host or WASM
  dependency. Unit-test world state, the input model, and rendering into
  the `Framebuffer` directly (e.g. assert pixels/regions for a known
  scene, assert sub-tile position math).
- These cover *presentation and local prediction* only. They must not be
  treated as proof of any rule the server owns.
- Run with `cargo test -p piler-coreclient`.

## Web host (browser integration)

The host (`webclient`) is intentionally **thin glue** — minimal
JavaScript. Most logic lives in the Rust core, so most testing happens
natively against the core. The host layer still needs integration checks:
does the WASM load, does the framebuffer reach the canvas, does input
arrive, does the WebSocket carry frames.

### Driving the canvas from automation (Chrome DevTools MCP / headless)

A `<canvas>` is **opaque to the accessibility tree** — DOM/a11y snapshots
see one blank box, never the tiles, characters, or placed objects inside
it. So element-based selectors cannot find anything the game draws. That
leaves three handles, in order of preference:

1. **A client debug/automation API** (preferred). The WASM core exposes a
   small introspection + input surface on `window` (e.g.
   `window.__piler`) so automation can:
   - **read state** — current room, entity positions (tile + sub-tile),
     placed objects, connection status;
   - **inject input intents** — move, interact, chat — deterministically,
     without synthesizing pixel-coordinate events.

   This is the reliable path and avoids fragile pixel-matching. It is a
   *client-side introspection hook only* — it asserts no authority, so it
   does not weaken the trust boundary (the server still validates every
   intent). Gate it to dev/test builds.

2. **Direct pixel reads** via injected JS — `getImageData` (2D) or
   `gl.readPixels` (WebGL) — plus screenshots, to verify the framebuffer
   actually rendered. Good for "did it draw," weak for "what is the game
   state."

3. **Coordinate input** — synthetic click/drag/key events at pixel
   positions. Real, but brittle; use sparingly where 1 suffices.

Concretely, Chrome DevTools MCP can: screenshot the canvas, run JS to read
`window.__piler` state or raw pixels, call exposed functions, and dispatch
coordinate/keyboard input. What it cannot do is select game objects as DOM
elements — hence the debug API.

### Implication for the build

Keep the JS glue minimal, and have the core export `window.__piler` (or
similar) in dev/test builds for both introspection and input injection.
Design this alongside the wasm-bindgen surface in roadmap step 4.

### Production builds: the debug interface must be absent, not just hidden

In production the debug surface is **compiled out entirely**, on both
sides of the boundary — not merely disabled at runtime. A hidden-but-
present hook is an attack surface (it lets a crafted page or a tampered
client read/drive game state) and undercuts the "untrusted client" stance.

- **WASM (Rust) side** — gate the whole debug API behind a Cargo feature
  (e.g. `debug-api`), **off by default**, and `#[cfg(feature =
  "debug-api")]` every export, struct, and the `window.__piler`
  installation. With the feature off, the code does not exist in the
  binary — there is nothing to call, nothing to tree-shake-miss. Dev/test
  builds turn the feature on; release builds never do.
- **DOM / JS glue side** — the production host must not reference or
  install `window.__piler`. Because the glue is thin and the wasm export
  is absent in release, the cleanest arrangement is: the glue *only* wires
  the hook when the wasm module reports the feature present (or build the
  debug bits into a separate dev-only entry that production never ships).
  No `__piler` in the production DOM, period.
- **Verify it's gone** — a release-build check (grep the built wasm/JS
  bundle for the hook symbol, and assert `window.__piler === undefined` in
  a production smoke test) so a regression that re-enables it fails CI.

Net: dev/test builds carry the hook for automation; release artifacts have
no debug symbol, no `window.__piler`, no injectable input path.
