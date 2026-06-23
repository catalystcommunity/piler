#!/usr/bin/env bash
# Regenerate generated code from piler.csil across all three languages.
#
#   Go   types → server/internal/csil/   (consumed by the server)
#   Rust types → coreclient/src/csil/     (consumed by coreclient)
#   TS   types → webclient/web/src/api/   (consumed by the harness)
#
# Types-only on every target: piler hand-routes the CSIL-Events World service by
# its @wire-id ordinals (server/internal/messages/ops.go, coreclient/src/client.rs),
# so it does NOT use csilgen's generated router/client — only the shared type
# definitions. The @wire-id service in piler.csil is the contract's source of
# truth; the runtime ordinal tables mirror it.
#
# Run from anywhere. Generated files are checked in.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SPEC="$REPO/csil/piler.csil"
GO_OUT="$REPO/server/internal/csil"
RUST_OUT="$REPO/coreclient/src/csil"
TS_OUT="$REPO/webclient/web/src/api"

# csilgen may live in ~/.local/bin or ~/.cargo/bin and not be on a
# non-interactive PATH; add both.
for d in "$HOME/.local/bin" "$HOME/.cargo/bin"; do
    [ -d "$d" ] && export PATH="$d:$PATH"
done

if ! command -v csilgen >/dev/null; then
    echo "ERROR: csilgen not on PATH (looked in \$PATH, ~/.local/bin, ~/.cargo/bin)" >&2
    exit 1
fi

csilgen validate --input "$SPEC"

# ---- Go (server) ----
# Clear stale files so a removed type doesn't linger (e.g. a dropped services file).
mkdir -p "$GO_OUT"
rm -f "$GO_OUT"/*.gen.go
csilgen generate --input "$SPEC" --target go-typesonly --output "$GO_OUT"
# csilgen emits `package api`; rename to match the import path.
for f in "$GO_OUT"/*.gen.go; do
    [ -e "$f" ] || continue
    sed -i 's/^package api$/package csil/' "$f"
done
command -v gofmt >/dev/null && gofmt -w "$GO_OUT" || true

# ---- Rust (coreclient) ----
# Types only: piler hand-routes, so the generated client/router isn't used.
# Clear stale files first so a dropped generator output doesn't linger.
mkdir -p "$RUST_OUT"
rm -f "$RUST_OUT"/*.rs
csilgen generate --input "$SPEC" --target rust-typesonly --output "$RUST_OUT"

# ---- TypeScript types (web harness) ----
mkdir -p "$TS_OUT"
csilgen generate --input "$SPEC" --target typescript-typesonly --output "$TS_OUT"

echo "Regenerated:"
echo "  Go:   $(ls "$GO_OUT"/*.gen.go 2>/dev/null | wc -l) file(s) in $GO_OUT"
echo "  Rust: $(ls "$RUST_OUT"/*.rs   2>/dev/null | wc -l) file(s) in $RUST_OUT"
echo "  TS:   $(ls "$TS_OUT"/*.ts     2>/dev/null | wc -l) file(s) in $TS_OUT"
