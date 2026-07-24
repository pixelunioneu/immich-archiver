package archive

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pixelunioneu/immich-archiver/internal/immich"
)

// fakeSource is an in-memory Source for exercising Syncer without HTTP.
type fakeSource struct {
	assets    []*immich.Asset
	byID      map[string]*immich.Asset
	files     map[string]string // asset id -> file content
	albums    []immich.Album
	albumAsts map[string][]*immich.Asset
}

func newFakeSource() *fakeSource {
	return &fakeSource{
		byID:      map[string]*immich.Asset{},
		files:     map[string]string{},
		albumAsts: map[string][]*immich.Asset{},
	}
}

func mustAsset(t *testing.T, id, name, fileCreatedAt string, extra map[string]any) *immich.Asset {
	t.Helper()
	m := map[string]any{
		"id":               id,
		"originalFileName": name,
		"fileCreatedAt":    fileCreatedAt,
	}
	for k, v := range extra {
		m[k] = v
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var a immich.Asset
	if err := json.Unmarshal(data, &a); err != nil {
		t.Fatal(err)
	}
	return &a
}

func (f *fakeSource) add(a *immich.Asset, content string) {
	f.assets = append(f.assets, a)
	f.byID[a.ID] = a
	f.files[a.ID] = content
}

func (f *fakeSource) SearchAssets(ctx context.Context, query immich.SearchMetadataQuery, fn func(*immich.Asset) error) error {
	for _, a := range f.assets {
		if err := fn(a); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeSource) DownloadOriginal(ctx context.Context, assetID string) (io.ReadCloser, error) {
	content, ok := f.files[assetID]
	if !ok {
		return nil, fmt.Errorf("no such asset %s", assetID)
	}
	return io.NopCloser(strings.NewReader(content)), nil
}

func (f *fakeSource) GetAsset(ctx context.Context, assetID string) (*immich.Asset, error) {
	a, ok := f.byID[assetID]
	if !ok {
		return nil, fmt.Errorf("no such asset %s", assetID)
	}
	return a, nil
}

func (f *fakeSource) ListAlbums(ctx context.Context, sharedOnly bool) ([]immich.Album, error) {
	return f.albums, nil
}

func (f *fakeSource) GetAlbum(ctx context.Context, albumID string) (*immich.AlbumDetail, error) {
	for _, al := range f.albums {
		if al.ID == albumID {
			return &immich.AlbumDetail{Album: al, Assets: f.albumAsts[albumID]}, nil
		}
	}
	return nil, fmt.Errorf("no such album %s", albumID)
}

func baseOptions(rootDir string) Options {
	return Options{
		RootDir:      rootDir,
		PathTemplate: DefaultPathTemplate,
		Concurrency:  2,
		Retries:      1,
		RetryDelay:   time.Millisecond,
	}
}

func TestSyncerDownloadsAndWritesSidecar(t *testing.T) {
	dir := t.TempDir()
	src := newFakeSource()
	src.add(mustAsset(t, "a1", "IMG_0001.jpg", "2005-06-15T10:00:00.000Z", nil), "photo-bytes")

	s := &Syncer{Source: src, Options: baseOptions(dir)}
	stats, err := s.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stats.Downloaded != 1 {
		t.Fatalf("stats = %+v", stats)
	}

	want := filepath.Join(dir, "2005", "2005-06", "IMG_0001.jpg")
	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("expected file at %s: %v", want, err)
	}
	if string(data) != "photo-bytes" {
		t.Fatalf("got %q", data)
	}
	sidecar, err := os.ReadFile(want + ".json")
	if err != nil {
		t.Fatalf("expected sidecar: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(sidecar, &decoded); err != nil || decoded["id"] != "a1" {
		t.Fatalf("sidecar content wrong: %s", sidecar)
	}
}

func TestSyncerSkipsAlreadyDownloaded(t *testing.T) {
	dir := t.TempDir()
	src := newFakeSource()
	src.add(mustAsset(t, "a1", "IMG_0001.jpg", "2005-06-15T10:00:00.000Z", nil), "photo-bytes")

	s := &Syncer{Source: src, Options: baseOptions(dir)}
	if _, err := s.Run(context.Background()); err != nil {
		t.Fatalf("first run: %v", err)
	}

	stats, err := s.Run(context.Background())
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if stats.Skipped != 1 || stats.Downloaded != 0 {
		t.Fatalf("stats = %+v, want 1 skipped", stats)
	}
}

func TestSyncerUnknownDateBucket(t *testing.T) {
	dir := t.TempDir()
	src := newFakeSource()
	src.add(mustAsset(t, "a1", "scan.jpg", "", nil), "bytes")

	s := &Syncer{Source: src, Options: baseOptions(dir)}
	if _, err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, unknownDateDir, "scan.jpg")); err != nil {
		t.Fatalf("expected file under unknown-date/: %v", err)
	}
}

