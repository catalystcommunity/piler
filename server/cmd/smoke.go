package cmd

import (
	"fmt"
	"net"
	"time"

	"github.com/fxamacker/cbor/v2"

	txp "github.com/catalystcommunity/csilgen/transports/go"

	"github.com/catalystcommunity/piler/server/internal/config"
	"github.com/catalystcommunity/piler/server/internal/csil"
)

// The World service ordinal + the operation ordinals/names this smoke client
// uses (compact profile keys by ordinal, verbose by name). These mirror the
// @wire-id annotations in csil/piler.csil; the smoke client is intentionally
// standalone (it does not import the server's internal packages), carrying its
// own copy of the contract exactly as a third-party client would.
const worldOrd uint64 = 1

// client→server op name → ordinal.
var c2sOp = map[string]uint64{
	"join": 0, "check-name": 1, "move": 2, "say": 4, "firework": 6,
}

// server→client op ordinal → event name.
var s2cName = map[uint64]string{
	0: "welcome", 1: "name-availability", 3: "tick", 5: "chat", 7: "burst", 8: "error",
}

// Smoke is a tiny TCP client exercising the full flow against a running server
// over the standardized CSIL-Events transport: handshake ($hello/$hello-ack),
// join → "welcome", move → see it applied in a "tick" snapshot, say → "chat".
// It follows whatever wire profile the server negotiates (compact or verbose),
// proving the end-to-end event-push path without a browser.
func Smoke(flags map[string]string) error {
	config.ApplyFlags(flags)

	conn, err := net.DialTimeout("tcp", config.TCPAddr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("dialing %s: %w", config.TCPAddr, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	c := &smokeClient{carrier: txp.NewStreamCarrier(conn)}
	if err := c.handshake(); err != nil {
		return fmt.Errorf("handshake: %w", err)
	}
	fmt.Printf("handshake OK (wire profile: %s)\n", c.profile)

	if err := c.send("join", csil.JoinRequest{Name: "smoke-tester"}); err != nil {
		return err
	}
	var welcome csil.JoinResponse
	if err := c.await("welcome", &welcome); err != nil {
		return err
	}
	spawn := welcome.Player.Pos
	fmt.Printf("welcomed as %s (%s) at tile (%d,%d) sub (%d,%d)\n",
		welcome.Player.Name, welcome.Player.PlayerId,
		spawn.TileX, spawn.TileY, spawn.SubX, spawn.SubY)

	if err := c.send("move", csil.MoveRequest{Dx: 600, Dy: -300}); err != nil {
		return err
	}
	moved, err := c.awaitMoved(welcome.Player.PlayerId, spawn)
	if err != nil {
		return err
	}
	fmt.Printf("moved (seen in tick) to tile (%d,%d) sub (%d,%d)\n",
		moved.Pos.TileX, moved.Pos.TileY, moved.Pos.SubX, moved.Pos.SubY)

	if err := c.send("say", csil.SayRequest{Message: "hello from the smoke client"}); err != nil {
		return err
	}
	var chat csil.ChatMessage
	if err := c.await("chat", &chat); err != nil {
		return err
	}
	fmt.Printf("chat broadcast: <%s> %s\n", chat.Name, chat.Message)
	fmt.Println("smoke flow OK")
	return nil
}

type smokeClient struct {
	carrier txp.FrameCarrier
	profile txp.Profile // negotiated at handshake; governs app frames
}

// handshake is the initiator side of the CSIL-Events control plane (always
// verbose): offer both profiles in $hello and adopt whichever the server picks.
func (c *smokeClient) handshake() error {
	svc := "World"
	hello, err := txp.Hello{
		Versions: []uint64{txp.VERSION},
		Profiles: []string{txp.ProfileCompact.String(), txp.ProfileVerbose.String()},
		Service:  &svc,
	}.Encode()
	if err != nil {
		return err
	}
	helloFrame, err := txp.NewVerboseEvent(nil, txp.HelloName, hello).Encode(txp.ProfileVerbose)
	if err != nil {
		return err
	}
	if err := c.carrier.SendFrame(helloFrame); err != nil {
		return err
	}

	frame, err := c.carrier.RecvFrame()
	if err != nil {
		return err
	}
	if frame == nil {
		return fmt.Errorf("server closed before $hello-ack")
	}
	ev, err := txp.DecodeEvent(frame, txp.ProfileVerbose)
	if err != nil {
		return err
	}
	if ev.Event == nil || *ev.Event != txp.HelloAckName {
		return fmt.Errorf("expected %s, got %v", txp.HelloAckName, ev.Event)
	}
	ack, err := txp.DecodeHelloAck(ev.Payload)
	if err != nil {
		return err
	}
	p, ok := txp.ParseProfile(ack.Profile)
	if !ok {
		return fmt.Errorf("server chose unknown profile %q", ack.Profile)
	}
	c.profile = p
	return nil
}

// send encodes a client→server World op in the negotiated profile.
func (c *smokeClient) send(name string, payload any) error {
	body, err := cbor.Marshal(payload)
	if err != nil {
		return err
	}
	var ev txp.Event
	if c.profile == txp.ProfileVerbose {
		ev = txp.NewVerboseEvent(nil, name, body)
	} else {
		ev = txp.NewCompactEvent(worldOrd, c2sOp[name], body)
	}
	frame, err := ev.Encode(c.profile)
	if err != nil {
		return err
	}
	return c.carrier.SendFrame(frame)
}

// await reads server events until one with the wanted name arrives, decoding it.
func (c *smokeClient) await(name string, out any) error {
	for {
		evName, payload, err := c.read()
		if err != nil {
			return err
		}
		switch evName {
		case name:
			if out != nil {
				return cbor.Unmarshal(payload, out)
			}
			return nil
		case "error":
			var e csil.ErrorEvent
			_ = cbor.Unmarshal(payload, &e)
			return fmt.Errorf("server error %d: %s", e.Code, e.Message)
		}
	}
}

// awaitMoved reads tick snapshots until our player's position differs from spawn.
func (c *smokeClient) awaitMoved(myID csil.PlayerID, spawn csil.Position) (csil.Player, error) {
	for i := 0; i < 90; i++ {
		var tk csil.Tick
		if err := c.await("tick", &tk); err != nil {
			return csil.Player{}, err
		}
		for _, p := range tk.Players {
			if p.PlayerId == myID && p.Pos != spawn {
				return p, nil
			}
		}
	}
	return csil.Player{}, fmt.Errorf("player did not move within the expected number of ticks")
}

// read returns the next server event's name (resolved across profiles) + payload.
func (c *smokeClient) read() (string, []byte, error) {
	frame, err := c.carrier.RecvFrame()
	if err != nil {
		return "", nil, err
	}
	if frame == nil {
		return "", nil, fmt.Errorf("server closed the connection")
	}
	ev, err := txp.DecodeEvent(frame, c.profile)
	if err != nil {
		return "", nil, err
	}
	if c.profile == txp.ProfileVerbose {
		if ev.Event == nil {
			return "", nil, fmt.Errorf("verbose event missing name")
		}
		return *ev.Event, ev.Payload, nil
	}
	if ev.OpOrd == nil {
		return "", nil, fmt.Errorf("compact event missing op ordinal")
	}
	return s2cName[*ev.OpOrd], ev.Payload, nil
}
