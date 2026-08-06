package archive

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pixelunioneu/immich-archiver/internal/immich"
)

// Source is the subset of the Immich client the syncer depends on. Defined
// as an interface so tests can substitute an in-memory fake.
type Source interface {
	SearchAssets(ctx context.Context, query immich.SearchMetadataQuery, fn func(*immich.Asset) error) error
	DownloadOriginal(ctx context.Context, assetID string) (io.ReadCloser, error)
	GetAsset(ctx context.Context, assetID string) (*immich.Asset, error)
	ListAlbums(ctx context.Context, sharedOnly bool) ([]immich.Album, error)
	GetAlbum(ctx context.Context, albumID string) (*immich.AlbumDetail, error)
}

// Options configures a sync run.
type Options struct {
	RootDir            string
	PathTemplate       string
	IncludeShared      bool
	SharedRootDir      string
	SharedPathTemplate string
	Concurrency        int
	Retries            int
	RetryDelay         time.Duration
	DryRun             bool

	// RefreshSidecars rewrites the sidecar of an asset already on disk
	// instead of skipping it outright. Without this, an archive built by an
	// older version keeps its sidecars forever: assets present on disk never
	// reach the write path, so a metadata fix only ever reaches newly
	// downloaded files. Original files are still never re-downloaded.
	RefreshSidecars bool
}

const unknownDateDir = "unknown-date"

// Action describes what happened to one asset during the run, reported via
// Options-independent Reporter callbacks so callers can render progress.
type Action string

const (
	ActionDownloaded Action = "downloaded"
	ActionSkipped    Action = "skipped"     // already present on disk
	ActionWouldFetch Action = "would-fetch" // dry-run
	ActionRefreshed  Action = "refreshed"   // file kept, sidecar rewritten
	ActionFailed     Action = "failed"
)

// Event is reported once per processed asset (and once more per paired
// live-photo video component).
type Event struct {
	AssetID  string
	Filename string
	Action   Action
	Err      error
}

// Reporter receives one Event per asset processed. May be called
// concurrently from multiple workers.
type Reporter func(Event)

// Stats summarizes a completed run.
type Stats struct {
	Downloaded int
	Skipped    int
	Refreshed  int
	Failed     int
}

// Syncer mirrors an Immich instance's assets onto local disk per Options.
type Syncer struct {
	Source   Source
	Options  Options
	Reporter Reporter

	downloadLocks keyedMutex
}

// keyedMutex hands out a per-key lock so callers can serialize work on the
// same key without blocking unrelated keys. Its zero value is ready to use.
//
// This exists because a live-photo video asset can be referenced by more
// than one still asset (common in Google Takeout imports with duplicated
// motion-photo pairs). Without per-asset locking, two workers could race to
// download the same video into the same "<filename>.part" temp file: one
// worker's successful rename would remove the temp file out from under the
// other, which then failed with "no such file or directory".
type keyedMutex struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// Lock blocks until key is uncontended, then returns a function to release it.
func (k *keyedMutex) Lock(key string) func() {
	k.mu.Lock()
	if k.locks == nil {
		k.locks = make(map[string]*sync.Mutex)
	}
	l, ok := k.locks[key]
	if !ok {
		l = &sync.Mutex{}
		k.locks[key] = l
	}
	k.mu.Unlock()

	l.Lock()
	return l.Unlock
}

// Run walks the owned library (and, if enabled, shared albums) and
// downloads every asset not already present on disk.
func (s *Syncer) Run(ctx context.Context) (Stats, error) {
	var stats Stats
	var mu sync.Mutex
	report := func(e Event) {
		mu.Lock()
		switch e.Action {
		case ActionDownloaded, ActionWouldFetch:
			stats.Downloaded++
		case ActionSkipped:
			stats.Skipped++
		case ActionRefreshed:
			stats.Refreshed++
		case ActionFailed:
			stats.Failed++
		}
		mu.Unlock()
		if s.Reporter != nil {
			s.Reporter(e)
		}
	}

	concurrency := s.Options.Concurrency
	if concurrency < 1 {
		concurrency = 1
	}

	jobs := make(chan *immich.Asset)
	var wg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for a := range jobs {
				s.processAsset(ctx, a, s.Options.RootDir, s.Options.PathTemplate, report)
			}
		}()
	}

	err := s.Source.SearchAssets(ctx, immich.SearchMetadataQuery{
		WithDeleted: false,
		IsArchived:  nil,
		WithExif:    true,
		WithPeople:  true,
	}, func(a *immich.Asset) error {
		if a.IsTrashed {
			return nil
		}
		select {
		case jobs <- a:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	})
	close(jobs)
	wg.Wait()
	if err != nil {
		return stats, fmt.Errorf("listing assets: %w", err)
	}

	if s.Options.IncludeShared {
		if err := s.runShared(ctx, report); err != nil {
			return stats, fmt.Errorf("syncing shared assets: %w", err)
		}
	}

	return stats, nil
}

