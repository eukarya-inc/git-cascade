package cmd

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "git-cascade",
	Short: "Scan GitHub repositories for compliance",
	Long: `git-cascade scans all repositories in a GitHub organization against
a set of compliance rules defined in YAML configuration files.

By default, it loads rules from the "compliance" repository in your
organization. Authentication can be done via a Personal Access Token
or GitHub App credentials.`,
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}
