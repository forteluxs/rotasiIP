# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build
go build -o mubeng .

# Run all tests
go test ./...

# Run tests for a specific package
go test ./pkg/mubeng/...
go test ./pkg/helper/...
go test ./pkg/helper/awsurl/...

# Run a single test
go test ./pkg/mubeng/ -run TestTransport
go test ./pkg/helper/ -run TestEval

# Run tests with race detector (important — this codebase has concurrent code)
go test -race ./...
```

## Architecture

### Entry point & flow

`main.go` → `runner.Options()` (parses flags, validates, loads proxy file) → `runner.New()` (branches to checker or server mode).

Validation in `internal/runner/validator.go` runs before either mode starts: resolves the proxy file, loads it into a `ProxyManager`, validates `--method`, parses `--auth`, and opens the output file.

### Two modes

**Proxy checker** (`-c`): `internal/checker/checker.go` fans out goroutines via `sourcegraph/conc/pool`, each calling `check()` which creates its own `retryablehttp.Client`, calls `ipinfo.io/json` through the proxy, and returns an `IPInfo` struct. Each goroutine is fully independent — no shared state.

**IP rotator** (`-a`): `internal/server/server.go` starts an `elazarl/goproxy` HTTPS MITM server. All request handling flows through three hooks registered in `Run()`:
- `onRequest` — rotates proxy, builds `retryablehttp.Client`, forwards request
- `onConnect` — validates `Proxy-Authorization` header for CONNECT tunnels
- `onResponse` — strips hop-by-hop headers from responses

### Proxy rotation

`internal/server/vars.go` holds two package-level counters: `ok` (request count since last rotation) and `currentProxy` (the proxy address currently in use). `rotateProxy()` in `handler.go` is guarded by `rotateMu` — it returns `currentProxy` unchanged until `ok >= Options.Rotate`, then fetches a new proxy via `ProxyManager.Rotate()`.

The separate `mutex` in `vars.go` is used only when `--sync` is enabled: it's held across the entire `onRequest` call (including the blocking `<-resChan` wait), making request handling fully sequential. Do **not** lock `rotateMu` inside `mutex` — they must stay independent to avoid deadlock.

### ProxyManager

`internal/proxymanager/proxymanager.go` loads the proxy list from file into a global `manager` singleton. `New()` always writes to this global and returns it. `Reload()` (triggered by `--watch` on file write) calls `New()` for its side effect, then restores `CurrentIndex` on the same pointer.

`Proxies []string` entries may contain `{{USERNAME}}`/`{{PASSWORD}}` env-var templates (resolved by `pkg/helper.Eval`) or `{{uint32}}`/`{{uint32n N}}` function templates (resolved by `pkg/helper.EvalFunc` at rotation time, not at load time).

### Transport switching

`pkg/mubeng/transport.go` selects transport based on proxy URL scheme:
- `http`/`https` → `http.Transport.Proxy`
- `socks4`/`socks4a`/`socks5` → `h12.io/socks` dial function
- `aws://` → returns an empty `http.Transport` with `ErrSwitchTransportAWSProtocolScheme` (caller handles AWS separately via `internal/proxygateway`)

### AWS API Gateway proxying

When a proxy address starts with `aws://`, `internal/proxygateway/proxygateway.go` creates or reuses an AWS API Gateway REST API that acts as an HTTP proxy. Gateways are cached in `Proxy.Gateways map[string]*ProxyGateway` keyed by `slug(baseURL-region)`. On each request the URL is rewritten to point at the gateway endpoint; the gateway forwards to the original target. `Send()` must use a **copy** of `pg.endpoint` per call — never mutate `pg.endpoint.Path` directly.

### Proxy file format

One proxy per line. Supported schemes: `http://`, `https://`, `socks4://`, `socks4a://`, `socks5://`, `aws://ACCESS_KEY:SECRET_KEY@region`. Lines with unrecognised schemes are silently skipped at load time. Env-var templating (`{{VAR}}`) is expanded at load; function templates (`{{uint32}}`) are expanded at rotation time.

