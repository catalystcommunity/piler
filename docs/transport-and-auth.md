# Transport & authentication

> Working intent. LinkKeys is under active development; we adopt
> capabilities as they land and fill gaps locally in the meantime.

## Transports

piler speaks the **standardized CSIL transport family** from csilgen
(`github.com/catalystcommunity/csilgen`, `transports/{go,rust}`). The live
protocol is **CSIL-Events** — a persistent, bidirectional, typed event stream
(csilgen's `docs/csil-events-transport.md`) — which is the realtime sibling of
CSIL-RPC and exactly the shape piler needs (movement, chat, presence flowing
continuously both ways). The transport library was in fact inspired by piler's
own event-push PoC; we now consume the shared implementation rather than a
hand-rolled envelope.

The library owns the envelope codec, framing, and connection lifecycle; the
byte carrier is **injected** (bring-your-own-carrier). piler plugs in two:

- **TCP** — native clients (and the smoke client). The library's
  `StreamCarrier`: a 4-byte big-endian length prefix + CBOR frame, with a
  max-frame guard.
- **WebSocket** — browsers (they cannot open raw TCP). A tiny `FrameCarrier`
  adapter over `catalystcommunity/websocks` binary messages (one WS binary
  message = one event frame; the WS framing supplies the boundaries).

Both carry the identical event set, so server logic and the client core are
carrier-agnostic; only the host's socket layer differs.

## Framing — two profiles

The server picks the wire profile via `PILER_WIRE_PROFILE` (default `compact`,
or `verbose`); the client follows it via the handshake. Routing is by event
*name* internally — the profile only changes how a name maps to the wire.

- **compact** (default): a positional CBOR array `[service_ord, op_ord, payload]`
  (or `[…, id, payload]` when correlated). The `World` service is ordinal `1`;
  each operation has an ordinal. Cheap for a 30 Hz loop — a few bytes of framing,
  no text keys re-spelled per frame.
- **verbose**: a text-keyed map `{ event, payload, … }` keyed by operation name.
  Debuggable; handy when watching the wire.

The `@wire-id`'d `World` service in `csil/piler.csil` is the source of truth for
the compact ordinals. csilgen validates `@wire-id` (uniqueness, reserved-0,
all-or-nothing) and uses it for breaking-change detection, but its generators
**don't emit ordinal constants**, so the server and client mirror the ordinals
(`server/internal/messages/ops.go`, `coreclient/src/client.rs`) — keep them in
sync with the spec.

- **Operations are separate when they're semantically separate.** Each is a
  one-way (`->` client push / `<- ` server push) or a request/reply (`<->`):
  `join`(0)↔`welcome`, `check-name`(1)↔`name-availability` are request/reply
  (reply optional, correlatable by `id`); `move`(2), `say`(4), `firework`(6) are
  client pushes; `tick`(3), `chat`(5), `burst`(7, the firework broadcast),
  `error`(8) are server pushes. `tick` is its own op, **not** a reply to `move`.
- **No request/reply correlation in use yet:** the `id` slot exists for the
  request/reply ops but the live path is fire-and-forget event-push.
- **Connection lifecycle / control plane.** Every connection opens with a
  handshake on the reserved control service (ordinal `0`), **always verbose**
  (the baseline the initiator can speak before the profile is known): the client
  sends `$hello` (offered versions + profiles + bound service), the server
  replies `$hello-ack` announcing the chosen profile; no application events flow
  before the ack, and they use the negotiated profile thereafter. The client
  withholds app frames until the ack so none is sent in the wrong profile.
  Heartbeats ride `$ping`/`$pong`, orderly shutdown `$close` — service ordinal
  `0`, handled in-band by the dispatcher.

## Authentication — LinkKeys

- Auth uses **LinkKeys** (`catalystcommunity/linkkeys`) as the identity /
  IDP layer.
- The credential has a natural home: CSIL-Events' `$hello` carries an optional
  `auth` field, bound once at handshake for the life of the connection (the
  spec's intended seam for exactly this). Browser and native flows both put the
  LinkKeys-minted credential there; mTLS streams may instead use the peer
  identity.
- A session, once authenticated, is bound to a character/identity; the
  server authorizes every intent against that session — **never** trusting
  a client-asserted identity.
- Specifics (token shape, refresh, capability set) are deferred until we
  see what LinkKeys exposes; design to swap details without touching game
  logic.

## Chat

Chat is a first-class feature and rides the same transport as a CSIL
service (channels, room-local vs. global, moderation hooks). Designed
later.
