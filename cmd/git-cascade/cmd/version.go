package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is set at build time via ldflags.
var Version = "dev"

func init() {
	rootCmd.AddCommand(versionCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("git-cascade %s\n", Version)
	},
}
