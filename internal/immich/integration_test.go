//go:build integration

package immich

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

var errStopEarly = errors.New("stop early")

// These tests hit a real, live Immich instance and are excluded from the
// default `go test ./...` run (and therefore never gate Dependabot
// auto-merge or PR checks). They run only in the scheduled/manual
// "integration" GitHub Actions workflow, which supplies
// IMMICH_TEST_URL/IMMICH_TEST_API_KEY as repository secrets.
func testClient(t *testing.T) *Client {
	t.Helper()
	url := os.Getenv("IMMICH_TEST_URL")
	key := os.Getenv("IMMICH_TEST_API_KEY")
	if url == "" || key == "" {
		t.Skip("IMMICH_TEST_URL / IMMICH_TEST_API_KEY not set")
	}
	c := NewClient(url, key)
	c.HTTPClient.Timeout = 30 * time.Second
	return c
}

func TestIntegrationPing(t *testing.T) {
	c := testClient(t)
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping against live server: %v", err)
	}
}

func TestIntegrationSearchAssetsFirstPage(t *testing.T) {
	c := testClient(t)
	count := 0
	err := c.SearchAssets(context.Background(), SearchMetadataQuery{}, func(a *Asset) error {
		count++
		if count >= 5 {
			return errStopEarly
		}
		return nil
	})
	if err != nil && err != errStopEarly {
		t.Fatalf("SearchAssets against live server: %v", err)
	}
}
