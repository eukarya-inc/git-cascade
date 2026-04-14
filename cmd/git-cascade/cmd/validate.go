package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/eukarya-inc/git-cascade/internal/config"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(validateCmd)

	validateCmd.Flags().StringVar(&validateFlags.configDir, "local-config", "", "Path to a local config directory to validate")
}

var validateFlags struct {
	configDir string
}

var validateCmd = &cobra.Command{
	Use:   "validate [file...]",
	Short: "Validate compliance config files",
	Long: `Validate one or more compliance config YAML files for correctness.

Checks that:
  - The file is valid YAML
  - version is set
  - Every rule has an id, name, and valid severity
  - No duplicate rule IDs exist within a file

You can pass individual files as arguments, or use --local-config to validate
all YAML files in a directory.

Examples:
  git-cascade validate rules.yaml
  git-cascade validate rules/*.yaml
  git-cascade validate --local-config ./rules/`,
	RunE: runValidate,
}

func runValidate(_ *cobra.Command, args []string) error {
	paths := args

	if validateFlags.configDir != "" {
		entries, err := os.ReadDir(validateFlags.configDir)
		if err != nil {
			return fmt.Errorf("reading directory %s: %w", validateFlags.configDir, err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ext := filepath.Ext(e.Name())
			if ext == ".yaml" || ext == ".yml" {
				paths = append(paths, filepath.Join(validateFlags.configDir, e.Name()))
			}
		}
	}

	if len(paths) == 0 {
		return fmt.Errorf("no config files specified — pass file paths as arguments or use --local-config")
	}

	ok := true
	for _, path := range paths {
		cfg, err := config.Load(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "FAIL  %s\n      %v\n", path, err)
			ok = false
			continue
		}
		fmt.Printf("OK    %s  (%d rules)\n", path, len(cfg.Rules))
	}

	if !ok {
		return fmt.Errorf("one or more config files failed validation")
	}
	return nil
}