func TestSyncerSkipsTrashed(t *testing.T) {
	dir := t.TempDir()
	src := newFakeSource()
	src.add(mustAsset(t, "a1", "trashed.jpg", "2005-06-15T10:00:00.000Z", map[string]any{"isTrashed": true}), "bytes")

	s := &Syncer{Source: src, Options: baseOptions(dir)}
	stats, err := s.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stats.Downloaded != 0 || stats.Skipped != 0 {
		t.Fatalf("expected trashed asset to be entirely ignored, got %+v", stats)
	}
}

func TestSyncerDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	src := newFakeSource()
	src.add(mustAsset(t, "a1", "IMG_0001.jpg", "2005-06-15T10:00:00.000Z", nil), "photo-bytes")

	opts := baseOptions(dir)
	opts.DryRun = true
	s := &Syncer{Source: src, Options: opts}
	stats, err := s.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stats.Downloaded != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	if _, err := os.Stat(filepath.Join(dir, "2005", "2005-06", "IMG_0001.jpg")); !os.IsNotExist(err) {
		t.Fatal("expected dry-run to not write any file")
	}
}

func TestSyncerLivePhotoPairing(t *testing.T) {
	dir := t.TempDir()
	src := newFakeSource()
	still := mustAsset(t, "still-1", "IMG_1234.heic", "2005-06-15T10:00:00.000Z", map[string]any{"livePhotoVideoId": "video-1"})
	video := mustAsset(t, "video-1", "IMG_1234.mov", "2005-06-15T10:00:00.000Z", nil)
	src.add(still, "still-bytes")
	src.byID["video-1"] = video
	src.files["video-1"] = "video-bytes"

	s := &Syncer{Source: src, Options: baseOptions(dir)}
	stats, err := s.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stats.Downloaded != 2 {
		t.Fatalf("stats = %+v, want 2 downloads (still + video)", stats)
	}

	subdir := filepath.Join(dir, "2005", "2005-06")
	if _, err := os.Stat(filepath.Join(subdir, "IMG_1234.heic")); err != nil {
		t.Fatalf("missing still: %v", err)
	}
	if _, err := os.Stat(filepath.Join(subdir, "IMG_1234.mov")); err != nil {
		t.Fatalf("missing paired video: %v", err)
	}
}

func TestSyncerSharedAlbumsIntoSeparateRoot(t *testing.T) {
	rootDir := t.TempDir()
	sharedDir := filepath.Join(rootDir, "shared-with-me")
	src := newFakeSource()
	shared := mustAsset(t, "shared-1", "friend.jpg", "2010-01-05T10:00:00.000Z", nil)
	src.byID["shared-1"] = shared
	src.files["shared-1"] = "shared-bytes"
	src.albums = []immich.Album{{ID: "al1", AlbumName: "From Alice", Shared: true}}
	src.albumAsts["al1"] = []*immich.Asset{shared}

	opts := baseOptions(rootDir)
	opts.IncludeShared = true
	opts.SharedRootDir = sharedDir
	s := &Syncer{Source: src, Options: opts}

	stats, err := s.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stats.Downloaded != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	want := filepath.Join(sharedDir, "2010", "2010-01", "friend.jpg")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected shared asset at %s: %v", want, err)
	}
}

func TestSyncerSharedAlbumDedupesAcrossAlbums(t *testing.T) {
	rootDir := t.TempDir()
	sharedDir := filepath.Join(rootDir, "shared-with-me")
	src := newFakeSource()
	shared := mustAsset(t, "shared-1", "friend.jpg", "2010-01-05T10:00:00.000Z", nil)
	src.byID["shared-1"] = shared
	src.files["shared-1"] = "shared-bytes"
	src.albums = []immich.Album{
		{ID: "al1", AlbumName: "From Alice", Shared: true},
		{ID: "al2", AlbumName: "From Bob", Shared: true},
	}
	src.albumAsts["al1"] = []*immich.Asset{shared}
	src.albumAsts["al2"] = []*immich.Asset{shared} // same asset shared in two albums

	opts := baseOptions(rootDir)
	opts.IncludeShared = true
	opts.SharedRootDir = sharedDir
	s := &Syncer{Source: src, Options: opts}

	stats, err := s.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stats.Downloaded != 1 {
		t.Fatalf("expected the duplicate shared asset to be downloaded once, got %+v", stats)
	}
}

func TestSyncerNotIncludeSharedByDefault(t *testing.T) {
	rootDir := t.TempDir()
	src := newFakeSource()
	src.albums = []immich.Album{{ID: "al1", Shared: true}}
	src.albumAsts["al1"] = []*immich.Asset{mustAsset(t, "shared-1", "friend.jpg", "2010-01-05T10:00:00.000Z", nil)}

	s := &Syncer{Source: src, Options: baseOptions(rootDir)}
	stats, err := s.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stats.Downloaded != 0 {
		t.Fatalf("expected shared assets untouched without --include-shared, got %+v", stats)
	}
}
