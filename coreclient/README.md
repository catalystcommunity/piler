# coreclient

The platform-agnostic Rust game client core. Holds world state, the input
model, and rendering into an abstract framebuffer/bitbuffer. **No host- or
WASM-specific dependencies** — so it is unit-tested natively and reused by
any host (the `webclient`, or a future native client).

See [../docs/architecture.md](../docs/architecture.md) for the core-vs-host split.

## Status

Scaffolding: a `Framebuffer` (owned RGBA8 buffer) and a version constant.
Game state, input, and the render path come in roadmap step 3.

## Build & test

```bash
cargo test -p piler-coreclient      # native unit tests
```

Versioned independently of the server, against the versioned API.
