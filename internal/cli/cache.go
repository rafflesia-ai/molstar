package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/molstar/internal/job"
)

func (a app) cacheCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Inspect and prune the Mol* download cache",
	}
	cmd.AddCommand(a.cacheListCommand())
	cmd.AddCommand(a.cacheExplainCommand())
	cmd.AddCommand(a.cacheVerifyCommand())
	cmd.AddCommand(a.cachePruneCommand())
	return cmd
}

func (a app) cacheExplainCommand() *cobra.Command {
	var cacheDir string
	var provider string
	var format string
	var jsonReport bool
	cmd := &cobra.Command{
		Use:   "explain INPUT",
		Short: "Explain the cache key and offline availability for an input",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("cache explain", jsonReport, func() error {
				if err := exactArgs(args, 1, "cache explain"); err != nil {
					return markError(kindInvalidInput, err)
				}
				input := cacheExplainInput(args[0], provider, format)
				report := map[string]any{
					"ok":     true,
					"input":  input,
					"cache":  cacheDir,
					"format": input.ResolvedFormat(),
				}
				if err := job.EnforceLocalPathPolicy(input, job.Runtime{}); err != nil {
					report["blocked"] = err.Error()
				}
				resolved, err := input.ResolvedURL()
				if err != nil {
					return markError(kindInvalidInput, err)
				}
				report["url"] = resolved
				report["requires_network"] = input.RequiresNetwork()
				if input.RequiresNetwork() {
					cachePath := job.CachePathFor(cacheDir, resolved, input.ResolvedFormat())
					report["cache_path"] = cachePath
					hit := job.PathExists(cachePath)
					report["cache_hit"] = hit
					report["offline_available"] = hit
					if err := job.EnforceURLPolicy(resolved, job.Runtime{}); err != nil {
						report["blocked"] = err.Error()
					}
					if hit {
						entry, err := job.ReadCacheEntry(cachePath)
						if err != nil {
							report["cache_error"] = err.Error()
						} else {
							report["entry"] = entry
							report["age_seconds"] = int64(time.Since(entry.CachedAt).Seconds())
						}
					}
				} else {
					localPath := input.LocalPath()
					report["local_path"] = localPath
					report["offline_available"] = localPath != "" && job.PathExists(localPath)
					if localPath != "" {
						if info, err := os.Stat(localPath); err == nil {
							report["bytes"] = info.Size()
							report["modified_at"] = info.ModTime().UTC().Format(time.RFC3339)
						}
					}
				}
				if jsonReport {
					return writeJSON(a.stdout, report)
				}
				fmt.Fprintf(a.stdout, "url: %s\n", resolved)
				if cachePath, ok := report["cache_path"].(string); ok {
					fmt.Fprintf(a.stdout, "cache_path: %s\n", cachePath)
					fmt.Fprintf(a.stdout, "cache_hit: %v\n", report["cache_hit"])
				}
				fmt.Fprintf(a.stdout, "offline_available: %v\n", report["offline_available"])
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&cacheDir, "cache", ".molstar-cache", "cache directory")
	cmd.Flags().StringVar(&provider, "provider", "pdbe", "identifier provider for bare IDs")
	cmd.Flags().StringVar(&format, "format", "", "input format override")
	cmd.Flags().BoolVar(&jsonReport, "json", false, "write JSON report")
	return cmd
}

func cacheExplainInput(value string, provider string, format string) job.Input {
	trimmed := strings.TrimSpace(value)
	switch {
	case strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") || strings.HasPrefix(trimmed, "file://"):
		return job.Input{URL: trimmed, Format: format}
	case job.PathExists(trimmed) || strings.Contains(trimmed, string(os.PathSeparator)):
		return job.Input{Path: trimmed, Format: format}
	default:
		return job.Input{ID: trimmed, Provider: provider, Format: format}
	}
}

func (a app) cacheListCommand() *cobra.Command {
	var cacheDir string
	var jsonReport bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List cached remote inputs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("cache list", jsonReport, func() error {
				if len(args) != 0 {
					return markError(kindInvalidInput, fmt.Errorf("cache list does not accept positional arguments"))
				}
				entries, err := job.ListCache(cacheDir)
				if err != nil {
					return markError(kindRuntime, err)
				}
				if jsonReport {
					return writeJSON(a.stdout, map[string]any{"ok": true, "entries": entries})
				}
				for _, entry := range entries {
					fmt.Fprintf(a.stdout, "%s\t%d\t%s\n", entry.Path, entry.Bytes, entry.URL)
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&cacheDir, "cache", ".molstar-cache", "cache directory")
	cmd.Flags().BoolVar(&jsonReport, "json", false, "write JSON report")
	return cmd
}

func (a app) cacheVerifyCommand() *cobra.Command {
	var cacheDir string
	var jsonReport bool
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify cache sidecar hashes against cached files",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("cache verify", jsonReport, func() error {
				if len(args) != 0 {
					return markError(kindInvalidInput, fmt.Errorf("cache verify does not accept positional arguments"))
				}
				entries, err := job.ListCache(cacheDir)
				if err != nil {
					return markError(kindRuntime, err)
				}
				ok := true
				for _, entry := range entries {
					if !entry.Verified {
						ok = false
						break
					}
				}
				if jsonReport {
					return writeJSON(a.stdout, map[string]any{"ok": ok, "entries": entries})
				}
				for _, entry := range entries {
					status := "OK"
					if !entry.Verified {
						status = "BAD"
					}
					fmt.Fprintf(a.stdout, "%-4s %s\n", status, entry.Path)
				}
				if !ok {
					return markError(kindRuntime, fmt.Errorf("cache verification failed"))
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&cacheDir, "cache", ".molstar-cache", "cache directory")
	cmd.Flags().BoolVar(&jsonReport, "json", false, "write JSON report")
	return cmd
}

func (a app) cachePruneCommand() *cobra.Command {
	var cacheDir string
	var olderThan string
	var dryRun bool
	var jsonReport bool
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Prune cached remote inputs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("cache prune", jsonReport, func() error {
				if len(args) != 0 {
					return markError(kindInvalidInput, fmt.Errorf("cache prune does not accept positional arguments"))
				}
				duration := time.Duration(0)
				if olderThan != "" {
					parsed, err := parseRetentionDuration(olderThan)
					if err != nil {
						return markError(kindInvalidInput, err)
					}
					duration = parsed
				}
				removed, err := job.PruneCache(cacheDir, duration, dryRun)
				if err != nil {
					return markError(kindRuntime, err)
				}
				if jsonReport {
					return writeJSON(a.stdout, map[string]any{"ok": true, "dry_run": dryRun, "removed": removed})
				}
				for _, path := range removed {
					if dryRun {
						fmt.Fprintln(a.stdout, "would remove", path)
					} else {
						fmt.Fprintln(a.stdout, "removed", path)
					}
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&cacheDir, "cache", ".molstar-cache", "cache directory")
	cmd.Flags().StringVar(&olderThan, "older-than", "", "only prune entries older than this age, e.g. 30d, 720h")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print what would be removed")
	cmd.Flags().BoolVar(&jsonReport, "json", false, "write JSON report")
	return cmd
}
