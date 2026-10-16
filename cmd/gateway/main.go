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
	"net/url"
	"os"

	"github.com/yourname/api-gateway/internal/config"
	"github.com/yourname/api-gateway/internal/proxy"
	"github.com/yourname/api-gateway/internal/router"
)

func main() {
	configPath := flag.String("config", "configs/routes.yaml", "path to the route table")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	logger.Info("starting gateway", "config", *configPath)

	cfg, err := config.Load(*configPath) // fail fast on error
	if err != nil {
		logger.Error("failed to load config", "err", err)
		os.Exit(1)
	}

	transport := proxy.NewTransport(proxy.TransportOptions{}) // one shared pool (M4 tunes it)
	handlers := make(map[string]http.Handler, len(cfg.Routes))

	for _, route := range cfg.Routes {
		up := cfg.Upstreams[route.Upstream]

		target, err := url.Parse(up.Instances[0]) // TODO(M7): pick a healthy instance, not just the first
		if err != nil {
			logger.Error("invalid upstream URL", "err", err)
			os.Exit(1)
		}

		handlers[route.Name] = proxy.New(target, transport, logger)
	}

	rt, err := router.New(cfg.Routes, handlers)
	if err != nil {
		logger.Error("failed to create router", "err", err)
		os.Exit(1)
	}

	// TODO(M3): wrap the handler: middleware.Chain(handler, Recovery, RequestID, AccessLog, ...)
	//   Write down WHY you chose that order.
	var handler http.Handler = rt

	// TODO(M4): this server has NO timeouts, which is exactly the problem M4
	// is about. Set ReadHeaderTimeout, ReadTimeout, WriteTimeout, IdleTimeout
	// (from cfg.Server) and be ready to explain each one. Also look at
	// MaxHeaderBytes and (Go 1.27) MaxHeaderValueCount: what attack does each
	// one bound, and what are the defaults if you leave them unset?
	srv := &http.Server{
		Addr:    cfg.Server.Listen,
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
