package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/pixelunioneu/immich-archiver/internal/archive"
	"github.com/pixelunioneu/immich-archiver/internal/immich"
	"github.com/spf13/cobra"
)

// version is set at build time via -ldflags "-X .../cmd.version=v1.2.3".
var version = "dev"

type flags struct {
	url                string
	apiKey             string
	dir                string
	pathTemplate       string
	includeShared      bool
	sharedDir          string
	sharedPathTemplate string
	concurrency        int
	retries            int
	dryRun             bool
	refreshSidecars    bool
	verbose            bool
}

// Execute builds and runs the root command.
func Execute() error {
	return newRootCmd().Execute()
}

func newRootCmd() *cobra.Command {
	f := &flags{}

	cmd := &cobra.Command{
		Use:           "immich-archiver",
		Short:         "Mirror an Immich instance's timeline onto local disk",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return validateFlags(cmd, f)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSync(cmd, f)
		},
	}

	cmd.Flags().StringVar(&f.url, "url", os.Getenv("IMMICH_URL"), "Immich server URL (env IMMICH_URL)")
	cmd.Flags().StringVar(&f.apiKey, "api-key", os.Getenv("IMMICH_API_KEY"), "Immich user API key (env IMMICH_API_KEY)")
	cmd.Flags().StringVar(&f.dir, "dir", os.Getenv("IMMICH_DIR"), "destination root directory (required, env IMMICH_DIR)")
	cmd.Flags().StringVar(&f.pathTemplate, "path-template", archive.DefaultPathTemplate, "folder structure template; supports {year}, {month}, {day}")
	cmd.Flags().BoolVar(&f.includeShared, "include-shared", false, "also mirror assets shared with you into a separate folder")
	cmd.Flags().StringVar(&f.sharedDir, "shared-dir", "", "destination root for shared assets (default: <dir>/shared-with-me)")
	cmd.Flags().StringVar(&f.sharedPathTemplate, "shared-path-template", "", "folder structure template for shared assets (default: same as --path-template)")
	cmd.Flags().IntVar(&f.concurrency, "concurrency", 4, "number of assets to download in parallel")
	cmd.Flags().IntVar(&f.retries, "retries", 3, "number of retry attempts for failed downloads/requests")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "list what would be downloaded without writing anything")
	cmd.Flags().BoolVar(&f.refreshSidecars, "refresh-sidecars", false, "rewrite the .json sidecar of assets already on disk (originals are not re-downloaded)")
	cmd.Flags().BoolVarP(&f.verbose, "verbose", "v", false, "log a line per asset instead of showing a progress bar")

	return cmd
}

// validateFlags checks required inputs before RunE. Failures print the full
// usage/help text (unlike runtime errors from RunE, which stay terse) so a
// missing/misspelled flag is immediately actionable.
func validateFlags(cmd *cobra.Command, f *flags) error {
	var missing []string
	if f.dir == "" {
		missing = append(missing, "--dir (or IMMICH_DIR)")
	}
	if f.url == "" {
		missing = append(missing, "--url (or IMMICH_URL)")
	}
	if f.apiKey == "" {
		missing = append(missing, "--api-key (or IMMICH_API_KEY)")
	}
	if len(missing) == 0 {
		return nil
	}
	_, _ = fmt.Fprintln(cmd.OutOrStderr(), cmd.UsageString())
	return fmt.Errorf("missing required flag(s): %s", strings.Join(missing, ", "))
}

func runSync(cmd *cobra.Command, f *flags) error {
	client := immich.NewClient(f.url, f.apiKey)
	client.Retries = f.retries

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	if err := client.Ping(ctx); err != nil {
		return fmt.Errorf("could not reach %s: %w", f.url, err)
	}

	sharedDir := f.sharedDir
	if sharedDir == "" {
		sharedDir = f.dir + string(os.PathSeparator) + "shared-with-me"
	}

	p := newProgress(cmd.OutOrStdout(), f.verbose, f.dryRun, f.refreshSidecars)
	s := &archive.Syncer{
		Source: client,
		Options: archive.Options{
			RootDir:            f.dir,
			PathTemplate:       f.pathTemplate,
			IncludeShared:      f.includeShared,
			SharedRootDir:      sharedDir,
			SharedPathTemplate: f.sharedPathTemplate,
			Concurrency:        f.concurrency,
			Retries:            f.retries,
			RetryDelay:         2 * time.Second,
			DryRun:             f.dryRun,
			RefreshSidecars:    f.refreshSidecars,
		},
		Reporter: p.report,
	}

	stats, err := s.Run(ctx)
	p.finish(stats)
	if err != nil {
		return err
	}
	if stats.Failed > 0 {
		return fmt.Errorf("%d asset(s) failed to sync", stats.Failed)
	}
	return nil
}
