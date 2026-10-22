# Trust boundaries

This plugin is a Traefik middleware. It runs inside the Traefik process, under
the Yaegi interpreter, and has no listener, credentials, storage or network
client of its own. Everything it handles arrives through the two
`http.Handler` calls Traefik makes. Read this before reporting a finding, and
update it in the same change as any code that moves a boundary.

## The surface a client can reach

The plugin does no authentication. A client reaches it if it can reach a Traefik
router the operator attached the middleware to, which for a public site means
any anonymous client on the internet.

| Entry point | File | Untrusted input |
| --- | --- | --- |
| `rewriteBody.ServeHTTP` | `handler/handler.go` | The request's method, `Host`, URL path, and its `Accept`, `Accept-Encoding`, `Connection` and `Upgrade` headers. The query string, other headers and the request body pass through to the backend unread. |
| The same call, response side | `handler/handler.go`, `httputil/response_writer.go` | The backend's status, `Content-Type`, `Content-Encoding`, and its body. A client influences these only as far as the backend lets request data into its page. |

Nothing else is reachable. `CloseNotify`, `Hijack` and `Flush` on
`ResponseWrapper` delegate to the writer Traefik supplied.

## Trusted inputs

| Input | Who controls it | Notes |
| --- | --- | --- |
| `Config` (`rewrites`, `monitoring`, `lastModified`, `logLevel`) | The operator, in Traefik's static or dynamic configuration | Reaching it needs write access to that configuration, which already means control of the proxy. A finding that starts with "set `regex` to..." is not a finding. |
| The backend | The operator | Trusted as a service. The plugin does not sanitize what it returns, so stored XSS or injection in a backend page passes through a rewrite unchanged. |
| Traefik | The operator | The plugin trusts the request and writer Traefik passes it. |

## The log sink

Logs go to the Traefik process's stdout and stderr, through `logger.LogWriter`.
Who reads them depends on the deployment, so treat them as readable by more
people than can reach the proxy.

| `logLevel` | What it writes |
| --- | --- |
| `0` Info (default) and above | Errors only: a body that could not be decoded, encoded or written. No request headers, URL or address. The plugin does not recover panics, so none is logged here. |
| `-1` Debug | The configuration, including each `replacement`, once at start-up. Per request: method, host, path and the `Accept`, `Accept-Encoding` and `Upgrade` headers. Never the other headers, the query string or the client address. |
| `-2` Trace | Everything at Debug, plus every response body before and after rewriting. |

Debug is meant to be safe to leave on. Trace is not, and says so in the README.
Do not put a secret in a `replacement`, because Debug prints it.

## Not a boundary

* **The plugin is not a security control.** It is not a sanitizer, a WAF or a
  redaction layer. A response with a `Content-Encoding` it cannot decode, a
  content type nobody monitors, an unmonitored method or a WebSocket upgrade is
  forwarded untouched, by design. Not rewriting such a response is not a bypass.
* **The request `Accept` header is an opt-out, not a gate.** `*/*`, a range and
  a missing header are open-ended and defer to the response `Content-Type`,
  which decides. That is how `fetch` responses get rewritten. A client whose
  `Accept` names only types the plugin does not monitor, `application/json`
  under the default config, gets the response forwarded unrewritten. That is a
  client choosing not to be rewritten, so it is not a bypass. The plugin adds
  `Vary: Accept` so a shared cache keeps the two copies apart; see the limits
  below.
* **Rewrites run on the response only.** The plugin never changes what the
  backend receives, apart from narrowing `Accept-Encoding` to encodings it can
  decode.

## Public by design

The module, its source and its test fixtures are public. `fixtures/` holds two
sample configuration files with placeholder values. The plugin has no keys,
tokens or credentials, and none ship with it.

## Project-specific non-findings

* **Trace logs response bodies.** It is the documented one-off debugging level.
* **A rewritten response is buffered in full.** A regular expression can match
  across writes, so the plugin needs the whole body. The README tells operators
  to cap it with Traefik's Buffering middleware.
* **Regular expressions are the operator's.** They compile with Go's RE2, which
  runs in linear time, so a pattern cannot backtrack catastrophically.
  `replacement` expands `$1` and `${name}` by design.
* **No dependencies and no `go.sum`.** The module imports only the standard
  library. The supply-chain surface is the CI toolchain, not the module graph.
  The workflow's actions are pinned to commit SHAs and its two tools are
  installed from release tarballs checked against a recorded SHA-256, so a
  moved tag or branch upstream does not change what CI runs. Dependabot bumps
  the actions. Yaegi publishes no attestation, so its hash pins only the
  tarball that was current when it was recorded.
* **The interpreter's standard library is frozen at Go 1.22.** Yaegi, not the
  compiler, decides which APIs the plugin may call. That limits what a fix can
  use. It is not a vulnerability.
* **A rewritten response keeps the backend's `ETag`.** After an operator
  changes or removes a rewrite, a client holding the old rewritten page is
  told by its own cache that the tag still matches. The same keeps a partial
  `206` rewritten on its own under the backend's `Content-Range`. Both are
  correctness defects that only the requesting client or an operator's own
  configuration change can reach, and neither is a security matter.

## Known hardening gaps

Reproduced against Traefik 3.7.16, and not vulnerabilities: the bytes involved
are the trusted backend's own and no client reaches the cause.

* **`Vary: Accept` depends on the cache.** The plugin adds it to a rewritten
  response, and to every request it skips on `Accept`, since it has to decide
  before the backend has said what it is sending. A CDN that ignores `Vary`, or
  honors it only for a short list of headers, can still store one copy and
  serve it to every client. Key such a cache on `Accept`, or do not cache the
  routes this middleware is on.
* **A handler below the plugin can drop `Vary` on a skipped request.** The
  value goes on before the call, so a handler that replaces `Vary` rather than
  adding to it removes it. The rewrite path adds it after the backend's headers
  are set and is not affected.
* **A panic below the plugin is Traefik's to handle.** The plugin does not
  recover it, so a route answers `500`, or closes the connection once headers
  are out, as it does without the plugin. A client cannot cause this in stock
  Traefik: it needs a defect in another plugin or middleware.

If a route's auth posture changes, or the plugin gains a listener, a network
client, a credential or a new input, update the tables above in the same change.
