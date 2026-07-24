package archive

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// FileFetcher downloads an asset's original bytes. Satisfied by
// *immich.Client.DownloadOriginal.
type FileFetcher func(ctx context.Context, assetID string) (io.ReadCloser, error)

// DownloadToFile fetches assetID via fetch and writes it to
// filepath.Join(dir, filename), downloading into a "<filename>.part"
// temp file first and atomically renaming it into place only once the
// full transfer succeeds. This guarantees a crash or interrupted run never
// leaves a truncated file under the final name, which would otherwise be
// mistaken for a complete download on the next sync.
//
// The transfer (fetch + copy) is retried up to retries times on failure,
// waiting delay between attempts.
func DownloadToFile(ctx context.Context, fetch FileFetcher, assetID, dir, filename string, retries int, delay time.Duration) error {
	finalPath := filepath.Join(dir, filename)
	tempPath := finalPath + ".part"

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}

	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		if err := attemptDownload(ctx, fetch, assetID, tempPath); err != nil {
			lastErr = err
			_ = os.Remove(tempPath)
			continue
		}

		if err := os.Rename(tempPath, finalPath); err != nil {
			return fmt.Errorf("finalizing %s: %w", finalPath, err)
		}
		return nil
	}
	return fmt.Errorf("downloading asset %s after %d attempts: %w", assetID, retries+1, lastErr)
}

func attemptDownload(ctx context.Context, fetch FileFetcher, assetID, tempPath string) error {
	rc, err := fetch(ctx, assetID)
	if err != nil {
		return fmt.Errorf("fetching asset %s: %w", assetID, err)
	}
	defer func() { _ = rc.Close() }()

	f, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("creating temp file %s: %w", tempPath, err)
	}
	defer func() { _ = f.Close() }()

	if _, err := io.Copy(f, rc); err != nil {
		return fmt.Errorf("writing %s: %w", tempPath, err)
	}
	return nil
}

// WriteSidecar writes rawJSON to filepath.Join(dir, sidecarName(filename)),
// via the same temp+rename pattern as DownloadToFile so a crash mid-write
// can't leave a truncated sidecar that ResolveDestination would then fail
// to parse as a match.
func WriteSidecar(dir, filename string, rawJSON []byte) error {
	finalPath := filepath.Join(dir, sidecarName(filename))
	tempPath := finalPath + ".part"

	if err := os.WriteFile(tempPath, rawJSON, 0o644); err != nil {
		return fmt.Errorf("writing temp sidecar %s: %w", tempPath, err)
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		return fmt.Errorf("finalizing sidecar %s: %w", finalPath, err)
	}
	return nil
}
