# api-gateway

A learning-by-doing API Gateway in Go, built stdlib-first, milestone by milestone.

Requires **Go 1.27+** (`go version` to check).

This repo is a **scaffold**: every package compiles, but the interesting parts are
stubs marked with `TODO(Mx)`, where `Mx` is the milestone in the learning plan
that teaches it. Find your next task with:

```sh
grep -rn "TODO(M1)" .
```

## Layout

```text
cmd/
  gateway/     wiring only: flags, config, middleware chain, server lifecycle
  toysvc/      ONE configurable fake backend, run many times with different flags
internal/
  config/      parse + validate routes.yaml               (M2)
  router/      request -> Route matching                   (M2)
  proxy/       ReverseProxy + shared Transport              (M1, M4)
  middleware/  recovery, request ID, access log, auth, rate limit (M3, M5, M6)
  ratelimit/   token bucket (in-memory, later Redis)        (M6)
  upstream/    pool, balancer, health checks                (M7)
  resilience/  retry, circuit breaker, bulkhead             (M8)
  aggregate/   fan-out composition endpoints                (M9)
  telemetry/   metrics + tracing                            (M10)
configs/routes.yaml          example route table
deploy/docker-compose.yaml   the lab: toy backends (+ gateway later)
NOTES.md                     your learning journal, one entry per milestone
```

## Quick start

```sh
make build        # compile both binaries into ./bin
make test         # go test -race ./...
make lab-up       # start toy backends with docker compose
make run          # run the gateway locally on :8080
```

Before your first commit, replace the placeholder module path:

```sh
go mod edit -module github.com/<your-user>/api-gateway
grep -rl "github.com/yourname/api-gateway" . | xargs sed -i 's#github.com/yourname/api-gateway#github.com/<your-user>/api-gateway#g'
```

## Milestone map

| Milestone | Where you'll work |
| --- | --- |
| M0 Toy backends and lab | `cmd/toysvc`, `deploy/`, `Makefile` |
| M1 Single-route reverse proxy | `internal/proxy`, `cmd/gateway` |
| M2 Config-driven routing | `internal/config`, `internal/router` |
| M3 Middleware chain | `internal/middleware` (chain, requestid, accesslog, recovery) |
| M4 Timeouts and Transport | `internal/proxy/transport.go`, `cmd/gateway` server timeouts |
| M5 Edge authentication | `internal/middleware/auth.go` |
| M6 Rate limiting | `internal/ratelimit`, `internal/middleware/ratelimit.go` |
| M7 Load balancing + health | `internal/upstream` |
| M8 Retry, breaker, bulkhead | `internal/resilience` |
| M9 Aggregation | `internal/aggregate` |
| M10 Observability | `internal/telemetry` |
| M11 Production readiness | `cmd/gateway` (shutdown, reload), `Dockerfile` |

## Definition of done (per milestone)

1. It works against the toy backends.
2. At least one table-driven test, using `httptest.NewServer` as a fake upstream.
3. You ran the milestone's "break it" experiment.
4. You wrote a `NOTES.md` entry in your own words.
