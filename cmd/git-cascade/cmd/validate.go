package cmd

import (
	"fmt"
	"os"

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
	// --local-config: merge all files first, then validate as a unit.
	if validateFlags.configDir != "" {
		cfg, err := config.LoadAll(validateFlags.configDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "FAIL  %s\n      %v\n", validateFlags.configDir, err)
			return fmt.Errorf("one or more config files failed validation")
		}
		fmt.Printf("OK    %s  (%d rules)\n", validateFlags.configDir, len(cfg.Rules))
		return nil
	}

	if len(args) == 0 {
		return fmt.Errorf("no config files specified — pass file paths as arguments or use --local-config")
	}

	ok := true
	for _, path := range args {
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
