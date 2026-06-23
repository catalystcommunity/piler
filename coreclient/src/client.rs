//! The network layer of the client: builds CSIL-Events frames to send and
//! applies inbound events, holding the player's view of the world (me + room).
//! Pure: owns no socket and no UI. The `app` module drives it; the host moves
//! the bytes.
//!
//! Every frame is a CSIL-Events event (csilgen's csil-events-transport.md). The
//! `$hello` handshake is always **verbose**; the server's `$hello-ack` announces
//! the profile for the rest of the connection (compact or verbose), and the
//! client switches to it. The World service (ordinal 1) and its operation
//! ordinals mirror the `@wire-id`s in `csil/piler.csil` (the source of truth) —
//! keep the `op` constants below in sync with that spec and the server's ops.go.

use std::fmt;

use serde::de::DeserializeOwned;
use serde::Serialize;

use csilgen_transport::conventions::CONTROL_SERVICE_ORD;
use csilgen_transport::events::{control, Event as WireEvent, Heartbeat, Hello, HelloAck, Profile};
use csilgen_transport::VERSION as TRANSPORT_VERSION;

use crate::csil::{
    ChatMessage, CheckNameRequest, FireworkEvent, JoinRequest, JoinResponse, MoveRequest, Player,
    RoomState, SayRequest, Tick,
};

/// The single CSIL service this connection binds to (advertised in `$hello`).
const SERVICE: &str = "World";

/// The World service ordinal (`@wire-id(1)`; 0 is the reserved control plane).
const WORLD_ORD: u64 = 1;

/// World operation ordinals (mirror csil/piler.csil's `@wire-id`s). Client→server
/// ops the client sends, and server→client ops it receives, share a numbering
/// space but are used in disjoint contexts.
mod op {
    // client→server
    pub const JOIN: u64 = 0;
    pub const CHECK_NAME: u64 = 1;
    pub const MOVE: u64 = 2;
    pub const SAY: u64 = 4;
    pub const FIREWORK: u64 = 6;
    // server→client
    pub const WELCOME: u64 = 0;
    pub const NAME_AVAILABILITY: u64 = 1;
    pub const TICK: u64 = 3;
    pub const CHAT: u64 = 5;
    pub const BURST: u64 = 7;
    pub const ERROR: u64 = 8;
}

/// Verbose names for the server→client ops (used to classify verbose frames).
fn s2c_op(name: &str) -> Option<u64> {
    Some(match name {
        "welcome" => op::WELCOME,
        "name-availability" => op::NAME_AVAILABILITY,
        "tick" => op::TICK,
        "chat" => op::CHAT,
        "burst" => op::BURST,
        "error" => op::ERROR,
        _ => return None,
    })
}

/// Map a verbose control name to its op ordinal.
fn control_op(name: &str) -> Option<u64> {
    Some(match name {
        control::HELLO_ACK_NAME => control::HELLO_ACK,
        control::PING_NAME => control::PING,
        control::PONG_NAME => control::PONG,
        control::CLOSE_NAME => control::CLOSE,
        control::ERROR_NAME => control::ERROR,
        _ => return None,
    })
}

/// What an applied server event meant.
#[derive(Debug, Clone)]
pub enum Event {
    /// The server accepted our `$hello`; the connection is live.
    HelloAck,
    /// A control-plane `$ping`; the caller should reply with `build_pong`.
    Ping { nonce: u64 },
    /// The peer sent `$close`; the connection is winding down.
    Closed,
    Welcome,
    Tick,
    Chat(ChatMessage),
    NameAvailability { name: String, available: bool },
    Firework { player_id: String },
}

/// Client-side failures.
#[derive(Debug, Clone)]
pub enum ClientError {
    Server { code: i64, message: String },
    Decode(String),
    Protocol(String),
}

impl fmt::Display for ClientError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            ClientError::Server { code, message } => write!(f, "server error {code}: {message}"),
            ClientError::Decode(m) => write!(f, "decode error: {m}"),
            ClientError::Protocol(m) => write!(f, "protocol error: {m}"),
        }
    }
}

impl std::error::Error for ClientError {}

const CHAT_LIMIT: usize = 50;

/// The network client. Open with `build_hello`, build messages with `build_*`,
/// send the bytes, and feed every received frame to `apply_frame`.
#[derive(Debug)]
pub struct Client {
    me: Option<Player>,
    room: Option<RoomState>,
    /// Wire profile for app frames; verbose until `$hello-ack` sets the
    /// negotiated profile.
    profile: Profile,
}

