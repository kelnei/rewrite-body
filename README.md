# Rewrite Body

[![Main](https://github.com/kelnei/rewrite-body/actions/workflows/main.yml/badge.svg)](https://github.com/kelnei/rewrite-body/actions/workflows/main.yml)

A [Traefik](https://traefik.io) middleware plugin that rewrites HTTP response
bodies with regular expressions — including bodies that arrive `gzip` or
`deflate` encoded, which it decodes, rewrites and re-encodes on the way through.

> **Fork notice.** This is a maintained fork of
> [packruler/rewrite-body](https://github.com/packruler/rewrite-body) at v1.2.0,
> which is itself a fork of Traefik's
> [plugin-rewritebody](https://github.com/traefik/plugin-rewritebody). Neither
> has taken a commit since November 2022. This one is kept working against
> current Traefik: v1.3.0 is verified against Traefik 3.7.16, compiled and
> under Yaegi. See [`CHANGELOG.md`](CHANGELOG.md) for what changed and
> [`NOTICE`](NOTICE) for every modification relative to upstream.

## What it is good for

* **Repointing absolute URLs baked into content.** A database restored from
  production is full of production hostnames; rewriting them in the response
  lets a staging or development environment serve that data as-is, instead of
  running search-and-replace over the database after every restore.
* **Injecting markup into pages you do not control.** Adding a stylesheet or a
  script before `</head>` in an application you cannot patch.
* **Correcting a string an upstream hard-codes.** A wrong link, a stale brand
  name, a scheme that should be `https`.

It works on any response body the plugin is allowed to buffer — see
[How it works](#how-it-works) for exactly which those are, and
[Operating notes](#operating-notes) for what it costs.

## Requirements

Traefik v3, verified against 3.7.16. The plugin has no Go dependencies.

## Installation

Plugins are declared in Traefik's **static** configuration. Load a tagged
release:

```yaml
experimental:
  plugins:
    rewrite-body:
      moduleName: "github.com/kelnei/rewrite-body"
      version: "v1.3.0"
```

Or run it from a working copy — mount the source at
`/plugins-local/src/github.com/kelnei/rewrite-body` and declare it local.
Traefik builds a local plugin at startup, so restart Traefik after changing the
source:

```yaml
experimental:
  localPlugins:
    rewrite-body:
      moduleName: "github.com/kelnei/rewrite-body"
```

Either way, middleware references in the dynamic configuration use the map name
you chose (`rewrite-body` above), not the module path.

## Configuration

Create a [middleware](https://doc.traefik.io/traefik/middlewares/overview/) in
the dynamic configuration and attach it to a router. Every option below is
shown with its default:

```yaml
http:
  routers:
    my-router:
      rule: "Host(`localhost`)"
      service: my-service
      middlewares:
        - rewrite-foo

  middlewares:
    rewrite-foo:
      plugin:
        rewrite-body:
          # Applied in order: each pattern sees the output of the one before it.
          rewrites:
            - regex: "foo"
              replacement: "bar"

          # Keep the upstream Last-Modified on responses this plugin rewrites.
          # Defaults to false, which removes it, because a rewritten body is no
          # longer the one that header describes. A response the plugin forwards
          # untouched always keeps it.
          lastModified: false

          # Trace: -2, Debug: -1, Info: 0, Warning: 1, Error: 2.
          # Debug logs the per-request routing decisions: method, host, path and
          # the Accept, Accept-Encoding and Upgrade headers, but not the other
          # headers or the query string. Trace additionally dumps every response
          # body before and after rewriting, so it is for one-off debugging, not
          # a busy route, and its output holds whatever the pages hold, personal
          # data and tokens included.
          logLevel: 0

          # Which responses are considered at all.
          monitoring:
            # Matched against the response Content-Type as a substring, so
            # text/html covers "text/html; charset=utf-8". Wildcards are not
            # supported here -- write the types out.
            types:
              - text/html
            # Matched exactly, and always ALL CAPS. HEAD is never processed
            # whatever this says: its response has no body to rewrite.
            methods:
              - GET
```

| Option | Type | Default | Notes |
| --- | --- | --- | --- |
| `rewrites[].regex` | string | — | [RE2](https://github.com/google/re2/wiki/Syntax) syntax, as in Go's `regexp`. Applied to the whole body, so a match may span the writes the backend made. |
| `rewrites[].replacement` | string | — | `$1` and `${name}` expand to submatches; use `$$` for a literal `$`. |
| `lastModified` | bool | `false` | `false` removes `Last-Modified` from rewritten responses. |
| `logLevel` | int8 | `0` (Info) | `-2` Trace, `-1` Debug, `0` Info, `1` Warning, `2` Error. |
| `monitoring.types` | []string | `["text/html"]` | Substring match on the response `Content-Type`. |
| `monitoring.methods` | []string | `["GET"]` | Exact match. `HEAD` is always excluded. |

### Example: repoint production URLs at a staging host

```yaml
http:
  middlewares:
    stage-urls:
      plugin:
        rewrite-body:
          monitoring:
            types:
              - text/html
              - application/json
          rewrites:
            # Absolute URLs, keeping the path.
            - regex: "https://www\\.example\\.com"
              replacement: "https://stage.example.com"
            # Protocol-relative and escaped forms as they appear in JSON.
            - regex: "https:\\\\/\\\\/www\\.example\\.com"
              replacement: "https:\\/\\/stage.example.com"
```

### Example: inject a stylesheet

```yaml
http:
  middlewares:
    inject-css:
      plugin:
        rewrite-body:
          rewrites:
            - regex: "</head>"
              replacement: '<link rel="stylesheet" href="/custom.css"></head>'
```

## How it works

### Which responses are processed

A response is rewritten only when all of the following hold. They are checked in
order of cost, cheapest first:

1. The request method is in `monitoring.methods`, and is not `HEAD`.
2. The request is not a WebSocket upgrade.
3. The request's `Accept` header does not rule out every monitored type. `*/*`,
   a range such as `text/*`, and a missing header are all open-ended and pass
   this gate — `Accept` states what a client would like, not what the response
   will be, so it can only be used to skip requests that clearly want something
   else. This is why `fetch` and `XMLHttpRequest` responses are rewritten.
4. The response `Content-Type` names one of `monitoring.types`. This is the
   check that actually decides.
5. The response `Content-Encoding` is one of `gzip`, `deflate`, `identity`, or
   absent.

### What happens to everything else

Anything failing 4 or 5 is forwarded to the client as the backend sent it —
write by write, headers untouched, including the `Content-Length` and
`Last-Modified` it arrived with. Nothing is buffered, so a download or an
open-ended stream that happens to reach the plugin costs no memory here and is
not held back.

Anything failing 1 to 3 never reaches the plugin's writer at all.

### Encodings

| `Content-Encoding` | Handling |
| --- | --- |
| absent, `identity` | Rewritten directly. |
| `gzip` | Decoded with [`compress/gzip`](https://pkg.go.dev/compress/gzip), rewritten, re-encoded. |
| `deflate` | Decoded and re-encoded with [`compress/zlib`](https://pkg.go.dev/compress/zlib), since HTTP's `deflate` means the zlib format of RFC 1950. A body arriving as bare RFC 1951 DEFLATE without the zlib wrapper is also decoded, because plenty of servers send it that way. |
| anything else, `br` included | Forwarded untouched. |

If a body cannot be decoded, or cannot be re-encoded after rewriting, the
original response is sent instead. The rewrite is abandoned rather than the body
truncated.

### Headers

`Content-Length` is dropped from a rewritten response, because the rewrite
changes the length; the final write establishes the new one. Forwarded responses
keep theirs.

## Operating notes

* **A rewritten response is buffered in full.** A regular expression can match
  across the writes a backend makes, so the whole body has to be present before
  any pattern runs. Budget one body's worth of memory per in-flight rewritten
  request. To bound it, chain Traefik's
  [Buffering middleware](https://doc.traefik.io/traefik/middlewares/http/buffering/),
  which can cap a response size.
* **A monitored response that streams is buffered too**, and so arrives at the
  client when it finishes rather than as it is produced. That is inherent to
  rewriting. Keep streaming endpoints — Server-Sent Events, long-poll, chunked
  progress — out of `monitoring.types`, and they pass straight through.
* **Traefik does not call the plugin's `Flush`.** Yaegi hands compiled code a
  wrapper satisfying only `http.ResponseWriter`, so Traefik's check for an
  `http.Flusher` fails and it stops flushing for the route. A forwarded body
  still leaves as `net/http`'s own response buffer fills, so a long stream keeps
  moving, but a stream under about 2 kB in total arrives at the end.
* **Aborted upstream responses log at ERROR.** When a backend abandons a
  response, this plugin re-panics `http.ErrAbortHandler` so Traefik performs the
  abort rather than sending a `200` with no body. Yaegi wraps the panic value,
  which defeats Traefik's identity check on the sentinel, so its recovery
  middleware logs each abort at ERROR where an unplugged route logs DEBUG. Noise
  only.

## Development

```bash
make          # lint and test
make test     # go test -v -cover ./...
make lint     # golangci-lint run
make yaegi_test
```

`yaegi test` resolves imports through `GOPATH` rather than the module cache, so
the checkout has to sit at its own module path for that target to find the
plugin's own packages:

```bash
mkdir -p "$GOPATH/src/github.com/kelnei"
ln -s "$PWD" "$GOPATH/src/github.com/kelnei/rewrite-body"
```

CI runs the whole set on every Go release upstream still supports, compiled and
under Yaegi.

### Traefik interprets this plugin, so its Go is not the Go you build with

Traefik does not compile plugins. It runs them through
[Yaegi](https://github.com/traefik/yaegi), and Traefik 3.7.16 pins
`yaegi v0.16.1` — released April 2024, still the newest release, and lightly
maintained since. Two consequences the compiler and `go test` will let you walk
straight into:

**The standard library is frozen at Go 1.22.** Yaegi resolves stdlib names
through symbol tables generated at its own release, and every table in v0.16.1
is `go1_22_*`. Anything added to the standard library after that does not exist
at runtime, whatever `go.mod` says. `httptest.NewRequestWithContext` (Go 1.23)
is a worked example: it compiles, `go test` passes, and then the interpreter
rejects it.

```text
package httptest "net/http/httptest" has no symbol NewRequestWithContext
```

**Interfaces implemented here can be invisible to Traefik.** Yaegi hands
compiled code a wrapper satisfying the one interface it was asked for, so a type
assertion inside Traefik for a second interface fails even though the method is
right there. `ResponseWrapper.Flush` is the live case, described under
[Operating notes](#operating-notes).

`make yaegi_test` catches the first of these and runs in CI on every push. The
second shows up only against a running Traefik, so measure on the wire before
trusting a change to the response path.

None of this is what the `go` directive in `go.mod` controls. That is the
minimum toolchain, and it constrains the compiler and the linters; Yaegi caps
what the code may actually use.

## Licence and credits

Licensed under the Apache License, Version 2.0 — see [`LICENCE`](LICENCE).

This product includes software developed by
[packruler](https://github.com/packruler/rewrite-body), which is itself a fork
of Traefik's [plugin-rewritebody](https://github.com/traefik/plugin-rewritebody).
[`NOTICE`](NOTICE) carries the attribution the licence requires, along with an
itemised list of every modification made in this fork.
