package cmd

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func newCompletionCommand(streams IOStreams) *cobra.Command {
	command := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell|install]",
		Short: "generate shell completion script",
		Long: `To load completions:

  Bash:
    source <(keycloak completion bash)

  Zsh:
    source <(keycloak completion zsh)

  Fish:
    keycloak completion fish | source

  PowerShell:
    keycloak completion powershell | Out-String | Invoke-Expression`,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell", "install"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "install":
				return printCompletionInstall(streams.Out)
			case "bash":
				return cmd.Root().GenBashCompletion(streams.Out)
			case "zsh":
				return cmd.Root().GenZshCompletion(streams.Out)
			case "fish":
				return cmd.Root().GenFishCompletion(streams.Out, true)
			case "powershell":
				return cmd.Root().GenPowerShellCompletionWithDesc(streams.Out)
			}
			return nil
		},
	}
	command.SetOut(streams.Out)
	command.SetErr(streams.ErrOut)
	return command
}

func printCompletionInstall(out io.Writer) error {
	_, err := fmt.Fprintln(out, `# Add the appropriate line to your shell profile:

# bash (~/.bashrc or ~/.bash_profile):
source <(keycloak completion bash)

# zsh (~/.zshrc):
source <(keycloak completion zsh)

# fish (~/.config/fish/config.fish):
keycloak completion fish | source

# PowerShell ($PROFILE):
keycloak completion powershell | Out-String | Invoke-Expression`)
	return err
}
