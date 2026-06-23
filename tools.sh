#!/usr/bin/env bash
#
# piler dev workflow. One script to bring the PoC stack up and down.
#
#   ./tools.sh dev         # postgres + migrate + build wasm + server + vite, tail logs
#   ./tools.sh dev-down    # stop server, vite, postgres (keeps the pg volume)
#   ./tools.sh smoke       # run the TCP smoke client against a running server
#   ./tools.sh regen       # regenerate code from csil/piler.csil
#   ./tools.sh build-wasm  # rebuild the WASM client into webclient/web/wasm
#   ./tools.sh migrate     # run db migrations only
#   ./tools.sh pg-up|pg-down
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Make user-installed toolchains reachable from non-interactive shells.
for d in "$HOME/.local/bin" "$HOME/.cargo/bin"; do
    [ -d "$d" ] && export PATH="$d:$PATH"
done

# Source local config if present (falls back to the defaults below + the
# server's own env defaults).
if [ -f .env.dev ]; then
    set -a; . ./.env.dev; set +a
fi

PG_CONTAINER="piler-postgres-dev"
PG_IMAGE="postgres:17"
PG_USER="${PG_USER:-piler}"
PG_PASSWORD="${PG_PASSWORD:-devpass}"
PG_DB="${PG_DB:-piler}"
PG_PORT="${PG_PORT:-5433}"
PG_VOLUME="piler_postgres_data"

LOG_DIR="$SCRIPT_DIR/logs"
mkdir -p "$LOG_DIR"

say() { printf '▶ %s\n' "$*"; }

pg_up() {
    if docker ps --filter "name=^${PG_CONTAINER}$" --format '{{.Names}}' | grep -q .; then
        say "postgres already running"
        return
    fi
    if docker ps -a --filter "name=^${PG_CONTAINER}$" --format '{{.Names}}' | grep -q .; then
        say "starting existing postgres container"
        docker start "$PG_CONTAINER" >/dev/null
    else
        say "creating postgres container ($PG_IMAGE) on port $PG_PORT"
        docker run -d \
            --name "$PG_CONTAINER" \
            -e POSTGRES_USER="$PG_USER" \
            -e POSTGRES_PASSWORD="$PG_PASSWORD" \
            -e POSTGRES_DB="$PG_DB" \
            -p "${PG_PORT}:5432" \
            -v "${PG_VOLUME}:/var/lib/postgresql/data" \
            "$PG_IMAGE" >/dev/null
    fi

    say "waiting for postgres to accept connections"
    for _ in $(seq 1 30); do
        if docker exec "$PG_CONTAINER" pg_isready -U "$PG_USER" -d "$PG_DB" >/dev/null 2>&1; then
            say "postgres ready"
            return
        fi
        sleep 1
    done
    echo "postgres did not become ready in time" >&2
    exit 1
}

pg_down() {
    if docker ps --filter "name=^${PG_CONTAINER}$" --format '{{.Names}}' | grep -q .; then
        say "stopping postgres"
        docker stop "$PG_CONTAINER" >/dev/null
    fi
}

