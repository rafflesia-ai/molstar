package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func (a app) completionCommand() *cobra.Command {
	var out string
	var outDir string
	cmd := &cobra.Command{
		Use:   "completion SHELL",
		Short: "Generate shell completion scripts",
		Long:  "Generate shell completion scripts for bash, zsh, fish, powershell, or all shells.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := exactArgs(args, 1, "completion"); err != nil {
				return markError(kindInvalidInput, err)
			}
			root := cmd.Root()
			shell := args[0]
			if shell == "all" {
				if out != "" {
					return markError(kindInvalidInput, fmt.Errorf("--out cannot be used with completion all; use --out-dir"))
				}
				if outDir == "" {
					return markError(kindInvalidInput, fmt.Errorf("completion all requires --out-dir"))
				}
				for _, item := range []string{"bash", "zsh", "fish", "powershell"} {
					target := filepath.Join(outDir, completionFilename(item))
					if err := writeCompletion(root, item, target); err != nil {
						return markError(kindRender, err)
					}
				}
				return nil
			}
			if !supportedCompletionShell(shell) {
				return markError(kindInvalidInput, fmt.Errorf("unsupported shell %q; expected bash, zsh, fish, powershell, or all", shell))
			}
			target := out
			if target == "" && outDir != "" {
				target = filepath.Join(outDir, completionFilename(shell))
			}
			if target != "" {
				return markError(kindRender, writeCompletion(root, shell, target))
			}
			return markError(kindRender, generateCompletion(root, shell, a.stdout))
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "write completion script to this file")
	cmd.Flags().StringVar(&outDir, "out-dir", "", "write completion script into this directory")
	return cmd
}

func supportedCompletionShell(shell string) bool {
	switch shell {
	case "bash", "zsh", "fish", "powershell":
		return true
	default:
		return false
	}
}

func completionFilename(shell string) string {
	switch shell {
	case "bash":
		return "molstar.bash"
	case "zsh":
		return "_molstar"
	case "fish":
		return "molstar.fish"
	case "powershell":
		return "molstar.ps1"
	default:
		return "molstar." + shell
	}
}

func writeCompletion(root *cobra.Command, shell string, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	return generateCompletion(root, shell, file)
}

func generateCompletion(root *cobra.Command, shell string, out io.Writer) error {
	switch shell {
	case "bash":
		return root.GenBashCompletion(out)
	case "zsh":
		return root.GenZshCompletion(out)
	case "fish":
		return root.GenFishCompletion(out, true)
	case "powershell":
		return root.GenPowerShellCompletion(out)
	default:
		return fmt.Errorf("unsupported shell %q", shell)
	}
}
