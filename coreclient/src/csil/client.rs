//! Generated transport-agnostic service clients from CSIL specification

use super::types::*;

/// Error from a generated client call: a structured error the service returned,
/// or a transport-level failure. The caller-supplied `Transport` decides how an
/// error response maps onto `Service`.
#[derive(Debug, Clone)]
pub enum ClientError {
    Service { code: i64, message: String },
    Transport(String),
}

impl std::fmt::Display for ClientError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            ClientError::Service { code, message } => write!(f, "service error {code}: {message}"),
            ClientError::Transport(msg) => write!(f, "transport error: {msg}"),
        }
    }
}

impl std::error::Error for ClientError {}

/// The wire is the caller's concern: an implementation encodes `req` (CBOR over
/// HTTP, say), performs the call named by `(service, method)`, and decodes the
/// response into `Res`, or yields a `ClientError`.
pub trait Transport {
    fn call<Req, Res>(&self, service: &str, method: &str, req: &Req) -> Result<Res, ClientError>
    where
        Req: serde::Serialize,
        Res: serde::de::DeserializeOwned;
}

/// Typed client for the WorldService service.
pub struct WorldClient<T: Transport> {
    transport: T,
}

impl<T: Transport> WorldClient<T> {
    pub fn new(transport: T) -> Self {
        Self { transport }
    }

    /// join (request/response).
    pub fn join(&self, req: JoinRequest) -> Result<JoinResponse, ClientError> {
        self.transport.call("world", "Join", &req)
    }

    /// move-player (request/response).
    pub fn move_player(&self, req: MoveRequest) -> Result<Player, ClientError> {
        self.transport.call("world", "MovePlayer", &req)
    }

    /// get-room-state (request/response).
    pub fn get_room_state(&self, req: GetRoomStateRequest) -> Result<RoomState, ClientError> {
        self.transport.call("world", "GetRoomState", &req)
    }

    /// say (request/response).
    pub fn say(&self, req: SayRequest) -> Result<RoomState, ClientError> {
        self.transport.call("world", "Say", &req)
    }
}

