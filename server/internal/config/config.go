// Package config holds server configuration, sourced from environment
// variables with sensible local-dev defaults and overridable by CLI flags.
package config

import (
	"os"
	"strconv"
)

var (
	// DBUri is the PostgreSQL connection string.
	DBUri = getEnv("PILER_DB_URI", "postgresql://piler:devpass@localhost:5433/piler?sslmode=disable")

	// WSAddr is the listen address for the HTTP server hosting the
	// WebSocket endpoint (/ws) and health probe (/health).
	WSAddr = getEnv("PILER_WS_ADDR", ":6080")

	// TCPAddr is the listen address for the raw-TCP CSIL transport.
	TCPAddr = getEnv("PILER_TCP_ADDR", ":6081")

	// SubResolution (SUB) is the fixed-point sub-tile resolution: sub_x and
	// sub_y range over [0, SUB). See docs/positioning.md.
	SubResolution = getEnvUint("PILER_SUB_RESOLUTION", 1000)

	// FieldWidthSub / FieldHeightSub bound the play area, in sub-units
	// (tile*SUB + sub). Defaults describe a 1920x1080 field at 40 px/tile
	// with SUB=1000: 1920/40*1000 = 48000 wide, 1080/40*1000 = 27000 tall.
	// The server clamps movement to [0, width] x [0, height]; clients read
	// these (via RoomState) to size and draw the field.
	FieldWidthSub  = getEnvUint("PILER_FIELD_WIDTH_SUB", 48000)
	FieldHeightSub = getEnvUint("PILER_FIELD_HEIGHT_SUB", 27000)

	// WireProfile selects the CSIL-Events wire profile for post-handshake
	// frames: "compact" (default; positional ordinal-keyed arrays) or "verbose"
	// (text-keyed maps, debuggable). The handshake is always verbose; clients
	// follow the server's choice via the $hello-ack.
	WireProfile = getEnv("PILER_WIRE_PROFILE", "compact")
)

// ApplyFlags overrides config from parsed CLI flags (e.g. --db-uri=...).
func ApplyFlags(flags map[string]string) {
	if v, ok := flags["db-uri"]; ok {
		DBUri = v
	}
	if v, ok := flags["ws-addr"]; ok {
		WSAddr = v
	}
	if v, ok := flags["tcp-addr"]; ok {
		TCPAddr = v
	}
	if v, ok := flags["sub-resolution"]; ok {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			SubResolution = n
		}
	}
	if v, ok := flags["field-width-sub"]; ok {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			FieldWidthSub = n
		}
	}
	if v, ok := flags["field-height-sub"]; ok {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			FieldHeightSub = n
		}
	}
	if v, ok := flags["wire-profile"]; ok {
		WireProfile = v
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvUint(key string, def uint64) uint64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}
