package immich

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ErrDownloadStalled is returned when a download receives no data for
// longer than Client.DownloadIdleTimeout.
var ErrDownloadStalled = errors.New("download stalled")

// Client talks to a single Immich server, authenticated with a user API key.
type Client struct {
	BaseURL string
	APIKey  string

	// HTTPClient is used for JSON API calls. Its Timeout bounds the whole
	// request, which is fine for small responses.
	HTTPClient *http.Client

	// DownloadClient is used for original-file downloads. It must not set
	// an overall Timeout: that would also cover reading the body, so any
	// file too large to stream within it would always fail. Hung transfers
	// are caught by DownloadIdleTimeout instead.
	DownloadClient *http.Client

	// DownloadIdleTimeout aborts a download attempt when no response
	// headers or body bytes arrive for this long.
	DownloadIdleTimeout time.Duration

	Retries    int
	RetryDelay time.Duration
}

// NewClient builds a Client with sane defaults. baseURL may or may not
// include a trailing "/api" path segment; both are accepted.
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL:             strings.TrimRight(baseURL, "/"),
		APIKey:              apiKey,
		HTTPClient:          &http.Client{Timeout: 60 * time.Second},
		DownloadClient:      &http.Client{},
		DownloadIdleTimeout: 60 * time.Second,
		Retries:             3,
		RetryDelay:          2 * time.Second,
	}
}

func (c *Client) apiURL(path string) string {
	base := c.BaseURL
	if !strings.HasSuffix(base, "/api") {
		base += "/api"
	}
	return base + path
}

// doJSON issues an HTTP request and, on success, decodes the JSON response
// body into out (if non-nil). It retries on network errors and 5xx
// responses, honoring Client.Retries/RetryDelay.
func (c *Client) doJSON(ctx context.Context, method, path string, body, out any) error {
	var bodyBytes []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request body: %w", err)
		}
		bodyBytes = b
	}

	var lastErr error
	attempts := c.Retries + 1
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(c.RetryDelay):
			}
		}

		var reqBody io.Reader
		if bodyBytes != nil {
			reqBody = bytes.NewReader(bodyBytes)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.apiURL(path), reqBody)
		if err != nil {
			return fmt.Errorf("building request: %w", err)
		}
		req.Header.Set("x-api-key", c.APIKey)
		req.Header.Set("Accept", "application/json")
		if bodyBytes != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request failed: %w", err)
			continue
		}

		if resp.StatusCode >= 500 {
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("server returned %s for %s %s", resp.Status, method, path)
			continue
		}
		if resp.StatusCode >= 400 {
			data, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, string(data))
		}

		defer func() { _ = resp.Body.Close() }()
		if out != nil {
			if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
				return fmt.Errorf("decoding response from %s %s: %w", method, path, err)
			}
		}
		return nil
	}
	return fmt.Errorf("giving up after %d attempts: %w", attempts, lastErr)
}

// downloadWithRetry streams GET path with the same retry policy as
// doJSON, since large binary downloads see the same class of transient
// network/5xx failures. Each attempt is guarded by an idle timer rather
// than an overall deadline, so a download may take as long as it needs
// while data keeps flowing.
func (c *Client) downloadWithRetry(ctx context.Context, path string) (io.ReadCloser, error) {
	var lastErr error
	attempts := c.Retries + 1
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(c.RetryDelay):
			}
		}

		body, err := c.downloadAttempt(ctx, path)
		if err == nil {
			return body, nil
		}
		var permanent *permanentError
		if errors.As(err, &permanent) {
			return nil, permanent.err
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		lastErr = err
	}
	return nil, fmt.Errorf("giving up after %d attempts: %w", attempts, lastErr)
}

// permanentError marks a download failure that retrying cannot fix.
type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }

