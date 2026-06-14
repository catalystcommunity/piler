# Transport & authentication

> Working intent. LinkKeys is under active development; we adopt
> capabilities as they land and fill gaps locally in the meantime.

## Transports

The same CSIL/CBOR RPC contract runs over two carriers:

- **TCP** — native clients. Length-prefixed CBOR frames, straightforward.
- **WebSocket** — browsers (they cannot open raw TCP). We bridge with the
  Go WebSocket server in `catalystcommunity/websocks`; CBOR rides in
  binary WS frames.

Both carry the identical message set so server logic and the client core
are transport-agnostic; only the host's socket layer differs.

## Framing

- Payloads are **CBOR**, shaped by the CSIL service/method definitions.
- Each RPC is a request/response (and server→client push for broadcasts:
  room state deltas, chat, presence). Exact envelope (method id, request
  id, payload) to be defined alongside the first CSIL service.
- Reference pattern: longhouse's `POST /api/csil/{service}/{method}` with
  CBOR bodies — we keep the `{service}/{method}` + CBOR shape but frame it
  over TCP/WS instead of HTTP.

## Authentication — LinkKeys

- Auth uses **LinkKeys** (`catalystcommunity/linkkeys`) as the identity /
  IDP layer.
- Browser flow will need the WebSocket handshake to carry or follow a
  LinkKeys-minted credential; native flow attaches it to the TCP session.
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
