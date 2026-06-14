package cmd

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/catalystcommunity/piler/server/internal/config"
	"github.com/catalystcommunity/piler/server/internal/rpc"
	"github.com/catalystcommunity/piler/server/internal/store"
	"github.com/catalystcommunity/piler/server/internal/transport"
	"github.com/catalystcommunity/piler/server/internal/world"
)

// tickRate is the authoritative simulation rate (30 Hz).
const tickRate = 30

// Serve runs migrations, connects the store, wires the dispatcher, and
// serves the TCP and WebSocket transports until interrupted.
func Serve(flags map[string]string) error {
	config.ApplyFlags(flags)

	if err := RunMigrations(); err != nil {
		return fmt.Errorf("migrations: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.NewPostgres(ctx, config.DBUri)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	defer st.Close()

	w := world.New(st, config.SubResolution, config.FieldWidthSub, config.FieldHeightSub)
	d := rpc.New()
	w.Register(d)

	// Authoritative simulation tick (apply move intents, step bots, broadcast
	// roster snapshots) at a fixed rate.
	go func() {
		ticker := time.NewTicker(time.Second / tickRate)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.Tick()
			}
		}
	}()

	// Raw-TCP transport (native clients).
	tcpErr := make(chan error, 1)
	go func() { tcpErr <- transport.ServeTCP(ctx, config.TCPAddr, d, w.Remove) }()

	// HTTP server hosting the WebSocket endpoint + health probe (browsers).
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("/ws", transport.WSHandler(ctx, d, w.Remove))

	httpSrv := &http.Server{Addr: config.WSAddr, Handler: mux}
	httpErr := make(chan error, 1)
	go func() {
		log.Printf("piler: WebSocket/HTTP transport listening on %s (/ws, /health)", config.WSAddr)
		httpErr <- httpSrv.ListenAndServe()
	}()

	log.Printf("piler: server up (sub-resolution=%d). Ctrl-C to stop.", config.SubResolution)

	select {
	case <-ctx.Done():
		log.Print("piler: shutting down")
		_ = httpSrv.Shutdown(context.Background())
		return nil
	case err := <-tcpErr:
		return fmt.Errorf("tcp transport: %w", err)
	case err := <-httpErr:
		if err == http.ErrServerClosed {
			return nil
		}
		return fmt.Errorf("http transport: %w", err)
	}
}
