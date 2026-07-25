package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

func (a app) docsCommand() *cobra.Command {
	var outDir string
	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Generate CLI reference documentation",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return markError(kindInvalidInput, fmt.Errorf("docs does not accept positional arguments"))
			}
			target, err := filepath.Abs(outDir)
			if err != nil {
				return markError(kindInvalidInput, err)
			}
			if err := os.MkdirAll(target, 0o755); err != nil {
				return markError(kindRender, err)
			}
			root := cmd.Root()
			root.DisableAutoGenTag = true
			if err := doc.GenMarkdownTree(root, target); err != nil {
				return markError(kindRender, err)
			}
			if err := normalizeGeneratedMarkdown(target); err != nil {
				return markError(kindRender, err)
			}
			fmt.Fprintf(a.stdout, "wrote CLI docs to %s\n", target)
			return nil
		},
	}
	cmd.Flags().StringVar(&outDir, "out", "docs/cli", "directory to write Markdown docs into")
	return cmd
}

func normalizeGeneratedMarkdown(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		normalized := strings.TrimRight(string(data), "\n") + "\n"
		if normalized == string(data) {
			continue
		}
		if err := os.WriteFile(path, []byte(normalized), 0o644); err != nil {
			return err
		}
	}
	return nil
}
