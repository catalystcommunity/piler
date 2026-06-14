# webclient

The browser **host** for the WASM client. A future native client wraps the
same `coreclient` the same way (with a TCP transport).

See [../docs/architecture.md](../docs/architecture.md) for the host model.

## Layout

- `src/lib.rs` — the wasm-bindgen `Client`: `join`, `moveBy`, `say`,
  `getRoomState`, `applyFrame`, `state`. It owns no socket and no canvas;
  it converts between JS values and the pure `coreclient`. This exported
  surface IS the interface the harness (and any browser host) uses.
- `web/` — the vanilla-TS vite harness. Loads the WASM, owns the WebSocket
  (binary CBOR frames) to the server, drives the client API, and renders
  state. No framework.
- `canvas-test.html` — standalone (no WASM, no server) test proving the
  canvas fills the viewport and tracks resizes (window resize,
  devicePixelRatio, fullscreen; press `f` for the JS Fullscreen API path).
- `index.html` — the original full-window host-shell placeholder; the real
  host is `web/`.

## Build & test

```bash
cargo test -p piler-webclient                       # native frame-builder test
# from the repo root:
wasm-pack build webclient --target web --out-dir web/wasm --out-name piler
cd webclient/web && npm install && npm run dev       # run the harness
```

Or just `./tools.sh dev` from the repo root for the whole stack.

## Note on shapes

The WASM serializes state with **snake_case** keys and **BigInt** for
64-bit ints (serde_wasm_bindgen). The harness reads snake_case and
`Number()`-coerces positions. The generated camelCase types in
`web/src/api` are for a future JSON path, not this WASM boundary.
