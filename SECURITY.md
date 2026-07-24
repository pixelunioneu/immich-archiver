# Security Policy

## Reporting a vulnerability

Please **do not** open a public GitHub issue for security vulnerabilities (e.g. anything that
could leak an Immich API key, a path traversal via crafted filenames/sidecars, or SSRF via the
`--url` flag).

Instead, report it privately via [GitHub Security Advisories](https://github.com/pixelunioneu/immich-archiver/security/advisories/new)
for this repository, or email security@pixelunion.eu.

Please include:
- A description of the issue and its impact
- Steps to reproduce (a minimal example is ideal)
- The version/commit affected

We'll acknowledge reports within a few business days and aim to publish a fix and advisory before
any public disclosure.

## Supported versions

Only the latest released version is supported with security fixes.
