// Command server is the authoritative piler world server. It serves the
// CSIL/CBOR RPC contract over raw TCP (native clients) and WebSocket
// (browsers, via catalystcommunity/websocks), backed by PostgreSQL.
//
// Usage:
//
//	server serve     # run migrations, then serve TCP + WebSocket
//	server migrate   # run migrations and exit
package main

import (
	"fmt"
	"os"

	"github.com/catalystcommunity/piler/server/cmd"
)

func main() {
	if err := cmd.Run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
