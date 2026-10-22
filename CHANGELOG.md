# Changelog

This project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased

### Fixed

- **Responses whose rewrite depends on `Accept` now say so with
  `Vary: Accept`.** A page was rewritten for a request whose `Accept` header
  allowed HTML and sent as the backend wrote it to one that asked for something
  else, from the same URL and with no `Vary` to tell a shared cache the two
  differ. A rewritten response now carries `Vary: Accept`, added after the
  backend's own headers so a handler that sets `Vary` cannot replace it, and a
  forwarded response does not, since its bytes do not depend on the request. A
  request skipped on its `Accept` header gets it before the backend answers,
  whatever the response turns out to be. Caches that key on the exact `Accept`
  string will hold a copy per value; the README lists the other limits.
  Measured against Traefik 3.7.16: `HEAD`, `POST` and WebSocket upgrade
  requests get no `Vary` from the plugin.
- **A request counts as a WebSocket upgrade only when it is one.** The check
  matched `websocket` in the first `Upgrade` header line, case-sensitively, and
  ignored `Connection`. A plain request that merely carried
  `Upgrade: websocket` was skipped and its page left unrewritten, though a
  reverse proxy honors `Upgrade` only when `Connection` names it, so it is an
  ordinary request. A request is now skipped when `Connection` lists `upgrade`
  and `Upgrade` lists `websocket`, as the case-insensitive, comma-separated
  lists RFC 6455 defines. That also recognizes `Upgrade: WebSocket` and a
  `websocket` that sits on a later header line.
- **A panic in the handler behind the plugin is no longer swallowed.**
  `ServeHTTP` recovered every panic below it, logged a warning and returned, so
  Traefik completed the response with whatever had been written: a `200` with
  `Content-Length: 0`, carrying any cache headers the handler had set, where the
  same route without this plugin answers `500`, or closes the connection once
  the headers are out. v1.3.0 re-panicked `http.ErrAbortHandler` only; every
  other panic was still swallowed. The plugin now leaves panics to Traefik's
  recovery middleware, so a route behaves the same with it as without. The
  README's note that aborted responses log at ERROR is gone with the re-panic
  it described: a backend that drops the connection mid-body logs at DEBUG,
  as it does on a route without the plugin.
- **Debug logging no longer writes request credentials.** The two per-request
  Debug lines printed the whole `*http.Request`, which includes every header
  (`Authorization` and `Cookie` among them), the query string and the client
  address, and Debug is meant to be usable as an operator log level. They now
  log the method, host, path and the `Accept`, `Accept-Encoding` and `Upgrade`
  headers. `ResponseWrapper.LogHeaders`, which runs when a rewritten body cannot
  be written to the client, printed the response headers and with them any
  `Set-Cookie`; it now logs the status, `Content-Type` and `Content-Encoding`.
  Only routes configured with `logLevel` `-1` or lower were affected, since the
  default of `0` never wrote these lines. Where a route ran at Debug on v1.3.0
  or earlier, treat the logs it wrote as holding credentials. Trace still logs
  response bodies in full, and the README now says what that can contain.

## v1.3.0 — 2026-07-25

