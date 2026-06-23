package messages

import txp "github.com/catalystcommunity/csilgen/transports/go"

// The live protocol is the CSIL-Events `World` service. The wire profile is
// chosen at startup (SetProfile, from config) and applies to every post-handshake
// frame; the handshake itself is always verbose (see handshake.go). Routing is
// always by event NAME internally — the profile only changes how names map to
// the wire (verbose: the name is on the wire; compact: the @wire-id ordinal is).
//
// These ordinals mirror the `@wire-id` annotations in `csil/piler.csil` (the
// source of truth). csilgen validates `@wire-id` and uses it for breaking-change
// detection but its generators don't emit ordinal constants, so we mirror them
// here by hand — keep the two in sync. Operations are separate where they're
// semantically separate (tick is its own server push, not a reply to move):
//
//	op  dir  name               type
//	 0  <->  join               JoinRequest / JoinResponse ("welcome")
//	 1  <->  check-name         CheckNameRequest / NameAvailability
//	 2  ->   move               MoveRequest        (client intent)
//	 3  <-   tick               Tick               (server snapshot)
//	 4  ->   say                SayRequest         (client send)
//	 5  <-   chat               ChatMessage        (server broadcast)
//	 6  ->   firework           FireworkIntent     (client intent)
//	 7  <-   burst              FireworkEvent      (server broadcast to others)
//	 8  <-   error              ErrorEvent         (server push)
//
// Control-plane frames (handshake, heartbeat, close) ride service ordinal 0 with
// the library's control op ordinals; see codec.go and handshake.go.

// WorldServiceOrd is the World service ordinal (`@wire-id(1)`; 0 is reserved for
// the control plane).
const WorldServiceOrd uint64 = 1

// appProfile is the wire profile for post-handshake frames. Set once at startup
// from config; defaults to compact.
var appProfile = txp.ProfileCompact

// SetProfile fixes the post-handshake wire profile (call once at startup).
func SetProfile(p txp.Profile) { appProfile = p }

// ActiveProfile reports the configured post-handshake wire profile.
func ActiveProfile() txp.Profile { return appProfile }

// ParseProfile maps a config string ("compact"/"verbose") to a profile.
func ParseProfile(s string) (txp.Profile, bool) { return txp.ParseProfile(s) }

// inboundOp maps a client→server operation ordinal (compact) to its handler name.
var inboundOp = map[uint64]string{
	0: "join",
	1: "check-name",
	2: "move",
	4: "say",
	6: "firework",
}

// outboundOp maps a server→client event name to its operation ordinal (compact).
var outboundOp = map[string]uint64{
	"welcome":           0,
	"name-availability": 1,
	"tick":              3,
	"chat":              5,
	"burst":             7,
	"error":             8,
}

// OutboundOp returns the compact op ordinal for a server→client event name
// (false if it isn't a server-pushed event). Exported so clients/tests that
// decode compact World frames can resolve event names to ordinals.
func OutboundOp(name string) (uint64, bool) {
	o, ok := outboundOp[name]
	return o, ok
}

// InboundOpName returns the handler name for a client→server op ordinal (false
// if the ordinal isn't a client-sent World op).
func InboundOpName(op uint64) (string, bool) {
	n, ok := inboundOp[op]
	return n, ok
}