impl Default for Client {
    fn default() -> Self {
        Client { me: None, room: None, profile: Profile::Verbose }
    }
}

impl Client {
    pub fn new() -> Self {
        Client::default()
    }

    pub fn me(&self) -> Option<&Player> {
        self.me.as_ref()
    }

    pub fn room(&self) -> Option<&RoomState> {
        self.room.as_ref()
    }

    /// The CSIL-Events `$hello` opening the connection (always verbose): offer
    /// both profiles and bind to the World service. Must be the first frame sent.
    pub fn build_hello(&self) -> Vec<u8> {
        let payload = Hello {
            versions: vec![TRANSPORT_VERSION],
            profiles: vec![
                Profile::Compact.as_str().to_string(),
                Profile::Verbose.as_str().to_string(),
            ],
            service: Some(SERVICE.to_string()),
            auth: None,
        }
        .encode()
        .expect("encode hello");
        WireEvent::verbose(None, control::HELLO_NAME, payload)
            .encode(Profile::Verbose)
            .expect("encode hello frame")
    }

    /// A `$pong` answering a `$ping`, echoing the nonce (in the active profile).
    pub fn build_pong(&self, nonce: u64) -> Vec<u8> {
        let payload = Heartbeat { nonce, at: None }.encode().expect("encode pong");
        self.control_frame(control::PONG_NAME, control::PONG, payload)
    }

    pub fn build_join(&self, name: String, room_id: Option<String>) -> Vec<u8> {
        self.app_frame("join", op::JOIN, &JoinRequest { name, room_id })
    }

    pub fn build_move(&self, dx: i64, dy: i64) -> Vec<u8> {
        self.app_frame("move", op::MOVE, &MoveRequest { dx, dy })
    }

    pub fn build_say(&self, msg: String) -> Vec<u8> {
        self.app_frame("say", op::SAY, &SayRequest { message: msg })
    }

    pub fn build_check_name(&self, name: String) -> Vec<u8> {
        self.app_frame("check-name", op::CHECK_NAME, &CheckNameRequest { name })
    }

    /// Intent to set off a firework. Empty payload — the server uses the
    /// connection's session to know who fired and where.
    pub fn build_firework(&self) -> Vec<u8> {
        self.app_frame_raw("firework", op::FIREWORK, Vec::new())
    }

    /// Apply an inbound CSIL-Events frame, updating local state and reporting
    /// what happened. An "error" event becomes `Err(ClientError::Server)`; a
    /// transport `$error` becomes `Err(ClientError::Protocol)`.
    pub fn apply_frame(&mut self, frame: &[u8]) -> Result<Event, ClientError> {
        let ev = WireEvent::decode(frame, self.profile)
            .map_err(|e| ClientError::Decode(e.to_string()))?;
        let (control, opn) = self.classify(&ev)?;

        if control {
            return match opn {
                control::HELLO_ACK => {
                    // Adopt the negotiated profile for all subsequent frames.
                    let ack = HelloAck::decode(&ev.payload)
                        .map_err(|e| ClientError::Decode(e.to_string()))?;
                    if let Some(p) = Profile::parse(&ack.profile) {
                        self.profile = p;
                    }
                    Ok(Event::HelloAck)
                }
                control::PING => {
                    let hb = Heartbeat::decode(&ev.payload)
                        .map_err(|e| ClientError::Decode(e.to_string()))?;
                    Ok(Event::Ping { nonce: hb.nonce })
                }
                control::CLOSE => Ok(Event::Closed),
                control::ERROR => Err(ClientError::Protocol("transport $error".into())),
                other => Err(ClientError::Protocol(format!("unknown control op {other}"))),
            };
        }

        match opn {
            op::WELCOME => {
                let w: JoinResponse = decode(&ev.payload)?;
                self.me = Some(w.player.clone());
                self.room = Some(w.room);
                Ok(Event::Welcome)
            }
            op::TICK => {
                let t: Tick = decode(&ev.payload)?;
                if let Some(room) = self.room.as_mut() {
                    room.players = t.players;
                }
                self.refresh_me();
                Ok(Event::Tick)
            }
            op::CHAT => {
                let m: ChatMessage = decode(&ev.payload)?;
                if let Some(room) = self.room.as_mut() {
                    room.recent_chat.push(m.clone());
                    let len = room.recent_chat.len();
                    if len > CHAT_LIMIT {
                        room.recent_chat.drain(0..len - CHAT_LIMIT);
                    }
                }
                Ok(Event::Chat(m))
            }
            op::NAME_AVAILABILITY => {
                let na: crate::csil::NameAvailability = decode(&ev.payload)?;
                Ok(Event::NameAvailability {
                    name: na.name,
                    available: na.available,
                })
            }
            op::BURST => {
                let fw: FireworkEvent = decode(&ev.payload)?;
                Ok(Event::Firework {
                    player_id: fw.player_id,
                })
            }
            op::ERROR => {
                let e: crate::csil::ErrorEvent = decode(&ev.payload)?;
                Err(ClientError::Server {
                    code: e.code,
                    message: e.message,
                })
            }
            other => Err(ClientError::Protocol(format!("unknown operation {other}"))),
        }
    }

