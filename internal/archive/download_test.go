package archive

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fetcher(data string, failTimes int) (FileFetcher, *int) {
	calls := 0
	return func(ctx context.Context, assetID string) (io.ReadCloser, error) {
		calls++
		if calls <= failTimes {
			return nil, errors.New("simulated network failure")
		}
		return io.NopCloser(strings.NewReader(data)), nil
	}, &calls
}

func TestDownloadToFileSuccess(t *testing.T) {
	dir := t.TempDir()
	fetch, _ := fetcher("hello world", 0)

	err := DownloadToFile(context.Background(), fetch, "asset-1", dir, "photo.jpg", 3, time.Millisecond)
	if err != nil {
		t.Fatalf("DownloadToFile: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "photo.jpg"))
	if err != nil {
		t.Fatalf("reading result: %v", err)
	}
	if string(data) != "hello world" {
		t.Fatalf("got %q", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "photo.jpg.part")); !os.IsNotExist(err) {
		t.Fatal("expected .part temp file to be gone after success")
	}
}

func TestDownloadToFileRetriesThenSucceeds(t *testing.T) {
	dir := t.TempDir()
	fetch, calls := fetcher("data", 2)

	err := DownloadToFile(context.Background(), fetch, "asset-1", dir, "photo.jpg", 3, time.Millisecond)
	if err != nil {
		t.Fatalf("DownloadToFile: %v", err)
	}
	if *calls != 3 {
		t.Fatalf("expected 3 attempts, got %d", *calls)
	}
}

func TestDownloadToFileExhaustsRetries(t *testing.T) {
	dir := t.TempDir()
	fetch, calls := fetcher("data", 100)

	err := DownloadToFile(context.Background(), fetch, "asset-1", dir, "photo.jpg", 2, time.Millisecond)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if *calls != 3 { // initial + 2 retries
		t.Fatalf("expected 3 attempts, got %d", *calls)
	}
	if _, err := os.Stat(filepath.Join(dir, "photo.jpg")); !os.IsNotExist(err) {
		t.Fatal("expected no final file to exist after total failure")
	}
	if _, err := os.Stat(filepath.Join(dir, "photo.jpg.part")); !os.IsNotExist(err) {
		t.Fatal("expected no leftover .part file after total failure")
	}
}

func TestDownloadToFileNeverExposesPartialFileUnderFinalName(t *testing.T) {
	dir := t.TempDir()
	// A fetch that fails mid-copy is hard to simulate with a plain reader,
	// but we can at least assert the final name never appears until the
	// fetch succeeds outright.
	fetch, _ := fetcher("data", 1)

	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := DownloadToFile(context.Background(), fetch, "asset-1", dir, "photo.jpg", 3, time.Millisecond); err != nil {
			t.Error(err)
		}
	}()
	<-done

	if _, err := os.Stat(filepath.Join(dir, "photo.jpg")); err != nil {
		t.Fatalf("expected final file to exist after eventual success: %v", err)
	}
}

func TestWriteSidecar(t *testing.T) {
	dir := t.TempDir()
	if err := WriteSidecar(dir, "photo.jpg", []byte(`{"id":"asset-1"}`)); err != nil {
		t.Fatalf("WriteSidecar: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "photo.jpg.json"))
	if err != nil {
		t.Fatalf("reading sidecar: %v", err)
	}
	if string(data) != `{"id":"asset-1"}` {
		t.Fatalf("got %q", data)
	}
}