func (c *Client) downloadAttempt(ctx context.Context, path string) (io.ReadCloser, error) {
	attemptCtx, cancel := context.WithCancelCause(ctx)
	idle := c.DownloadIdleTimeout
	timer := time.AfterFunc(idle, func() { cancel(ErrDownloadStalled) })
	abort := func() {
		timer.Stop()
		cancel(nil)
	}

	req, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, c.apiURL(path), nil)
	if err != nil {
		abort()
		return nil, &permanentError{fmt.Errorf("building request: %w", err)}
	}
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := c.DownloadClient.Do(req)
	if err != nil {
		stalled := errors.Is(context.Cause(attemptCtx), ErrDownloadStalled)
		abort()
		if stalled {
			return nil, fmt.Errorf("no response for GET %s within %s: %w", path, idle, ErrDownloadStalled)
		}
		return nil, fmt.Errorf("request failed: %w", err)
	}
	if resp.StatusCode >= 500 {
		_ = resp.Body.Close()
		abort()
		return nil, fmt.Errorf("server returned %s for GET %s", resp.Status, path)
	}
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		abort()
		return nil, &permanentError{fmt.Errorf("GET %s: %s: %s", path, resp.Status, string(data))}
	}
	timer.Reset(idle)
	return &idleTimeoutBody{body: resp.Body, ctx: attemptCtx, timer: timer, idle: idle, abort: abort}, nil
}

// idleTimeoutBody pushes the attempt's idle deadline forward every time
// body bytes arrive, and reports ErrDownloadStalled when it fires.
type idleTimeoutBody struct {
	body  io.ReadCloser
	ctx   context.Context
	timer *time.Timer
	idle  time.Duration
	abort func()
}

func (b *idleTimeoutBody) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	if n > 0 {
		b.timer.Reset(b.idle)
	}
	if err != nil && err != io.EOF && errors.Is(context.Cause(b.ctx), ErrDownloadStalled) {
		err = fmt.Errorf("no data received for %s: %w", b.idle, ErrDownloadStalled)
	}
	return n, err
}

func (b *idleTimeoutBody) Close() error {
	b.abort()
	return b.body.Close()
}

// Ping verifies the server is reachable and the API key is valid.
func (c *Client) Ping(ctx context.Context) error {
	var out struct {
		Res string `json:"res"`
	}
	return c.doJSON(ctx, http.MethodGet, "/server/ping", nil, &out)
}

const searchPageSize = 1000

// SearchAssets streams all assets matching the query across every page,
// invoking fn for each one. Iteration stops early if fn returns an error.
func (c *Client) SearchAssets(ctx context.Context, query SearchMetadataQuery, fn func(*Asset) error) error {
	query.Size = searchPageSize
	page := 1
	for {
		query.Page = page
		var resp searchMetadataResponse
		if err := c.doJSON(ctx, http.MethodPost, "/search/metadata", query, &resp); err != nil {
			return fmt.Errorf("searching metadata (page %d): %w", page, err)
		}
		for _, a := range resp.Assets.Items {
			if err := fn(a); err != nil {
				return err
			}
		}
		if resp.Assets.NextPage == "" {
			return nil
		}
		next, err := strconv.Atoi(resp.Assets.NextPage)
		if err != nil {
			return fmt.Errorf("parsing nextPage %q: %w", resp.Assets.NextPage, err)
		}
		page = next
	}
}

// DownloadOriginal returns a stream of the asset's original file bytes.
// The caller must close the returned reader.
func (c *Client) DownloadOriginal(ctx context.Context, assetID string) (io.ReadCloser, error) {
	return c.downloadWithRetry(ctx, "/assets/"+assetID+"/original")
}

// GetAsset returns a single asset's full metadata by ID. Used to resolve the
// linked video component of a Live Photo, which Immich's search/metadata
// endpoint omits from normal listings.
func (c *Client) GetAsset(ctx context.Context, assetID string) (*Asset, error) {
	var a Asset
	if err := c.doJSON(ctx, http.MethodGet, "/assets/"+assetID, nil, &a); err != nil {
		return nil, fmt.Errorf("getting asset %s: %w", assetID, err)
	}
	return &a, nil
}

// ListAlbums returns every album visible to the current user.
func (c *Client) ListAlbums(ctx context.Context, sharedOnly bool) ([]Album, error) {
	path := "/albums"
	if sharedOnly {
		path += "?shared=true"
	}
	var albums []Album
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &albums); err != nil {
		return nil, fmt.Errorf("listing albums: %w", err)
	}
	return albums, nil
}

// GetAlbum returns an album's full detail, including its assets.
func (c *Client) GetAlbum(ctx context.Context, albumID string) (*AlbumDetail, error) {
	var detail AlbumDetail
	if err := c.doJSON(ctx, http.MethodGet, "/albums/"+albumID, nil, &detail); err != nil {
		return nil, fmt.Errorf("getting album %s: %w", albumID, err)
	}
	return &detail, nil
}
