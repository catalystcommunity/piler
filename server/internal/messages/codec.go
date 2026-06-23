package messages

import (
	"fmt"
	"strings"

	txp "github.com/catalystcommunity/csilgen/transports/go"
)

// codec.go bridges piler's name-based routing to either CSIL-Events wire profile.
// Verbose carries the operation name on the wire; compact carries its @wire-id
// ordinal. Either way the dispatcher and handlers work in terms of names, and
// the control plane in terms of the `$`-sigil control names.

// controlOrd maps a control op name to its ordinal (compact); controlName is the
// inverse. Used to frame the control plane in whichever profile is active.
var controlOrd = map[string]uint64{
	txp.HelloName:    txp.ControlHello,
	txp.HelloAckName: txp.ControlHelloAck,
	txp.PingName:     txp.ControlPing,
	txp.PongName:     txp.ControlPong,
	txp.CloseName:    txp.ControlClose,
	txp.ErrorName:    txp.ControlError,
}

var controlName = func() map[uint64]string {
	m := make(map[uint64]string, len(controlOrd))
	for name, ord := range controlOrd {
		m[ord] = name
	}
	return m
}()

// encodeAppFrame builds a server→client application-event frame for `event` in
// the given profile (verbose: by name; compact: by op ordinal).
func encodeAppFrame(profile txp.Profile, event string, payload []byte) ([]byte, error) {
	if profile == txp.ProfileVerbose {
		return txp.NewVerboseEvent(nil, event, payload).Encode(profile)
	}
	op, ok := outboundOp[event]
	if !ok {
		return nil, fmt.Errorf("no op ordinal for event %q", event)
	}
	return txp.NewCompactEvent(WorldServiceOrd, op, payload).Encode(profile)
}

// encodeControlFrame builds a control-plane frame (a `$`-named op) in the given
// profile (verbose: by `$`-name; compact: service ordinal 0 + control ordinal).
func encodeControlFrame(profile txp.Profile, name string, payload []byte) ([]byte, error) {
	if profile == txp.ProfileVerbose {
		return txp.NewVerboseEvent(nil, name, payload).Encode(profile)
	}
	op, ok := controlOrd[name]
	if !ok {
		return nil, fmt.Errorf("unknown control op %q", name)
	}
	return txp.NewCompactEvent(txp.ControlServiceOrd, op, payload).Encode(profile)
}

// inbound is a decoded frame resolved to a profile-independent form: a control
// flag, a name (the handler name for app events, or the `$`-name for control;
// empty when the op/service is unknown), and the payload.
type inbound struct {
	control bool
	name    string
	payload []byte
}

// decodeInbound decodes a frame in the given profile into its name-based form.
func decodeInbound(profile txp.Profile, frame []byte) (inbound, error) {
	ev, err := txp.DecodeEvent(frame, profile)
	if err != nil {
		return inbound{}, err
	}
	if profile == txp.ProfileVerbose {
		if ev.Event == nil {
			return inbound{}, fmt.Errorf("verbose event missing name")
		}
		n := *ev.Event
		return inbound{control: strings.HasPrefix(n, "$"), name: n, payload: ev.Payload}, nil
	}
	// compact
	if ev.ServiceOrd == nil || ev.OpOrd == nil {
		return inbound{}, fmt.Errorf("compact event missing service/op ordinal")
	}
	if *ev.ServiceOrd == txp.ControlServiceOrd {
		return inbound{control: true, name: controlName[*ev.OpOrd], payload: ev.Payload}, nil
	}
	if *ev.ServiceOrd != WorldServiceOrd {
		return inbound{payload: ev.Payload}, nil // unknown service → empty name
	}
	return inbound{name: inboundOp[*ev.OpOrd], payload: ev.Payload}, nil // "" if unknown op
}