    /// Resolve a decoded frame to `(is_control, op_ord)` regardless of profile.
    fn classify(&self, ev: &WireEvent) -> Result<(bool, u64), ClientError> {
        match self.profile {
            Profile::Verbose => {
                let name = ev
                    .event
                    .as_deref()
                    .ok_or_else(|| ClientError::Protocol("verbose event missing name".into()))?;
                if let Some(op) = control_op(name) {
                    return Ok((true, op));
                }
                s2c_op(name)
                    .map(|op| (false, op))
                    .ok_or_else(|| ClientError::Protocol(format!("unknown event {name}")))
            }
            Profile::Compact => {
                let svc = ev
                    .service_ord
                    .ok_or_else(|| ClientError::Protocol("event missing service ordinal".into()))?;
                let op = ev
                    .op_ord
                    .ok_or_else(|| ClientError::Protocol("event missing op ordinal".into()))?;
                Ok((svc == CONTROL_SERVICE_ORD, op))
            }
        }
    }

    fn app_frame<T: Serialize>(&self, name: &str, op: u64, payload: &T) -> Vec<u8> {
        self.app_frame_raw(name, op, encode(payload))
    }

    fn app_frame_raw(&self, name: &str, op: u64, payload: Vec<u8>) -> Vec<u8> {
        let ev = match self.profile {
            Profile::Verbose => WireEvent::verbose(None, name, payload),
            Profile::Compact => WireEvent::compact(WORLD_ORD, op, payload),
        };
        ev.encode(self.profile).expect("encode event")
    }

    fn control_frame(&self, name: &str, op: u64, payload: Vec<u8>) -> Vec<u8> {
        let ev = match self.profile {
            Profile::Verbose => WireEvent::verbose(None, name, payload),
            Profile::Compact => WireEvent::compact(CONTROL_SERVICE_ORD, op, payload),
        };
        ev.encode(self.profile).expect("encode control event")
    }

    fn refresh_me(&mut self) {
        let my_id = match self.me.as_ref() {
            Some(me) => me.player_id.clone(),
            None => return,
        };
        if let Some(room) = self.room.as_ref() {
            if let Some(p) = room.players.iter().find(|p| p.player_id == my_id) {
                self.me = Some(p.clone());
            }
        }
    }
}

fn encode<T: Serialize>(v: &T) -> Vec<u8> {
    let mut buf = Vec::new();
    ciborium::into_writer(v, &mut buf).expect("cbor encode");
    buf
}