First release of the [kelnei/rewrite-body](https://github.com/kelnei/rewrite-body)
fork, covering everything since upstream
[packruler/rewrite-body](https://github.com/packruler/rewrite-body) v1.2.0
(November 2022). Configuration is unchanged and no exported API was removed, so
upgrading is a version bump — but the response path changed in ways worth
reading, under **Changed** below.

Verified against Traefik 3.7.9, compiled and under Yaegi.

### Fixed

- An upstream that abandons a response mid-body now closes the connection
  instead of reaching the client as a `200` with `Content-Length: 0`.
  `http.ErrAbortHandler` is a sentinel asking `net/http` to abort; recovering it
  made the server complete a response that had deliberately been given up on.
- A body that fails to decode reaches the client intact. Decoding consumed part
  of the buffer it read from, and the error path then wrote what was left of it.
- A body that fails to re-encode is sent as it arrived, rather than as an empty
  body under a success status.
- `Content-Encoding: deflate` is written as zlib (RFC 1950), which is what HTTP
  means by deflate. It was raw DEFLATE, readable only by this plugin's own
  decoder. Both forms are accepted when decoding.
- `HEAD` is never processed. Its response has no body to rewrite, and buffering
  one cost the response its `Content-Length` — leaving the client waiting on
  bytes that never came.
- Monitored methods are matched exactly rather than by substring, and neither an
  empty configured method nor an empty configured content type acts as a
  wildcard. `types: [""]` used to monitor every response, binary included.
- A gzip body that fails part way through — a truncated response is the ordinary
  case — reports `*ReaderError` like the deflate path already did, and
  `ReaderError` unwraps to its cause.
- Log messages are formatted only when the configured level will print them.
  The handler logs the whole body twice per request, so this allocated roughly
  twice the page size on every request even with logging off.
- The log prefix no longer runs the level into the timestamp
  (`Rewrite-Body | DEBUG2026/07/24 20:31:41`).
- `CloseNotify` returns a channel that can fire, backed by the request context,
  rather than one nothing could ever send on. It no longer keeps the buffered
  body reachable for the life of the request, nor starts a goroutine that can
  never wake.
- A `103 Early Hints` preamble is passed through and waited out instead of being
  mistaken for the response that follows it.
- `.traefik.yml`'s `testData` said `monitor:` where the configuration key is
  `monitoring:`. Unknown keys are dropped in silence, so that block never bound
  and the defaults backfilled values that happened to match it.

### Changed

- **Only responses this plugin can rewrite are buffered.** Everything else is
  forwarded to the client write by write, with its headers untouched. A download
  or an open-ended stream that reaches the plugin no longer costs its whole
  length in memory, and a forwarded response keeps the `Content-Length` and
  `Last-Modified` the backend set — both were being stripped from bodies the
  plugin never touched. Measured against Traefik 3.7.9: a 24 kB chunked response
  delivered over 0.9s now arrives in the same three pieces as a route without
  the plugin, where it used to arrive whole at the end.
- **A request's `Accept` header no longer has to name a monitored type.** `*/*`,
  a range wildcard and a missing header are all open-ended and fall through to
  the response `Content-Type`, which is the check that decides. `fetch` and
  `XMLHttpRequest` responses are now rewritten; for a WordPress backend that
  means `admin-ajax.php` and the REST routes.
- `lastModified: false` drops `Last-Modified` from responses this plugin
  rewrites, not from ones it forwards untouched.
- Full response bodies log at Trace rather than Debug, leaving Debug usable as
  an operator log level.
- The 2022 workaround for a config parser that mangled string lists is gone.
  Traefik 3.7.9 delivers proper slices through both the file provider and docker
  labels.
- The README was rewritten. Every option is documented with its type and
  default, the gates a response passes are listed in the order they are applied,
  and what a rewrite costs in memory is stated rather than left to be
  discovered. The static configuration example drops the `pilot` block, which
  has had no meaning since Traefik Pilot shut down, and the description of which
  responses get processed no longer claims the `Content-Type` must contain
  `text` — it is matched against `monitoring.types`, `text/html` alone by
  default.

### Deprecated

- `httputil.ResponseWrapper.SetLastModified`. Pass `lastModified` to
  `WrapWriter`, which is the only setting the wrapper reads; the method has no
  effect once the header has gone out. It remains for callers outside this
  module, and removing it would be a major release.

### Known limitations

- Traefik never calls the wrapper's `Flush`. Yaegi hands compiled code a
  wrapper satisfying only `http.ResponseWriter`, so Traefik's check for an
  `http.Flusher` fails and it stops flushing for the route. A forwarded body
  still leaves as `net/http`'s own response buffer fills, so a long stream keeps
  moving, but a stream under about 2 kB in total arrives at the end.
- Rewriting needs the whole body, so a monitored response that streams — chunked
  `text/html`, for instance — is still buffered to completion. That is inherent:
  a regex can match across writes.
- Yaegi's standard library is frozen at Go 1.22, which constrains what this code
  may use regardless of the toolchain. See the Development section of the
  [README](README.md).

### Internal

- Module path is `github.com/kelnei/rewrite-body`.
- CI lints and tests on every supported Go release, compiled and under Yaegi,
  with golangci-lint v2. Go 1.25 is the declared minimum.
- Tests were added throughout, including for chunk-spanning matches, websocket
  upgrades, the abort path, and the buffering decisions above.

Every change relative to upstream is also itemised in [`NOTICE`](NOTICE), as the
Apache-2.0 licence requires.
