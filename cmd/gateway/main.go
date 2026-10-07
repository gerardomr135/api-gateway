// Command gateway is the API Gateway entry point.
//
// Keep this file about WIRING only: read flags, load config, build the
// handler chain, run the server(s), shut down cleanly. All real logic lives
// under internal/. If you find business logic creeping in here, move it out.
//
// Target startup sequence (you'll grow into it milestone by milestone):
//
//	flags -> logger -> config.Load (M2) -> shared Transport (M4)
//	      -> upstream pools + health checkers (M7)
//	      -> router (M2) -> middleware chain (M3, M5, M6)
//	      -> public server :8080 + admin server :9090 (M10/M11)
//	      -> wait for SIGINT/SIGTERM -> graceful shutdown (M11)
package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"
)

func main() {
	configPath := flag.String("config", "configs/routes.yaml", "path to the route table")
	// TODO(M1): before you have a config file loader, a single -upstream flag
	// (e.g. http://localhost:9001) is the fastest way to get M1 working.
	upstream := flag.String("upstream", "http://localhost:9001", "single upstream URL (M1 only)")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	logger.Info("starting gateway", "config", *configPath, "upstream", *upstream)

	// TODO(M1): parse *upstream with url.Parse and build a proxy with
	// proxy.New(...). Use it as the handler instead of notImplemented.
	//
	// TODO(M2): replace the single upstream with:
	//   cfg, err := config.Load(*configPath)   // fail fast on error
	//   rt := router.New(cfg.Routes, ...)
	//
	// TODO(M3): wrap the handler: middleware.Chain(handler, Recovery, RequestID, AccessLog, ...)
	//   Write down WHY you chose that order.
	var handler http.Handler = http.HandlerFunc(notImplemented)

	// TODO(M4): this server has NO timeouts, which is exactly the problem M4
	// is about. Set ReadHeaderTimeout, ReadTimeout, WriteTimeout, IdleTimeout
	// (from cfg.Server) and be ready to explain each one. Also look at
	// MaxHeaderBytes and (Go 1.27) MaxHeaderValueCount: what attack does each
	// one bound, and what are the defaults if you leave them unset?
	srv := &http.Server{
		Addr:    ":8080",
		Handler: handler,
	}

	// TODO(M11): run ListenAndServe in a goroutine, wait on
	// signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM), then call
	// srv.Shutdown with a deadline. Also start the admin server on its own port.
	logger.Info("listening", "addr", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

// notImplemented is a placeholder handler so the binary runs from day one.
// Delete it once M1 works.
func notImplemented(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "gateway: not implemented yet (start with M1)", http.StatusNotImplemented)
}
