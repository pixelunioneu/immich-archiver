package cmd

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/pixelunioneu/immich-archiver/internal/archive"
)

// progress renders sync feedback: a single self-overwriting status line by
// default, or one line per asset under --verbose.
type progress struct {
	out     io.Writer
	verbose bool
	dryRun  bool
	refresh bool // report the refreshed count; suppressed when nothing can refresh
	mu      sync.Mutex

	downloaded int64
	skipped    int64
	refreshed  int64
	failed     int64
}

func newProgress(out io.Writer, verbose, dryRun, refresh bool) *progress {
	return &progress{out: out, verbose: verbose, dryRun: dryRun, refresh: refresh}
}

func (p *progress) report(e archive.Event) {
	switch e.Action {
	case archive.ActionDownloaded, archive.ActionWouldFetch:
		atomic.AddInt64(&p.downloaded, 1)
	case archive.ActionSkipped:
		atomic.AddInt64(&p.skipped, 1)
	case archive.ActionRefreshed:
		atomic.AddInt64(&p.refreshed, 1)
	case archive.ActionFailed:
		atomic.AddInt64(&p.failed, 1)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.verbose {
		verb := "downloaded"
		if p.dryRun {
			verb = "would download"
		}
		switch e.Action {
		case archive.ActionDownloaded, archive.ActionWouldFetch:
			_, _ = fmt.Fprintf(p.out, "%s %s\n", verb, e.Filename)
		case archive.ActionSkipped:
			_, _ = fmt.Fprintf(p.out, "skipped (already present) %s\n", e.Filename)
		case archive.ActionRefreshed:
			refreshVerb := "refreshed sidecar"
			if p.dryRun {
				refreshVerb = "would refresh sidecar"
			}
			_, _ = fmt.Fprintf(p.out, "%s %s\n", refreshVerb, e.Filename)
		case archive.ActionFailed:
			_, _ = fmt.Fprintf(p.out, "FAILED %s: %v\n", e.Filename, e.Err)
		}
		return
	}

	_, _ = fmt.Fprintf(p.out, "\rdownloaded %d, skipped %d%s, failed %d",
		atomic.LoadInt64(&p.downloaded), atomic.LoadInt64(&p.skipped),
		p.refreshedSegment(atomic.LoadInt64(&p.refreshed)), atomic.LoadInt64(&p.failed))
}

// refreshedSegment renders ", refreshed N" only when sidecar refreshing is
// enabled, so the normal status line keeps its existing shape.
func (p *progress) refreshedSegment(n int64) string {
	if !p.refresh {
		return ""
	}
	return fmt.Sprintf(", refreshed %d", n)
}

func (p *progress) finish(stats archive.Stats) {
	if !p.verbose {
		_, _ = fmt.Fprintln(p.out)
	}
	verb := "Downloaded"
	if p.dryRun {
		verb = "Would download"
	}
	_, _ = fmt.Fprintf(p.out, "%s: %d, skipped: %d%s, failed: %d\n",
		verb, stats.Downloaded, stats.Skipped, p.refreshedSegment(int64(stats.Refreshed)), stats.Failed)
}