An optional `#CC` country-code suffix can be appended to any line (e.g. `socks5://1.2.3.4:4145#US`). The checker generates this format with `--output-format "{{proxy}}#{{country}}"`. The `--proxy-cc` flag uses these annotations to filter the pool at load time.

### Web dashboard (`--ui`)

`internal/dashboard/` is an optional HTTP server started as a goroutine from `server.Run()` when `--ui <addr>` is set (e.g. `--ui 0.0.0.0:9191`). It serves three endpoints:

- `GET /` — embedded single-page HTML (`internal/dashboard/static/index.html`, compiled in via `//go:embed`)
- `GET /api/status` — JSON snapshot of live stats and current config
- `POST /api/config` — update `Options.Rotate` and `Options.Method` at runtime without restart
- `GET /api/logs` — SSE stream of per-request log events

**Stats flow**: `common.ProxyStats` (added to `Options.Stats`) is the shared metrics object. `server/handler.go` calls `Stats.IncrReq()` and `Stats.SetCurrentProxy()` after each successful response; `Stats.IncrErr()` on each failed upstream request; `rotateProxy()` calls `Stats.IncrRotate()` on actual rotation. Uses `sync/atomic` for counters and `sync.RWMutex` for the proxy string. `Get()` returns `(req, rotates, errs int64, proxy string, uptime float64)` — uptime is computed from `startTime` set at `NewProxyStats()`. `/api/status` derives `rps = req / uptime`.

**Log broadcast flow**: `dashboard.LogBroadcaster` (package-level `*Broadcaster` in `internal/dashboard/broadcast.go`) is a fan-out pub/sub. `handler.go` marshals a JSON entry `{t,m,u,p,s,ms}` and calls `LogBroadcaster.Publish()` after each successful response. Each SSE client subscribes to a buffered channel; slow clients drop messages silently.

**Dependency direction**: `internal/server` imports `internal/dashboard` (to call `LogBroadcaster.Publish`). `internal/dashboard` imports only `common`. No circular dependency.

The HTML/JS frontend polls `/api/status` every 1.5 s and connects to `/api/logs` via `EventSource`. Config changes POST to `/api/config` and take effect immediately — including `rotate`, `method`, `delay`, and `rateLimit`.

### Rate limiter & delay

`internal/ratelimiter/ratelimiter.go` implements a minimum-interval limiter. `NewFromRPS(n)` enforces at most n req/s; `NewFromDelay(d)` enforces a fixed gap between calls. `--rate-limit` takes precedence over `--delay` when both are set. The `limiter` package-level var in `internal/server/vars.go` is initialised in `server.Run()` and called as `limiter.Wait()` at the top of each request goroutine in `onRequest` — before proxy rotation. The limiter is always non-nil (disabled limiter is a no-op).

### Transparent client wrapper

`pkg/client/client.go` provides `client.New(proxyAddr, timeout)` — a thin wrapper around `http.Client` pre-configured with a proxy transport pointing at a running mubeng rotator. Use this when building tools that should route traffic through mubeng without knowing about proxy rotation internals.

### Key invariants

- `proxymanager.New(filename, allowedCCs)` modifies the global `manager` in-place — it is not a pure constructor. `allowedCCs` is stored on the manager and reused by `Reload()`.
- Geo filtering (`--proxy-cc`) only excludes lines that **have** a `#CC` suffix not in the allowed set. Lines with no suffix are always included.
- `getEnviron()` in `pkg/helper/environ.go` populates a package-level map `m`; `Eval()` always refreshes it on each call.
- `internal/server` package-level vars (`handler`, `server`, `log`, `ok`, `currentProxy`, `limiter`) are intentionally global singletons — the server is designed for a single `Run()` call per process.
- The `errors` sub-package at `common/errors/errors.go` holds sentinel errors; use `errors.Is` for comparison, never string matching.
- `dashboard.LogBroadcaster` is a package-level singleton — safe to call `Publish()` from concurrent goroutines; dropping messages on full channels is intentional.