fn decode<T: DeserializeOwned>(payload: &[u8]) -> Result<T, ClientError> {
    ciborium::from_reader(payload).map_err(|e| ClientError::Decode(e.to_string()))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::csil::{NameAvailability, Position};

    fn ack_frame(profile: Profile) -> Vec<u8> {
        let payload = HelloAck { v: TRANSPORT_VERSION, profile: profile.as_str().to_string(), session: None }
            .encode()
            .unwrap();
        // The ack is always verbose.
        WireEvent::verbose(None, control::HELLO_ACK_NAME, payload)
            .encode(Profile::Verbose)
            .unwrap()
    }

    /// Build a server→client frame the way the server would, for the client's
    /// (already-negotiated) profile.
    fn server_frame<T: Serialize>(profile: Profile, name: &str, op: u64, payload: &T) -> Vec<u8> {
        let ev = match profile {
            Profile::Verbose => WireEvent::verbose(None, name, encode(payload)),
            Profile::Compact => WireEvent::compact(WORLD_ORD, op, encode(payload)),
        };
        ev.encode(profile).unwrap()
    }

    fn player(id: &str, name: &str) -> Player {
        Player {
            player_id: id.to_string(),
            name: name.to_string(),
            room_id: "lobby".to_string(),
            pos: Position { tile_x: 0, tile_y: 0, sub_x: 500, sub_y: 500, layer: 0 },
        }
    }

    fn room() -> RoomState {
        RoomState {
            room_id: "lobby".to_string(),
            players: vec![],
            recent_chat: vec![],
            field_w: 48000,
            field_h: 27000,
        }
    }

    #[test]
    fn hello_ack_switches_profile() {
        let mut c = Client::new();
        assert_eq!(c.profile, Profile::Verbose);
        assert!(matches!(c.apply_frame(&ack_frame(Profile::Compact)).unwrap(), Event::HelloAck));
        assert_eq!(c.profile, Profile::Compact, "client should adopt the negotiated profile");
    }

    #[test]
    fn full_flow_in_both_profiles() {
        for p in [Profile::Compact, Profile::Verbose] {
            let mut c = Client::new();
            c.apply_frame(&ack_frame(p)).unwrap(); // switch to p

            c.apply_frame(&server_frame(p, "welcome", op::WELCOME,
                &JoinResponse { player: player("me", "Me"), room: room() })).unwrap();
            assert_eq!(c.me().unwrap().player_id, "me");

            let mut moved = player("me", "Me");
            moved.pos.tile_x = 5;
            c.apply_frame(&server_frame(p, "tick", op::TICK, &Tick { players: vec![moved] })).unwrap();
            assert_eq!(c.me().unwrap().pos.tile_x, 5, "[{p:?}] tick should update me");

            let cm = ChatMessage { player_id: "x".into(), name: "X".into(), message: "hi".into(), at: "now".into() };
            match c.apply_frame(&server_frame(p, "chat", op::CHAT, &cm)).unwrap() {
                Event::Chat(m) => assert_eq!(m.message, "hi"),
                other => panic!("[{p:?}] expected chat, got {other:?}"),
            }

            match c.apply_frame(&server_frame(p, "name-availability", op::NAME_AVAILABILITY,
                &NameAvailability { name: "Ada".into(), available: false })).unwrap() {
                Event::NameAvailability { name, available } => { assert_eq!(name, "Ada"); assert!(!available); }
                other => panic!("[{p:?}] expected name-availability, got {other:?}"),
            }

            match c.apply_frame(&server_frame(p, "burst", op::BURST,
                &FireworkEvent { player_id: "z".into() })).unwrap() {
                Event::Firework { player_id } => assert_eq!(player_id, "z"),
                other => panic!("[{p:?}] expected firework, got {other:?}"),
            }

            let err = c.apply_frame(&server_frame(p, "error", op::ERROR,
                &crate::csil::ErrorEvent { code: 400, message: "nope".into() })).unwrap_err();
            assert!(matches!(err, ClientError::Server { code: 400, .. }), "[{p:?}] error should surface");
        }
    }

    #[test]
    fn ping_yields_nonce_and_pong_round_trips() {
        let mut c = Client::new();
        c.apply_frame(&ack_frame(Profile::Compact)).unwrap();
        // A $ping in the negotiated (compact) profile.
        let payload = Heartbeat { nonce: 7, at: None }.encode().unwrap();
        let ping = WireEvent::compact(CONTROL_SERVICE_ORD, control::PING, payload)
            .encode(Profile::Compact)
            .unwrap();
        match c.apply_frame(&ping).unwrap() {
            Event::Ping { nonce } => assert_eq!(nonce, 7),
            other => panic!("expected ping, got {other:?}"),
        }
        let pong = c.build_pong(7);
        let ev = WireEvent::decode(&pong, Profile::Compact).unwrap();
        assert_eq!(ev.service_ord, Some(CONTROL_SERVICE_ORD));
        assert_eq!(ev.op_ord, Some(control::PONG));
        assert_eq!(Heartbeat::decode(&ev.payload).unwrap().nonce, 7);
    }

    #[test]
    fn build_hello_is_verbose_and_offers_both() {
        let c = Client::new();
        let ev = WireEvent::decode(&c.build_hello(), Profile::Verbose).unwrap();
        assert_eq!(ev.event.as_deref(), Some(control::HELLO_NAME));
        let hello = Hello::decode(&ev.payload).unwrap();
        assert!(hello.versions.contains(&TRANSPORT_VERSION));
        assert!(hello.profiles.contains(&Profile::Compact.as_str().to_string()));
        assert!(hello.profiles.contains(&Profile::Verbose.as_str().to_string()));
    }

    #[test]
    fn build_move_uses_negotiated_profile() {
        let mut c = Client::new();
        c.apply_frame(&ack_frame(Profile::Compact)).unwrap();
        let ev = WireEvent::decode(&c.build_move(3, -4), Profile::Compact).unwrap();
        assert_eq!(ev.service_ord, Some(WORLD_ORD));
        assert_eq!(ev.op_ord, Some(op::MOVE));
    }
}
