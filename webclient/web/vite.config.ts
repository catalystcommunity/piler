import { defineConfig } from "vite";

// No framework — vanilla TS. Vite just serves the harness and bundles the
// WASM glue. The harness talks to the server's WebSocket endpoint directly
// (ws://localhost:6080/ws by default), so no dev proxy is needed.
export default defineConfig({
  // host: true binds 0.0.0.0 so others on the LAN can reach the dev server.
  // HMR still works for remote clients — Vite derives the HMR websocket host
  // from the page URL. The client's game WebSocket also uses location.hostname
  // (see src/main.ts), so a remote browser connects to the right server.
  server: { host: true, port: 5173 },
  // The harness uses top-level await (to init the WASM module), which needs
  // a modern target in both dev and the production build.
  esbuild: { target: "esnext" },
  build: { target: "esnext" },
});
