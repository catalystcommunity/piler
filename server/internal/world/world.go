// Package world is the authoritative simulation: it owns live presence
// (players + bots), steps the world on a fixed tick, and turns client
// messages into state changes and pushes. The server is the trust boundary —
// every rule lives here, never on a client.
package world

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/catalystcommunity/piler/server/internal/csil"
	"github.com/catalystcommunity/piler/server/internal/rpc"
	"github.com/catalystcommunity/piler/server/internal/store"
)

const (
	defaultRoom    = "lobby"
	minNameRunes   = 3
	maxNameRunes   = 32
	maxChatRunes   = 500
	recentChatSize = 50

	botSpeed       = 250    // sub-units per tick
	minBots        = 3
	maxBots        = 8
	botSayMinTicks = 30 * 10 // ~10s at 30Hz
	botSayMaxTicks = 30 * 15 // ~15s
)

// actor is one entity present in the world: a connected player (conn != nil)
// or a server-driven bot (conn == nil).
type actor struct {
	conn   *rpc.Conn
	player csil.Player

	// pending, unapplied move intent (player input accumulated since last tick)
	pendDx int64
	pendDy int64

	// bot state
	isBot     bool
	targetX   int64 // wander target, sub-units
	targetY   int64
	sayAtTick int64
}

// World holds all live state. A single mutex guards it; handlers and the tick
// briefly contend, which is fine at this scale.
type World struct {
	store  store.Store
	sub    uint64
	fieldW uint64
	fieldH uint64

	mu     sync.Mutex
	actors map[uint64]*actor
	rooms  map[string]map[uint64]*actor
	tickN  int64

	demoRunning bool
	demoBots    []uint64
}

func New(st store.Store, sub, fieldW, fieldH uint64) *World {
	return &World{
		store:  st,
		sub:    sub,
		fieldW: fieldW,
		fieldH: fieldH,
		actors: map[uint64]*actor{},
		rooms:  map[string]map[uint64]*actor{},
	}
}

// Register wires the message handlers into the dispatcher, keyed by kind.
func (w *World) Register(d *rpc.Dispatcher) {
	d.Register("join", w.join)
	d.Register("move", w.move)
	d.Register("say", w.say)
	d.Register("check-name", w.checkName)
	d.Register("firework", w.firework)
}

// Remove drops a connection's actor (on disconnect) and best-effort persists
// its last position. The next tick snapshot reflects the departure.
func (w *World) Remove(connID uint64) {
	w.mu.Lock()
	a := w.actors[connID]
	if a == nil {
		w.mu.Unlock()
		return
	}
	w.removeLocked(connID)
	pid, pos, isBot := string(a.player.PlayerId), a.player.Pos, a.isBot
	w.mu.Unlock()

	if !isBot {
		_ = w.store.UpdatePlayerPosition(context.Background(), pid, pos)
	}
}

// --- handlers ---

func (w *World) join(ctx context.Context, c *rpc.Conn, body []byte) error {
	var req csil.JoinRequest
	if err := rpc.Decode(body, &req); err != nil {
		return err
	}
	name := strings.TrimSpace(req.Name)
	if n := len([]rune(name)); n < minNameRunes || n > maxNameRunes {
		return rpc.BadRequest("name must be 3..32 characters")
	}
	roomID := defaultRoom
	if req.RoomId != nil && string(*req.RoomId) != "" {
		roomID = string(*req.RoomId)
	}
	exists, err := w.store.RoomExists(ctx, roomID)
	if err != nil {
		return rpc.Internal("room lookup failed")
	}
	if !exists {
		return rpc.BadRequest("unknown room: " + roomID)
	}

	p := csil.Player{
		PlayerId: csil.PlayerID(uuid.NewString()),
		Name:     name,
		RoomId:   csil.RoomID(roomID),
		Pos:      posFromTotal(int64(w.fieldW/2), int64(w.fieldH/2), int64(w.sub), 0),
	}

	w.mu.Lock()
	if _, joined := w.actors[c.ID]; joined {
		w.mu.Unlock()
		return rpc.BadRequest("already joined")
	}
	if w.nameTakenLocked(name) {
		w.mu.Unlock()
		return rpc.BadRequest("name already in use")
	}
	w.addLocked(c.ID, &actor{conn: c, player: p})
	players := w.playersLocked(roomID)
	w.mu.Unlock()

	_ = w.store.CreatePlayer(ctx, p) // best-effort persistence of the character
	chat, _ := w.store.RecentChat(ctx, roomID, recentChatSize)
	room := csil.RoomState{
		RoomId:     csil.RoomID(roomID),
		Players:    players,
		RecentChat: chat,
		FieldW:     w.fieldW,
		FieldH:     w.fieldH,
	}
	c.PushEvent("welcome", csil.JoinResponse{Player: p, Room: room})
	return nil
}

