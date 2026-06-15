# webclient

The browser **host** for the WASM client. A future native client wraps the
same `coreclient` the same way (with a TCP transport).

See [../docs/architecture.md](../docs/architecture.md) for the host model.

## Layout

- `src/lib.rs` — the wasm-bindgen `App`: a thin wrapper over
  `coreclient::App`. It forwards input (`keyDown`/`pointerDown`/`setText`/…),
  tunnels bytes (`receive` in, `drainOutbound` out), advances a frame
  (`render`), and exposes the RGBA framebuffer (`framePtr`/`frameLen`) for the
  host to blit. It owns no socket and no canvas — all state, input handling,
  and rendering live in the pure `coreclient`. This exported surface IS the
  interface the harness (and any browser host) uses.
- `web/` — the vanilla-TS vite harness. Loads the WASM, owns the WebSocket
  (binary CBOR frames) to the server, drives the client API, and renders
  state. No framework.
- `canvas-test.html` — standalone (no WASM, no server) test proving the
  canvas fills the viewport and tracks resizes (window resize,
  devicePixelRatio, fullscreen; press `f` for the JS Fullscreen API path).

## Build & test

```bash
cargo build -p piler-webclient                      # wasm wrapper compiles
# from the repo root:
wasm-pack build webclient --target web --out-dir web/wasm --out-name piler
cd webclient/web && npm install && npm run dev       # run the harness
```

Or just `./tools.sh dev` from the repo root for the whole stack.

This crate is a thin pass-through with no logic of its own, so it carries no
tests — the game logic and rendering it wraps are tested natively in
`coreclient` (`cargo test -p piler-coreclient`).

## Note on the WASM boundary

State never crosses as JS objects: the core renders into an RGBA framebuffer
and the host blits it with `putImageData`, so there is no struct
serialization (and no snake_case/`BigInt` coercion) on this path. The host
only moves opaque CBOR byte frames to/from the WebSocket. The generated
camelCase types in `web/src/api` are for a future JSON path, not this boundary.
