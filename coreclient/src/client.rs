//! The network layer of the client: builds CBOR `ClientMessage` frames to
//! send and applies inbound `ServerMessage` pushes, holding the player's view
//! of the world (me + room). Pure: owns no socket and no UI. The `app` module
//! drives it; the host moves the bytes.

use std::fmt;

use serde::de::DeserializeOwned;
use serde::Serialize;

use crate::csil::{
    ChatMessage, CheckNameRequest, ClientMessage, FireworkEvent, JoinRequest, JoinResponse,
    MoveRequest, Player, RoomState, SayRequest, ServerMessage, Tick,
};

/// What an applied server push meant.
#[derive(Debug, Clone)]
pub enum Event {
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

/// The network client. Build messages with `build_*`, send the bytes, and feed
/// every received frame to `apply_frame`.
#[derive(Debug, Default)]
pub struct Client {
    me: Option<Player>,
    room: Option<RoomState>,
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

    pub fn build_join(&self, name: String, room_id: Option<String>) -> Vec<u8> {
        message("join", &JoinRequest { name, room_id })
    }

    pub fn build_move(&self, dx: i64, dy: i64) -> Vec<u8> {
        message("move", &MoveRequest { dx, dy })
    }

    pub fn build_say(&self, msg: String) -> Vec<u8> {
        message("say", &SayRequest { message: msg })
    }

    pub fn build_check_name(&self, name: String) -> Vec<u8> {
        message("check-name", &CheckNameRequest { name })
    }

    /// Intent to set off a firework. No body — the server uses the connection's
    /// session to know who fired and where.
    pub fn build_firework(&self) -> Vec<u8> {
        message_empty("firework")
    }

    /// Apply an inbound server push, updating local state and reporting what
    /// happened. An "error" event becomes `Err(ClientError::Server)`.
    pub fn apply_frame(&mut self, frame: &[u8]) -> Result<Event, ClientError> {
        let sm: ServerMessage =
            ciborium::from_reader(frame).map_err(|e| ClientError::Decode(e.to_string()))?;
        match sm.event.as_str() {
            "welcome" => {
                let w: JoinResponse = decode(&sm.body)?;
                self.me = Some(w.player.clone());
                self.room = Some(w.room);
                Ok(Event::Welcome)
            }
            "tick" => {
                let t: Tick = decode(&sm.body)?;
                if let Some(room) = self.room.as_mut() {
                    room.players = t.players;
                }
                self.refresh_me();
                Ok(Event::Tick)
            }
            "chat" => {
                let m: ChatMessage = decode(&sm.body)?;
                if let Some(room) = self.room.as_mut() {
                    room.recent_chat.push(m.clone());
                    let len = room.recent_chat.len();
                    if len > CHAT_LIMIT {
                        room.recent_chat.drain(0..len - CHAT_LIMIT);
                    }
                }
                Ok(Event::Chat(m))
            }
            "name-availability" => {
                let na: crate::csil::NameAvailability = decode(&sm.body)?;
                Ok(Event::NameAvailability {
                    name: na.name,
                    available: na.available,
                })
            }
            "firework" => {
                let fw: FireworkEvent = decode(&sm.body)?;
                Ok(Event::Firework {
                    player_id: fw.player_id,
                })
            }
            "error" => {
                let e: crate::csil::ErrorEvent = decode(&sm.body)?;
                Err(ClientError::Server {
                    code: e.code,
                    message: e.message,
                })
            }
            other => Err(ClientError::Protocol(format!("unknown event {other}"))),
        }
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

fn message<T: Serialize>(kind: &str, payload: &T) -> Vec<u8> {
    encode(&ClientMessage {
        kind: kind.to_string(),
        body: encode(payload),
    })
}

/// A message whose intent needs no payload (the server uses session identity).
fn message_empty(kind: &str) -> Vec<u8> {
    encode(&ClientMessage {
        kind: kind.to_string(),
        body: Vec::new(),
    })
}

fn encode<T: Serialize>(v: &T) -> Vec<u8> {
    let mut buf = Vec::new();
    ciborium::into_writer(v, &mut buf).expect("cbor encode");
    buf
}

fn decode<T: DeserializeOwned>(body: &[u8]) -> Result<T, ClientError> {
    ciborium::from_reader(body).map_err(|e| ClientError::Decode(e.to_string()))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::csil::{NameAvailability, Position};

    fn server_frame<T: Serialize>(event: &str, payload: &T) -> Vec<u8> {
        encode(&ServerMessage {
            event: event.to_string(),
            body: encode(payload),
        })
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
    fn welcome_then_tick_updates_me() {
        let mut c = Client::new();
        c.apply_frame(&server_frame(
            "welcome",
            &JoinResponse { player: player("me", "Me"), room: room() },
        ))
        .unwrap();
        assert_eq!(c.me().unwrap().player_id, "me");

        let mut moved = player("me", "Me");
        moved.pos.tile_x = 5;
        c.apply_frame(&server_frame("tick", &Tick { players: vec![moved] })).unwrap();
        assert_eq!(c.me().unwrap().pos.tile_x, 5);
        assert_eq!(c.room().unwrap().players.len(), 1);
    }

    #[test]
    fn chat_appends() {
        let mut c = Client::new();
        c.apply_frame(&server_frame("welcome", &JoinResponse { player: player("me", "Me"), room: room() })).unwrap();
        let cm = ChatMessage { player_id: "x".into(), name: "X".into(), message: "hi".into(), at: "now".into() };
        match c.apply_frame(&server_frame("chat", &cm)).unwrap() {
            Event::Chat(m) => assert_eq!(m.message, "hi"),
            other => panic!("expected chat, got {other:?}"),
        }
        assert_eq!(c.room().unwrap().recent_chat.len(), 1);
    }

    #[test]
    fn name_availability_reported() {
        let mut c = Client::new();
        match c
            .apply_frame(&server_frame("name-availability", &NameAvailability { name: "Ada".into(), available: false }))
            .unwrap()
        {
            Event::NameAvailability { name, available } => {
                assert_eq!(name, "Ada");
                assert!(!available);
            }
            other => panic!("expected name-availability, got {other:?}"),
        }
    }

    #[test]
    fn error_surfaces() {
        let mut c = Client::new();
        let err = c
            .apply_frame(&server_frame("error", &crate::csil::ErrorEvent { code: 400, message: "nope".into() }))
            .unwrap_err();
        assert!(matches!(err, ClientError::Server { code: 400, .. }));
    }
}