func (w *World) move(_ context.Context, c *rpc.Conn, body []byte) error {
	var req csil.MoveRequest
	if err := rpc.Decode(body, &req); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	a := w.actors[c.ID]
	if a == nil {
		return rpc.Unauthorized("join a room before moving")
	}
	a.pendDx += req.Dx
	a.pendDy += req.Dy
	return nil
}

func (w *World) say(ctx context.Context, c *rpc.Conn, body []byte) error {
	var req csil.SayRequest
	if err := rpc.Decode(body, &req); err != nil {
		return err
	}
	msg := strings.TrimSpace(req.Message)

	w.mu.Lock()
	a := w.actors[c.ID]
	if a == nil {
		w.mu.Unlock()
		return rpc.Unauthorized("join a room before chatting")
	}
	name, pid, roomID := a.player.Name, string(a.player.PlayerId), string(a.player.RoomId)
	w.mu.Unlock()

	if strings.HasPrefix(msg, "/demo") {
		w.handleDemo(msg, roomID)
		return nil
	}
	if r := len([]rune(msg)); r < 1 || r > maxChatRunes {
		return rpc.BadRequest("message must be 1..500 characters")
	}
	cm := csil.ChatMessage{
		PlayerId: csil.PlayerID(pid),
		Name:     name,
		Message:  msg,
		At:       csil.Timestamp(time.Now().UTC().Format(time.RFC3339)),
	}
	_ = w.store.InsertChat(ctx, roomID, pid, name, msg)
	w.broadcastChat(roomID, cm)
	return nil
}

// firework is an intent (no body): the calling player set off a firework. We
// broadcast it to everyone else in the room so they render it above that
// player; the sender already shows its own locally.
func (w *World) firework(_ context.Context, c *rpc.Conn, _ []byte) error {
	w.mu.Lock()
	a := w.actors[c.ID]
	if a == nil {
		w.mu.Unlock()
		return rpc.Unauthorized("join a room before setting off fireworks")
	}
	pid, roomID := string(a.player.PlayerId), string(a.player.RoomId)
	w.mu.Unlock()

	w.broadcastFireworkExcept(roomID, c.ID, pid)
	return nil
}

func (w *World) checkName(_ context.Context, c *rpc.Conn, body []byte) error {
	var req csil.CheckNameRequest
	if err := rpc.Decode(body, &req); err != nil {
		return err
	}
	name := strings.TrimSpace(req.Name)
	w.mu.Lock()
	avail := len([]rune(name)) >= minNameRunes && !w.nameTakenLocked(name)
	w.mu.Unlock()
	c.PushEvent("name-availability", csil.NameAvailability{Name: req.Name, Available: avail})
	return nil
}

// --- tick ---

// Tick advances the simulation one step: apply each player's accumulated move
// intent, step the bots, then push a per-room roster snapshot to every
// connection. Called at a fixed rate (30Hz) by the server.
func (w *World) Tick() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.tickN++

	type chatOut struct {
		room string
		cm   csil.ChatMessage
	}
	var botChats []chatOut

	for _, room := range w.rooms {
		for _, a := range room {
			if a.isBot {
				w.stepBotLocked(a)
				if w.tickN >= a.sayAtTick {
					botChats = append(botChats, chatOut{
						room: string(a.player.RoomId),
						cm: csil.ChatMessage{
							PlayerId: a.player.PlayerId, Name: a.player.Name,
							Message: botPhrases[rand.Intn(len(botPhrases))],
							At:      csil.Timestamp(time.Now().UTC().Format(time.RFC3339)),
						},
					})
					a.sayAtTick = w.tickN + int64(botSayMinTicks+rand.Intn(botSayMaxTicks-botSayMinTicks+1))
				}
			} else if a.pendDx != 0 || a.pendDy != 0 {
				a.player.Pos = applyMove(a.player.Pos, a.pendDx, a.pendDy, w.sub, w.fieldW, w.fieldH)
				a.pendDx, a.pendDy = 0, 0
			}
		}
	}

	for roomID, room := range w.rooms {
		players := make([]csil.Player, 0, len(room))
		for _, a := range room {
			players = append(players, a.player)
		}
		frame := rpc.EncodeEvent("tick", csil.Tick{Players: players})
		for _, a := range room {
			if a.conn != nil {
				a.conn.SendRaw(frame)
			}
		}
		_ = roomID
	}

	for _, ch := range botChats {
		frame := rpc.EncodeEvent("chat", ch.cm)
		for _, a := range w.rooms[ch.room] {
			if a.conn != nil {
				a.conn.SendRaw(frame)
			}
		}
	}
}

