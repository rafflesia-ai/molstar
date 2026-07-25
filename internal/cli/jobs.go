package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type prunedJobRecord struct {
	ID   string `json:"id"`
	Path string `json:"path"`
	Age  string `json:"age"`
}

func (a app) jobsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "jobs",
		Short: "Manage persisted server jobs",
	}
	cmd.AddCommand(a.jobsPruneCommand())
	return cmd
}

func (a app) jobsPruneCommand() *cobra.Command {
	var jobStore string
	var ttlText string
	var dryRun bool
	var jsonReport bool
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Prune persisted server job records",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("jobs prune", jsonReport, func() error {
				if len(args) != 0 {
					return markError(kindInvalidInput, fmt.Errorf("jobs prune does not accept positional arguments"))
				}
				if strings.TrimSpace(jobStore) == "" {
					return markError(kindInvalidInput, fmt.Errorf("--job-store is required"))
				}
				ttl, err := time.ParseDuration(ttlText)
				if err != nil || ttl <= 0 {
					return markError(kindInvalidInput, fmt.Errorf("--ttl must be a positive duration such as 24h"))
				}
				removed, err := pruneJobStore(jobStore, ttl, time.Now(), dryRun)
				if err != nil {
					return markError(kindRuntime, err)
				}
				if jsonReport {
					return writeJSON(a.stdout, map[string]any{"ok": true, "dry_run": dryRun, "removed": removed})
				}
				for _, record := range removed {
					if dryRun {
						fmt.Fprintf(a.stdout, "would remove %s\t%s\t%s\n", record.ID, record.Age, record.Path)
					} else {
						fmt.Fprintf(a.stdout, "removed %s\t%s\t%s\n", record.ID, record.Age, record.Path)
					}
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&jobStore, "job-store", ".molstar-jobs", "persisted job store directory")
	cmd.Flags().StringVar(&ttlText, "ttl", "24h", "remove jobs older than this duration")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print records without removing them")
	cmd.Flags().BoolVar(&jsonReport, "json", false, "write JSON report")
	return cmd
}

func pruneJobStore(dir string, ttl time.Duration, now time.Time, dryRun bool) ([]prunedJobRecord, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []prunedJobRecord{}, nil
		}
		return nil, err
	}
	removed := []prunedJobRecord{}
	for _, entry := range entries {
		// Skip directories, non-JSON files, and hidden/AppleDouble "._*" files:
		// on a non-native filesystem an unparseable "._foo.json" would otherwise
		// abort the whole prune with an error.
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var record serverJobRecord
		if err := json.Unmarshal(data, &record); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		timestamp := firstNonEmpty(record.FinishedAt, record.SubmittedAt)
		if timestamp == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339Nano, timestamp)
		if err != nil {
			return nil, fmt.Errorf("%s: invalid job timestamp %q", path, timestamp)
		}
		age := now.Sub(parsed)
		if age <= ttl {
			continue
		}
		item := prunedJobRecord{ID: record.ID, Path: path, Age: age.Truncate(time.Second).String()}
		if !dryRun {
			if err := os.Remove(path); err != nil {
				return nil, err
			}
		}
		removed = append(removed, item)
	}
	return removed, nil
}
