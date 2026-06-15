package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/catalystcommunity/piler/server/internal/csil"
)

// Postgres is the production Store backed by a pgx connection pool.
type Postgres struct {
	pool *pgxpool.Pool
}

// NewPostgres connects to the database, retrying briefly so it tolerates a
// just-started container in local dev.
func NewPostgres(ctx context.Context, uri string) (*Postgres, error) {
	cfg, err := pgxpool.ParseConfig(uri)
	if err != nil {
		return nil, fmt.Errorf("parsing db uri: %w", err)
	}

	var pool *pgxpool.Pool
	const attempts = 30
	for i := 1; i <= attempts; i++ {
		pool, err = pgxpool.NewWithConfig(ctx, cfg)
		if err == nil {
			if err = pool.Ping(ctx); err == nil {
				return &Postgres{pool: pool}, nil
			}
			pool.Close()
		}
		if i == attempts {
			return nil, fmt.Errorf("connecting to postgres after %d attempts: %w", attempts, err)
		}
		time.Sleep(time.Second)
	}
	return &Postgres{pool: pool}, nil
}

func (s *Postgres) Close() { s.pool.Close() }

func (s *Postgres) RoomExists(ctx context.Context, roomID string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM rooms WHERE room_id = $1)`, roomID).Scan(&exists)
	return exists, err
}

func (s *Postgres) CreatePlayer(ctx context.Context, p csil.Player) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO players (player_id, name, room_id, tile_x, tile_y, sub_x, sub_y, layer)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		string(p.PlayerId), p.Name, string(p.RoomId),
		p.Pos.TileX, p.Pos.TileY, p.Pos.SubX, p.Pos.SubY, p.Pos.Layer)
	return err
}

func (s *Postgres) UpdatePlayerPosition(ctx context.Context, playerID string, pos csil.Position) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE players
		 SET tile_x = $2, tile_y = $3, sub_x = $4, sub_y = $5, layer = $6, updated_at = now()
		 WHERE player_id = $1`,
		playerID, pos.TileX, pos.TileY, pos.SubX, pos.SubY, pos.Layer)
	return err
}

func (s *Postgres) InsertChat(ctx context.Context, roomID, playerID, name, message string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO chat_messages (room_id, player_id, name, message)
		 VALUES ($1, $2, $3, $4)`, roomID, playerID, name, message)
	return err
}

func (s *Postgres) RecentChat(ctx context.Context, roomID string, limit int) ([]csil.ChatMessage, error) {
	// Pull the newest `limit` rows, then reverse to chronological order.
	rows, err := s.pool.Query(ctx,
		`SELECT player_id, name, message, created_at
		 FROM chat_messages WHERE room_id = $1
		 ORDER BY created_at DESC, id DESC LIMIT $2`, roomID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	msgs := []csil.ChatMessage{}
	for rows.Next() {
		var m csil.ChatMessage
		var pid string
		var at time.Time
		if err := rows.Scan(&pid, &m.Name, &m.Message, &at); err != nil {
			return nil, err
		}
		m.PlayerId = csil.PlayerID(pid)
		m.At = csil.Timestamp(at.UTC().Format(time.RFC3339))
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Reverse to oldest-first.
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, nil
}
