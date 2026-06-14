-- +goose Up

-- Discrete rooms (FF6-style). The PoC seeds a single "lobby".
CREATE TABLE rooms (
    room_id    text        PRIMARY KEY,
    name       text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Players and their authoritative position. Position is integer tile
-- coords + fixed-point sub-tile offset + a layer (see docs/positioning.md).
-- sub_x/sub_y are stored as bigint; the server keeps them in [0, SUB).
CREATE TABLE players (
    player_id  uuid        PRIMARY KEY,
    name       text        NOT NULL,
    room_id    text        NOT NULL REFERENCES rooms(room_id),
    tile_x     bigint      NOT NULL DEFAULT 0,
    tile_y     bigint      NOT NULL DEFAULT 0,
    sub_x      bigint      NOT NULL DEFAULT 0,
    sub_y      bigint      NOT NULL DEFAULT 0,
    layer      bigint      NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX players_room_idx ON players(room_id);

-- Room-scoped chat log. The PoC reads back the most recent N per room.
CREATE TABLE chat_messages (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    room_id    text        NOT NULL REFERENCES rooms(room_id),
    player_id  uuid        NOT NULL,
    name       text        NOT NULL,
    message    text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX chat_room_idx ON chat_messages(room_id, created_at);

-- Seed the default room so a fresh join has somewhere to land.
INSERT INTO rooms (room_id, name) VALUES ('lobby', 'Lobby');

-- +goose Down

DROP TABLE IF EXISTS chat_messages;
DROP TABLE IF EXISTS players;
DROP TABLE IF EXISTS rooms;