func (s *Syncer) runShared(ctx context.Context, report func(Event)) error {
	albums, err := s.Source.ListAlbums(ctx, true)
	if err != nil {
		return err
	}

	seen := make(map[string]bool)
	tmpl := s.Options.SharedPathTemplate
	if tmpl == "" {
		tmpl = s.Options.PathTemplate
	}

	for _, album := range albums {
		detail, err := s.Source.GetAlbum(ctx, album.ID)
		if err != nil {
			return fmt.Errorf("getting shared album %s: %w", album.ID, err)
		}
		for _, a := range detail.Assets {
			if a.IsTrashed || seen[a.ID] {
				continue
			}
			seen[a.ID] = true
			s.processAsset(ctx, a, s.Options.SharedRootDir, tmpl, report)
		}
	}
	return nil
}

func (s *Syncer) processAsset(ctx context.Context, a *immich.Asset, rootDir, tmpl string, report func(Event)) {
	dir, err := destinationDir(rootDir, tmpl, a)
	if err != nil {
		report(Event{AssetID: a.ID, Filename: a.OriginalFileName, Action: ActionFailed, Err: err})
		return
	}

	s.downloadOne(ctx, a, dir, a.OriginalFileName, report)

	if a.LivePhotoVideoID != "" {
		video, err := s.Source.GetAsset(ctx, a.LivePhotoVideoID)
		if err != nil {
			report(Event{AssetID: a.LivePhotoVideoID, Filename: a.OriginalFileName, Action: ActionFailed, Err: fmt.Errorf("fetching live-photo video for %s: %w", a.ID, err)})
			return
		}
		ext := filepath.Ext(video.OriginalFileName)
		base := strings.TrimSuffix(a.OriginalFileName, filepath.Ext(a.OriginalFileName))
		s.downloadOne(ctx, video, dir, base+ext, report)
	}
}

func (s *Syncer) downloadOne(ctx context.Context, a *immich.Asset, dir, desiredName string, report func(Event)) {
	// Lock on the target namespace, not just the asset ID: ResolveDestination
	// does a check-then-act filesystem scan, so two different assets that
	// want the same desiredName (e.g. duplicate stills sharing one
	// live-photo video, or literal duplicate imports) can otherwise race on
	// the same candidate path just as easily as two calls for the same
	// asset ID can.
	unlock := s.downloadLocks.Lock(filepath.Join(dir, desiredName))
	defer unlock()

	filename, exists, err := ResolveDestination(dir, desiredName, a.ID)
	if err != nil {
		report(Event{AssetID: a.ID, Filename: desiredName, Action: ActionFailed, Err: err})
		return
	}
	if exists {
		if !s.Options.RefreshSidecars {
			report(Event{AssetID: a.ID, Filename: filename, Action: ActionSkipped})
			return
		}
		if s.Options.DryRun {
			report(Event{AssetID: a.ID, Filename: filename, Action: ActionRefreshed})
			return
		}
		if err := WriteSidecar(dir, filename, a.RawJSON); err != nil {
			report(Event{AssetID: a.ID, Filename: filename, Action: ActionFailed, Err: err})
			return
		}
		report(Event{AssetID: a.ID, Filename: filename, Action: ActionRefreshed})
		return
	}
	if s.Options.DryRun {
		report(Event{AssetID: a.ID, Filename: filename, Action: ActionWouldFetch})
		return
	}

	if err := DownloadToFile(ctx, s.Source.DownloadOriginal, a.ID, dir, filename, s.Options.Retries, s.Options.RetryDelay); err != nil {
		report(Event{AssetID: a.ID, Filename: filename, Action: ActionFailed, Err: err})
		return
	}
	if err := WriteSidecar(dir, filename, a.RawJSON); err != nil {
		report(Event{AssetID: a.ID, Filename: filename, Action: ActionFailed, Err: err})
		return
	}
	report(Event{AssetID: a.ID, Filename: filename, Action: ActionDownloaded})
}

// dateLayout is Immich's timestamp format, e.g. "2005-06-15T10:30:00.000Z".
const dateLayout = time.RFC3339

func destinationDir(rootDir, tmpl string, a *immich.Asset) (string, error) {
	t, ok := assetTime(a)
	if !ok {
		return filepath.Join(rootDir, unknownDateDir), nil
	}
	rel, err := RenderPath(tmpl, t)
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, rel), nil
}

// assetTime resolves the timestamp used for foldering: fileCreatedAt,
// falling back to fileModifiedAt, falling back to "no usable date".
func assetTime(a *immich.Asset) (time.Time, bool) {
	if t, err := time.Parse(dateLayout, a.FileCreatedAt); err == nil {
		return t, true
	}
	if t, err := time.Parse(dateLayout, a.FileModifiedAt); err == nil {
		return t, true
	}
	return time.Time{}, false
}
