package cmd

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/fxamacker/cbor/v2"

	"github.com/catalystcommunity/piler/server/internal/config"
	"github.com/catalystcommunity/piler/server/internal/csil"
)

// Smoke is a tiny TCP client exercising the full flow against a running
// server: join → "welcome"; move → see it applied in a "tick" snapshot; say →
// "chat". Proves the end-to-end message/tick path over the binary-CBOR TCP
// transport without a browser.
func Smoke(flags map[string]string) error {
	config.ApplyFlags(flags)

	conn, err := net.DialTimeout("tcp", config.TCPAddr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("dialing %s: %w", config.TCPAddr, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	c := &smokeClient{conn: conn}

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

type smokeClient struct{ conn net.Conn }

func (c *smokeClient) send(kind string, payload any) error {
	bodyBytes, err := cbor.Marshal(payload)
	if err != nil {
		return err
	}
	frame, err := cbor.Marshal(csil.ClientMessage{Kind: kind, Body: bodyBytes})
	if err != nil {
		return err
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(frame)))
	if _, err := c.conn.Write(hdr[:]); err != nil {
		return err
	}
	_, err = c.conn.Write(frame)
	return err
}

// await reads pushes until one with the wanted event arrives, decoding it.
func (c *smokeClient) await(event string, out any) error {
	for {
		msg, err := c.read()
		if err != nil {
			return err
		}
		switch msg.Event {
		case event:
			if out != nil {
				return cbor.Unmarshal(msg.Body, out)
			}
			return nil
		case "error":
			var e csil.ErrorEvent
			_ = cbor.Unmarshal(msg.Body, &e)
			return fmt.Errorf("server error %d: %s", e.Code, e.Message)
		}
	}
}

// awaitMoved reads tick snapshots until our player's position differs from
// spawn (i.e. the move intent has been applied on a server tick).
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

func (c *smokeClient) read() (csil.ServerMessage, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(c.conn, hdr[:]); err != nil {
		return csil.ServerMessage{}, err
	}
	buf := make([]byte, binary.BigEndian.Uint32(hdr[:]))
	if _, err := io.ReadFull(c.conn, buf); err != nil {
		return csil.ServerMessage{}, err
	}
	var msg csil.ServerMessage
	return msg, cbor.Unmarshal(buf, &msg)
}
