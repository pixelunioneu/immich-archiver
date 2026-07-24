# Contributing to immich-archiver

Thanks for considering a contribution. This is a small, focused tool — keep changes scoped and
avoid adding abstractions or config surface the project doesn't need yet.

## Getting set up

The repo pins its Go toolchain via [mise](https://mise.jdx.dev):

```sh
mise install       # installs the pinned Go version
mise run build      # builds ./immich-archiver
mise run test        # unit tests, mocked Immich API, no network required
mise run check       # vet + lint + test
```

Without mise, any Go matching the version in `go.mod` works fine with plain `go build`/`go test`.

## Before opening a PR

- `mise run check` (or `go vet ./... && golangci-lint run && go test ./... -race`) passes locally.
- New behavior has unit test coverage. Sync/download logic is tested against a mocked Immich API
  or an in-memory fake `archive.Source` — see `internal/immich/client_test.go` and
  `internal/archive/sync_test.go` for the patterns. You should not need a real Immich server to
  write or run tests; the live-server suite (`go test -tags integration ./...`) is a separate,
  non-blocking CI job and isn't required for a PR to merge.
- Keep PRs small and single-purpose. If a change touches CLI flags, update the flag table in
  `README.md` too.

## Commit messages

Plain, descriptive commit messages explaining *why* a change was made. No enforced format.

## Reporting bugs / requesting features

Open a GitHub issue. Include your Immich server version and the exact command you ran when
reporting a bug — most issues in a tool like this come down to a specific asset shape (missing
EXIF, an unusual Live Photo pairing, a large library edge case) that's easiest to fix with a
reproducible example.

## Security

Found a vulnerability (e.g. something that could leak an API key, or a path traversal in how
filenames/sidecars are written)? Please report it privately rather than opening a public issue —
see [SECURITY.md](SECURITY.md).

## License

By contributing, you agree your contribution is licensed under the project's
[AGPL-3.0](LICENSE).
