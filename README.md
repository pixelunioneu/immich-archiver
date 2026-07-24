# immich-archiver

A small Go CLI, built by PixelUnion, that mirrors an [Immich](https://immich.app) instance's
timeline onto local disk — photos and videos organized into date-based folders, each with an
Immich-go-style sidecar JSON carrying the full original asset metadata.

## Usage

```sh
export IMMICH_URL=https://photos.example.com
export IMMICH_API_KEY=your-user-api-key

immich-archiver --dir /path/to/archive  # --dir is required
```

On a second run, assets already present on disk (verified by filename + a matching asset ID in
the sidecar) are skipped, so re-running is cheap.

### Layout

By default, assets land in `<dir>/{year}/{year}-{month}/`, e.g.:

```
/path/to/archive/2005/2005-06/IMG_0001.jpg
/path/to/archive/2005/2005-06/IMG_0001.jpg.json
```

Override the structure with `--path-template`, using `{year}`, `{month}`, `{day}` tokens, e.g.
`--path-template "{year}/{month}/{day}"`. Assets missing a usable date fall into `unknown-date/`.

Live Photos are downloaded as a still + a paired motion video sharing the same base filename.

### Flags

| Flag | Default | Description |
|---|---|---|
| `--url` | `$IMMICH_URL` | Immich server URL |
| `--api-key` | `$IMMICH_API_KEY` | Immich user API key |
| `--dir` | *(required)* | destination root directory |
| `--path-template` | `{year}/{year}-{month}` | folder structure template |
| `--include-shared` | `false` | also mirror assets from albums shared with you |
| `--shared-dir` | `<dir>/shared-with-me` | destination root for shared assets |
| `--shared-path-template` | same as `--path-template` | folder structure template for shared assets |
| `--concurrency` | `4` | parallel downloads |
| `--retries` | `3` | retry attempts on network/server errors |
| `--dry-run` | `false` | preview without writing |
| `--verbose` / `-v` | `false` | log one line per asset instead of a progress summary |

## Development

```sh
go build ./...
go test ./...             # unit tests only, against a mocked Immich API
go test -tags integration ./...  # requires IMMICH_TEST_URL / IMMICH_TEST_API_KEY
```

## Releases

Pushing a `vX.Y.Z` tag triggers [GoReleaser](https://goreleaser.com) to build binaries for
Linux, macOS, and Windows (amd64/arm64) and publish a GitHub Release. The latest release is
always listed at the project's GitHub Pages site.

## License

[AGPL-3.0](LICENSE)
