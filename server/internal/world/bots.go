// The /demo bot subsystem: server-driven wanderers that join a room on the
// "/demo [n]" chat command. The per-tick stepping is driven from World.Tick;
// everything else (spawn/despawn, naming, the wander step) lives here.
package world

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"

	"github.com/catalystcommunity/piler/server/internal/csil"
	"github.com/catalystcommunity/piler/server/internal/messages"
)

// Base names for /demo bots, in order. Indices beyond this fall back to
// "BotN" (and any collision with a live name gets a numeric suffix).
var botBaseNames = []string{
	"BotBob", "BotAlice", "BotJeff", "BotMimi", "BotZed", "BotPip", "BotLux", "BotGus",
}

// Short phrases bots pick from when they chat.
var botPhrases = []string{
	"hey there", "nice spot", "brb", "anyone around?", "this field is huge",
	"wanderlust", "just vibing", "tile life", "watch this", "over here!",
	"catch me", "la la la", "exploring", "is it lunch yet", "beep boop",
	"i am a bot", "do bots dream?", "hello world", "ping", "pong", "gg", "wp",
	"one more lap", "round and round", "to the corner!", "center stage",
	"edge runner", "mind the bounds", "so many tiles", "retro vibes",
	"FF6 forever", "who built this", "cozy little world", "say hi", "wave",
	"greetings", "howdy", "yo", "sup", "weeee", "zoom", "scenic route",
	"the long way", "lost again", "found you", "peekaboo", "race you",
	"slow and steady", "smooth moves", "groovy", "let's dance", "spin spin",
	"orbiting", "drifting", "strolling", "meandering", "patrolling", "on duty",
	"coffee break", "stretching my legs", "which way", "north?", "south!",
	"east", "west is best", "diagonal gang", "just passing through",
	"nothing to see here", "i live here now", "home sweet tile", "big plans",
	"stay awhile", "good times", "cheers", "high five", "teamwork",
	"adventure awaits", "onward", "let's go", "keep moving", "never stop",
	"almost there", "made it", "again!",
}

// stepBotLocked advances one bot toward its wander target, picking a fresh
// random target once it arrives. Caller holds w.mu.
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

// handleDemo parses the optional bot count from a "/demo [n]" command and
// toggles the demo bots for the room.
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
		id := messages.NextID()
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
