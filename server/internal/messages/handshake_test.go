package messages

import (
	"testing"

	txp "github.com/catalystcommunity/csilgen/transports/go"
)

// verboseHello builds a client $hello frame (always verbose) offering the given
// versions/profiles.
func verboseHello(t *testing.T, versions []uint64, profiles []string) []byte {
	t.Helper()
	svc := "World"
	hello, err := txp.Hello{Versions: versions, Profiles: profiles, Service: &svc}.Encode()
	if err != nil {
		t.Fatalf("encode hello: %v", err)
	}
	frame, err := txp.NewVerboseEvent(nil, txp.HelloName, hello).Encode(txp.ProfileVerbose)
	if err != nil {
		t.Fatalf("encode hello frame: %v", err)
	}
	return frame
}

// The handshake negotiates whatever profile the server is configured for, and
// answers with a verbose $hello-ack announcing it — for both compact and verbose.
func TestServerHandshakeNegotiatesConfiguredProfile(t *testing.T) {
	saved := appProfile
	defer SetProfile(saved)

	for _, want := range []txp.Profile{txp.ProfileCompact, txp.ProfileVerbose} {
		SetProfile(want)
		carrier := txp.NewLoopbackFrameCarrier()
		// Client offers BOTH; the server forces its configured one.
		carrier.PushInbound(verboseHello(t, []uint64{txp.VERSION},
			[]string{txp.ProfileCompact.String(), txp.ProfileVerbose.String()}))

		if err := ServerHandshake(carrier); err != nil {
			t.Fatalf("[%s] handshake: %v", want, err)
		}
		ackFrame := carrier.TakeOutbound()
		if ackFrame == nil {
			t.Fatalf("[%s] expected a $hello-ack", want)
		}
		// The ack is always verbose.
		ev, err := txp.DecodeEvent(ackFrame, txp.ProfileVerbose)
		if err != nil {
			t.Fatalf("[%s] decode ack: %v", want, err)
		}
		if ev.Event == nil || *ev.Event != txp.HelloAckName {
			t.Fatalf("[%s] event = %v, want %s", want, ev.Event, txp.HelloAckName)
		}
		ack, err := txp.DecodeHelloAck(ev.Payload)
		if err != nil {
			t.Fatalf("[%s] decode ack payload: %v", want, err)
		}
		if ack.Profile != want.String() {
			t.Fatalf("ack profile = %q, want %q", ack.Profile, want.String())
		}
	}
}

// An unsupported transport version is refused with a $close (status
// version-unsupported) and a handshake error.
func TestServerHandshakeRefusesUnsupportedVersion(t *testing.T) {
	carrier := txp.NewLoopbackFrameCarrier()
	carrier.PushInbound(verboseHello(t, []uint64{999}, []string{txp.ProfileCompact.String()}))

	if err := ServerHandshake(carrier); err == nil {
		t.Fatal("expected handshake to fail on unsupported version")
	}
	closeFrame := carrier.TakeOutbound()
	if closeFrame == nil {
		t.Fatal("expected a $close to be sent")
	}
	ev, err := txp.DecodeEvent(closeFrame, txp.ProfileVerbose)
	if err != nil {
		t.Fatalf("decode close: %v", err)
	}
	if ev.Event == nil || *ev.Event != txp.CloseName {
		t.Fatalf("event = %v, want %s", ev.Event, txp.CloseName)
	}
	cl, err := txp.DecodeClose(ev.Payload)
	if err != nil {
		t.Fatalf("decode close payload: %v", err)
	}
	if cl.Status.Code() != txp.StatusVersionUnsupported.Code() {
		t.Fatalf("close status = %d, want %d", cl.Status.Code(), txp.StatusVersionUnsupported.Code())
	}
}

// A client that can't speak the server's configured profile is refused.
func TestServerHandshakeRefusesProfileMismatch(t *testing.T) {
	saved := appProfile
	defer SetProfile(saved)
	SetProfile(txp.ProfileCompact)

	carrier := txp.NewLoopbackFrameCarrier()
	// Client offers verbose only; server wants compact.
	carrier.PushInbound(verboseHello(t, []uint64{txp.VERSION}, []string{txp.ProfileVerbose.String()}))

	if err := ServerHandshake(carrier); err == nil {
		t.Fatal("expected handshake to fail when the client can't speak the server's profile")
	}
}
