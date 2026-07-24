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
	mu      sync.Mutex

	downloaded int64
	skipped    int64
	failed     int64
}

func newProgress(out io.Writer, verbose, dryRun bool) *progress {
	return &progress{out: out, verbose: verbose, dryRun: dryRun}
}

func (p *progress) report(e archive.Event) {
	switch e.Action {
	case archive.ActionDownloaded, archive.ActionWouldFetch:
		atomic.AddInt64(&p.downloaded, 1)
	case archive.ActionSkipped:
		atomic.AddInt64(&p.skipped, 1)
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
			fmt.Fprintf(p.out, "%s %s\n", verb, e.Filename)
		case archive.ActionSkipped:
			fmt.Fprintf(p.out, "skipped (already present) %s\n", e.Filename)
		case archive.ActionFailed:
			fmt.Fprintf(p.out, "FAILED %s: %v\n", e.Filename, e.Err)
		}
		return
	}

	fmt.Fprintf(p.out, "\rdownloaded %d, skipped %d, failed %d",
		atomic.LoadInt64(&p.downloaded), atomic.LoadInt64(&p.skipped), atomic.LoadInt64(&p.failed))
}

func (p *progress) finish(stats archive.Stats) {
	if !p.verbose {
		fmt.Fprintln(p.out)
	}
	verb := "Downloaded"
	if p.dryRun {
		verb = "Would download"
	}
	fmt.Fprintf(p.out, "%s: %d, skipped: %d, failed: %d\n", verb, stats.Downloaded, stats.Skipped, stats.Failed)
}
