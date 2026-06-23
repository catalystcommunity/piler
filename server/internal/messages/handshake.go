package messages

import (
	"errors"
	"fmt"

	txp "github.com/catalystcommunity/csilgen/transports/go"
)

// ServerHandshake performs the CSIL-Events control-plane handshake as the
// accepting peer (csil-events-transport.md §3). The handshake is **always
// verbose** (the universal baseline the initiator can speak before it knows the
// negotiated profile): read the initiator's `$hello`, negotiate the profile —
// the server offers only its configured profile (ops.go's appProfile), so that
// is what gets chosen — and reply `$hello-ack` announcing it, or `$close` on
// mismatch. No application events flow before `$hello-ack`; subsequent frames
// use the negotiated (configured) profile.
//
// Auth (LinkKeys) will later ride Hello.auth and bind the session identity here.
func ServerHandshake(carrier txp.FrameCarrier) error {
	frame, err := carrier.RecvFrame()
	if err != nil {
		return err
	}
	if frame == nil {
		return errors.New("connection closed before $hello")
	}

	in, err := decodeInbound(txp.ProfileVerbose, frame)
	if err != nil {
		return fmt.Errorf("decoding $hello frame: %w", err)
	}
	if !in.control || in.name != txp.HelloName {
		return fmt.Errorf("expected %s as the first frame", txp.HelloName)
	}
	hello, err := txp.DecodeHello(in.payload)
	if err != nil {
		return fmt.Errorf("decoding hello payload: %w", err)
	}

	version, profile, ok := hello.Negotiate([]txp.Profile{appProfile})
	if !ok {
		// Nothing mutually supported: tell the peer, then fail the connection.
		_ = sendClose(carrier, txp.StatusVersionUnsupported, "no mutually supported version/profile")
		return errors.New("no mutually supported version/profile in $hello")
	}

	ack := txp.HelloAck{V: version, Profile: profile.String()}
	ackPayload, err := ack.Encode()
	if err != nil {
		return fmt.Errorf("encoding hello-ack: %w", err)
	}
	ackFrame, err := encodeControlFrame(txp.ProfileVerbose, txp.HelloAckName, ackPayload)
	if err != nil {
		return err
	}
	return carrier.SendFrame(ackFrame)
}

// sendClose sends a verbose control-plane $close carrying a transport status +
// reason (verbose because a rejected handshake never reached profile agreement).
func sendClose(carrier txp.FrameCarrier, status txp.Status, reason string) error {
	payload, err := txp.Close{Status: status, Reason: &reason}.Encode()
	if err != nil {
		return err
	}
	frame, err := encodeControlFrame(txp.ProfileVerbose, txp.CloseName, payload)
	if err != nil {
		return err
	}
	return carrier.SendFrame(frame)
}
