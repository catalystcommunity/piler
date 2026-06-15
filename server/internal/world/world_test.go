package world

import (
	"context"
	"testing"

	"github.com/fxamacker/cbor/v2"

	"github.com/catalystcommunity/piler/server/internal/csil"
	"github.com/catalystcommunity/piler/server/internal/rpc"
)

// fakeStore is an in-memory Store for API-level tests — no database needed.
type fakeStore struct {
	rooms map[string]bool
	chat  map[string][]csil.ChatMessage
}

func newFakeStore() *fakeStore {
	return &fakeStore{rooms: map[string]bool{"lobby": true}, chat: map[string][]csil.ChatMessage{}}
}

func (f *fakeStore) RoomExists(_ context.Context, roomID string) (bool, error) {
	return f.rooms[roomID], nil
}
func (f *fakeStore) CreatePlayer(_ context.Context, _ csil.Player) error { return nil }
func (f *fakeStore) UpdatePlayerPosition(_ context.Context, _ string, _ csil.Position) error {
	return nil
}
func (f *fakeStore) InsertChat(_ context.Context, roomID, playerID, name, message string) error {
	f.chat[roomID] = append(f.chat[roomID], csil.ChatMessage{
		PlayerId: csil.PlayerID(playerID), Name: name, Message: message, At: "now",
	})
	return nil
}
func (f *fakeStore) RecentChat(_ context.Context, roomID string, limit int) ([]csil.ChatMessage, error) {
	msgs := f.chat[roomID]
	if len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}
	return msgs, nil
}
func (f *fakeStore) Close() {}

// small field so the center is total (1000,1000) → tile(1,1).
func newWorld() *World { return New(newFakeStore(), 1000, 2000, 2000) }

func body(t *testing.T, v any) []byte {
	t.Helper()
	b, err := cbor.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func drain(t *testing.T, c *rpc.Conn) []csil.ServerMessage {
	t.Helper()
	var out []csil.ServerMessage
	for {
		select {
		case b := <-c.Out():
			var m csil.ServerMessage
			if err := cbor.Unmarshal(b, &m); err != nil {
				t.Fatalf("decode server message: %v", err)
			}
			out = append(out, m)
		default:
			return out
		}
	}
}

func find[T any](t *testing.T, msgs []csil.ServerMessage, event string) T {
	t.Helper()
	for _, m := range msgs {
		if m.Event == event {
			var v T
			if err := cbor.Unmarshal(m.Body, &v); err != nil {
				t.Fatalf("decode %q: %v", event, err)
			}
			return v
		}
	}
	t.Fatalf("no %q event in %v", event, names(msgs))
	var zero T
	return zero
}

func names(msgs []csil.ServerMessage) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.Event
	}
	return out
}

func TestJoinWelcomesAtCenter(t *testing.T) {
	w := newWorld()
	c := rpc.NewConn()
	if err := w.join(context.Background(), c, body(t, csil.JoinRequest{Name: "Ada"})); err != nil {
		t.Fatalf("join: %v", err)
	}
	wel := find[csil.JoinResponse](t, drain(t, c), "welcome")
	if wel.Player.Name != "Ada" || wel.Player.PlayerId == "" {
		t.Fatalf("welcome player = %+v", wel.Player)
	}
	if wel.Player.Pos.TileX != 1 || wel.Player.Pos.TileY != 1 {
		t.Fatalf("spawn tile = (%d,%d), want (1,1)", wel.Player.Pos.TileX, wel.Player.Pos.TileY)
	}
	if len(wel.Room.Players) != 1 {
		t.Fatalf("room players = %d, want 1", len(wel.Room.Players))
	}
}

func TestJoinRejectsShortAndDuplicateNames(t *testing.T) {
	w := newWorld()
	if err := w.join(context.Background(), rpc.NewConn(), body(t, csil.JoinRequest{Name: "ab"})); err == nil {
		t.Fatal("expected 2-char name to be rejected")
	}
	c1 := rpc.NewConn()
	if err := w.join(context.Background(), c1, body(t, csil.JoinRequest{Name: "Ada"})); err != nil {
		t.Fatalf("first join: %v", err)
	}
	// Case-insensitive duplicate.
	if err := w.join(context.Background(), rpc.NewConn(), body(t, csil.JoinRequest{Name: "ada"})); err == nil {
		t.Fatal("expected duplicate name to be rejected")
	}
}

