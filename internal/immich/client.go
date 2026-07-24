package immich

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Client talks to a single Immich server, authenticated with a user API key.
type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client

	Retries    int
	RetryDelay time.Duration
}

// NewClient builds a Client with sane defaults. baseURL may or may not
// include a trailing "/api" path segment; both are accepted.
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		APIKey:     apiKey,
		HTTPClient: &http.Client{Timeout: 60 * time.Second},
		Retries:    3,
		RetryDelay: 2 * time.Second,
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

// downloadOriginal streams GET /assets/{id}/original with the same
// retry policy as doJSON, since large binary downloads see the same
// class of transient network/5xx failures.
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

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL(path), nil)
		if err != nil {
			return nil, fmt.Errorf("building request: %w", err)
		}
		req.Header.Set("x-api-key", c.APIKey)
		req.Header.Set("Accept", "application/octet-stream")

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request failed: %w", err)
			continue
		}
		if resp.StatusCode >= 500 {
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("server returned %s for GET %s", resp.Status, path)
			continue
		}
		if resp.StatusCode >= 400 {
			data, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			return nil, fmt.Errorf("GET %s: %s: %s", path, resp.Status, string(data))
		}
		return resp.Body, nil
	}
	return nil, fmt.Errorf("giving up after %d attempts: %w", attempts, lastErr)
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
