# Learning journal

One entry per milestone, written in your own words, without looking at notes.
If you can't explain it here, you're not done with the milestone yet.

Template:

```text
## Mx — <title>   (date finished)

What I built:
What broke during the "break it" experiment, and why:
Self-check answers (2–3 sentences each):
  1.
  2.
Interview one-liner (how I'd explain this concept in 30 seconds):
Open questions to bring to review:
```

---

## M0 — Toy backends and lab

What I built:

What broke, and why:

Self-check answers:

Interview one-liner:

Open questions:

---

## M2 — Config-driven routing   (2026-10-17)

What I built:
- `internal/config`: loads `configs/routes.yaml` with yaml.v3 in strict mode
  (`KnownFields(true)`), applies defaults, then validates everything and reports
  ALL problems at once with `errors.Join`. Checks: unknown upstream, duplicate
  prefix / route name, bad instance URLs (scheme + host), prefix must start with
  `/` and not end with `/`, valid methods, proxy routes need an upstream and
  aggregate routes must not have one. A bad file stops startup (fail fast).
- `internal/router`: longest-prefix match on path-segment boundaries
  (`path == prefix || HasPrefix(path, prefix+"/")`), routes sorted by prefix
  length so config order never matters. Optional `strip_prefix`
  (`/api/users` -> `/`, `/api/users/42` -> `/42`). 404 when nothing matches,
  405 + `Allow` header when the prefix matches but the method doesn't (typed
  error `MethodNotAllowedError` that also satisfies `errors.Is(ErrMethodNotAllowed)`).
- `cmd/gateway/main.go`: one `ReverseProxy` per UPSTREAM, built once at startup
  and shared by every route that points at it (not one per route, not one per
  request). All proxies share one Transport.
- Tests: table-driven router tests, config validation tests, and an end-to-end
  test with `httptest` fake upstreams that checks what the upstream really received.

What broke during the "break it" experiment, and why:
- Routes `/api/user` and `/api/users`: `/api/users/1` goes to `/api/users`
  because the longest prefix wins; `/api/username` does NOT match `/api/user`
  because `username` isn't a whole segment. A plain `HasPrefix` would have sent
  `/api/username` to the `/api/user` route.
- Found in review: I reset `RawPath = ""` after rewriting the path, so
  `/api/users/a%2Fb` reached the upstream as `/a/b`. The decoded `Path` can't tell
  a `/` separator from `%2F` data; only `RawPath` remembers. Fixed by matching and
  stripping on the escaped path and setting `Path` and `RawPath` together.
- Found in review: `/api/products/../users/1` matched `products` but the upstream
  could resolve it to another path. Dot-segments now get a 400, and an encoded
  prefix like `/api/%75sers` doesn't match (404, fail closed).
- Found in review: yaml.Unmarshal ignored unknown keys, so a typo like
  `strip_prefx` silently disabled the feature. `KnownFields(true)` makes it an error.

Deep dive: why rewriting a path can silently change its meaning
  - `/` vs `%2F`: in `/files/q3%2Freport` the slash is DATA (one segment, a file
    named `q3/report`). In `/files/q3/report` it is a SEPARATOR (folder `q3`
    containing `report`). Same characters once decoded, different meaning.
  - Go keeps two fields because of this. For `GET /api/users/a%2Fb`:
        Path    = "/api/users/a/b"     decoded, good for matching
        RawPath = "/api/users/a%2Fb"   original encoding, only set when it
                                       differs from the default encoding of Path
    `Path` alone can't tell me whether a slash was `/` or `%2F`.
  - How my gateway broke it (strip_prefix on `/api/users/a%2Fb`):
        client sends          /api/users/a%2Fb
        gateway rewrites      Path="/a/b", RawPath=""     <- encoding hint dropped
        proxy writes          EscapedPath() -> "/a/b"     (builds it from Path)
        upstream receives     /a/b                        but the client sent /a%2Fb
    The reverse proxy did nothing wrong. It encoded what I left it. The bug was
    my rewrite. It also hit routes with strip_prefix false, because I reset
    RawPath unconditionally.
  - Impact: an upstream route `GET /files/{name}` sees two segments instead of
    one, so it returns 404 or reaches a different handler. Worse, if the gateway
    decides auth or routing on one reading of the path and the upstream acts on
    another, that mismatch is security-relevant (same family as `..`).
  - The invariant to keep: after any rewrite, Path and RawPath must describe the
    SAME path. Strip the prefix from the escaped path, then derive Path with
    `url.PathUnescape`.
  - The design decision this forces: match on decoded or escaped form? Matching
    decoded lets `/api/%75sers/1` hit `/api/users` while stripping the escaped
    form would not find the prefix. Options: reject encoded `/` and `\` with 400,
    or match and forward escaped paths consistently. I chose the second: match
    and strip on `EscapedPath()`, so an encoded prefix fails closed (404), and
    reject dot-segments with 400.
  - Why my old tests missed it: `/api/users/42` decodes and re-encodes to the
    same string. The fake upstream must record `r.RequestURI` (raw request
    line), NOT `r.URL.Path`, which is already decoded and hides the difference.
    I ran the new tests against the old code first and watched them fail.
  - Rule of thumb: whenever a proxy changes a path, don't treat decoded `Path`
    as the whole request. Preserve the encoded form or normalize it on purpose.

Self-check answers:
  1. Why should `/api/username` not match the prefix `/api/user`? A prefix is a
     list of path segments, not a string prefix. `/api/user` means "the `user`
     resource and anything under it", and `username` is a different resource.
     Matching raw strings would route requests to the wrong service, and once
     auth exists, to the wrong auth policy. I enforce the boundary by requiring
     the path to equal the prefix or to start with prefix + "/".
  2. Why 405 and not fall through to a shorter prefix? The longest matching prefix
     owns that part of the URL space. If it exists but doesn't allow the method,
     the honest answer is "this resource exists, wrong method". Falling through
     would let a broad route like `/api` accept a method the specific route
     deliberately forbids.
  3. Why one proxy per upstream? The upstream is the unit that owns state (later:
     balancer, health, circuit breaker, bulkhead). Two routes to the same upstream
     must share one breaker, otherwise each counts only half the failures. It
     also means the proxy is built once at startup, never per request.

Interview one-liner:
  The gateway loads and validates a route table at startup, and for each request
  picks the route with the longest matching path prefix on segment boundaries. It
  returns 404 or 405 itself and otherwise hands the request to a reverse proxy
  that was built once per upstream. Config is the contract, so any mistake in it
  stops the gateway from starting.

Open questions to bring to review:
- Should defaults (timeouts, auth) live in `applyDefaults`, or should a missing
  timeout be a validation error? I chose defaults, but a silent 5s default may
  hide a forgotten setting.
- Reject `%2F` outright (400) versus preserving it? I preserve it, but is that the
  safer default for a public gateway?
- Prefixes with characters that need escaping can never match now. Should
  `Validate` reject them?
- Aggregate routes (M9) have no upstream, so `proxies[route.Upstream]` is nil.
  Where should that case be handled, in `main.go` or in `router.New`?