func TestCheckNameAvailability(t *testing.T) {
	w := newWorld()
	c := rpc.NewConn()
	_ = w.join(context.Background(), c, body(t, csil.JoinRequest{Name: "Ada"}))
	_ = drain(t, c)

	probe := rpc.NewConn()
	_ = w.checkName(context.Background(), probe, body(t, csil.CheckNameRequest{Name: "Ada"}))
	taken := find[csil.NameAvailability](t, drain(t, probe), "name-availability")
	if taken.Available {
		t.Fatal("Ada should be taken")
	}
	_ = w.checkName(context.Background(), probe, body(t, csil.CheckNameRequest{Name: "Grace"}))
	free := find[csil.NameAvailability](t, drain(t, probe), "name-availability")
	if !free.Available {
		t.Fatal("Grace should be available")
	}
}

func TestMoveAppliesOnTickWithClamp(t *testing.T) {
	w := newWorld()
	c := rpc.NewConn()
	_ = w.join(context.Background(), c, body(t, csil.JoinRequest{Name: "Ada"}))
	_ = drain(t, c) // welcome

	// Intent +600 x, applied on tick: 1000+600 → tile 1 sub 600.
	if err := w.move(context.Background(), c, body(t, csil.MoveRequest{Dx: 600})); err != nil {
		t.Fatalf("move: %v", err)
	}
	w.Tick()
	p := find[csil.Tick](t, drain(t, c), "tick").Players[0]
	if p.Pos.TileX != 1 || p.Pos.SubX != 600 {
		t.Fatalf("after move: tile_x=%d sub_x=%d, want 1/600", p.Pos.TileX, p.Pos.SubX)
	}

	// Huge intent clamps to field width 2000 → tile 2 sub 0.
	_ = w.move(context.Background(), c, body(t, csil.MoveRequest{Dx: 100000}))
	w.Tick()
	p = find[csil.Tick](t, drain(t, c), "tick").Players[0]
	if p.Pos.TileX != 2 || p.Pos.SubX != 0 {
		t.Fatalf("clamp: tile_x=%d sub_x=%d, want 2/0", p.Pos.TileX, p.Pos.SubX)
	}

	// Leftward borrow across a tile boundary: total 2000-800 = 1200 → tile 1 sub 200.
	_ = w.move(context.Background(), c, body(t, csil.MoveRequest{Dx: -800}))
	w.Tick()
	p = find[csil.Tick](t, drain(t, c), "tick").Players[0]
	if p.Pos.TileX != 1 || p.Pos.SubX != 200 {
		t.Fatalf("borrow: tile_x=%d sub_x=%d, want 1/200", p.Pos.TileX, p.Pos.SubX)
	}

	// Borrow across the next boundary: total 1200-1000 = 200 → tile 0 sub 200.
	_ = w.move(context.Background(), c, body(t, csil.MoveRequest{Dx: -1000}))
	w.Tick()
	p = find[csil.Tick](t, drain(t, c), "tick").Players[0]
	if p.Pos.TileX != 0 || p.Pos.SubX != 200 {
		t.Fatalf("borrow across boundary: tile_x=%d sub_x=%d, want 0/200", p.Pos.TileX, p.Pos.SubX)
	}
}

func TestMoveBeforeJoinRejected(t *testing.T) {
	w := newWorld()
	if err := w.move(context.Background(), rpc.NewConn(), body(t, csil.MoveRequest{Dx: 1})); err == nil {
		t.Fatal("expected move-before-join to be rejected")
	}
}

func TestSayBroadcastsChat(t *testing.T) {
	w := newWorld()
	c := rpc.NewConn()
	_ = w.join(context.Background(), c, body(t, csil.JoinRequest{Name: "Lin"}))
	_ = drain(t, c)
	if err := w.say(context.Background(), c, body(t, csil.SayRequest{Message: "hello"})); err != nil {
		t.Fatalf("say: %v", err)
	}
	cm := find[csil.ChatMessage](t, drain(t, c), "chat")
	if cm.Message != "hello" || cm.Name != "Lin" {
		t.Fatalf("chat = <%s> %q", cm.Name, cm.Message)
	}
}

func TestDemoToggleAndClamp(t *testing.T) {
	w := newWorld()
	c := rpc.NewConn()
	_ = w.join(context.Background(), c, body(t, csil.JoinRequest{Name: "Ada"}))
	_ = drain(t, c)

	// "/demo 1" clamps up to the 3-bot minimum → 1 player + 3 bots = 4.
	_ = w.say(context.Background(), c, body(t, csil.SayRequest{Message: "/demo 1"}))
	w.Tick()
	if got := len(find[csil.Tick](t, drain(t, c), "tick").Players); got != 4 {
		t.Fatalf("after /demo 1: %d actors, want 4 (1 player + 3 bots)", got)
	}

	// "/demo" again toggles the bots off → back to just the player.
	_ = w.say(context.Background(), c, body(t, csil.SayRequest{Message: "/demo"}))
	w.Tick()
	if got := len(find[csil.Tick](t, drain(t, c), "tick").Players); got != 1 {
		t.Fatalf("after toggle off: %d actors, want 1", got)
	}
}
