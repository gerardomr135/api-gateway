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
	"github.com/yourname/api-gateway/internal/middleware"
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

	transportOpts := proxy.DefaultTransportOptions()
	transportOpts.ResponseHeaderTimeout = cfg.Server.ResponseHeaderTimeout
	transport := proxy.NewTransport(transportOpts) // one shared, tuned pool
	handlers := make(map[string]http.Handler, len(cfg.Routes))

	// Shared proxy for all routes to the same upstream (M7)
	proxies := make(map[string]http.Handler, len(cfg.Upstreams))
	for name, up := range cfg.Upstreams {
		// Initialize each upstream proxy
		// TODO(M7): pick a healthy instance, not just the first
		upURL, err := url.Parse(up.Instances[0])
		if err != nil {
			logger.Error("invalid upstream URL", "err", err)
			os.Exit(1)
		}
		proxies[name] = proxy.New(upURL, transport, logger)
	}
	for _, route := range cfg.Routes {
		handlers[route.Name] = middleware.Timeout(route.Timeout)(proxies[route.Upstream])
	}

	rt, err := router.New(cfg.Routes, handlers)
	if err != nil {
		logger.Error("failed to create router", "err", err)
		os.Exit(1)
	}

	// Order, outermost first:
	//   Recovery   - outermost so it catches a panic from every layer below it.
	//   RequestID  - before AccessLog so the log line can include the ID.
	//   AccessLog  - wraps the router so it sees the final status and the
	//                route name the router records.
	// TODO(M5, M6): auth and rate limiting go inside AccessLog so rejected
	// requests are still logged.
	handler := middleware.Chain(rt,
		middleware.Recovery(logger),
		middleware.RequestID(),
		middleware.AccessLog(logger),
	)
	// MaxHeaderBytes bounds the memory one request's headers can use
	// (default 1 MiB, from config). MaxHeaderValueCount, new in Go 1.27, bounds
	// how many header values are parsed (default 500); left at its default.
	// Both stop a client from making the server buffer or parse unbounded headers.
	srv := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           handler,
		ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout,
		ReadTimeout:       cfg.Server.ReadTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
		MaxHeaderBytes:    cfg.Server.MaxHeaderBytes,
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
