// Package cmd is the server's command-line surface: `serve` and `migrate`.
// It uses plain flag parsing (no cobra), mirroring longhouse.
package cmd

import (
	"fmt"
	"strings"
)

// Run dispatches a command from the argument list.
func Run(args []string) error {
	command := findCommand(args)
	flags := parseFlags(args)

	switch command {
	case "serve":
		return Serve(flags)
	case "migrate":
		return Migrate(flags)
	case "smoke":
		return Smoke(flags)
	case "", "help", "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command: %s", command)
	}
}

func usage() {
	fmt.Print(`piler server

Usage:
  server <command> [flags]

Commands:
  serve     Run migrations, then serve the TCP and WebSocket transports
  migrate   Run database migrations and exit
  smoke     Run a TCP client through join/move/say/get-room-state (dev check)

Flags (override PILER_* env vars):
  --db-uri=<uri>           PostgreSQL connection string
  --ws-addr=<addr>         HTTP/WebSocket listen address (default :6080)
  --tcp-addr=<addr>        Raw-TCP listen address (default :6081)
  --sub-resolution=<n>     Fixed-point sub-tile resolution (default 1000)
  --field-width-sub=<n>    Field width in sub-units (default 48000)
  --field-height-sub=<n>   Field height in sub-units (default 27000)
`)
}

// findCommand returns the first non-flag argument.
func findCommand(args []string) string {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}

// parseFlags collects --key=value and --flag (bool) arguments.
func parseFlags(args []string) map[string]string {
	flags := map[string]string{}
	for _, a := range args {
		if !strings.HasPrefix(a, "--") {
			continue
		}
		kv := strings.TrimPrefix(a, "--")
		if i := strings.IndexByte(kv, '='); i >= 0 {
			flags[kv[:i]] = kv[i+1:]
		} else {
			flags[kv] = "true"
		}
	}
	return flags
}