regen()      { ./csil/regenerate.sh; }
# Build the WASM client into webclient/web/wasm (gitignored — never committed).
#
# `cargo` alone only emits a raw .wasm; the JS bindings the host imports
# (web/wasm/piler.js + piler_bg.wasm) are produced by wasm-bindgen. So this needs
# the wasm32 target and the wasm-bindgen CLI (or wasm-pack, which wraps both).
build_wasm() {
    local out="webclient/web/wasm"
    # wasm-bindgen-cli must match the wasm-bindgen crate version, or it errors.
    local wb_ver
    wb_ver="$(awk '/name = "wasm-bindgen"$/{getline; gsub(/[",]/,"",$3); print $3; exit}' Cargo.lock)"

    # Shortcut: wasm-pack does cargo build + wasm-bindgen (+ wasm-opt) in one step.
    if command -v wasm-pack >/dev/null; then
        say "building WASM client (wasm-pack) → $out"
        wasm-pack build webclient --target web --out-dir web/wasm --out-name piler
        return
    fi

    # Fallback: cargo + wasm-bindgen directly.
    if ! rustc --print target-list 2>/dev/null | grep -qx wasm32-unknown-unknown; then
        echo "ERROR: rustc doesn't know the wasm32-unknown-unknown target." >&2
        echo "  Install it (rustup):  rustup target add wasm32-unknown-unknown" >&2
        echo "  (Arch system rust ships it; otherwise use rustup.)" >&2
        exit 1
    fi
    if ! command -v wasm-bindgen >/dev/null; then
        echo "ERROR: wasm-bindgen CLI not found (needed to generate the JS bindings;" >&2
        echo "       cargo alone only emits a raw .wasm with no JS glue)." >&2
        echo "  Install the matching version:  cargo install wasm-bindgen-cli --version ${wb_ver:-0.2}" >&2
        echo "  (or install wasm-pack:         cargo install wasm-pack)" >&2
        exit 1
    fi

    say "building WASM client (cargo + wasm-bindgen ${wb_ver:-?}) → $out"
    cargo build --release --target wasm32-unknown-unknown -p piler-webclient
    wasm-bindgen target/wasm32-unknown-unknown/release/piler_webclient.wasm \
        --target web --out-dir "$out" --out-name piler

    # Optional size optimization if wasm-opt is present.
    if command -v wasm-opt >/dev/null; then
        say "optimizing $out/piler_bg.wasm (wasm-opt)"
        wasm-opt -Oz "$out/piler_bg.wasm" -o "$out/piler_bg.wasm"
    fi
}
migrate() { say "running migrations"; ( cd server && go run . migrate ); }
smoke()   { ( cd server && go run . smoke ); }

dev() {
    pg_up
    migrate
    build_wasm

    # Ensure harness deps are installed.
    if [ ! -d webclient/web/node_modules ]; then
        say "installing web harness deps"
        ( cd webclient/web && npm install )
    fi

    say "starting server (logs/server.log)"
    ( cd server && exec go run . serve ) >"$LOG_DIR/server.log" 2>&1 &
    echo $! > "$LOG_DIR/server.pid"

    say "starting vite harness (logs/vite.log)"
    ( cd webclient/web && exec npm run dev ) >"$LOG_DIR/vite.log" 2>&1 &
    echo $! > "$LOG_DIR/vite.pid"

    cat <<EOF

piler dev is up:
  server  : TCP ${PILER_TCP_ADDR:-:6081}, WebSocket http://localhost${PILER_WS_ADDR:-:6080}/ws
  harness : http://localhost:5173   (web client — open in a browser to play)
  smoke   : ./tools.sh smoke        (TCP end-to-end check)

Both the browser (WebSocket) and smoke (TCP) paths are live and share the
same world state. Ctrl-C stops tailing (not the servers); run
./tools.sh dev-down to stop everything.

Tailing logs:
EOF
    tail -f "$LOG_DIR/server.log" "$LOG_DIR/vite.log"
}

dev_down() {
    for name in server vite; do
        local pid_file="$LOG_DIR/${name}.pid"
        if [ -f "$pid_file" ]; then
            local pid; pid="$(cat "$pid_file")"
            if kill -0 "$pid" 2>/dev/null; then
                say "stopping $name (pid $pid)"
                # kill the process group so child go/npm/node processes die too
                kill -- "-$pid" 2>/dev/null || kill "$pid" 2>/dev/null || true
            fi
            rm -f "$pid_file"
        fi
    done
    # Belt and suspenders: free the dev ports.
    if command -v fuser >/dev/null 2>&1; then
        fuser -k -TERM "6080/tcp" 2>/dev/null || true
        fuser -k -TERM "6081/tcp" 2>/dev/null || true
        fuser -k -TERM "5173/tcp" 2>/dev/null || true
    fi
    pg_down
}

case "${1:-}" in
    dev)        dev ;;
    dev-down)   dev_down ;;
    smoke)      smoke ;;
    regen)      regen ;;
    build-wasm) build_wasm ;;
    migrate)    pg_up; migrate ;;
    pg-up)      pg_up ;;
    pg-down)    pg_down ;;
    *)
        echo "usage: ./tools.sh {dev|dev-down|smoke|regen|build-wasm|migrate|pg-up|pg-down}" >&2
        exit 1 ;;
esac
