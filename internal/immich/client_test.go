package immich

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, "test-key")
	c.RetryDelay = time.Millisecond
	return c, srv
}

func TestSearchAssetsPaginates(t *testing.T) {
	pages := [][]byte{
		[]byte(`{"assets":{"total":2,"count":1,"items":[{"id":"a1","originalFileName":"a1.jpg"}],"nextPage":"2"}}`),
		[]byte(`{"assets":{"total":2,"count":1,"items":[{"id":"a2","originalFileName":"a2.jpg"}],"nextPage":null}}`),
	}
	var call int32

	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/search/metadata" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Fatalf("missing/incorrect x-api-key header: %q", got)
		}
		idx := atomic.AddInt32(&call, 1) - 1
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(pages[idx])
	})

	var got []string
	err := c.SearchAssets(context.Background(), SearchMetadataQuery{}, func(a *Asset) error {
		got = append(got, a.ID)
		return nil
	})
	if err != nil {
		t.Fatalf("SearchAssets: %v", err)
	}
	if len(got) != 2 || got[0] != "a1" || got[1] != "a2" {
		t.Fatalf("got %v, want [a1 a2]", got)
	}
	if call != 2 {
		t.Fatalf("expected 2 requests, got %d", call)
	}
}

func TestSearchAssetsPreservesRawJSON(t *testing.T) {
	body := `{"assets":{"total":1,"count":1,"items":[{"id":"a1","originalFileName":"a1.jpg","exifInfo":{"make":"Canon"}}],"nextPage":null}}`
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	})

	var raw json.RawMessage
	err := c.SearchAssets(context.Background(), SearchMetadataQuery{}, func(a *Asset) error {
		raw = a.RawJSON
		return nil
	})
	if err != nil {
		t.Fatalf("SearchAssets: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("raw JSON not preserved: %v", err)
	}
	exif, ok := decoded["exifInfo"].(map[string]any)
	if !ok || exif["make"] != "Canon" {
		t.Fatalf("expected exifInfo.make=Canon preserved in raw JSON, got %v", decoded)
	}
}

func TestDoJSONRetriesOn5xxThenSucceeds(t *testing.T) {
	var call int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&call, 1) <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"res":"pong"}`))
	})
	c.Retries = 3

	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if call != 3 {
		t.Fatalf("expected 3 requests (2 failures + 1 success), got %d", call)
	}
}

func TestDoJSONGivesUpAfterConfiguredRetries(t *testing.T) {
	var call int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&call, 1)
		w.WriteHeader(http.StatusInternalServerError)
	})
	c.Retries = 2

	if err := c.Ping(context.Background()); err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if call != 3 { // initial attempt + 2 retries
		t.Fatalf("expected 3 requests, got %d", call)
	}
}

func TestDoJSONDoesNotRetryOn4xx(t *testing.T) {
	var call int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&call, 1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"invalid api key"}`))
	})
	c.Retries = 3

	if err := c.Ping(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if call != 1 {
		t.Fatalf("expected exactly 1 request for a 4xx (no retry), got %d", call)
	}
}

func TestDownloadOriginal(t *testing.T) {
	want := []byte("fake-image-bytes")
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/assets/asset-123/original" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write(want)
	})

	rc, err := c.DownloadOriginal(context.Background(), "asset-123")
	if err != nil {
		t.Fatalf("DownloadOriginal: %v", err)
	}
	defer func() { _ = rc.Close() }()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestListAlbumsSharedFilter(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("shared") != "true" {
			t.Fatalf("expected shared=true query param, got %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`[{"id":"al1","albumName":"Family","shared":true}]`))
	})

	albums, err := c.ListAlbums(context.Background(), true)
	if err != nil {
		t.Fatalf("ListAlbums: %v", err)
	}
	if len(albums) != 1 || albums[0].ID != "al1" {
		t.Fatalf("unexpected albums: %+v", albums)
	}
}

// A download that takes longer than the API client's overall timeout must
// still succeed as long as bytes keep arriving. Regression test for large
// videos failing with "Client.Timeout ... while reading body".
func TestDownloadOriginalOutlastsAPITimeout(t *testing.T) {
	const chunks = 10
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < chunks; i++ {
			_, _ = w.Write([]byte("x"))
			w.(http.Flusher).Flush()
			time.Sleep(30 * time.Millisecond)
		}
	})
	c.HTTPClient.Timeout = 100 * time.Millisecond
	c.DownloadIdleTimeout = 200 * time.Millisecond

	rc, err := c.DownloadOriginal(context.Background(), "big")
	if err != nil {
		t.Fatalf("DownloadOriginal: %v", err)
	}
	defer func() { _ = rc.Close() }()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	if len(got) != chunks {
		t.Fatalf("got %d bytes, want %d", len(got), chunks)
	}
}

// A download whose body stops producing bytes must be aborted after the
// idle timeout instead of hanging forever.
func TestDownloadOriginalAbortsWhenBodyStalls(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})
	c.DownloadIdleTimeout = 50 * time.Millisecond

	rc, err := c.DownloadOriginal(context.Background(), "stuck")
	if err != nil {
		t.Fatalf("DownloadOriginal: %v", err)
	}
	defer func() { _ = rc.Close() }()

	start := time.Now()
	_, err = io.ReadAll(rc)
	if !errors.Is(err, ErrDownloadStalled) {
		t.Fatalf("expected ErrDownloadStalled, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("stall detection took %s", elapsed)
	}
}

// A server that never sends response headers must be treated as a stall
// and retried, then reported as such.
func TestDownloadOriginalRetriesWhenHeadersStall(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})
	c.DownloadIdleTimeout = 50 * time.Millisecond

	_, err := c.DownloadOriginal(context.Background(), "silent")
	if !errors.Is(err, ErrDownloadStalled) {
		t.Fatalf("expected ErrDownloadStalled, got %v", err)
	}
	if got, want := calls.Load(), int32(c.Retries+1); got != want {
		t.Fatalf("got %d attempts, want %d", got, want)
	}
}
