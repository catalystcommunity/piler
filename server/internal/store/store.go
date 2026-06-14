// Package store is the persistence boundary for piler's authoritative
// long-term state. Handlers depend on the Store interface; the PostgreSQL
// implementation lives alongside, and tests use an in-memory fake.
package store

import (
	"context"

	"github.com/catalystcommunity/piler/server/internal/csil"
)

// Store persists rooms, players (with position), and chat. It deals in the
// generated CSIL types so handlers don't translate between layers.
type Store interface {
	// RoomExists reports whether a room with the given id exists.
	RoomExists(ctx context.Context, roomID string) (bool, error)

	// CreatePlayer inserts a new player row (caller assigns PlayerId).
	CreatePlayer(ctx context.Context, p csil.Player) error

	// GetPlayer returns a player by id.
	GetPlayer(ctx context.Context, playerID string) (csil.Player, error)

	// UpdatePlayerPosition persists a player's new position.
	UpdatePlayerPosition(ctx context.Context, playerID string, pos csil.Position) error

	// ListPlayersInRoom returns all players currently in a room.
	ListPlayersInRoom(ctx context.Context, roomID string) ([]csil.Player, error)

	// InsertChat appends a chat message to a room's log.
	InsertChat(ctx context.Context, roomID, playerID, name, message string) error

	// RecentChat returns up to limit most-recent messages for a room, in
	// chronological (oldest-first) order.
	RecentChat(ctx context.Context, roomID string, limit int) ([]csil.ChatMessage, error)

	// Close releases resources (e.g. the connection pool).
	Close()
}

// ErrNotFound is returned by Store implementations when a row is absent.
type ErrNotFound struct{ What string }

func (e *ErrNotFound) Error() string { return e.What + " not found" }
