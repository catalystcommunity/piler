//! Generated types from CSIL specification

use serde::{Deserialize, Serialize};

pub type PlayerID = String;

pub type RoomID = String;

pub type Timestamp = String;

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct Position {
    pub tile_x: i64,
    pub tile_y: i64,
    pub sub_x: u64,
    pub sub_y: u64,
    pub layer: i64,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct Player {
    pub player_id: PlayerID,
    pub name: String,
    pub room_id: RoomID,
    pub pos: Position,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct ChatMessage {
    pub player_id: PlayerID,
    pub name: String,
    pub message: String,
    pub at: Timestamp,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct RoomState {
    pub room_id: RoomID,
    pub players: Vec<Player>,
    pub recent_chat: Vec<ChatMessage>,
    pub field_w: u64,
    pub field_h: u64,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct JoinRequest {
    pub name: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub room_id: Option<RoomID>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct JoinResponse {
    pub player: Player,
    pub room: RoomState,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct MoveRequest {
    pub dx: i64,
    pub dy: i64,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct SayRequest {
    pub message: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct FireworkIntent {
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct CheckNameRequest {
    pub name: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct NameAvailability {
    pub name: String,
    pub available: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct Tick {
    pub players: Vec<Player>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct FireworkEvent {
    pub player_id: PlayerID,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct ErrorEvent {
    pub code: i64,
    pub message: String,
}

