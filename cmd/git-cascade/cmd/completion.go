package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(completionCmd)
}

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell autocompletion script",
	Long: `Generate an autocompletion script for git-cascade for the specified shell.

bash:
  # Load for current session:
  source <(git-cascade completion bash)

  # Install permanently (Linux):
  git-cascade completion bash > /etc/bash_completion.d/git-cascade

  # Install permanently (macOS with Homebrew bash-completion):
  git-cascade completion bash > $(brew --prefix)/etc/bash_completion.d/git-cascade

zsh:
  # Load for current session:
  source <(git-cascade completion zsh)

  # Install permanently:
  git-cascade completion zsh > "${fpath[1]}/_git-cascade"

fish:
  # Load for current session:
  git-cascade completion fish | source

  # Install permanently:
  git-cascade completion fish > ~/.config/fish/completions/git-cascade.fish

powershell:
  # Load for current session:
  git-cascade completion powershell | Out-String | Invoke-Expression

  # Install permanently: add the above line to your PowerShell profile.`,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
	Args:                  cobra.ExactArgs(1),
	DisableFlagsInUseLine: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return rootCmd.GenBashCompletion(os.Stdout)
		case "zsh":
			return rootCmd.GenZshCompletion(os.Stdout)
		case "fish":
			return rootCmd.GenFishCompletion(os.Stdout, true)
		case "powershell":
			return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
		default:
			return cmd.Help()
		}
	},
}