func (w *World) stepBotLocked(a *actor) {
	s := int64(w.sub)
	curX := a.player.Pos.TileX*s + int64(a.player.Pos.SubX)
	curY := a.player.Pos.TileY*s + int64(a.player.Pos.SubY)
	if absI64(a.targetX-curX) <= botSpeed && absI64(a.targetY-curY) <= botSpeed {
		a.targetX = rand.Int63n(int64(w.fieldW) + 1)
		a.targetY = rand.Int63n(int64(w.fieldH) + 1)
	}
	dx := clampStep(a.targetX-curX, botSpeed)
	dy := clampStep(a.targetY-curY, botSpeed)
	a.player.Pos = applyMove(a.player.Pos, dx, dy, w.sub, w.fieldW, w.fieldH)
}

// --- /demo ---

func (w *World) handleDemo(msg, roomID string) {
	n := 0
	if fields := strings.Fields(msg); len(fields) >= 2 {
		if v, err := strconv.Atoi(fields[1]); err == nil {
			n = v
		}
	}
	w.toggleDemo(roomID, n)
}

func (w *World) toggleDemo(roomID string, n int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.demoRunning {
		for _, id := range w.demoBots {
			w.removeLocked(id)
		}
		w.demoBots = nil
		w.demoRunning = false
		return
	}

	if n < minBots {
		n = minBots
	}
	if n > maxBots {
		n = maxBots
	}
	for i := 0; i < n; i++ {
		id := rpc.NextID()
		name := w.botNameLocked(i)
		bx := rand.Int63n(int64(w.fieldW) + 1)
		by := rand.Int63n(int64(w.fieldH) + 1)
		a := &actor{
			isBot: true,
			player: csil.Player{
				PlayerId: csil.PlayerID("bot:" + name), // stable id → stable identicon
				Name:     name,
				RoomId:   csil.RoomID(roomID),
				Pos:      posFromTotal(bx, by, int64(w.sub), 0),
			},
			targetX:   rand.Int63n(int64(w.fieldW) + 1),
			targetY:   rand.Int63n(int64(w.fieldH) + 1),
			sayAtTick: w.tickN + int64(botSayMinTicks+rand.Intn(botSayMaxTicks-botSayMinTicks+1)),
		}
		w.addLocked(id, a)
		w.demoBots = append(w.demoBots, id)
	}
	w.demoRunning = true
}

func (w *World) botNameLocked(i int) string {
	base := fmt.Sprintf("Bot%d", i+1)
	if i < len(botBaseNames) {
		base = botBaseNames[i]
	}
	name, n := base, 2
	for w.nameTakenLocked(name) {
		name = fmt.Sprintf("%s%d", base, n)
		n++
	}
	return name
}

// --- locked helpers ---

func (w *World) addLocked(id uint64, a *actor) {
	w.actors[id] = a
	room := string(a.player.RoomId)
	if w.rooms[room] == nil {
		w.rooms[room] = map[uint64]*actor{}
	}
	w.rooms[room][id] = a
}

func (w *World) removeLocked(id uint64) {
	a := w.actors[id]
	if a == nil {
		return
	}
	delete(w.actors, id)
	room := string(a.player.RoomId)
	if m := w.rooms[room]; m != nil {
		delete(m, id)
		if len(m) == 0 {
			delete(w.rooms, room)
		}
	}
}

func (w *World) nameTakenLocked(name string) bool {
	for _, a := range w.actors {
		if strings.EqualFold(a.player.Name, name) {
			return true
		}
	}
	return false
}

func (w *World) playersLocked(roomID string) []csil.Player {
	m := w.rooms[roomID]
	out := make([]csil.Player, 0, len(m))
	for _, a := range m {
		out = append(out, a.player)
	}
	return out
}

func (w *World) broadcastChat(roomID string, cm csil.ChatMessage) {
	frame := rpc.EncodeEvent("chat", cm)
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, a := range w.rooms[roomID] {
		if a.conn != nil {
			a.conn.SendRaw(frame)
		}
	}
}

func (w *World) broadcastFireworkExcept(roomID string, exceptID uint64, pid string) {
	frame := rpc.EncodeEvent("firework", csil.FireworkEvent{PlayerId: csil.PlayerID(pid)})
	w.mu.Lock()
	defer w.mu.Unlock()
	for id, a := range w.rooms[roomID] {
		if a.conn != nil && id != exceptID {
			a.conn.SendRaw(frame)
		}
	}
}
