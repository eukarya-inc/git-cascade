package main

import (
	"fmt"
	"os"

	"github.com/eukarya-inc/git-cascade/cmd/git-cascade/cmd"

	// Register all compliance checkers
	_ "github.com/eukarya-inc/git-cascade/internal/compliance/checks"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
