package debug

import (
	"os"

	"github.com/git-town/git-town/v12/src/cli/dialog"
	"github.com/git-town/git-town/v12/src/cli/dialog/components"
	"github.com/git-town/git-town/v12/src/config/configdomain"
	"github.com/spf13/cobra"
)

func enterPerennialRegex() *cobra.Command {
	return &cobra.Command{
		Use: "perennial-regex",
		RunE: func(cmd *cobra.Command, args []string) error {
			dialogInputs := components.LoadTestInputs(os.Environ())
			_, _, err := dialog.PerennialRegex(configdomain.PerennialRegex(""), dialogInputs.Next())
			return err
		},
	}
}
