# Security policy

## Reporting a vulnerability

Report it privately through GitHub:
[Report a vulnerability](https://github.com/kelnei/rewrite-body/security/advisories/new).
Issues are disabled on this repository, and a vulnerability should not go in a
public pull request.

Say which version you tested, which Traefik version loaded it, and how to
reproduce the problem. The reproduction that helps most is a request and a
backend response, plus the plugin configuration, that show the behavior.

The maintainer handles reports on a best-effort basis.

## Supported versions

Only the latest release is maintained. A fix ships as a new release, never as a
change to a published tag.

## What counts

[`docs/TRUST_BOUNDARIES.md`](docs/TRUST_BOUNDARIES.md) says who can reach the
plugin and what each input is trusted with. Read it before reporting. It also
lists the behavior that is intended, such as the operator's own configuration
being trusted and Trace logging response bodies.
